package overlay

import "testing"

const serveAuthTokenOverlay = `module: github.com/example/app

layers:
  domain:
    - internal/domain/...

layer_rules:
  domain: []

aggregates: {}
configs: []

serve:
  http_addr: "127.0.0.1:47823"
  auth_token: "  file-token  "
`

func TestServeAuthToken_ReadsTheOverlayAndTrimsIt(t *testing.T) {
	got, err := ServeAuthToken(writeOverlay(t, serveAuthTokenOverlay))
	if err != nil {
		t.Fatalf("ServeAuthToken: %v", err)
	}
	if want := "file-token"; got != want {
		t.Errorf("token = %q, want %q — a token with stray whitespace never matches a header", got, want)
	}
}

// No token configured, and no overlay at all, both mean the same thing: the
// endpoint is open. Anything else would close it on projects that never asked.
func TestServeAuthToken_AbsentMeansOpen(t *testing.T) {
	for name, root := range map[string]string{
		"no serve block": writeOverlay(t, serveAddrOverlayNoServe),
		"no overlay":     t.TempDir(),
	} {
		got, err := ServeAuthToken(root)
		if err != nil {
			t.Fatalf("%s: ServeAuthToken: %v", name, err)
		}
		if got != "" {
			t.Errorf("%s: token = %q, want it empty", name, got)
		}
	}
}
