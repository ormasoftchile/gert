package runtime

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"text/template"
	"time"

	"github.com/expr-lang/expr"
	"github.com/ormasoftchile/gert/pkg/assertions"
	"github.com/ormasoftchile/gert/pkg/evidence"
	"github.com/ormasoftchile/gert/pkg/governance"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/replay"
	"github.com/ormasoftchile/gert/pkg/schema"
	"github.com/ormasoftchile/gert/pkg/tools"

	"gopkg.in/yaml.v3"
)

// GenerateRunID creates a run ID in format YYYYMMDDTHHmmss-xxxx.
func GenerateRunID() string {
	ts := time.Now().Format("20060102T150405")
	suffix := make([]byte, 4)
	rand.Read(suffix)
	return fmt.Sprintf("%s-%x", ts, suffix)
}

// templateVarRe extracts variable names from Go template expressions like {{ .varName }}.
var templateVarRe = regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)

// Engine is the runtime execution engine that drives runbook execution.
type Engine struct {
	Runbook     *schema.Runbook
	State       *RunState
	Gov         *governance.GovernanceEngine
	Redact      []*governance.CompiledRedaction
	Executor    providers.CommandExecutor
	Collector   providers.EvidenceCollector
	Trace       *TraceWriter
	BaseDir     string // .runbook/runs/<run_id>/
	xtsProvider *providers.XTSProvider
	XTSScenario *replay.XTSScenario // nil unless replay mode with scenario dir
	ICMID       string              // ICM incident ID (optional)
	RunbookPath string              // path to the runbook file
	ToolManager *tools.Manager      // tool definition manager (nil = no tools)
	outcome     *OutcomeRecord      // set by outcome evaluation
	stepCounts  StepsSummary        // incremented during execution
	ChainDepth  int                 // current chain depth (0 = root)
	ParentRunID string              // parent run ID (if chained)
	ChildRuns   []ChildRunRef       // child runs spawned by this engine
}

// NewEngine creates a new engine for executing a runbook.
func NewEngine(rb *schema.Runbook, executor providers.CommandExecutor, collector providers.EvidenceCollector, mode string, actor string) (*Engine, error) {
	runID := GenerateRunID()
	baseDir := filepath.Join(".runbook", "runs", runID)

	// Create directory structure
	for _, sub := range []string{"snapshots", "attachments"} {
		if err := os.MkdirAll(filepath.Join(baseDir, sub), 0755); err != nil {
			return nil, fmt.Errorf("create run directory: %w", err)
		}
	}

	// Set up trace writer
	trace, err := NewTraceWriter(filepath.Join(baseDir, "trace.jsonl"))
	if err != nil {
		return nil, fmt.Errorf("create trace writer: %w", err)
	}

	// Set up governance
	gov := governance.NewGovernanceEngine(rb.Meta.Governance)

	// Compile redaction rules
	var redactRules []*governance.CompiledRedaction
	if rb.Meta.Governance != nil && len(rb.Meta.Governance.Redact) > 0 {
		redactRules, err = governance.CompileRedactionRules(rb.Meta.Governance.Redact)
		if err != nil {
			return nil, fmt.Errorf("compile redaction rules: %w", err)
		}
	}

	// Initialize vars from meta.vars
	vars := make(map[string]string)
	for k, v := range rb.Meta.Vars {
		vars[k] = v
	}

	state := &RunState{
		RunID:            runID,
		RunbookPath:      "",
		Mode:             mode,
		StartedAt:        time.Now(),
		Actor:            actor,
		CurrentStepIndex: 0,
		Vars:             vars,
		Captures:         make(map[string]string),
		History:          nil,
	}

	// Initialize XTS provider if runbook has xts config
	// Initialize XTS provider (legacy path — provides CLIPath for tool registration)
	var xtsProv *providers.XTSProvider
	if rb.Meta.XTS != nil {
		var err2 error
		xtsProv, err2 = providers.NewXTSProvider(rb.Meta.XTS)
		if err2 != nil {
			// Non-fatal: warn but allow non-xts steps to run
			fmt.Fprintf(os.Stderr, "warning: XTS provider init failed: %v\n", err2)
		}
	}

	return &Engine{
		Runbook:     rb,
		State:       state,
		Gov:         gov,
		Redact:      redactRules,
		Executor:    executor,
		Collector:   collector,
		Trace:       trace,
		BaseDir:     baseDir,
		xtsProvider: xtsProv,
	}, nil
}

// Run executes the runbook. Uses tree: if present, otherwise flat steps.
func (e *Engine) Run(ctx context.Context) error {
	defer e.Trace.Close()

	if len(e.Runbook.Tree) > 0 {
		return e.runTree(ctx, e.Runbook.Tree)
	}
	return e.runFlat(ctx)
}

