package plugin

import (
	"fmt"
	"sort"
	"sync"
)

// registry is the package-level plugin registry. Plugins call
// RegisterPlugin from init() (or a constructor invoked from main) and
// archai's bootstrap iterates Registered() to wire them up.
//
// Design: a process-global registry mirrors how net/http handlers,
// database/sql drivers and image format decoders work in the standard
// library. It keeps plugin source files self-contained — they need
// nothing more than `import _ "archai/internal/plugins/foo"` to wire
// themselves up. The trade-off (test isolation) is handled by
// Reset() which is package-private and only used by tests in this
// same package.
var registry struct {
	mu      sync.Mutex
	plugins []Plugin
	names   map[string]struct{}
}

// RegisterPlugin adds p to the process-wide registry. Calling it with
// a duplicate manifest name panics — that's almost always a bug
// (two plugins fighting for the same namespace) and we'd rather fail
// at init than silently drop one.
//
// Plugins are typically registered from a package init() function:
//
//	func init() { plugin.RegisterPlugin(&MyPlugin{}) }
func RegisterPlugin(p Plugin) {
	if p == nil {
		panic("plugin: RegisterPlugin called with nil Plugin")
	}
	name := p.Manifest().Name
	if name == "" {
		panic("plugin: RegisterPlugin called with empty Manifest.Name")
	}

	registry.mu.Lock()
	defer registry.mu.Unlock()
	if registry.names == nil {
		registry.names = make(map[string]struct{})
	}
	if _, dup := registry.names[name]; dup {
		panic(fmt.Sprintf("plugin: duplicate plugin name %q registered", name))
	}
	registry.names[name] = struct{}{}
	registry.plugins = append(registry.plugins, p)
}

// Registered returns a snapshot of currently registered plugins,
// sorted by manifest name for deterministic bootstrap order.
func Registered() []Plugin {
	registry.mu.Lock()
	defer registry.mu.Unlock()

	out := make([]Plugin, len(registry.plugins))
	copy(out, registry.plugins)
	sort.Slice(out, func(i, j int) bool {
		return out[i].Manifest().Name < out[j].Manifest().Name
	})
	return out
}

// resetRegistryForTest clears the registry. Only the plugin package's
// own tests use it; production code never resets the registry.
func resetRegistryForTest() {
	registry.mu.Lock()
	defer registry.mu.Unlock()
	registry.plugins = nil
	registry.names = nil
}
