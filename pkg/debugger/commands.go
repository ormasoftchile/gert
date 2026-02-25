package debugger

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/evidence"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/runtime"
)

// handleNext executes the next step and advances.
func (d *Debugger) handleNext(ctx context.Context) error {
	if d.state.CurrentStepIndex >= len(d.runbook.Steps) {
		fmt.Fprintf(d.output, "All steps completed.\n")
		return nil
	}

	step := d.runbook.Steps[d.state.CurrentStepIndex]
	fmt.Fprintf(d.output, "Executing step %d: %s [%s]\n", d.state.CurrentStepIndex+1, step.Title, step.ID)

	result, err := d.engine.ExecuteStep(ctx, d.state.CurrentStepIndex)
	if err != nil {
		return err
	}

	// State is already updated by engine.ExecuteStep (history, captures, index)

	// Display result
	if result.Status == "passed" {
		fmt.Fprintf(d.output, "  ✓ %s passed\n", step.ID)
	} else {
		fmt.Fprintf(d.output, "  ✗ %s failed: %s\n", step.ID, result.Error)
	}
	return nil
}

// handleContinue executes all remaining steps.
func (d *Debugger) handleContinue(ctx context.Context) error {
	for d.state.CurrentStepIndex < len(d.runbook.Steps) {
		if err := d.handleNext(ctx); err != nil {
			return err
		}
		// Stop on failure
		if len(d.state.History) > 0 {
			last := d.state.History[len(d.state.History)-1]
			if last.Status == "failed" {
				fmt.Fprintf(d.output, "Halted on failure.\n")
				return nil
			}
		}
	}
	fmt.Fprintf(d.output, "All steps completed.\n")
	return nil
}

// handlePrint displays vars or captures.
func (d *Debugger) handlePrint(parts []string) {
	if len(parts) < 2 {
		fmt.Fprintf(d.output, "Usage: print vars|captures\n")
		return
	}
	switch parts[1] {
	case "vars":
		if len(d.state.Vars) == 0 {
			fmt.Fprintf(d.output, "No variables defined.\n")
			return
		}
		for k, v := range d.state.Vars {
			fmt.Fprintf(d.output, "  %s = %q\n", k, v)
		}
	case "captures":
		if len(d.state.Captures) == 0 {
			fmt.Fprintf(d.output, "No captures recorded.\n")
			return
		}
		for k, v := range d.state.Captures {
			display := v
			if len(display) > 200 {
				display = display[:200] + "..."
			}
			fmt.Fprintf(d.output, "  %s = %q\n", k, display)
		}
	default:
		fmt.Fprintf(d.output, "Unknown print target: %q. Use 'vars' or 'captures'.\n", parts[1])
	}
}

// handleHistory shows completed step results.
func (d *Debugger) handleHistory() {
	if len(d.state.History) == 0 {
		fmt.Fprintf(d.output, "No steps executed yet.\n")
		return
	}
	for _, r := range d.state.History {
		status := "✓"
		if r.Status == "failed" {
			status = "✗"
		}
		fmt.Fprintf(d.output, "  %s [%d] %s — %s\n", status, r.StepIndex, r.StepID, r.Status)
		if r.Error != "" {
			fmt.Fprintf(d.output, "       error: %s\n", r.Error)
		}
	}
}

// handleEvidence handles evidence set/check/attach commands.
func (d *Debugger) handleEvidence(parts []string) {
	if len(parts) < 2 {
		fmt.Fprintf(d.output, "Usage: evidence set <name> <value> | evidence check <name> <item> | evidence attach <name> <path>\n")
		return
	}
	switch parts[1] {
	case "set":
		if len(parts) < 4 {
			fmt.Fprintf(d.output, "Usage: evidence set <name> <value>\n")
			return
		}
		name := parts[2]
		value := strings.Join(parts[3:], " ")
		if d.currentEvidence() == nil {
			d.initCurrentEvidence()
		}
		d.currentEvidence()[name] = evidence.NewTextEvidence(value)
		fmt.Fprintf(d.output, "  Evidence %q set to %q\n", name, value)

	case "check":
		if len(parts) < 4 {
			fmt.Fprintf(d.output, "Usage: evidence check <name> <item>\n")
			return
		}
		name := parts[2]
		item := strings.Join(parts[3:], " ")
		ev := d.currentEvidence()
		if ev == nil {
			d.initCurrentEvidence()
			ev = d.currentEvidence()
		}
		if ev[name] == nil {
			ev[name] = &providers.EvidenceValue{Kind: "checklist", Items: make(map[string]bool)}
		}
		ev[name].Items[item] = true
		fmt.Fprintf(d.output, "  Checklist %q: marked %q complete\n", name, item)

	case "attach":
		if len(parts) < 4 {
			fmt.Fprintf(d.output, "Usage: evidence attach <name> <path>\n")
			return
		}
		name := parts[2]
		path := parts[3]
		hash, size, err := evidence.HashFile(path)
		if err != nil {
			fmt.Fprintf(d.output, "  Error: %v\n", err)
			return
		}
		if d.currentEvidence() == nil {
			d.initCurrentEvidence()
		}
		d.currentEvidence()[name] = &providers.EvidenceValue{
			Kind:   "attachment",
			Path:   path,
			SHA256: hash,
			Size:   size,
		}
		fmt.Fprintf(d.output, "  Attachment %q: %s (%d bytes, sha256=%s...)\n", name, path, size, hash[:12])

	default:
		fmt.Fprintf(d.output, "Unknown evidence command: %q\n", parts[1])
	}
}

