// Package vertex provides an Embedder adapter for the Gemini embedding models
// served by Vertex AI (the publisher-model :predict endpoint), authenticated
// with Google Application Default Credentials.
package vertex

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	// DefaultModel is the embedding model used when none is configured.
	DefaultModel = "gemini-embedding-001"

	// DefaultLocation is the Vertex AI location used when none is configured.
	DefaultLocation = "global"

	// DefaultDim is the output dimensionality requested when none is
	// configured. gemini-embedding-001 produces 3072 natively and is trained
	// to be truncated to 1536 or 768; 768 keeps a quarter of the storage.
	DefaultDim = 768

	// DefaultDocumentTask and DefaultQueryTask are the task types sent with
	// indexed code and with search queries. The model embeds the two sides
	// differently, and the code-retrieval query type is the one meant for a
	// natural-language question answered by code.
	DefaultDocumentTask = "RETRIEVAL_DOCUMENT"
	DefaultQueryTask    = "CODE_RETRIEVAL_QUERY"

	// DefaultConcurrency bounds the requests in flight. gemini-embedding-001
	// takes one text per request, so a batch of texts is that many requests.
	DefaultConcurrency = 8

	// DefaultTimeout bounds one request, retries excluded.
	DefaultTimeout = 30 * time.Second

	// DefaultMaxAttempts is how many times one document is sent before its
	// error is returned.
	DefaultMaxAttempts = 6

	// queryAttempts is the same for a search query. Somebody is waiting for
	// that answer, and search falls back to the lexical channel: one retry
	// covers a blip, a backoff ladder would hold the answer for half a
	// minute.
	queryAttempts = 2

	scope = "https://www.googleapis.com/auth/cloud-platform"
)

// TokenSource yields the bearer token for a request.
type TokenSource func(ctx context.Context) (string, error)

// Embedder implements retrieval.Embedder over Vertex AI.
type Embedder struct {
	project      string
	location     string
	model        string
	endpoint     string // "" derives it from location
	dim          int
	documentTask string
	queryTask    string
	concurrency  int
	timeout      time.Duration
	maxAttempts  int
	baseBackoff  time.Duration
	client       *http.Client
	token        TokenSource
}

// Option configures the Embedder.
type Option func(*Embedder)

// WithProject sets the Google Cloud project the requests are billed to.
func WithProject(project string) Option {
	return func(e *Embedder) {
		if project != "" {
			e.project = project
		}
	}
}

// WithLocation sets the Vertex AI location ("global", "us-central1", ...).
func WithLocation(location string) Option {
	return func(e *Embedder) {
		if location != "" {
			e.location = location
		}
	}
}

// WithModel sets the embedding model name.
func WithModel(model string) Option {
	return func(e *Embedder) {
		if model != "" {
			e.model = model
		}
	}
}

// WithEndpoint replaces the base URL derived from the location.
func WithEndpoint(endpoint string) Option {
	return func(e *Embedder) {
		if endpoint != "" {
			e.endpoint = endpoint
		}
	}
}

// WithDim sets the requested output dimensionality.
func WithDim(dim int) Option {
	return func(e *Embedder) {
		if dim > 0 {
			e.dim = dim
		}
	}
}

// WithTasks sets the task types sent with documents and with queries.
func WithTasks(document, query string) Option {
	return func(e *Embedder) {
		if document != "" {
			e.documentTask = document
		}
		if query != "" {
			e.queryTask = query
		}
	}
}

// WithConcurrency bounds the requests in flight.
func WithConcurrency(n int) Option {
	return func(e *Embedder) {
		if n > 0 {
			e.concurrency = n
		}
	}
}

// WithTimeout bounds one request.
func WithTimeout(d time.Duration) Option {
	return func(e *Embedder) {
		if d > 0 {
			e.timeout = d
		}
	}
}

// WithRetry sets how many times one text is sent and the first backoff,
// which doubles per attempt.
func WithRetry(maxAttempts int, baseBackoff time.Duration) Option {
	return func(e *Embedder) {
		if maxAttempts > 0 {
			e.maxAttempts = maxAttempts
		}
		if baseBackoff > 0 {
			e.baseBackoff = baseBackoff
		}
	}
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(e *Embedder) {
		if client != nil {
			e.client = client
		}
	}
}

// WithTokenSource replaces Application Default Credentials.
func WithTokenSource(token TokenSource) Option {
	return func(e *Embedder) {
		if token != nil {
			e.token = token
		}
	}
}

