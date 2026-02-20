package runtime

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

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