// runTree recursively walks the tree, executing steps and evaluating branches.
func (e *Engine) runTree(ctx context.Context, nodes []schema.TreeNode) error {
	for _, node := range nodes {
		step := node.Step
		stepIdx := e.stepCounts.Total

		fmt.Printf("\n▶ Step: %s [%s]\n", step.Title, step.ID)

		// Execute the step
		result, err := e.executeStep(ctx, stepIdx, step)
		if err != nil {
			return fmt.Errorf("step %q: %w", step.ID, err)
		}

		// Write trace
		if err := e.Trace.Write(result); err != nil {
			return fmt.Errorf("write trace for step %q: %w", step.ID, err)
		}

		// Save snapshot
		e.State.History = append(e.State.History, result)
		snapshotPath := filepath.Join(e.BaseDir, "snapshots", fmt.Sprintf("step-%04d.json", stepIdx))
		if err := SaveSnapshot(e.State, snapshotPath); err != nil {
			return fmt.Errorf("save snapshot for step %q: %w", step.ID, err)
		}

		// Merge captures
		for k, v := range result.Captures {
			e.State.Captures[k] = v
		}

		if result.Status == "failed" {
			e.stepCounts.Failed++
			e.stepCounts.Total++
			fmt.Printf("  ✗ Step %q failed: %s\n", step.ID, result.Error)
			return fmt.Errorf("step %q failed: %s", step.ID, result.Error)
		}

		e.stepCounts.Passed++
		e.stepCounts.Total++
		fmt.Printf("  ✓ Step %q passed\n", step.ID)

		// Evaluate outcomes
		if len(step.Outcomes) > 0 {
			for _, outcome := range step.Outcomes {
				triggered, err := e.evalCondition(outcome.When)
				if err != nil {
					return fmt.Errorf("step %q outcome when: %w", step.ID, err)
				}
				if triggered {
					rec, _ := e.resolveTemplate(outcome.Recommendation)
					if rec == "" {
						rec = outcome.Recommendation
					}
					e.outcome = &OutcomeRecord{
						State:          outcome.State,
						StepID:         step.ID,
						Recommendation: strings.TrimSpace(rec),
					}
					fmt.Printf("\n■ Outcome: %s (at step %q)\n", outcome.State, step.ID)
					if rec != "" {
						fmt.Printf("  Recommendation: %s\n", strings.TrimSpace(rec))
					}
					fmt.Printf("  Artifacts: %s\n", e.BaseDir)
					if outcome.NextRunbook != nil {
						return e.chainToRunbook(ctx, outcome)
					}
					return nil
				}
			}
		}

		// Evaluate branches
		if len(node.Branches) > 0 {
			for _, branch := range node.Branches {
				matched, err := e.evalCondition(branch.Condition)
				if err != nil {
					return fmt.Errorf("step %q branch condition: %w", step.ID, err)
				}
				if matched {
					fmt.Printf("\n  → Branch: %s\n", branch.Label)
					if err := e.runTree(ctx, branch.Steps); err != nil {
						return err
					}
					break // only first matching branch
				}
			}
		}
	}

	fmt.Printf("\n✓ Runbook completed successfully (%d steps)\n", e.stepCounts.Total)
	fmt.Printf("  Artifacts: %s\n", e.BaseDir)
	return nil
}

// runFlat executes flat steps[] (backward compatibility).
func (e *Engine) runFlat(ctx context.Context) error {

	for i := e.State.CurrentStepIndex; i < len(e.Runbook.Steps); i++ {
		e.State.CurrentStepIndex = i
		step := e.Runbook.Steps[i]

		// Evaluate when: guard — skip step if condition is false/empty
		if step.When != "" {
			matched, err := e.evalCondition(step.When)
			if err != nil {
				return fmt.Errorf("step %q when: %w", step.ID, err)
			}
			if !matched {
				e.stepCounts.Skipped++
				e.stepCounts.Total++
				fmt.Printf("\n⊘ Step %d/%d: %s [%s] — skipped (when: %s → false)\n", i+1, len(e.Runbook.Steps), step.Title, step.ID, step.When)
				// Record skip in trace
				skipResult := &providers.StepResult{
					RunID:     e.State.RunID,
					StepID:    step.ID,
					StepIndex: i,
					Status:    "skipped",
					Actor:     "engine",
					StartedAt: time.Now(),
					EndedAt:   time.Now(),
					Captures:  make(map[string]string),
				}
				if err := e.Trace.Write(skipResult); err != nil {
					return fmt.Errorf("write trace for skipped step %q: %w", step.ID, err)
				}
				e.State.History = append(e.State.History, skipResult)
				continue
			}
		}

		fmt.Printf("\n▶ Step %d/%d: %s [%s]\n", i+1, len(e.Runbook.Steps), step.Title, step.ID)

		result, err := e.executeStep(ctx, i, step)
		if err != nil {
			return fmt.Errorf("step %q: %w", step.ID, err)
		}

		// Write trace
		if err := e.Trace.Write(result); err != nil {
			return fmt.Errorf("write trace for step %q: %w", step.ID, err)
		}

		// Save snapshot
		e.State.History = append(e.State.History, result)
		snapshotPath := filepath.Join(e.BaseDir, "snapshots", fmt.Sprintf("step-%04d.json", i))
		if err := SaveSnapshot(e.State, snapshotPath); err != nil {
			return fmt.Errorf("save snapshot for step %q: %w", step.ID, err)
		}

		// Merge captures into engine state
		for k, v := range result.Captures {
			e.State.Captures[k] = v
		}

		// Halt on failure
		if result.Status == "failed" {
			e.stepCounts.Failed++
			e.stepCounts.Total++
			fmt.Printf("  ✗ Step %q failed: %s\n", step.ID, result.Error)
			fmt.Printf("  Artifacts: %s\n", e.BaseDir)
			fmt.Printf("  Resume with: gert exec <runbook> --resume %s\n", e.State.RunID)
			return fmt.Errorf("step %q failed: %s", step.ID, result.Error)
		}

		e.stepCounts.Passed++
		e.stepCounts.Total++
		fmt.Printf("  ✓ Step %q passed\n", step.ID)

		// Evaluate outcomes — check if this step reached a terminal state
		if len(step.Outcomes) > 0 {
			for _, outcome := range step.Outcomes {
				triggered, err := e.evalCondition(outcome.When)
				if err != nil {
					return fmt.Errorf("step %q outcome when: %w", step.ID, err)
				}
				if triggered {
					rec, _ := e.resolveTemplate(outcome.Recommendation)
					if rec == "" {
						rec = outcome.Recommendation
					}
					e.outcome = &OutcomeRecord{
						State:          outcome.State,
						StepID:         step.ID,
						Recommendation: strings.TrimSpace(rec),
					}
					fmt.Printf("\n■ Outcome: %s (at step %q)\n", outcome.State, step.ID)
					if rec != "" {
						fmt.Printf("  Recommendation: %s\n", strings.TrimSpace(rec))
					}
					fmt.Printf("  Artifacts: %s\n", e.BaseDir)

					// Chain to next runbook if specified
					if outcome.NextRunbook != nil {
						return e.chainToRunbook(ctx, outcome)
					}
					return nil
				}
			}
		}
	}

	fmt.Printf("\n✓ Runbook completed successfully (%d steps)\n", len(e.Runbook.Steps))
	fmt.Printf("  Artifacts: %s\n", e.BaseDir)
	return nil
}

// MaxChainDepth limits how deep runbook chaining can go.
const MaxChainDepth = 5

