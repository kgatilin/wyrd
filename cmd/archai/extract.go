package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/kgatilin/archai/internal/adapter/golang"
	yamlAdapter "github.com/kgatilin/archai/internal/adapter/yaml"
	"github.com/kgatilin/archai/internal/domain"
	"github.com/spf13/cobra"
)

// newExtractCmd returns the `archai extract <path>` command.
//
// extract mirrors the MCP `extract` tool but from the CLI: it loads the
// Go packages rooted at <path> (default ".") via the same adapter the
// MCP daemon uses, and emits one serialized document per package.
//
// With no --out flag every package is streamed to stdout separated by
// `---\n` (yaml) or as a JSON array (json). When --out is set each
// package is written to <out>/<relative-pkg-path>/internal.yaml (or
// .json) and a one-line summary is printed.
func newExtractCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "extract [path]",
		Short: "Extract Go packages and dump per-package YAML or JSON",
		Long: `Run the same extraction the MCP daemon uses over a local Go project
and emit per-package documents.

Default: one YAML document per package streamed to stdout, separated by
'---'. With --out <dir>, each package is written to
<dir>/<pkg-path>/internal.<ext> and a summary line is printed.

Examples:
  # Stream YAML to stdout for the current project
  archai extract .

  # Dump JSON to stdout
  archai extract . --format json

  # Write per-package files under .arch/packages/
  archai extract . --out .arch/packages`,
		Args: cobra.MaximumNArgs(1),
		RunE: runExtract,
	}
	cmd.Flags().String("out", "", "Output directory for per-package files (default: stream to stdout)")
	cmd.Flags().StringP("format", "f", "yaml", "Output format: yaml or json")
	return cmd
}

// runExtract implements `archai extract`. It reuses the same golang
// reader wired into `serve` and the MCP `extract` tool.
func runExtract(cmd *cobra.Command, args []string) error {
	path := "."
	if len(args) > 0 {
		path = args[0]
	}
	outDir, _ := cmd.Flags().GetString("out")
	format, _ := cmd.Flags().GetString("format")

	switch format {
	case "", "yaml", "json":
	default:
		return fmt.Errorf("unsupported format %q (use yaml or json)", format)
	}
	if format == "" {
		format = "yaml"
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	models, err := extractModels(ctx, path)
	if err != nil {
		return err
	}
	// Deterministic order makes stdout diffable and output files stable.
	sort.Slice(models, func(i, j int) bool { return models[i].Path < models[j].Path })

	if outDir == "" {
		return streamExtract(cmd.OutOrStdout(), models, format)
	}
	return dumpExtract(cmd.OutOrStdout(), models, format, outDir)
}

// extractModels runs the Go adapter against a single project path.
//
// The Go reader drives golang.org/x/tools/go/packages.Load, which
// resolves `./...` relative to the current working directory. To keep
// this command usable from anywhere we temporarily chdir into the
// target directory, load, then restore. Non-directory paths are passed
// through to packages.Load verbatim.
func extractModels(ctx context.Context, path string) ([]domain.PackageModel, error) {
	reader := golang.NewReader()

	info, err := os.Stat(path)
	if err != nil {
		return nil, fmt.Errorf("stat %s: %w", path, err)
	}
	if !info.IsDir() {
		models, err := reader.Read(ctx, []string{path})
		if err != nil {
			return nil, fmt.Errorf("reading Go packages at %s: %w", path, err)
		}
		return models, nil
	}

	prev, err := os.Getwd()
	if err != nil {
		return nil, fmt.Errorf("resolving cwd: %w", err)
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolving %s: %w", path, err)
	}
	if err := os.Chdir(abs); err != nil {
		return nil, fmt.Errorf("chdir %s: %w", abs, err)
	}
	defer os.Chdir(prev) //nolint:errcheck // best-effort restore

	models, err := reader.Read(ctx, []string{"./..."})
	if err != nil {
		return nil, fmt.Errorf("reading Go packages at %s: %w", path, err)
	}
	return models, nil
}

// streamExtract writes every package document to w.
//
// YAML: one document per package, separated by the standard "---\n"
// delimiter.
//
// JSON: a single JSON array containing every package document (so the
// whole stream is still valid JSON).
func streamExtract(w io.Writer, models []domain.PackageModel, format string) error {
	if format == "json" {
		// Emit one JSON array — each element is a package document.
		fmt.Fprint(w, "[\n")
		for i, m := range models {
			data, err := yamlAdapter.MarshalPackage(m, "json")
			if err != nil {
				return err
			}
			// data has a trailing newline; trim before embedding in array.
			trimmed := strings.TrimRight(string(data), "\n")
			// Indent each line by two spaces for readability.
			lines := strings.Split(trimmed, "\n")
			for j, line := range lines {
				lines[j] = "  " + line
			}
			indented := strings.Join(lines, "\n")
			fmt.Fprint(w, indented)
			if i < len(models)-1 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprint(w, "\n")
		}
		fmt.Fprint(w, "]\n")
		return nil
	}

	for i, m := range models {
		data, err := yamlAdapter.MarshalPackage(m, "yaml")
		if err != nil {
			return err
		}
		if i > 0 {
			fmt.Fprint(w, "---\n")
		}
		// yamlv3.Marshal already terminates with \n.
		fmt.Fprint(w, string(data))
	}
	return nil
}

// dumpExtract writes one file per package under outDir and prints a
// short summary to w. The on-disk layout mirrors the convention used by
// per-package .arch/internal.yaml specs so the dump can be consumed by
// the YAML reader directly.
func dumpExtract(w io.Writer, models []domain.PackageModel, format, outDir string) error {
	ext := "yaml"
	if format == "json" {
		ext = "json"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return fmt.Errorf("creating %s: %w", outDir, err)
	}
	for _, m := range models {
		data, err := yamlAdapter.MarshalPackage(m, format)
		if err != nil {
			return err
		}
		// Guard against absolute or oddly-formatted package paths so we
		// never escape outDir. filepath.Clean + rejection of leading ..
		// is sufficient because Read() returns module-relative paths.
		rel := filepath.Clean(m.Path)
		if rel == "." || rel == "" || strings.HasPrefix(rel, "..") || filepath.IsAbs(rel) {
			rel = "root"
		}
		out := filepath.Join(outDir, rel, "internal."+ext)
		if err := os.MkdirAll(filepath.Dir(out), 0o755); err != nil {
			return fmt.Errorf("creating %s: %w", filepath.Dir(out), err)
		}
		if err := os.WriteFile(out, data, 0o644); err != nil {
			return fmt.Errorf("writing %s: %w", out, err)
		}
	}
	fmt.Fprintf(w, "Extracted %d package(s) to %s\n", len(models), outDir)
	return nil
}
