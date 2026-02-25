package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/ormasoftchile/gert/pkg/governance"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// ResumeEngine creates an Engine that resumes from the most recent snapshot.
func ResumeEngine(rb *schema.Runbook, executor providers.CommandExecutor, collector providers.EvidenceCollector, runID string) (*Engine, error) {
	baseDir := filepath.Join(".runbook", "runs", runID)

	// Find the most recent snapshot
	snapshotDir := filepath.Join(baseDir, "snapshots")
	entries, err := os.ReadDir(snapshotDir)
	if err != nil {
		return nil, fmt.Errorf("read snapshot dir: %w", err)
	}

	if len(entries) == 0 {
		return nil, fmt.Errorf("no snapshots found for run %s", runID)
	}

	// Sort and pick the last snapshot
	sort.Slice(entries, func(i, j int) bool {
		return entries[i].Name() < entries[j].Name()
	})
	lastSnapshot := entries[len(entries)-1]
	snapshotPath := filepath.Join(snapshotDir, lastSnapshot.Name())

	state, err := LoadSnapshot(snapshotPath)
	if err != nil {
		return nil, fmt.Errorf("load snapshot: %w", err)
	}

	// Resume from the NEXT step after the last completed one
	state.CurrentStepIndex++

	// Re-open trace for append
	trace, err := NewTraceWriter(filepath.Join(baseDir, "trace.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("reopen trace: %w", err)
	}

	// Rebuild governance
	gov := governance.NewGovernanceEngine(rb.Meta.Governance)
	var redactRules []*governance.CompiledRedaction
	if rb.Meta.Governance != nil && len(rb.Meta.Governance.Redact) > 0 {
		redactRules, err = governance.CompileRedactionRules(rb.Meta.Governance.Redact)
		if err != nil {
			return nil, fmt.Errorf("compile redaction rules: %w", err)
		}
	}

	fmt.Printf("Resuming run %s from step %d/%d\n", runID, state.CurrentStepIndex+1, len(rb.Steps))

	return &Engine{
		Runbook:   rb,
		State:     state,
		Gov:       gov,
		Redact:    redactRules,
		Executor:  executor,
		Collector: collector,
		Trace:     trace,
		BaseDir:   baseDir,
	}, nil
}

// ResumeForServe creates an engine that resumes an existing run, reusing its
// run directory and restoring state from the provided parameters. Used by the
// serve layer to restore a session after process restart.
func ResumeForServe(rb *schema.Runbook, executor providers.CommandExecutor, collector providers.EvidenceCollector,
	runID string, vars, captures map[string]string, history []*providers.StepResult,
	mode, actor string, startedAt time.Time) (*Engine, error) {

	baseDir := filepath.Join(".runbook", "runs", runID)

	// Ensure directories exist
	for _, sub := range []string{"snapshots", "attachments"} {
		if err := os.MkdirAll(filepath.Join(baseDir, sub), 0755); err != nil {
			return nil, fmt.Errorf("ensure run directory: %w", err)
		}
	}

	// Re-open trace for append
	trace, err := NewTraceWriter(filepath.Join(baseDir, "trace.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("reopen trace: %w", err)
	}

	// Rebuild governance
	gov := governance.NewGovernanceEngine(rb.Meta.Governance)
	var redactRules []*governance.CompiledRedaction
	if rb.Meta.Governance != nil && len(rb.Meta.Governance.Redact) > 0 {
		redactRules, err = governance.CompileRedactionRules(rb.Meta.Governance.Redact)
		if err != nil {
			return nil, fmt.Errorf("compile redaction rules: %w", err)
		}
	}

	state := &RunState{
		RunID:     runID,
		Mode:      mode,
		StartedAt: startedAt,
		Actor:     actor,
		Vars:      vars,
		Captures:  captures,
		History:   history,
	}

	e := &Engine{
		Runbook:   rb,
		State:     state,
		Gov:       gov,
		Redact:    redactRules,
		Executor:  executor,
		Collector: collector,
		Trace:     trace,
		BaseDir:   baseDir,
	}
	e.RestoreStepCounts()
	return e, nil
}

// RestoreStepCounts rebuilds the step counters from the engine's history.
// Called after resume to ensure BuildManifest reports correct totals.
func (e *Engine) RestoreStepCounts() {
	e.stepCounts = StepsSummary{}
	for _, h := range e.State.History {
		switch h.Status {
		case "passed":
			e.stepCounts.Passed++
		case "failed":
			e.stepCounts.Failed++
		case "skipped":
			e.stepCounts.Skipped++
		}
		e.stepCounts.Total++
	}
}