// chainToRunbook loads and runs a child runbook from an outcome's next_runbook.
func (e *Engine) chainToRunbook(ctx context.Context, outcome schema.Outcome) error {
	nr := outcome.NextRunbook

	// Resolve template vars in the file path
	resolvedFile, err := e.resolveTemplate(nr.File)
	if err != nil {
		return fmt.Errorf("resolve next_runbook file: %w", err)
	}

	// Resolve relative to the parent runbook's directory
	if !filepath.IsAbs(resolvedFile) && e.RunbookPath != "" {
		resolvedFile = filepath.Join(filepath.Dir(e.RunbookPath), resolvedFile)
	}

	fmt.Printf("\n→ Chaining to: %s\n", resolvedFile)

	// Load and validate child runbook
	childRB, errs := schema.ValidateFile(resolvedFile)
	if len(errs) > 0 {
		return fmt.Errorf("child runbook validation failed: %v", errs[0])
	}

	// Resolve inputs: merge parent captures + explicit input mappings
	if childRB.Meta.Vars == nil {
		childRB.Meta.Vars = make(map[string]string)
	}
	// First, carry forward all parent captures
	for k, v := range e.State.Captures {
		childRB.Meta.Vars[k] = v
	}
	// Then apply explicit input mappings (may reference parent vars/captures via templates)
	for k, v := range nr.Inputs {
		resolved, err := e.resolveTemplate(v)
		if err != nil {
			return fmt.Errorf("resolve next_runbook input %q: %w", k, err)
		}
		childRB.Meta.Vars[k] = resolved
	}
	// Also carry forward parent vars (lower priority than explicit mappings)
	for k, v := range e.State.Vars {
		if _, exists := childRB.Meta.Vars[k]; !exists {
			childRB.Meta.Vars[k] = v
		}
	}

	// Check chain depth
	depth := e.ChainDepth + 1
	if depth > MaxChainDepth {
		return fmt.Errorf("runbook chain depth %d exceeds maximum %d", depth, MaxChainDepth)
	}

	// Create child engine
	childEngine, err := NewEngine(childRB, e.Executor, e.Collector, e.State.Mode, e.State.Actor)
	if err != nil {
		return fmt.Errorf("create child engine: %w", err)
	}
	childEngine.ICMID = e.ICMID
	childEngine.RunbookPath = resolvedFile
	childEngine.ChainDepth = depth
	childEngine.ParentRunID = e.State.RunID

	// Inherit XTS provider and scenario
	childEngine.xtsProvider = e.xtsProvider
	childEngine.XTSScenario = e.XTSScenario

	fmt.Printf("  Child Run ID: %s (depth: %d)\n", childEngine.GetRunID(), depth)

	// Run child
	childErr := childEngine.Run(ctx)

	// Write child manifest
	childEngine.WriteManifest()

	// Record child in parent manifest
	childOutcome := ""
	if childEngine.outcome != nil {
		childOutcome = childEngine.outcome.State
	}
	e.ChildRuns = append(e.ChildRuns, ChildRunRef{
		RunID:   childEngine.GetRunID(),
		Runbook: resolvedFile,
		Outcome: childOutcome,
	})

	return childErr
}

// resolveImport resolves an invoke runbook alias to a file path using imports.
// If the alias is found in the imports map, returns the resolved path.
// Otherwise, treats it as a direct file path.
func (e *Engine) resolveImport(alias string) string {
	if e.Runbook.Imports != nil {
		if path, ok := e.Runbook.Imports[alias]; ok {
			return path
		}
	}
	return alias
}

// executeInvokeStep runs a child runbook inline as a sub-procedure.
// It creates a child engine, maps inputs, runs the child tree to completion,
// evaluates the gate condition, and maps captures back to the parent.
func (e *Engine) executeInvokeStep(ctx context.Context, step schema.Step, result *providers.StepResult) {
	if step.Invoke == nil {
		result.Status = "failed"
		result.Error = "invoke step missing invoke config"
		return
	}

	// Resolve the runbook alias → file path
	resolvedFile := e.resolveImport(step.Invoke.Runbook)

	// Resolve template vars in the file path
	resolvedFile = e.ResolveTemplatePublic(resolvedFile)

	// Resolve relative to the parent runbook's directory
	if !filepath.IsAbs(resolvedFile) && e.RunbookPath != "" {
		resolvedFile = filepath.Join(filepath.Dir(e.RunbookPath), resolvedFile)
	}

	fmt.Fprintf(os.Stderr, "  → Invoking child runbook: %s\n", resolvedFile)

	// Load and validate child runbook
	childRB, errs := schema.ValidateFile(resolvedFile)
	if len(errs) > 0 {
		result.Status = "failed"
		result.Error = fmt.Sprintf("child runbook validation failed: %v", errs[0])
		return
	}

	// Resolve inputs: apply explicit input mappings from parent vars/captures
	if childRB.Meta.Vars == nil {
		childRB.Meta.Vars = make(map[string]string)
	}
	for k, v := range step.Invoke.Inputs {
		resolved := e.ResolveTemplatePublic(v)
		childRB.Meta.Vars[k] = resolved
	}

	// Check chain depth
	depth := e.ChainDepth + 1
	if depth > MaxChainDepth {
		result.Status = "failed"
		result.Error = fmt.Sprintf("invoke chain depth %d exceeds maximum %d", depth, MaxChainDepth)
		return
	}

	// Create child engine
	childEngine, err := NewEngine(childRB, e.Executor, e.Collector, e.State.Mode, e.State.Actor)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("create child engine: %v", err)
		return
	}
	childEngine.ICMID = e.ICMID
	childEngine.RunbookPath = resolvedFile
	childEngine.ChainDepth = depth
	childEngine.ParentRunID = e.State.RunID

	// Inherit XTS provider and scenario
	childEngine.xtsProvider = e.xtsProvider
	childEngine.XTSScenario = e.XTSScenario

	fmt.Fprintf(os.Stderr, "  Child Run ID: %s (depth: %d)\n", childEngine.GetRunID(), depth)

	// Run child runbook to completion
	childErr := childEngine.Run(ctx)
	childEngine.WriteManifest()

	// Determine child outcome
	childOutcome := ""
	if childEngine.outcome != nil {
		childOutcome = childEngine.outcome.State
	}

	// Record child run reference
	e.ChildRuns = append(e.ChildRuns, ChildRunRef{
		RunID:   childEngine.GetRunID(),
		Runbook: resolvedFile,
		Outcome: childOutcome,
	})

	// Handle child execution error
	if childErr != nil {
		if step.Gate != nil && step.Gate.OnError == "skip" {
			fmt.Fprintf(os.Stderr, "  ⊘ Child runbook errored but gate.on_error=skip: %v\n", childErr)
			result.Status = "skipped"
			result.Error = fmt.Sprintf("child runbook error (skipped via gate): %v", childErr)
			result.Actor = "engine"
			return
		}
		result.Status = "failed"
		result.Error = fmt.Sprintf("child runbook failed: %v", childErr)
		return
	}

	// Evaluate gate: check if child outcome should stop the parent
	if step.Gate != nil && len(step.Gate.StopIf) > 0 {
		for _, stopState := range step.Gate.StopIf {
			if childOutcome == stopState {
				fmt.Fprintf(os.Stderr, "  ■ Gate triggered: child outcome %q matches stop_if\n", childOutcome)
				result.Status = "failed"
				result.Error = fmt.Sprintf("gate: child outcome %q matches stop_if", childOutcome)
				// Propagate child outcome to parent
				if childEngine.outcome != nil {
					e.outcome = childEngine.outcome
				}
				return
			}
		}
	}

	// Map child captures back to parent via step.Capture
	for parentKey, childKey := range step.Capture {
		if val, ok := childEngine.State.Captures[childKey]; ok {
			result.Captures[parentKey] = val
		} else if val, ok := childEngine.State.Vars[childKey]; ok {
			result.Captures[parentKey] = val
		}
	}

	result.Status = "passed"
	result.Actor = "engine"
	fmt.Fprintf(os.Stderr, "  ✓ Child runbook completed: outcome=%s\n", childOutcome)
}

