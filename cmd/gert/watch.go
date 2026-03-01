package main

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/kernel/engine"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
	kvalidate "github.com/ormasoftchile/gert/pkg/kernel/validate"
	"github.com/spf13/cobra"
)

var (
	watchInterval string
	watchStopOn   string
	watchVars     []string
)

var watchCmd = &cobra.Command{
	Use:   "watch [runbook.yaml]",
	Short: "Run a runbook repeatedly at an interval",
	Args:  cobra.ExactArgs(1),
	RunE:  runWatch,
}

func runWatch(cmd *cobra.Command, args []string) error {
	filePath := args[0]
	interval, err := time.ParseDuration(watchInterval)
	if err != nil {
		return fmt.Errorf("invalid --interval: %w", err)
	}

	stopCategories := make(map[string]bool)
	if watchStopOn != "" {
		for _, cat := range strings.Split(watchStopOn, ",") {
			stopCategories[strings.TrimSpace(cat)] = true
		}
	}

	// Parse --var flags
	vars := make(map[string]string)
	for _, v := range watchVars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --var %q: expected key=value", v)
		}
		vars[parts[0]] = parts[1]
	}

	// Validate once upfront
	rb, errs := kvalidate.ValidateFile(filePath)
	if errs != nil {
		for _, e := range errs {
			if e.Severity == "error" {
				return fmt.Errorf("validation failed")
			}
		}
	}

	ctx := context.Background()
	run := 0

	for {
		run++
		runID := fmt.Sprintf("watch-%d", run)
		ts := time.Now().Format("15:04:05")
		tracePath := fmt.Sprintf("traces/watch-%s-%03d.jsonl", time.Now().Format("20060102"), run)

		// Resolve inputs
		resolved, err := engine.ResolveInputs(ctx, rb, vars, nil)
		if err != nil {
			fmt.Printf("%s  ! input resolution error: %v\n", ts, err)
			return err
		}

		// Set up trace writer
		var tw *trace.Writer
		tw, err = trace.NewFileWriter(tracePath, runID)
		if err != nil {
			// Trace file is optional for watch; continue without
			tw = nil
		}

		cfg := engine.RunConfig{
			RunID:   runID,
			Mode:    "real",
			Vars:    resolved.Vars,
			BaseDir: filepath.Dir(filePath),
			Trace:   tw,
		}

		eng := engine.New(rb, cfg)
		result := eng.Run(ctx)

		// Print one-line summary
		outcomeStr := "no_outcome"
		outcomeCode := ""
		if result.Outcome != nil {
			outcomeStr = string(result.Outcome.Category)
			outcomeCode = result.Outcome.Code
		}
		fmt.Printf("%s  %s %s (%s)   %s\n", ts, statusIcon(result.Status), outcomeStr, outcomeCode, result.Duration.Truncate(time.Millisecond))

		// Check stop conditions
		if result.Error != nil {
			fmt.Printf("  Watch stopped: engine error: %v\n", result.Error)
			return result.Error
		}

		if result.Outcome != nil && stopCategories[string(result.Outcome.Category)] {
			fmt.Printf("  Watch stopped: outcome %q matched --stop-on\n", result.Outcome.Category)
			return nil
		}

		time.Sleep(interval)
	}
}

func statusIcon(status string) string {
	switch status {
	case "completed":
		return "✓"
	case "failed":
		return "✗"
	default:
		return "!"
	}
}

func init() {
	watchCmd.Flags().StringVar(&watchInterval, "interval", "5m", "Time between runs (e.g., 5m, 30s)")
	watchCmd.Flags().StringVar(&watchStopOn, "stop-on", "", "Comma-separated outcome categories that stop the loop")
	watchCmd.Flags().StringArrayVar(&watchVars, "var", nil, "Set a variable (key=value), repeatable")
	rootCmd.AddCommand(watchCmd)
}