// handleApprove records an approval for the current step.
func (d *Debugger) handleApprove(parts []string) {
	actor := d.actor
	// Look for --as flag in parts
	for i, p := range parts {
		if p == "--as" && i+1 < len(parts) {
			actor = parts[i+1]
			break
		}
	}
	if actor == "" {
		fmt.Fprintf(d.output, "Usage: approve --as <identity>\n")
		return
	}
	fmt.Fprintf(d.output, "  Approval recorded from %q at %s\n", actor, time.Now().Format(time.RFC3339))
}

// handleSnapshot saves a snapshot of the current state.
func (d *Debugger) handleSnapshot() {
	snapshotPath := filepath.Join(d.engine.GetBaseDir(), "snapshots",
		fmt.Sprintf("step-%04d.json", d.state.CurrentStepIndex))
	if err := runtime.SaveSnapshot(d.state, snapshotPath); err != nil {
		fmt.Fprintf(d.output, "  Error: %v\n", err)
		return
	}
	fmt.Fprintf(d.output, "  Snapshot saved: %s\n", snapshotPath)
}

// handleDump outputs the full current state as JSON.
func (d *Debugger) handleDump() {
	data, err := json.MarshalIndent(d.state, "", "  ")
	if err != nil {
		fmt.Fprintf(d.output, "  Error marshaling state: %v\n", err)
		return
	}
	fmt.Fprintln(d.output, string(data))
}

// handleHelp displays available commands.
func (d *Debugger) handleHelp() {
	fmt.Fprintln(d.output, "Available commands:")
	fmt.Fprintln(d.output, "  next (n)         Execute the next step")
	fmt.Fprintln(d.output, "  continue (c)     Execute all remaining steps")
	fmt.Fprintln(d.output, "  print vars       Show current variables")
	fmt.Fprintln(d.output, "  print captures   Show captured values")
	fmt.Fprintln(d.output, "  history          Show executed step results")
	fmt.Fprintln(d.output, "  evidence set     Set text evidence: evidence set <name> <value>")
	fmt.Fprintln(d.output, "  evidence check   Mark checklist item: evidence check <name> <item>")
	fmt.Fprintln(d.output, "  evidence attach  Attach file: evidence attach <name> <path>")
	fmt.Fprintln(d.output, "  approve          Record approval: approve --as <identity>")
	fmt.Fprintln(d.output, "  snapshot         Save state snapshot")
	fmt.Fprintln(d.output, "  dump             Output full state as JSON")
	fmt.Fprintln(d.output, "  help (?)         Show this help")
	fmt.Fprintln(d.output, "  quit (q)         Exit debugger")
}

// currentEvidence returns the current step's evidence map (from pending result).
func (d *Debugger) currentEvidence() map[string]*providers.EvidenceValue {
	if len(d.state.History) == 0 {
		return nil
	}
	last := d.state.History[len(d.state.History)-1]
	return last.Evidence
}

// initCurrentEvidence ensures there's an evidence map on the latest history entry.
func (d *Debugger) initCurrentEvidence() {
	if len(d.state.History) == 0 {
		// Create a placeholder result for the current step
		d.state.History = append(d.state.History, &providers.StepResult{
			RunID:    d.state.RunID,
			StepID:   d.runbook.Steps[d.state.CurrentStepIndex].ID,
			Evidence: make(map[string]*providers.EvidenceValue),
		})
	} else {
		last := d.state.History[len(d.state.History)-1]
		if last.Evidence == nil {
			last.Evidence = make(map[string]*providers.EvidenceValue)
		}
	}
}
