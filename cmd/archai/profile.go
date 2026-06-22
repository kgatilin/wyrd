package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"sort"
	"time"

	"github.com/kgatilin/archai/internal/adapter/uigraph"
	"github.com/kgatilin/archai/internal/serve"
	"github.com/kgatilin/archai/internal/service"
	"github.com/spf13/cobra"
)

func newProfileCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "profile",
		Short: "Profile archai project loading",
		Long:  "Profile archai project loading and review UI graph projection stages.",
	}
	cmd.AddCommand(newProfileLoadCmd())
	return cmd
}

func newProfileLoadCmd() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "load [root]",
		Short: "Profile project model load and UIGraph projection",
		Long: `Profile the same expensive stages used by archai serve and the
review UI's /api/uigraph endpoint: serve state load (including model cache,
archai.yaml overlay, and active target), UIGraph projection, and JSON
serialization.

Examples:
  archai profile load
  archai profile load /path/to/project --repeat 3
  archai profile load /path/to/project --json
  archai profile load /path/to/project --cpuprofile /tmp/archai-load.pprof`,
		Args: cobra.MaximumNArgs(1),
		RunE: runProfileLoad,
	}
	cmd.Flags().Int("repeat", 1, "Number of profiling runs")
	cmd.Flags().Bool("json", false, "Emit machine-readable JSON")
	cmd.Flags().String("cpuprofile", "", "Write a Go CPU profile while the command runs")
	return cmd
}

type profileLoadReport struct {
	Root     string           `json:"root"`
	Runs     []profileLoadRun `json:"runs"`
	Summary  profileSummary   `json:"summary"`
	Warnings []string         `json:"warnings,omitempty"`
}

type profileLoadRun struct {
	Index        int            `json:"index"`
	Packages     int            `json:"packages"`
	Components   int            `json:"components"`
	Edges        int            `json:"edges"`
	Relations    int            `json:"relations"`
	JSONBytes    int            `json:"jsonBytes"`
	Overlay      bool           `json:"overlay"`
	Target       string         `json:"target,omitempty"`
	Stages       []profileStage `json:"stages"`
	TotalSeconds float64        `json:"totalSeconds"`
	stageByName  map[string]time.Duration
}

type profileStage struct {
	Name    string  `json:"name"`
	Seconds float64 `json:"seconds"`
}

type profileSummary struct {
	Packages       int                `json:"packages"`
	Components     int                `json:"components"`
	Edges          int                `json:"edges"`
	Relations      int                `json:"relations"`
	JSONBytes      int                `json:"jsonBytes"`
	AverageSeconds float64            `json:"averageSeconds"`
	SlowestStage   string             `json:"slowestStage,omitempty"`
	StageAverages  map[string]float64 `json:"stageAverages"`
}

func runProfileLoad(cmd *cobra.Command, args []string) error {
	root := "."
	if len(args) > 0 {
		root = args[0]
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return fmt.Errorf("resolving root %s: %w", root, err)
	}
	repeat, _ := cmd.Flags().GetInt("repeat")
	if repeat < 1 {
		return fmt.Errorf("--repeat must be >= 1")
	}
	asJSON, _ := cmd.Flags().GetBool("json")
	cpuProfile, _ := cmd.Flags().GetString("cpuprofile")

	var profileFile *os.File
	if cpuProfile != "" {
		profileFile, err = os.Create(cpuProfile)
		if err != nil {
			return fmt.Errorf("creating CPU profile %s: %w", cpuProfile, err)
		}
		defer profileFile.Close()
		if err := pprof.StartCPUProfile(profileFile); err != nil {
			return fmt.Errorf("starting CPU profile: %w", err)
		}
		defer pprof.StopCPUProfile()
	}

	reader, note := assembleServeReader()
	warnings := []string{}
	if note != "" {
		warnings = append(warnings, note)
		if !asJSON {
			fmt.Fprintln(cmd.ErrOrStderr(), note)
		}
	}

	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	report := profileLoadReport{
		Root:     absRoot,
		Runs:     make([]profileLoadRun, 0, repeat),
		Warnings: warnings,
	}
	for i := 1; i <= repeat; i++ {
		run, err := profileLoadOnce(ctx, absRoot, reader, i)
		if err != nil {
			return err
		}
		report.Runs = append(report.Runs, run)
	}
	report.Summary = summarizeProfileRuns(report.Runs)

	if asJSON {
		data, err := json.MarshalIndent(report, "", "  ")
		if err != nil {
			return fmt.Errorf("marshal profile report: %w", err)
		}
		fmt.Fprintln(cmd.OutOrStdout(), string(data))
		return nil
	}
	printProfileLoadReport(cmd.OutOrStdout(), report, cpuProfile)
	return nil
}