// New creates a Vertex AI embedder. Configuration is read from:
// 1. Explicit options (highest priority)
// 2. Environment variables:
//   - WYRD_VERTEX_PROJECT, else GOOGLE_CLOUD_PROJECT (required)
//   - WYRD_VERTEX_LOCATION, else GOOGLE_CLOUD_LOCATION (default: global)
//   - WYRD_EMBED_MODEL (default: gemini-embedding-001)
//   - WYRD_EMBED_DIM (default: 768)
//   - WYRD_EMBED_DOCUMENT_TASK (default: RETRIEVAL_DOCUMENT)
//   - WYRD_EMBED_QUERY_TASK (default: CODE_RETRIEVAL_QUERY)
//   - WYRD_EMBED_CONCURRENCY (default: 8)
//   - WYRD_EMBED_ENDPOINT (default: derived from the location)
//
// 3. Defaults
//
// Credentials are Application Default Credentials: a workload identity, an
// attached service account, GOOGLE_APPLICATION_CREDENTIALS, or a gcloud
// login. They are resolved on the first request, not here.
func New(opts ...Option) *Embedder {
	e := &Embedder{
		location:     DefaultLocation,
		model:        DefaultModel,
		dim:          DefaultDim,
		documentTask: DefaultDocumentTask,
		queryTask:    DefaultQueryTask,
		concurrency:  DefaultConcurrency,
		timeout:      DefaultTimeout,
		maxAttempts:  DefaultMaxAttempts,
		baseBackoff:  time.Second,
		client:       http.DefaultClient,
	}
	env := func(names ...string) string {
		for _, n := range names {
			if v := os.Getenv(n); v != "" {
				return v
			}
		}
		return ""
	}
	envInt := func(name string) int {
		n, _ := strconv.Atoi(os.Getenv(name))
		return n
	}
	for _, opt := range []Option{
		WithProject(env("WYRD_VERTEX_PROJECT", "GOOGLE_CLOUD_PROJECT")),
		WithLocation(env("WYRD_VERTEX_LOCATION", "GOOGLE_CLOUD_LOCATION")),
		WithModel(env("WYRD_EMBED_MODEL")),
		WithEndpoint(env("WYRD_EMBED_ENDPOINT")),
		WithDim(envInt("WYRD_EMBED_DIM")),
		WithTasks(env("WYRD_EMBED_DOCUMENT_TASK"), env("WYRD_EMBED_QUERY_TASK")),
		WithConcurrency(envInt("WYRD_EMBED_CONCURRENCY")),
	} {
		opt(e)
	}
	for _, opt := range opts {
		opt(e)
	}
	if e.token == nil {
		e.token = defaultCredentials()
	}
	return e
}

// defaultCredentials resolves Application Default Credentials once, on the
// first request, and reuses the token until it expires.
func defaultCredentials() TokenSource {
	var (
		once sync.Once
		src  oauth2.TokenSource
		err  error
	)
	return func(ctx context.Context) (string, error) {
		once.Do(func() {
			// The source outlives the request that created it.
			src, err = google.DefaultTokenSource(context.WithoutCancel(ctx), scope)
		})
		if err != nil {
			return "", fmt.Errorf("resolving application default credentials: %w", err)
		}
		tok, err := src.Token()
		if err != nil {
			return "", fmt.Errorf("obtaining an access token: %w", err)
		}
		return tok.AccessToken, nil
	}
}

// HasProject reports whether a project is configured; without one no request
// can be addressed.
func (e *Embedder) HasProject() bool {
	return e.project != ""
}

// Dim returns the requested output dimensionality. It is a setting, so it is
// known before the first embed.
func (e *Embedder) Dim() int {
	return e.dim
}

// ID returns the embedder identifier. Beyond "provider:model" it carries the
// dimensionality and both task types: each changes what a vector means, so
// vectors stored under another combination must not be reused.
func (e *Embedder) ID() string {
	return fmt.Sprintf("vertex:%s/%d/%s+%s", e.model, e.dim, e.documentTask, e.queryTask)
}

