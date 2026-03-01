package main

import (
	"fmt"
	"os"
	"path/filepath"
	"time"

	ktesting "github.com/ormasoftchile/gert/pkg/kernel/testing"
	"github.com/spf13/cobra"
)

var diffCmd = &cobra.Command{
	Use:   "diff [runbook.yaml]",
	Short: "Compare scenario test outcomes for change detection",
	Args:  cobra.ExactArgs(1),
	RunE:  runDiff,
}

func runDiff(cmd *cobra.Command, args []string) error {
	filePath := args[0]

	runner := &ktesting.Runner{
		Timeout:  30 * time.Second,
		FailFast: false,
	}

	output, err := runner.RunAll(filePath)
	if err != nil {
		return fmt.Errorf("run scenarios: %w", err)
	}

	fmt.Printf("  %s\n", filepath.Base(filePath))
	for _, s := range output.Scenarios {
		icon := "="
		if s.Status == "failed" || s.Status == "error" {
			icon = "≠"
		}
		fmt.Printf("    %s %s: %s\n", icon, s.ScenarioName, s.Status)
	}

	changed := output.Summary.Failed + output.Summary.Errors
	fmt.Printf("\n  %d same, %d changed\n", output.Summary.Passed, changed)
	if changed > 0 {
		return fmt.Errorf("%d scenario(s) changed", changed)
	}
	return nil
}

func init() {
	rootCmd.AddCommand(diffCmd)
}

// --- outcomes ---

var (
	outcomesRunbook string
	outcomesJSON    bool
)

var outcomesCmd = &cobra.Command{
	Use:   "outcomes",
	Short: "Aggregate outcomes from trace files",
	RunE:  runOutcomes,
}

func runOutcomes(cmd *cobra.Command, args []string) error {
	// Scan traces directory for trace files
	pattern := "traces/*.jsonl"
	files, err := filepath.Glob(pattern)
	if err != nil {
		return fmt.Errorf("glob traces: %w", err)
	}

	if len(files) == 0 {
		fmt.Println("No trace files found in traces/")
		return nil
	}

	type OutcomeTally struct {
		Category string
		Count    int
	}

	tally := make(map[string]int)
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			continue
		}
		// Simple scan for outcome_resolved events
		lines := splitLines(data)
		for _, line := range lines {
			if contains(line, "outcome_resolved") {
				cat := extractField(line, "category")
				if cat != "" {
					tally[cat]++
				}
			}
		}
	}

	if outcomesJSON {
		fmt.Print("{")
		first := true
		for cat, count := range tally {
			if !first {
				fmt.Print(",")
			}
			fmt.Printf("%q:%d", cat, count)
			first = false
		}
		fmt.Println("}")
	} else {
		fmt.Printf("  Outcomes from %d trace file(s):\n", len(files))
		for cat, count := range tally {
			fmt.Printf("    %s: %d\n", cat, count)
		}
	}

	return nil
}

func splitLines(data []byte) []string {
	var lines []string
	start := 0
	for i, b := range data {
		if b == '\n' {
			lines = append(lines, string(data[start:i]))
			start = i + 1
		}
	}
	if start < len(data) {
		lines = append(lines, string(data[start:]))
	}
	return lines
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && findSubstr(s, substr) >= 0
}

func findSubstr(s, substr string) int {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return i
		}
	}
	return -1
}

func extractField(line, field string) string {
	pattern := `"` + field + `":"`
	idx := findSubstr(line, pattern)
	if idx < 0 {
		return ""
	}
	start := idx + len(pattern)
	end := findSubstr(line[start:], `"`)
	if end < 0 {
		return ""
	}
	return line[start : start+end]
}

func init() {
	outcomesCmd.Flags().StringVar(&outcomesRunbook, "runbook", "", "Filter by runbook name")
	outcomesCmd.Flags().BoolVar(&outcomesJSON, "json", false, "JSON output")
	rootCmd.AddCommand(outcomesCmd)
}