func profileLoadOnce(ctx context.Context, root string, reader service.ModelReader, index int) (profileLoadRun, error) {
	run := profileLoadRun{Index: index, stageByName: map[string]time.Duration{}}
	totalStart := time.Now()

	state := serve.NewState(root, serve.WithReader(reader))
	if err := measureStage(&run, "state.load", func() error {
		return state.Load(ctx)
	}); err != nil {
		return run, fmt.Errorf("profile load: loading serve state: %w", err)
	}
	snap := state.Snapshot()
	run.Packages = len(snap.Packages)
	run.Overlay = snap.Overlay != nil
	run.Target = snap.CurrentTarget

	var graph uigraph.UIGraph
	if err := measureStage(&run, "uigraph.project", func() error {
		projected, err := uigraph.ProjectWithPublicDiff(snap.Packages, snap.Overlay, nil, nil)
		if err != nil {
			return err
		}
		graph = projected
		return nil
	}); err != nil {
		return run, fmt.Errorf("profile load: projecting uigraph: %w", err)
	}
	run.Components = len(graph.Components)
	run.Edges = len(graph.Edges)
	run.Relations = len(graph.Relations)

	if err := measureStage(&run, "uigraph.json", func() error {
		data, err := json.Marshal(graph)
		if err != nil {
			return err
		}
		run.JSONBytes = len(data)
		return nil
	}); err != nil {
		return run, fmt.Errorf("profile load: serializing uigraph: %w", err)
	}

	run.TotalSeconds = seconds(time.Since(totalStart))
	return run, nil
}

func measureStage(run *profileLoadRun, name string, fn func() error) error {
	start := time.Now()
	err := fn()
	elapsed := time.Since(start)
	run.stageByName[name] = elapsed
	run.Stages = append(run.Stages, profileStage{Name: name, Seconds: seconds(elapsed)})
	return err
}

func summarizeProfileRuns(runs []profileLoadRun) profileSummary {
	stageTotals := map[string]time.Duration{}
	var total time.Duration
	var last profileLoadRun
	for _, run := range runs {
		last = run
		total += durationFromSeconds(run.TotalSeconds)
		for _, stage := range run.Stages {
			stageTotals[stage.Name] += durationFromSeconds(stage.Seconds)
		}
	}

	stageAverages := make(map[string]float64, len(stageTotals))
	slowestStage := ""
	var slowest time.Duration
	for name, duration := range stageTotals {
		avg := duration / time.Duration(len(runs))
		stageAverages[name] = seconds(avg)
		if avg > slowest {
			slowest = avg
			slowestStage = name
		}
	}
	return profileSummary{
		Packages:       last.Packages,
		Components:     last.Components,
		Edges:          last.Edges,
		Relations:      last.Relations,
		JSONBytes:      last.JSONBytes,
		AverageSeconds: seconds(total / time.Duration(len(runs))),
		SlowestStage:   slowestStage,
		StageAverages:  stageAverages,
	}
}

func printProfileLoadReport(w interface{ Write([]byte) (int, error) }, report profileLoadReport, cpuProfile string) {
	fmt.Fprintf(w, "archai profile load: %s\n", report.Root)
	if len(report.Warnings) > 0 {
		for _, warning := range report.Warnings {
			fmt.Fprintf(w, "warning: %s\n", warning)
		}
	}
	for _, run := range report.Runs {
		fmt.Fprintf(w, "\nrun %d: %.3fs, %d package(s), %d relation(s), %.2f MiB JSON\n",
			run.Index,
			run.TotalSeconds,
			run.Packages,
			run.Relations,
			float64(run.JSONBytes)/(1024*1024),
		)
		for _, stage := range run.Stages {
			fmt.Fprintf(w, "  %-16s %8.3fs\n", stage.Name, stage.Seconds)
		}
	}

	fmt.Fprintf(w, "\nsummary: avg %.3fs", report.Summary.AverageSeconds)
	if report.Summary.SlowestStage != "" {
		fmt.Fprintf(w, ", slowest stage %s", report.Summary.SlowestStage)
	}
	fmt.Fprintln(w)

	names := make([]string, 0, len(report.Summary.StageAverages))
	for name := range report.Summary.StageAverages {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		fmt.Fprintf(w, "  avg %-12s %8.3fs\n", name, report.Summary.StageAverages[name])
	}
	if cpuProfile != "" {
		fmt.Fprintf(w, "\nCPU profile: %s\n", cpuProfile)
	}
}

func seconds(d time.Duration) float64 {
	return float64(d) / float64(time.Second)
}

func durationFromSeconds(s float64) time.Duration {
	return time.Duration(s * float64(time.Second))
}