// executeStep runs a single step based on its type.
func (e *Engine) executeStep(ctx context.Context, index int, step schema.Step) (*providers.StepResult, error) {
	start := time.Now()
	result := &providers.StepResult{
		RunID:     e.State.RunID,
		StepID:    step.ID,
		StepIndex: index,
		StartedAt: start,
		Captures:  make(map[string]string),
	}

	// Evaluate precondition: if check succeeds and skip_if_succeeds is true, auto-skip
	if step.Precondition != nil && step.Precondition.SkipIfSucceeds && len(step.Precondition.Check) > 0 {
		resolvedCheck, err := e.resolveArgv(step.Precondition.Check)
		if err == nil {
			probeResult, probeErr := e.Executor.Execute(ctx, resolvedCheck[0], resolvedCheck[1:], nil)
			if probeErr == nil && probeResult.ExitCode == 0 {
				msg := step.Precondition.Message
				if msg == "" {
					msg = fmt.Sprintf("precondition satisfied: %s", strings.Join(resolvedCheck, " "))
				}
				result.Status = "skipped"
				result.Actor = "engine"
				result.EndedAt = time.Now()
				result.Error = msg
				fmt.Fprintf(os.Stderr, "  ⊘ Precondition satisfied for %q: %s\n", step.ID, msg)
				return result, nil
			}
		}
	}

	// Create step context with timeout
	stepCtx := ctx
	if step.Type == "cli" {
		timeout := e.getStepTimeout(step)
		if timeout > 0 {
			var cancel context.CancelFunc
			stepCtx, cancel = context.WithTimeout(ctx, timeout)
			defer cancel()
		}
	}

	switch step.Type {
	case "cli":
		e.executeCLIStep(stepCtx, step, result)
	case "manual":
		e.executeManualStep(stepCtx, step, result)
	case "xts":
		// Route through tool manager if available (Phase C migration)
		if e.ToolManager != nil {
			defaultEnv := ""
			viewsRoot := ""
			if e.Runbook.Meta.XTS != nil {
				defaultEnv = e.Runbook.Meta.XTS.Environment
				viewsRoot = e.Runbook.Meta.XTS.ViewsRoot
			}
			synthStep := tools.DesugarXTSToToolStep(step, defaultEnv, viewsRoot, "")
			e.executeToolStep(stepCtx, synthStep, result)
		} else {
			e.executeXTSStep(stepCtx, step, result)
		}
	case "invoke":
		e.executeInvokeStep(stepCtx, step, result)
	case "tool":
		e.executeToolStep(stepCtx, step, result)
	default:
		result.Status = "failed"
		result.Error = fmt.Sprintf("unknown step type: %q", step.Type)
	}

	result.EndedAt = time.Now()
	return result, nil
}

// executeCLIStep handles CLI step execution.
func (e *Engine) executeCLIStep(ctx context.Context, step schema.Step, result *providers.StepResult) {
	result.Actor = "engine"

	if step.With == nil || len(step.With.Argv) == 0 {
		result.Status = "failed"
		result.Error = "CLI step has no argv"
		return
	}

	// Resolve template variables in argv
	resolvedArgv, err := e.resolveArgv(step.With.Argv)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("resolve variables: %v", err)
		return
	}

	// Governance: check command against allowlist/denylist
	if err := e.Gov.CheckCommand(resolvedArgv[0]); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("governance: %v", err)
		return
	}

	// Execute command (real, replay, or dry-run based on injected executor)
	cmdResult, err := e.Executor.Execute(ctx, resolvedArgv[0], resolvedArgv[1:], nil)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("execute: %v", err)
		return
	}

	// Apply redaction
	stdout := string(cmdResult.Stdout)
	stderr := string(cmdResult.Stderr)
	if len(e.Redact) > 0 {
		stdout = governance.RedactOutput(stdout, e.Redact)
		stderr = governance.RedactOutput(stderr, e.Redact)
	}

	// Extract captures
	for name, source := range step.Capture {
		switch source {
		case "stdout":
			result.Captures[name] = strings.TrimSpace(stdout)
		case "stderr":
			result.Captures[name] = strings.TrimSpace(stderr)
		}
	}

	// Evaluate assertions
	allPassed := true
	for _, a := range step.Assertions {
		ar := assertions.Evaluate(a, stdout, cmdResult.ExitCode)
		result.Assertions = append(result.Assertions, ar)
		if !ar.Passed {
			allPassed = false
		}
	}

	if allPassed {
		result.Status = "passed"
	} else {
		result.Status = "failed"
		result.Error = "one or more assertions failed"
	}
}