// Embed generates one embedding per text, as documents.
func (e *Embedder) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	if len(texts) == 0 {
		return nil, nil
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	results := make([][]float32, len(texts))
	sem := make(chan struct{}, e.concurrency)
	var (
		wg    sync.WaitGroup
		mu    sync.Mutex
		first error
	)
	for i, text := range texts {
		select {
		case sem <- struct{}{}:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			defer func() { <-sem }()
			vec, err := e.embedOne(ctx, text, e.documentTask, e.maxAttempts)
			if err != nil {
				mu.Lock()
				if first == nil {
					first = fmt.Errorf("embedding text %d of %d: %w", i+1, len(texts), err)
				}
				mu.Unlock()
				cancel()
				return
			}
			results[i] = vec
		}()
	}
	wg.Wait()
	if first != nil {
		return nil, first
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return results, nil
}

// EmbedQuery embeds a search query with the query task type, which places it
// in the space the document vectors are searched in.
func (e *Embedder) EmbedQuery(ctx context.Context, query string) ([]float32, error) {
	return e.embedOne(ctx, query, e.queryTask, min(queryAttempts, e.maxAttempts))
}

type predictRequest struct {
	Instances  []instance `json:"instances"`
	Parameters parameters `json:"parameters"`
}

type instance struct {
	Content  string `json:"content"`
	TaskType string `json:"task_type,omitempty"`
}

type parameters struct {
	OutputDimensionality int  `json:"outputDimensionality,omitempty"`
	AutoTruncate         bool `json:"autoTruncate"`
}

type predictResponse struct {
	Predictions []struct {
		Embeddings struct {
			Values []float32 `json:"values"`
		} `json:"embeddings"`
	} `json:"predictions"`
}

// statusError is a non-200 answer; retryAfter is the server's own hint.
type statusError struct {
	code       int
	body       string
	retryAfter time.Duration
}

func (s *statusError) Error() string {
	return fmt.Sprintf("vertex returned status %d: %s", s.code, s.body)
}

// embedOne sends one text, retrying a throttled or failed request with
// exponential backoff. A request the server rejected for what it contains
// is returned at once.
func (e *Embedder) embedOne(ctx context.Context, text, task string, maxAttempts int) ([]float32, error) {
	if e.project == "" {
		return nil, errors.New("vertex: no project configured (WYRD_VERTEX_PROJECT or GOOGLE_CLOUD_PROJECT)")
	}
	backoff := e.baseBackoff
	var last error
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		vec, err := e.predict(ctx, text, task)
		if err == nil {
			return vec, nil
		}
		last = err
		wait, retry := retryable(err, backoff)
		if !retry || attempt == maxAttempts {
			break
		}
		select {
		case <-time.After(wait):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		backoff *= 2
	}
	return nil, last
}

// retryable reports whether err is worth another attempt and how long to
// wait first: throttling and server errors are, as is a transport failure;
// the caller's own cancellation and a rejected request are not.
func retryable(err error, backoff time.Duration) (time.Duration, bool) {
	if errors.Is(err, context.Canceled) {
		return 0, false
	}
	var status *statusError
	if errors.As(err, &status) {
		if status.code != http.StatusTooManyRequests && status.code < 500 {
			return 0, false
		}
		if status.retryAfter > backoff {
			return status.retryAfter, true
		}
	}
	return backoff, true
}

func (e *Embedder) predict(ctx context.Context, text, task string) ([]float32, error) {
	body, err := json.Marshal(predictRequest{
		Instances:  []instance{{Content: text, TaskType: task}},
		Parameters: parameters{OutputDimensionality: e.dim, AutoTruncate: true},
	})
	if err != nil {
		return nil, fmt.Errorf("marshaling request: %w", err)
	}
	token, err := e.token(ctx)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(ctx, e.timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.url(), bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("creating request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sending request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		retryAfter, _ := strconv.Atoi(resp.Header.Get("Retry-After"))
		return nil, &statusError{
			code:       resp.StatusCode,
			body:       string(bytes.TrimSpace(snippet)),
			retryAfter: time.Duration(retryAfter) * time.Second,
		}
	}

	var out predictResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("decoding response: %w", err)
	}
	if len(out.Predictions) != 1 {
		return nil, fmt.Errorf("expected 1 prediction, got %d", len(out.Predictions))
	}
	vec := out.Predictions[0].Embeddings.Values
	if len(vec) != e.dim {
		return nil, fmt.Errorf("model %s returned %d dimensions, %d requested", e.model, len(vec), e.dim)
	}
	normalize(vec)
	return vec, nil
}

// url is the publisher-model predict endpoint. The global location has no
// regional host prefix.
func (e *Embedder) url() string {
	base := e.endpoint
	if base == "" {
		base = "https://aiplatform.googleapis.com"
		if e.location != "global" {
			base = "https://" + e.location + "-aiplatform.googleapis.com"
		}
	}
	return fmt.Sprintf("%s/v1/projects/%s/locations/%s/publishers/google/models/%s:predict",
		base, e.project, e.location, e.model)
}

// normalize scales vec to unit length in place. The model returns unit
// vectors only at its native dimensionality; a truncated one is not, and
// mean-pooling chunk vectors assumes they are.
func normalize(vec []float32) {
	var sum float64
	for _, v := range vec {
		sum += float64(v) * float64(v)
	}
	if sum == 0 {
		return
	}
	norm := float32(math.Sqrt(sum))
	for i := range vec {
		vec[i] /= norm
	}
}
