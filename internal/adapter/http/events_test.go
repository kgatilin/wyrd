package http

import (
	"bufio"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/kgatilin/archai/internal/serve"
)

func TestModelEvents_StreamPackageReload(t *testing.T) {
	state := serve.NewState(t.TempDir())
	srv, err := NewServer(state)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	mux := nethttp.NewServeMux()
	srv.routes(mux)
	ts := httptest.NewServer(mux)
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/api/events")
	if err != nil {
		t.Fatalf("GET /api/events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != nethttp.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}

	lines := make(chan string, 16)
	done := make(chan struct{})
	go func() {
		defer close(done)
		reader := bufio.NewReader(resp.Body)
		for {
			line, err := reader.ReadString('\n')
			if line != "" {
				lines <- line
			}
			if err != nil {
				if err != io.EOF {
					lines <- "ERR:" + err.Error()
				}
				return
			}
		}
	}()

	waitForLine(t, lines, []string{"event: ready"}, 2*time.Second)
	state.PublishPackageReload([]string{"internal/event"})
	waitForLine(t, lines, []string{"event: model-changed"}, 2*time.Second)
	waitForLine(t, lines, []string{`"kind":"package-reload"`, `"internal/event"`}, 2*time.Second)

	resp.Body.Close()
	<-done
}

func waitForLine(t *testing.T, lines <-chan string, wants []string, timeout time.Duration) {
	t.Helper()
	deadline := time.After(timeout)
	for {
		select {
		case line := <-lines:
			if containsAll(line, wants) {
				return
			}
		case <-deadline:
			t.Fatalf("timed out waiting for line containing %v", wants)
		}
	}
}

func containsAll(line string, wants []string) bool {
	for _, want := range wants {
		if !strings.Contains(line, want) {
			return false
		}
	}
	return true
}