// executeToolStep handles type:tool step execution via the tool manager.
func (e *Engine) executeToolStep(ctx context.Context, step schema.Step, result *providers.StepResult) {
	result.Actor = "engine"

	if step.Tool == nil {
		result.Status = "failed"
		result.Error = "tool step has no tool configuration"
		return
	}

	if e.ToolManager == nil {
		result.Status = "failed"
		result.Error = "no tool manager configured — tools: not loaded"
		return
	}

	// Resolve template expressions in tool args
	resolvedArgs := make(map[string]string)
	for k, v := range step.Tool.Args {
		resolved, err := e.resolveTemplate(v)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("resolve tool arg %q: %v", k, err)
			return
		}
		resolvedArgs[k] = resolved
	}

	// Build vars map from engine state for argv template resolution
	vars := make(map[string]string)
	for k, v := range e.State.Vars {
		vars[k] = v
	}
	for k, v := range e.State.Captures {
		vars[k] = v
	}

	// Execute tool action
	actionResult, err := e.ToolManager.Execute(ctx, step.Tool.Name, step.Tool.Action, resolvedArgs, vars)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("tool execute: %v", err)
		return
	}

	// Handle approval requirement — tool governance says this action needs approval
	if actionResult.RequiresApproval {
		approvalMin := actionResult.ApprovalMin
		if approvalMin < 1 {
			approvalMin = 1
		}
		fmt.Fprintf(os.Stderr, "  ⚠ Tool action %s.%s requires approval (min=%d)\n",
			step.Tool.Name, step.Tool.Action, approvalMin)
		approvals, err := e.Collector.PromptApproval(nil, approvalMin)
		if err != nil || len(approvals) < approvalMin {
			result.Status = "failed"
			result.Error = fmt.Sprintf("tool action %s.%s requires %d approval(s) — denied or insufficient",
				step.Tool.Name, step.Tool.Action, approvalMin)
			return
		}
		// Approval granted — re-execute with approval bypass
		actionResult, err = e.ToolManager.ExecuteApproved(ctx, step.Tool.Name, step.Tool.Action, resolvedArgs, vars)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("tool execute (approved): %v", err)
			return
		}
	}

	// Map tool captures to step captures
	stdout := actionResult.Stdout
	for name, source := range step.Capture {
		switch source {
		case "stdout":
			result.Captures[name] = strings.TrimSpace(stdout)
		case "stderr":
			result.Captures[name] = strings.TrimSpace(actionResult.Stderr)
		default:
			// Check if source matches a tool-level capture name
			if val, ok := actionResult.Captures[source]; ok {
				result.Captures[name] = val
			}
		}
	}

	// Evaluate assertions against stdout
	allPassed := true
	for _, a := range step.Assertions {
		ar := assertions.Evaluate(a, stdout, actionResult.ExitCode)
		result.Assertions = append(result.Assertions, ar)
		if !ar.Passed {
			allPassed = false
		}
	}

	if actionResult.ExitCode != 0 {
		result.Status = "failed"
		result.Error = fmt.Sprintf("tool exited with code %d", actionResult.ExitCode)
		return
	}

	if allPassed {
		result.Status = "passed"
	} else {
		result.Status = "failed"
		result.Error = "one or more assertions failed"
	}
}

// executeManualStep handles manual step execution.
func (e *Engine) executeManualStep(ctx context.Context, step schema.Step, result *providers.StepResult) {
	result.Actor = "human"

	// Set current step ID on ScenarioCollector if present (replay mode)
	if sc, ok := e.Collector.(*providers.ScenarioCollector); ok {
		sc.CurrentStepID = step.ID
	}

	// Display instructions
	instructions, err := e.resolveTemplate(step.Instructions)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("resolve instructions: %v", err)
		return
	}
	fmt.Println(instructions)

	// Collect evidence
	result.Evidence = make(map[string]*providers.EvidenceValue)
	for _, req := range step.RequiredEvidence {
		switch req.Kind {
		case "text":
			text, err := e.Collector.PromptText(req.Name, "")
			if err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("collect evidence %q: %v", req.Name, err)
				return
			}
			result.Evidence[req.Name] = evidence.NewTextEvidence(text)

		case "checklist":
			items, err := e.Collector.PromptChecklist(req.Name, req.Items)
			if err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("collect checklist %q: %v", req.Name, err)
				return
			}
			result.Evidence[req.Name] = evidence.NewChecklistEvidence(items)

		case "attachment":
			info, err := e.Collector.PromptAttachment(req.Name, "")
			if err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("collect attachment %q: %v", req.Name, err)
				return
			}
			result.Evidence[req.Name] = &providers.EvidenceValue{
				Kind:   "attachment",
				Path:   info.Path,
				SHA256: info.SHA256,
				Size:   info.Size,
			}
		}
	}

	// Handle approvals
	if step.Approvals != nil && step.Approvals.Min > 0 {
		roles := step.Approvals.Roles
		if len(roles) == 0 {
			roles = []string{"any"}
		}
		_, err := e.Collector.PromptApproval(roles, step.Approvals.Min)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("approval: %v", err)
			return
		}
	}

	// Handle choices: resolve the selected value from the evidence collector
	// and store it as a capture so downstream conditions can reference it.
	if step.Choices != nil && step.Choices.Variable != "" {
		text, err := e.Collector.PromptText(step.Choices.Variable, step.Choices.Prompt)
		if err != nil {
			// In dry-run mode, default to the first option
			if e.State.Mode == "dry-run" && len(step.Choices.Options) > 0 {
				text = step.Choices.Options[0].Value
			} else {
				result.Status = "failed"
				result.Error = fmt.Sprintf("collect choice %q: %v", step.Choices.Variable, err)
				return
			}
		}
		// Validate the choice is one of the known options
		valid := false
		for _, opt := range step.Choices.Options {
			if opt.Value == text {
				valid = true
				break
			}
		}
		if !valid && len(step.Choices.Options) > 0 {
			// Accept it anyway but log a warning
			fmt.Fprintf(os.Stderr, "  warning: choice value %q not in options for %q\n", text, step.Choices.Variable)
		}
		// Store the choice as a variable and capture
		e.State.Vars[step.Choices.Variable] = text
		e.State.Captures[step.Choices.Variable] = text
		result.Captures[step.Choices.Variable] = text
	}

	result.Status = "passed"
}

