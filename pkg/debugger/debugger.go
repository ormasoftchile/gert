// Package debugger implements the interactive REPL debugger for runbooks.
package debugger

import (
	"context"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/chzyer/readline"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/runtime"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// Debugger provides an interactive REPL for stepping through runbook execution.
type Debugger struct {
	runbook   *schema.Runbook
	engine    *runtime.Engine
	state     *runtime.RunState
	output    io.Writer
	rl        *readline.Instance
	executor  providers.CommandExecutor
	collector providers.EvidenceCollector
	mode      string
	actor     string
}

// New creates a new debugger for the given runbook.
func New(rb *schema.Runbook, executor providers.CommandExecutor, collector providers.EvidenceCollector, mode, actor string) (*Debugger, error) {
	eng, err := runtime.NewEngine(rb, executor, collector, mode, actor)
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}

	d := &Debugger{
		runbook:   rb,
		engine:    eng,
		state:     eng.State,
		output:    os.Stdout,
		executor:  executor,
		collector: collector,
		mode:      mode,
		actor:     actor,
	}
	return d, nil
}

// Engine returns the underlying runtime engine for external configuration.
func (d *Debugger) Engine() *runtime.Engine {
	return d.engine
}

// Run starts the interactive REPL loop.
func (d *Debugger) Run(ctx context.Context) error {
	commands := []string{"next", "continue", "dump", "print vars", "print captures",
		"history", "evidence set", "evidence check", "evidence attach",
		"approve", "snapshot", "help", "quit"}

	var completer = readline.NewPrefixCompleter()
	for _, cmd := range commands {
		completer.Children = append(completer.Children,
			readline.PcItem(cmd))
	}

	rl, err := readline.NewEx(&readline.Config{
		Prompt:          d.buildPrompt(),
		AutoComplete:    completer,
		InterruptPrompt: "^C",
		EOFPrompt:       "quit",
	})
	if err != nil {
		return fmt.Errorf("init readline: %w", err)
	}
	d.rl = rl
	defer rl.Close()

	fmt.Fprintf(d.output, "gert debugger — %d steps, mode=%s\n", len(d.runbook.Steps), d.mode)
	fmt.Fprintf(d.output, "Type 'help' for available commands, 'next' to execute next step.\n\n")

	for {
		rl.SetPrompt(d.buildPrompt())
		line, err := rl.Readline()
		if err != nil {
			if err == readline.ErrInterrupt || err == io.EOF {
				return nil
			}
			return err
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		parts := strings.Fields(line)
		cmd := parts[0]

		switch cmd {
		case "next", "n":
			if err := d.handleNext(ctx); err != nil {
				fmt.Fprintf(d.output, "Error: %v\n", err)
			}
		case "continue", "c":
			if err := d.handleContinue(ctx); err != nil {
				fmt.Fprintf(d.output, "Error: %v\n", err)
			}
		case "print", "p":
			d.handlePrint(parts)
		case "history", "h":
			d.handleHistory()
		case "evidence":
			d.handleEvidence(parts)
		case "approve":
			d.handleApprove(parts)
		case "snapshot":
			d.handleSnapshot()
		case "dump":
			d.handleDump()
		case "help", "?":
			d.handleHelp()
		case "quit", "q":
			fmt.Fprintf(d.output, "Exiting debugger.\n")
			return nil
		default:
			fmt.Fprintf(d.output, "Unknown command: %q. Type 'help' for available commands.\n", cmd)
		}
	}
}

// buildPrompt creates the prompt string: gert[step N/total | step_id]>
func (d *Debugger) buildPrompt() string {
	stepIdx := d.state.CurrentStepIndex
	total := len(d.runbook.Steps)
	if stepIdx >= total {
		return "gert[done]> "
	}
	stepID := d.runbook.Steps[stepIdx].ID
	return fmt.Sprintf("gert[%d/%d | %s]> ", stepIdx+1, total, stepID)
}
