package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"

	"github.com/spf13/cobra"

	"github.com/kgatilin/archai/internal/overlay"
	"github.com/kgatilin/archai/internal/plugin"

	// Built-in plugins. Importing them for side effects registers
	// each one with the plugin package's global registry. Adding a
	// new built-in plugin is a one-line change here.
	_ "github.com/kgatilin/archai/internal/plugins/complexity"
)

// cliHost is a Host implementation used outside the long-running
// daemon — i.e. for plugin CLI commands invoked from `archai
// <plugin-cmd>`. It loads the project model lazily on first access
// (so plugin commands that don't touch the model stay fast) and
// re-uses cmd/archai's existing model loaders (loadCurrentModel,
// resolveOverlay) so behavior matches the rest of the CLI.
//
// Subscribe is a no-op: the CLI is short-lived; plugins that need
// real-time updates run inside `archai serve`, where serve.Host
// provides the live event bus.
type cliHost struct {
	logger *slog.Logger

	mu       sync.Mutex
	loaded   bool
	loadErr  error
	model    *plugin.Model
	bus      *plugin.EventBus
	rootPath string
}

func newCLIHost() *cliHost {
	return &cliHost{
		logger: slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})),
		bus:    plugin.NewEventBus(),
	}
}

func (h *cliHost) ensureLoaded() error {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.loaded {
		return h.loadErr
	}
	h.loaded = true

	root, err := os.Getwd()
	if err != nil {
		h.loadErr = fmt.Errorf("plugin host: getwd: %w", err)
		return h.loadErr
	}
	h.rootPath = root

	pkgs, err := loadCurrentModel(context.Background(), root)
	if err != nil {
		h.loadErr = fmt.Errorf("plugin host: load model: %w", err)
		return h.loadErr
	}

	overlayPath, goModPath := resolveOverlay("")
	var cfg *overlay.Config
	module := ""
	if overlayPath != "" {
		cfg, err = overlay.LoadComposed(overlayPath)
		if err != nil {
			h.logger.Warn("plugin host: load overlay", "path", overlayPath, "err", err)
		} else {
			if goModPath != "" {
				if verr := overlay.Validate(cfg, goModPath); verr != nil {
					h.logger.Warn("plugin host: overlay validation", "err", verr)
				}
			}
			merged, _, mergeErr := overlay.Merge(pkgs, cfg)
			if mergeErr == nil {
				pkgs = merged
			}
			module = cfg.Module
		}
	}

	h.model = plugin.BuildModel(module, pkgs, cfg)
	return nil
}

// CurrentModel implements plugin.Host.
func (h *cliHost) CurrentModel() *plugin.Model {
	if err := h.ensureLoaded(); err != nil {
		h.logger.Error("plugin host: model unavailable", "err", err)
		return nil
	}
	return h.model
}

// Targets implements plugin.Host. Returns nil for the CLI host: no
// long-running state, plugins that need targets should call Target
// or run inside serve.
func (h *cliHost) Targets() []plugin.TargetMeta { return nil }

// Target implements plugin.Host.
func (h *cliHost) Target(string) (*plugin.TargetSnapshot, error) {
	return nil, errors.New("plugin: target loading is only available inside `archai serve`")
}

// ActiveTarget implements plugin.Host.
func (h *cliHost) ActiveTarget() *plugin.TargetSnapshot { return nil }

// Diff implements plugin.Host.
func (h *cliHost) Diff(string, string) (*plugin.Diff, error) {
	return nil, errors.New("plugin: Host.Diff is only available inside `archai serve`")
}

// Validate implements plugin.Host.
func (h *cliHost) Validate(string) (*plugin.ValidationReport, error) {
	return nil, errors.New("plugin: Host.Validate is only available inside `archai serve`")
}

// Subscribe implements plugin.Host. Returns the no-op bus's
// Unsubscribe — events are never published in CLI mode.
func (h *cliHost) Subscribe(handler func(plugin.ModelEvent)) plugin.Unsubscribe {
	return h.bus.Subscribe(handler)
}

// Logger implements plugin.Host.
func (h *cliHost) Logger() *slog.Logger { return h.logger }

// wirePlugins runs the in-process plugin bootstrap and adds every
// plugin-contributed CLI command to root. Errors from individual
// plugin Inits are reported to stderr but do not abort startup —
// other commands should still work.
//
// M12 mounts plugin commands at the cobra root level (no `archai
// plugins <name>` grouping). M13 (#66) replaces this with a proper
// `plugins` subcommand and per-plugin namespacing.
func wirePlugins(rootCmd *cobra.Command) plugin.BootstrapResult {
	host := newCLIHost()
	res, err := plugin.Bootstrap(context.Background(), host, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "archai: plugin bootstrap: %v\n", err)
	}
	plugin.AddCLICommandsToRoot(rootCmd, res.CLICommands)
	return res
}