// executeXTSStep handles XTS step execution via the XTS provider.
func (e *Engine) executeXTSStep(ctx context.Context, step schema.Step, result *providers.StepResult) {
	result.Actor = "engine"

	if step.XTS == nil {
		result.Status = "failed"
		result.Error = "XTS step has no xts configuration"
		return
	}

	// Replay mode: use pre-recorded step response from scenario
	if e.XTSScenario != nil {
		if respData, ok := e.XTSScenario.FindStepResponse(step.ID); ok {
			fmt.Printf("  [replay] Using scenario response for step %q\n", step.ID)
			// Parse the JSON response as XTSOutput
			var xtsOut providers.XTSOutput
			if err := json.Unmarshal(respData, &xtsOut); err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("replay: parse scenario response for %q: %v", step.ID, err)
				return
			}
			// Extract captures
			for name, expr := range step.Capture {
				val, err := providers.EvaluateXTSCapturePublic(expr, &xtsOut, string(respData))
				if err != nil {
					if xtsOut.RowCount == 0 {
						result.Captures[name] = ""
						continue
					}
					result.Status = "failed"
					result.Error = fmt.Sprintf("replay capture %q: %v", name, err)
					return
				}
				result.Captures[name] = val
			}
			result.Status = "passed"
			return
		}
		// No scenario data for this step — fall through to real execution
		fmt.Printf("  [replay] No scenario data for step %q, executing live\n", step.ID)
	}

	if e.xtsProvider == nil {
		result.Status = "failed"
		result.Error = "XTS provider not initialized (check meta.xts configuration and xts-cli availability)"
		return
	}

	// Pre-flight: check for empty input vars referenced in this step's templates
	if warnings := e.checkUnresolvedVars(step); len(warnings) > 0 {
		result.Status = "failed"
		result.Error = fmt.Sprintf("unresolved or empty inputs: %s", strings.Join(warnings, "; "))
		return
	}

	// Resolve template variables in XTS params before execution
	resolvedStep := step
	resolvedXTS := *step.XTS
	if len(resolvedXTS.Params) > 0 {
		resolvedParams := make(map[string]string, len(resolvedXTS.Params))
		for k, v := range resolvedXTS.Params {
			resolved, err := e.resolveTemplate(v)
			if err != nil {
				result.Status = "failed"
				result.Error = fmt.Sprintf("resolve param %q: %v", k, err)
				return
			}
			resolvedParams[k] = resolved
		}
		resolvedXTS.Params = resolvedParams
	}

	// Resolve template in query text
	if resolvedXTS.Query != "" {
		resolved, err := e.resolveTemplate(resolvedXTS.Query)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("resolve query: %v", err)
			return
		}
		resolvedXTS.Query = resolved
	}

	// Inject default environment from meta.xts if not set on step
	if resolvedXTS.Environment == "" && e.Runbook.Meta.XTS != nil {
		resolvedXTS.Environment = e.Runbook.Meta.XTS.Environment
	}
	// Resolve template in environment (may be {{ .environment }} from inputs)
	if strings.Contains(resolvedXTS.Environment, "{{") {
		resolved, err := e.resolveTemplate(resolvedXTS.Environment)
		if err != nil {
			result.Status = "failed"
			result.Error = fmt.Sprintf("resolve environment: %v", err)
			return
		}
		resolvedXTS.Environment = resolved
	}

	resolvedStep.XTS = &resolvedXTS

	// Dry-run: show the command that would execute, skip actual execution
	if e.State.Mode == "dry-run" {
		argv, err := e.xtsProvider.BuildArgvPublic(&resolvedXTS, resolvedXTS.Environment, e.State.Vars, e.State.Captures)
		if err != nil {
			fmt.Printf("  [dry-run] would execute xts-cli (failed to build argv: %v)\n", err)
		} else {
			fmt.Printf("  [dry-run] would execute: %s %v\n", e.xtsProvider.CLIPath, argv)
		}
		// Generate placeholder captures
		for name := range step.Capture {
			result.Captures[name] = "<dry-run>"
		}
		result.Status = "passed"
		return
	}

	// Build execution context
	execCtx := &providers.ExecutionContext{
		RunID:           e.State.RunID,
		Mode:            e.State.Mode,
		Vars:            e.State.Vars,
		Captures:        e.State.Captures,
		CommandExecutor: e.Executor,
		Governance:      e.Runbook.Meta.Governance,
	}

	xtsResult, err := e.xtsProvider.Execute(ctx, execCtx, resolvedStep)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("xts provider: %v", err)
		return
	}

	// Copy results from provider
	result.Status = xtsResult.Status
	result.Error = xtsResult.Error
	result.Captures = xtsResult.Captures

	// Auto-save XTS step response for scenario capture
	if len(xtsResult.RawResponse) > 0 && e.State.Mode == "real" {
		stepsDir := filepath.Join(e.BaseDir, "steps")
		os.MkdirAll(stepsDir, 0755)
		stepFile := filepath.Join(stepsDir, fmt.Sprintf("%03d-%s.json", result.StepIndex, strings.ReplaceAll(step.ID, "_", "-")))
		if err := os.WriteFile(stepFile, xtsResult.RawResponse, 0644); err != nil {
			fmt.Fprintf(os.Stderr, "  warning: failed to save step response: %v\n", err)
		}
	}
}

// resolveArgv resolves template expressions in argv elements.
func (e *Engine) resolveArgv(argv []string) ([]string, error) {
	resolved := make([]string, len(argv))
	for i, arg := range argv {
		r, err := e.resolveTemplate(arg)
		if err != nil {
			return nil, err
		}
		resolved[i] = r
	}
	return resolved, nil
}

