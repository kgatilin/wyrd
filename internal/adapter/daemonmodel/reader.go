// Package daemonmodel reads the Go model of a worktree from the daemon the
// user configured for its repository (internal/clientconfig), so a gate run
// costs the daemon's sync instead of a parse of the module. The daemon
// answers only when it is looking at the same source with the same reader;
// anything else is a local parse.
package daemonmodel

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/kgatilin/wyrd/internal/adapter/golang"
	"github.com/kgatilin/wyrd/internal/clientconfig"
	"github.com/kgatilin/wyrd/internal/domain"
	"github.com/kgatilin/wyrd/internal/serve/modelstore"
	"github.com/kgatilin/wyrd/internal/service"
	"github.com/kgatilin/wyrd/internal/treestamp"
)

// timeout bounds one model request. A daemon still parsing a cold worktree
// answers within it; one that does not is no faster than a local parse.
const timeout = 2 * time.Minute

type reader struct {
	local  service.ModelReader
	warn   io.Writer
	client *http.Client
}

// NewReader returns a reader that asks the configured daemon for the model
// of the working directory and uses local for everything the daemon cannot
// answer: a read of anything but the whole module, a repository with no
// daemon configured, and a daemon that is unreachable, fails, or refuses
// because its source tree or reader differs. A configured daemon that does
// not answer is reported on warn; the read still succeeds through local.
func NewReader(local service.ModelReader, warn io.Writer) service.ModelReader {
	return &reader{local: local, warn: warn, client: &http.Client{Timeout: timeout}}
}

func (r *reader) Read(ctx context.Context, paths []string) ([]domain.PackageModel, error) {
	if len(paths) != 1 || paths[0] != "./..." {
		return r.local.Read(ctx, paths)
	}
	root, err := os.Getwd()
	if err != nil {
		return r.local.Read(ctx, paths)
	}
	base, err := clientconfig.URL(root)
	if err != nil {
		fmt.Fprintf(r.warn, "%v; parsing locally\n", err)
		return r.local.Read(ctx, paths)
	}
	if base == "" {
		return r.local.Read(ctx, paths)
	}
	models, err := r.fetch(ctx, base, root)
	if err != nil {
		fmt.Fprintf(r.warn, "daemon %s: %v; parsing locally\n", base, err)
		return r.local.Read(ctx, paths)
	}
	return models, nil
}

func (r *reader) fetch(ctx context.Context, base, root string) ([]domain.PackageModel, error) {
	tree, err := treestamp.Scan(root)
	if err != nil {
		return nil, err
	}
	q := url.Values{
		"tree":   {tree.Fingerprint()},
		"reader": {modelstore.StampKey(golang.ReaderStamp)},
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, base+"/api/model?"+q.Encode(), nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("%s: %s", resp.Status, strings.TrimSpace(string(msg)))
	}
	var body struct {
		Packages []domain.PackageModel `json:"packages"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, fmt.Errorf("decoding the model: %w", err)
	}
	if body.Packages == nil {
		return nil, errors.New("the answer carries no model")
	}
	return body.Packages, nil
}
