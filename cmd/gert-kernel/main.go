// Package main provides the kernel/v0 CLI entrypoint.
// This is a minimal wrapper — the kernel CLI has four verbs:
//
//	gert validate <file>
//	gert exec <file>      (Phase 3+)
//	gert test <file...>   (Phase 5)
//	gert schema            (exports JSON Schema)
package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/kernel/engine"
	kschema "github.com/ormasoftchile/gert/pkg/kernel/schema"
	ktesting "github.com/ormasoftchile/gert/pkg/kernel/testing"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
	kvalidate "github.com/ormasoftchile/gert/pkg/kernel/validate"
	"github.com/spf13/cobra"
)

var (
	version = "dev"
	commit  = "unknown"
)

func main() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

var rootCmd = &cobra.Command{
	Use:   "gert",
	Short: "Governed Executable Runbook Engine — kernel/v0",
}

// --- validate ---

var validateCmd = &cobra.Command{
	Use:   "validate [runbook.yaml]",
	Short: "Validate a kernel/v0 runbook YAML (3-phase pipeline)",
	Args:  cobra.ExactArgs(1),
	RunE:  runValidate,
}

func runValidate(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	ext := strings.ToLower(filepath.Ext(filePath))
	if ext == ".md" || ext == ".markdown" {
		return fmt.Errorf("%s is a Markdown file — only .yaml files are supported", filePath)
	}

	// Detect if this is a tool definition by peeking at the file
	if isToolFile(filePath) {
		return runValidateTool(filePath)
	}

	rb, errs := kvalidate.ValidateFile(filePath)
	if len(errs) > 0 {
		var errors []*kvalidate.ValidationError
		var warnings []*kvalidate.ValidationError
		for _, e := range errs {
			if e.Severity == "warning" {
				warnings = append(warnings, e)
			} else {
				errors = append(errors, e)
			}
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "  ⚠ [%s] %s\n", w.Phase, w.Message)
			if w.Path != "" {
				fmt.Fprintf(os.Stderr, "    at: %s\n", w.Path)
			}
		}
		if len(errors) > 0 {
			fmt.Fprintf(os.Stderr, "Validation failed: %d error(s)\n\n", len(errors))
			for i, e := range errors {
				fmt.Fprintf(os.Stderr, "  %d. [%s] %s\n", i+1, e.Phase, e.Message)
				if e.Path != "" {
					fmt.Fprintf(os.Stderr, "     at: %s\n", e.Path)
				}
			}
			return fmt.Errorf("validation failed with %d error(s)", len(errors))
		}
	}
	fmt.Printf("✓ %s is valid (%d steps)\n", rb.Meta.Name, len(rb.Steps))
	return nil
}

func runValidateTool(filePath string) error {
	td, errs := kvalidate.ValidateToolFile(filePath)
	if len(errs) > 0 {
		var errors []*kvalidate.ValidationError
		for _, e := range errs {
			if e.Severity == "error" {
				errors = append(errors, e)
			}
		}
		if len(errors) > 0 {
			fmt.Fprintf(os.Stderr, "Validation failed: %d error(s)\n\n", len(errors))
			for i, e := range errors {
				fmt.Fprintf(os.Stderr, "  %d. [%s] %s\n", i+1, e.Phase, e.Message)
				if e.Path != "" {
					fmt.Fprintf(os.Stderr, "     at: %s\n", e.Path)
				}
			}
			return fmt.Errorf("validation failed with %d error(s)", len(errors))
		}
	}
	name := ""
	if td != nil {
		name = td.Meta.Name
	}
	fmt.Printf("✓ tool %s is valid (%d actions)\n", name, len(td.Actions))

	// Print warnings after success line
	for _, e := range errs {
		if e.Severity == "warning" {
			fmt.Fprintf(os.Stderr, "  ⚠ [%s] %s\n", e.Phase, e.Message)
			if e.Path != "" {
				fmt.Fprintf(os.Stderr, "    at: %s\n", e.Path)
			}
		}
	}
	return nil
}

// isToolFile peeks at the file to check if apiVersion starts with "tool/".
func isToolFile(path string) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 256)
	n, _ := f.Read(buf)
	return strings.Contains(string(buf[:n]), "apiVersion: tool/")
}

// --- version ---

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version info",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("gert kernel/v0 %s (%s)\n", version, commit)
	},
}

// --- exec ---

var (
	execMode  string
	execVars  []string
	execTrace string
)

var execCmd = &cobra.Command{
	Use:   "exec [runbook.yaml]",
	Short: "Execute a kernel/v0 runbook",
	Args:  cobra.ExactArgs(1),
	RunE:  runExec,
}