// checkUnresolvedVars scans a step's templates for {{ .varName }} references and
// returns warnings for any that are empty or missing in the current state. This
// catches misconfigured inputs early with a clear message instead of a cryptic
// xts-cli failure.
func (e *Engine) checkUnresolvedVars(step schema.Step) []string {
	var warnings []string

	// Collect all template strings from this step
	var templates []string
	if step.XTS != nil {
		templates = append(templates, step.XTS.Query, step.XTS.Environment)
		for _, v := range step.XTS.Params {
			templates = append(templates, v)
		}
	}
	if step.Instructions != "" {
		templates = append(templates, step.Instructions)
	}
	// Also check meta-level environment if step has none
	if step.XTS != nil && step.XTS.Environment == "" && e.Runbook.Meta.XTS != nil {
		templates = append(templates, e.Runbook.Meta.XTS.Environment)
	}

	// Extract {{ .varName }} references
	seen := make(map[string]bool)
	for _, t := range templates {
		for _, match := range templateVarRe.FindAllStringSubmatch(t, -1) {
			varName := strings.TrimSpace(match[1])
			if seen[varName] {
				continue
			}
			seen[varName] = true
			// Check in vars, then captures
			val, inVars := e.State.Vars[varName]
			if !inVars {
				val, inVars = e.State.Captures[varName]
			}
			if !inVars {
				warnings = append(warnings, fmt.Sprintf("%q is not set (check inputs)", varName))
			} else if strings.TrimSpace(val) == "" {
				// Build a hint if we have an InputDef with an example
				hint := ""
				if e.Runbook.Meta.Inputs != nil {
					if inp, ok := e.Runbook.Meta.Inputs[varName]; ok && inp.Example != "" {
						hint = fmt.Sprintf(" (example: %s)", inp.Example)
					}
				}
				warnings = append(warnings, fmt.Sprintf("%q is empty%s", varName, hint))
			}
		}
	}
	return warnings
}

// buildEnv merges vars and captures into a single map for template/expr evaluation.
func (e *Engine) buildEnv() map[string]interface{} {
	data := make(map[string]interface{})
	for k, v := range e.State.Vars {
		data[k] = v
	}
	for k, v := range e.State.Captures {
		data[k] = parseCapture(v)
	}
	return data
}

// evalCondition evaluates a condition expression using expr-lang.
// Supports clean syntax: len(arr) > 1, status == "resolved", x != "", etc.
// For backwards compatibility, if the expression contains {{ }}, falls back to Go templates.
func (e *Engine) evalCondition(exprStr string) (bool, error) {
	exprStr = strings.TrimSpace(exprStr)
	if exprStr == "" {
		return true, nil // empty condition = always true
	}

	// Backwards compat: if it's a Go template, evaluate it the old way
	if strings.Contains(exprStr, "{{") {
		val, err := e.resolveTemplate(exprStr)
		if err != nil {
			return false, err
		}
		val = strings.TrimSpace(val)
		return val != "" && val != "false" && val != "0" && val != "<no value>", nil
	}

	// Use expr-lang
	env := e.buildEnv()
	program, err := expr.Compile(exprStr, expr.Env(env), expr.AsBool())
	if err != nil {
		return false, fmt.Errorf("compile condition %q: %w", exprStr, err)
	}
	output, err := expr.Run(program, env)
	if err != nil {
		return false, fmt.Errorf("eval condition %q: %w", exprStr, err)
	}
	result, ok := output.(bool)
	if !ok {
		return false, fmt.Errorf("condition %q did not return bool (got %T: %v)", exprStr, output, output)
	}
	return result, nil
}

// runbookFuncMap provides template functions available in runbook expressions.
// These supplement the built-in Go template functions (eq, ne, and, or, not, etc.).
var runbookFuncMap = template.FuncMap{
	// hasPrefix reports whether s begins with prefix.
	"hasPrefix": strings.HasPrefix,
	// hasSuffix reports whether s ends with suffix.
	"hasSuffix": strings.HasSuffix,
	// contains reports whether substr is within s.
	"contains": strings.Contains,
	// list creates a []string from its arguments.
	"list": func(args ...string) []string { return args },
	// has reports whether item is in the list.
	"has": func(item string, list []string) bool {
		for _, v := range list {
			if v == item {
				return true
			}
		}
		return false
	},
	// lower/upper for case-insensitive matching.
	"lower": strings.ToLower,
	"upper": strings.ToUpper,
	// split splits a string by separator, returning []string for use with index.
	"split": strings.Split,
	// join joins a string slice with separator.
	"join": strings.Join,
	// replace replaces all occurrences of old with new in s.
	"replace": strings.ReplaceAll,
	// trimPrefix/trimSuffix.
	"trimPrefix": strings.TrimPrefix,
	"trimSuffix": strings.TrimSuffix,
}

// resolveTemplate resolves Go template expressions against vars + captures.
func (e *Engine) resolveTemplate(tmplStr string) (string, error) {
	if !strings.Contains(tmplStr, "{{") {
		return tmplStr, nil
	}

	data := e.buildEnv()

	tmpl, err := template.New("resolve").Funcs(runbookFuncMap).Option("missingkey=error").Parse(tmplStr)
	if err != nil {
		return "", fmt.Errorf("parse template: %w", err)
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute template: %w", err)
	}
	return buf.String(), nil
}

// parseCapture attempts to parse a capture value as JSON array or object.
// If it's a JSON array, returns []interface{} so template functions like len work.
// Otherwise returns the original string.
func parseCapture(v string) interface{} {
	v = strings.TrimSpace(v)
	if len(v) > 1 && v[0] == '[' {
		var arr []interface{}
		if err := json.Unmarshal([]byte(v), &arr); err == nil {
			return arr
		}
	}
	if len(v) > 1 && v[0] == '{' {
		var obj map[string]interface{}
		if err := json.Unmarshal([]byte(v), &obj); err == nil {
			return obj
		}
	}
	return v
}

// getStepTimeout returns the timeout for a step, falling back to defaults.
func (e *Engine) getStepTimeout(step schema.Step) time.Duration {
	if step.Timeout != "" {
		d, err := parseDuration(step.Timeout)
		if err == nil {
			return d
		}
	}
	if e.Runbook.Meta.Defaults != nil && e.Runbook.Meta.Defaults.Timeout != "" {
		d, err := parseDuration(e.Runbook.Meta.Defaults.Timeout)
		if err == nil {
			return d
		}
	}
	return 0 // no timeout
}

// parseDuration parses duration strings like "30s", "5m", "1h".
func parseDuration(s string) (time.Duration, error) {
	return time.ParseDuration(s)
}

