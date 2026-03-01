// Package main provides the gert-tui binary — Bubble Tea terminal UI.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/ormasoftchile/gert/pkg/ecosystem/tui"
	"github.com/ormasoftchile/gert/pkg/kernel/replay"
	kvalidate "github.com/ormasoftchile/gert/pkg/kernel/validate"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: gert-tui <runbook.yaml> [--mode real|dry-run|replay] [--var key=value] [--scenario name]")
		os.Exit(1)
	}

	filePath := os.Args[1]
	mode := "real"
	scenario := ""
	vars := make(map[string]string)

	// Parse flags
	for i := 2; i < len(os.Args); i++ {
		arg := os.Args[i]
		switch {
		case arg == "--mode" && i+1 < len(os.Args):
			i++
			mode = os.Args[i]
		case arg == "--scenario" && i+1 < len(os.Args):
			i++
			scenario = os.Args[i]
		case strings.HasPrefix(arg, "--var") && i+1 < len(os.Args):
			i++
			parts := strings.SplitN(os.Args[i], "=", 2)
			if len(parts) == 2 {
				vars[parts[0]] = parts[1]
			}
		}
	}

	// Validate and load runbook
	rb, errs := kvalidate.ValidateFile(filePath)
	if errs != nil {
		for _, e := range errs {
			if e.Severity == "error" {
				fmt.Fprintf(os.Stderr, "  [%s] %s\n", e.Phase, e.Message)
			}
		}
		for _, e := range errs {
			if e.Severity == "error" {
				fmt.Fprintln(os.Stderr, "Validation failed")
				os.Exit(1)
			}
		}
	}

	model := tui.NewModel(rb)

	// Build run config for engine
	runCfg := tui.RunConfig{
		Mode:        mode,
		Vars:        vars,
		RunbookPath: filePath,
		Scenario:    scenario,
	}

	// If replay mode with scenario, set up replay executor
	if mode == "replay" && scenario != "" {
		dir := filepath.Dir(filePath)
		base := strings.TrimSuffix(filepath.Base(filePath), filepath.Ext(filePath))
		scenarioDir := filepath.Join(dir, "scenarios", base, scenario)
		s, err := replay.LoadScenarioDir(scenarioDir)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Error loading scenario %q: %v\n", scenario, err)
			os.Exit(1)
		}
		runCfg.ToolExec = replay.NewReplayExecutor(s)
		runCfg.Mode = "replay"
	}

	p := tea.NewProgram(model, tea.WithAltScreen())

	// Start engine — streams trace events back to TUI via p.Send()
	tui.StartEngine(model, runCfg, p)

	if _, err := p.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