func runExec(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	// Validate first
	rb, errs := kvalidate.ValidateFile(filePath)
	if errs != nil {
		for _, e := range errs {
			if e.Severity == "error" {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", e.Phase, e.Message)
			}
		}
		for _, e := range errs {
			if e.Severity == "error" {
				return fmt.Errorf("validation failed")
			}
		}
	}

	// Parse --var flags
	vars := make(map[string]string)
	for _, v := range execVars {
		parts := strings.SplitN(v, "=", 2)
		if len(parts) != 2 {
			return fmt.Errorf("invalid --var %q: expected key=value", v)
		}
		vars[parts[0]] = parts[1]
	}

	// Set up trace writer
	var tw *trace.Writer
	if execTrace != "" {
		var err error
		tw, err = trace.NewFileWriter(execTrace, "run-1")
		if err != nil {
			return fmt.Errorf("trace: %w", err)
		}
	}

	// Build run config
	baseDir := filepath.Dir(filePath)
	cfg := engine.RunConfig{
		RunID:   "run-1",
		Mode:    execMode,
		Vars:    vars,
		BaseDir: baseDir,
		Trace:   tw,
	}

	eng := engine.New(rb, cfg)
	result := eng.Run()

	if result.Outcome != nil {
		fmt.Printf("\n✓ Outcome: %s (%s)\n", result.Outcome.Category, result.Outcome.Code)
		if result.Outcome.Meta != nil {
			for k, v := range result.Outcome.Meta {
				fmt.Printf("  %s: %v\n", k, v)
			}
		}
	}

	if result.Error != nil {
		return result.Error
	}

	fmt.Printf("  Duration: %s\n", result.Duration)
	return nil
}

func init() {
	execCmd.Flags().StringVar(&execMode, "mode", "real", "Execution mode: real or dry-run")
	execCmd.Flags().StringArrayVar(&execVars, "var", nil, "Set a variable (key=value), repeatable")
	execCmd.Flags().StringVar(&execTrace, "trace", "", "Write trace to JSONL file")

	testCmd.Flags().StringVar(&testScenario, "scenario", "", "Run only the named scenario (default: all)")
	testCmd.Flags().BoolVar(&testJSON, "json", false, "Output results as JSON")
	testCmd.Flags().BoolVar(&testFailFast, "fail-fast", false, "Stop after first failure")
	testCmd.Flags().StringVar(&testTimeout, "timeout", "30s", "Per-scenario timeout")

	rootCmd.AddCommand(validateCmd)
	rootCmd.AddCommand(execCmd)
	rootCmd.AddCommand(testCmd)
	rootCmd.AddCommand(schemaCmd)
	rootCmd.AddCommand(versionCmd)
}

// --- schema ---

var schemaCmd = &cobra.Command{
	Use:   "schema",
	Short: "Export JSON Schema to stdout",
}

var schemaRunbookCmd = &cobra.Command{
	Use:   "runbook",
	Short: "Export kernel/v0 runbook JSON Schema",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := kschema.GenerateRunbookJSONSchema()
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	},
}

var schemaToolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Export tool/v0 JSON Schema",
	RunE: func(cmd *cobra.Command, args []string) error {
		data, err := kschema.GenerateToolJSONSchema()
		if err != nil {
			return err
		}
		fmt.Println(string(data))
		return nil
	},
}

func init() {
	schemaCmd.AddCommand(schemaRunbookCmd)
	schemaCmd.AddCommand(schemaToolCmd)
}

// --- test ---

var (
	testScenario string
	testJSON     bool
	testFailFast bool
	testTimeout  string
)

var testCmd = &cobra.Command{
	Use:   "test [runbook.yaml...]",
	Short: "Run scenario replay tests with assertions",
	Args:  cobra.MinimumNArgs(1),
	RunE:  runTest,
}

func runTest(cmd *cobra.Command, args []string) error {
	timeout, err := time.ParseDuration(testTimeout)
	if err != nil {
		return fmt.Errorf("invalid --timeout: %w", err)
	}

	runner := &ktesting.Runner{
		Timeout:  timeout,
		FailFast: testFailFast,
	}

	allPassed := true

	for _, filePath := range args {
		var output *ktesting.TestOutput
		var err error

		if testScenario != "" {
			result, e := runner.RunScenario(filePath, testScenario)
			if e != nil {
				return e
			}
			output = &ktesting.TestOutput{
				Runbook:   filepath.Base(filePath),
				Scenarios: []ktesting.TestResult{*result},
				Summary: ktesting.TestSummary{
					Total: 1,
				},
			}
			switch result.Status {
			case "passed":
				output.Summary.Passed = 1
			case "failed":
				output.Summary.Failed = 1
			case "skipped":
				output.Summary.Skipped = 1
			case "error":
				output.Summary.Errors = 1
			}
		} else {
			output, err = runner.RunAll(filePath)
			if err != nil {
				return err
			}
		}

		if testJSON {
			enc := json.NewEncoder(os.Stdout)
			enc.SetIndent("", "  ")
			enc.Encode(output)
		} else {
			printTestOutput(output)
		}

		if output.Summary.Failed > 0 || output.Summary.Errors > 0 {
			allPassed = false
		}
	}

	if !allPassed {
		return fmt.Errorf("tests failed")
	}
	return nil
}

func printTestOutput(output *ktesting.TestOutput) {
	fmt.Printf("\n  %s\n", output.Runbook)
	for _, s := range output.Scenarios {
		icon := "✓"
		switch s.Status {
		case "failed":
			icon = "✗"
		case "error":
			icon = "!"
		case "skipped":
			icon = "○"
		}
		fmt.Printf("    %s %s (%dms)\n", icon, s.ScenarioName, s.DurationMs)
		if s.Error != "" {
			fmt.Printf("      error: %s\n", s.Error)
		}
		for _, a := range s.Assertions {
			if !a.Passed {
				fmt.Printf("      ✗ %s: %s\n", a.Type, a.Message)
			}
		}
	}
	fmt.Printf("\n  %d passed, %d failed, %d skipped, %d errors (total: %d)\n",
		output.Summary.Passed, output.Summary.Failed, output.Summary.Skipped, output.Summary.Errors, output.Summary.Total)
}