// ExecuteStep runs a single step by index and returns the result.
// This is the public entry point used by the debugger.
func (e *Engine) ExecuteStep(ctx context.Context, index int) (*providers.StepResult, error) {
	if index < 0 || index >= len(e.Runbook.Steps) {
		return nil, fmt.Errorf("step index %d out of range [0, %d)", index, len(e.Runbook.Steps))
	}
	step := e.Runbook.Steps[index]
	result, err := e.executeStep(ctx, index, step)
	if err != nil {
		return nil, err
	}

	// Write trace
	if err := e.Trace.Write(result); err != nil {
		return nil, fmt.Errorf("write trace: %w", err)
	}

	// Save snapshot
	e.State.History = append(e.State.History, result)
	for k, v := range result.Captures {
		e.State.Captures[k] = v
	}
	e.State.CurrentStepIndex = index + 1
	snapshotPath := filepath.Join(e.BaseDir, "snapshots", fmt.Sprintf("step-%04d.json", index))
	if err := SaveSnapshot(e.State, snapshotPath); err != nil {
		return nil, fmt.Errorf("save snapshot: %w", err)
	}

	return result, nil
}

// GetRunID returns the current run ID.
func (e *Engine) GetRunID() string {
	return e.State.RunID
}

// GetBaseDir returns the run artifacts directory.
func (e *Engine) GetBaseDir() string {
	return e.BaseDir
}

// ResolveTemplatePublic exposes template resolution for the serve package.
// Returns the resolved string, or "<no value>" on error (e.g. missing variable).
func (e *Engine) ResolveTemplatePublic(tmpl string) string {
	result, err := e.resolveTemplate(tmpl)
	if err != nil {
		return "<no value>"
	}
	return result
}

// EvalConditionPublic exposes condition evaluation for the serve package.
func (e *Engine) EvalConditionPublic(condition string) bool {
	result, err := e.evalCondition(condition)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  [condition error] %s: %v\n", condition, err)
		return false
	}
	return result
}

// BuildManifest produces a RunManifest from the current engine state.
func (e *Engine) BuildManifest() *RunManifest {
	return &RunManifest{
		RunID:          e.State.RunID,
		ICMID:          e.ICMID,
		Runbook:        e.RunbookPath,
		Actor:          e.State.Actor,
		Mode:           e.State.Mode,
		StartedAt:      e.State.StartedAt.UTC().Format(time.RFC3339),
		EndedAt:        time.Now().UTC().Format(time.RFC3339),
		Outcome:        e.outcome,
		InputsResolved: e.State.Vars,
		StepsSummary:   e.stepCounts,
		ParentRunID:    e.ParentRunID,
		ChildRuns:      e.ChildRuns,
	}
}

// WriteManifest writes run.yaml to the run artifacts directory.
func (e *Engine) WriteManifest() error {
	m := e.BuildManifest()
	data, err := yaml.Marshal(m)
	if err != nil {
		return fmt.Errorf("marshal manifest: %w", err)
	}
	path := filepath.Join(e.BaseDir, "run.yaml")
	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write manifest: %w", err)
	}
	return nil
}

// ExecuteTreeStep executes a single tree step by index and step definition.
// Unlike ExecuteStep, this doesn't look up the step from Runbook.Steps — it takes
// the step directly, supporting tree runbooks where steps aren't in a flat list.
func (e *Engine) ExecuteTreeStep(ctx context.Context, index int, step schema.Step) (*providers.StepResult, error) {
	result, err := e.executeStep(ctx, index, step)
	if err != nil {
		return nil, err
	}

	// Write trace
	if err := e.Trace.Write(result); err != nil {
		return nil, fmt.Errorf("write trace: %w", err)
	}

	// Save to history and merge captures
	e.State.History = append(e.State.History, result)
	for k, v := range result.Captures {
		e.State.Captures[k] = v
	}

	// Save snapshot
	snapshotPath := filepath.Join(e.BaseDir, "snapshots", fmt.Sprintf("step-%04d.json", index))
	if err := SaveSnapshot(e.State, snapshotPath); err != nil {
		return nil, fmt.Errorf("save snapshot: %w", err)
	}

	// Update step counts
	if result.Status == "failed" {
		e.stepCounts.Failed++
	} else {
		e.stepCounts.Passed++
	}
	e.stepCounts.Total++

	return result, nil
}

// SaveScenario writes the current run's inputs and XTS step responses to a
// replay scenario folder. The folder will contain inputs.yaml and steps/*.json,
// matching the format expected by LoadXTSScenario.
func (e *Engine) SaveScenario(outputDir string) error {
	// Write inputs.yaml from resolved vars
	if len(e.State.Vars) > 0 {
		data, err := yaml.Marshal(e.State.Vars)
		if err != nil {
			return fmt.Errorf("marshal inputs: %w", err)
		}
		if err := os.WriteFile(filepath.Join(outputDir, "inputs.yaml"), data, 0644); err != nil {
			return fmt.Errorf("write inputs.yaml: %w", err)
		}
	}

	// Copy step response JSON files from the run's auto-save directory
	srcStepsDir := filepath.Join(e.BaseDir, "steps")
	if entries, err := os.ReadDir(srcStepsDir); err == nil && len(entries) > 0 {
		dstStepsDir := filepath.Join(outputDir, "steps")
		if err := os.MkdirAll(dstStepsDir, 0755); err != nil {
			return fmt.Errorf("create steps dir: %w", err)
		}
		for _, entry := range entries {
			if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".json") {
				continue
			}
			data, err := os.ReadFile(filepath.Join(srcStepsDir, entry.Name()))
			if err != nil {
				return fmt.Errorf("read step file %s: %w", entry.Name(), err)
			}
			if err := os.WriteFile(filepath.Join(dstStepsDir, entry.Name()), data, 0644); err != nil {
				return fmt.Errorf("write step file %s: %w", entry.Name(), err)
			}
		}
	}

	return nil
}

// SetOutcome sets the engine outcome record (used by serve for step-by-step tree execution).
// SetVar sets a variable in the engine state (used for choice captures).
func (e *Engine) SetVar(name, value string) {
	e.State.Vars[name] = value
	e.State.Captures[name] = value
}

func (e *Engine) SetOutcome(state string, stepID string, recommendation string) {
	e.outcome = &OutcomeRecord{
		State:          state,
		StepID:         stepID,
		Recommendation: recommendation,
	}
}

// GetOutcome returns the engine's outcome record (nil if no outcome reached).
func (e *Engine) GetOutcome() *OutcomeRecord {
	return e.outcome
}

// GetXTSCLIPath returns the resolved XTS CLI binary path, or empty string
// if no XTS provider is configured.
func (e *Engine) GetXTSCLIPath() string {
	if e.xtsProvider != nil {
		return e.xtsProvider.CLIPath
	}
	return ""
}
