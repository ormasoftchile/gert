package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/ormasoftchile/gert/pkg/kernel/engine"
	kvalidate "github.com/ormasoftchile/gert/pkg/kernel/validate"
	"github.com/spf13/cobra"
)

var resumeRunID string

var resumeCmd = &cobra.Command{
	Use:   "resume",
	Short: "Resume a paused run from persisted state",
	RunE:  runResume,
}

func runResume(cmd *cobra.Command, args []string) error {
	if resumeRunID == "" {
		return fmt.Errorf("--run is required")
	}

	state, err := engine.LoadState(resumeRunID)
	if err != nil {
		return fmt.Errorf("load state: %w", err)
	}

	// Validate the runbook
	rb, errs := kvalidate.ValidateFile(state.RunbookPath)
	if errs != nil {
		for _, e := range errs {
			if e.Severity == "error" {
				return fmt.Errorf("validation failed for %s", state.RunbookPath)
			}
		}
	}

	// Build RunConfig from persisted state
	vars := make(map[string]string)
	for k, v := range state.Vars {
		vars[k] = fmt.Sprint(v)
	}

	cfg := engine.RunConfig{
		RunID:   state.RunID,
		Mode:    "real",
		Vars:    vars,
		BaseDir: filepath.Dir(state.RunbookPath),
	}

	eng := engine.New(rb, cfg)
	result := eng.Run(context.Background())

	if result.Outcome != nil {
		fmt.Printf("\n✓ Outcome: %s (%s)\n", result.Outcome.Category, result.Outcome.Code)
	}

	if result.Error != nil {
		return result.Error
	}

	fmt.Printf("  Duration: %s\n", result.Duration)

	// Clean up state file on success
	stateDir := filepath.Join("runs", resumeRunID)
	os.RemoveAll(stateDir)

	return nil
}

func init() {
	resumeCmd.Flags().StringVar(&resumeRunID, "run", "", "Run ID to resume")
	rootCmd.AddCommand(resumeCmd)
}
