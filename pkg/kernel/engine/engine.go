// Package engine implements the kernel/v0 sequential execution engine.
package engine

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
	"github.com/ormasoftchile/gert/pkg/kernel/eval"
	"github.com/ormasoftchile/gert/pkg/kernel/executor"
	"github.com/ormasoftchile/gert/pkg/kernel/governance"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
	"github.com/ormasoftchile/gert/pkg/kernel/validate"
)

// ToolExecutor executes tool actions. The default implementation spawns processes;
// replay mode substitutes canned responses.
type ToolExecutor interface {
	Execute(toolDef *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*executor.Result, error)
}

// defaultExecutor delegates to executor.RunTool.
type defaultExecutor struct{}

func (d *defaultExecutor) Execute(td *schema.ToolDefinition, action string, inputs map[string]any, vars map[string]any) (*executor.Result, error) {
	return executor.RunTool(td, action, inputs, vars)
}

// RunConfig configures a runbook execution.
type RunConfig struct {
	RunID       string
	Mode        string // "real", "dry-run", "replay"
	Vars        map[string]string
	BaseDir     string
	ProjectRoot string
	Trace       *trace.Writer
	Stdin       io.Reader      // for manual step input; defaults to os.Stdin
	Stdout      io.Writer      // for output; defaults to os.Stdout
	ToolExec    ToolExecutor   // custom tool executor (e.g., replay); nil uses default
}

// RunResult is the outcome of executing a runbook.
type RunResult struct {
	Outcome  *schema.Outcome
	Status   string // "completed", "failed", "error"
	Duration time.Duration
	Error    error
}

// Engine executes kernel/v0 runbooks.
type Engine struct {
	cfg          RunConfig
	rb           *schema.Runbook
	vars         map[string]any
	trace        *trace.Writer
	tools        map[string]*schema.ToolDefinition
	startTime    time.Time
	toolExec     ToolExecutor
	VisitedSteps []string // ordered list of step IDs executed (for test harness)
}

// New creates an engine for the given runbook.
func New(rb *schema.Runbook, cfg RunConfig) *Engine {
	vars := make(map[string]any)

	// Seed with constants
	for k, v := range rb.Meta.Constants {
		vars[k] = v
	}

	// Seed with input defaults, then CLI overrides
	for name, paramDef := range rb.Meta.Inputs {
		if paramDef.Default != nil {
			vars[name] = paramDef.Default
		}
	}
	for k, v := range cfg.Vars {
		vars[k] = v
	}

	if cfg.Stdin == nil {
		cfg.Stdin = os.Stdin
	}
	if cfg.Stdout == nil {
		cfg.Stdout = os.Stdout
	}

	te := cfg.ToolExec
	if te == nil {
		te = &defaultExecutor{}
	}

	return &Engine{
		cfg:      cfg,
		rb:       rb,
		vars:     vars,
		trace:    cfg.Trace,
		toolExec: te,
		tools: make(map[string]*schema.ToolDefinition),
	}
}

// Run executes the runbook sequentially.
func (e *Engine) Run() *RunResult {
	e.startTime = time.Now()

	// Emit run_start
	if e.trace != nil {
		inputsAny := make(map[string]any, len(e.cfg.Vars))
		for k, v := range e.cfg.Vars {
			inputsAny[k] = v
		}
		constantsAny := make(map[string]any, len(e.rb.Meta.Constants))
		for k, v := range e.rb.Meta.Constants {
			constantsAny[k] = v
		}
		e.trace.EmitRunStart(e.rb.Meta.Name, inputsAny, constantsAny)
	}

	// Pre-load tool definitions
	e.loadTools()

	// Execute steps
	result := e.executeSteps(e.rb.Steps, true)

	duration := time.Since(e.startTime)
	result.Duration = duration

	// Emit run_complete
	if e.trace != nil {
		var outcomeMap map[string]any
		if result.Outcome != nil {
			outcomeMap = map[string]any{
				"category": string(result.Outcome.Category),
				"code":     result.Outcome.Code,
			}
			if result.Outcome.Meta != nil {
				outcomeMap["meta"] = result.Outcome.Meta
			}
		}
		e.trace.EmitRunComplete(outcomeMap, result.Status, duration)
	}

	return result
}

// executeSteps runs a list of steps sequentially.
// If requireEnd is true, returns an error if execution completes without an end step.
// For top-level execution requireEnd=true; for parallel/branch sub-blocks requireEnd=false.
func (e *Engine) executeSteps(steps []schema.Step, requireEnd bool) *RunResult {
	// retryCounts tracks how many times a backward next has jumped to each target
	retryCounts := make(map[string]int)

	for i := 0; i < len(steps); i++ {
		step := steps[i]
		stepID := step.ID
		if stepID == "" {
			stepID = fmt.Sprintf("_step_%d", i)
		}

		// Evaluate `when` guard
		if step.When != "" {
			shouldRun, err := eval.EvalBool(step.When, e.vars)
			if err != nil {
				return &RunResult{
					Status: "error",
					Error:  fmt.Errorf("step %s: when guard: %w", stepID, err),
				}
			}
			if !shouldRun {
				// Emit skipped
				if e.trace != nil {
					e.trace.EmitStepStart(stepID, string(step.Type), nil)
					e.trace.EmitStepComplete(stepID, trace.StatusSkipped, nil, 0, nil)
				}
				continue
			}
		}

		// Handle for_each expansion
		if step.ForEach != nil {
			result := e.executeForEach(step, stepID)
			if result != nil {
				return result
			}
			continue // for_each handled the step; proceed to next
		}

		// Execute the step
		result := e.executeStep(step, stepID)
		if result != nil {
			return result
		}

		// Handle `next` — jump to a different step
		if step.Next != nil {
			target, max, _, err := schema.ParseNext(step.Next)
			if err != nil {
				return &RunResult{Status: "error", Error: fmt.Errorf("step %s: %w", stepID, err)}
			}
			if target != "" {
				// Find target index in this scope
				targetIdx := -1
				for j, s := range steps {
					if s.ID == target {
						targetIdx = j
						break
					}
				}
				if targetIdx < 0 {
					return &RunResult{
						Status: "error",
						Error:  fmt.Errorf("step %s: next target %q not found", stepID, target),
					}
				}

				isBackward := targetIdx <= i
				if isBackward {
					// Enforce max bound
					retryCounts[target]++
					count := retryCounts[target]
					e.vars[target+".retry_count"] = count

					if max > 0 && count > max {
						// Max exceeded — stop jumping, continue to next step
						continue
					}
				}

				i = targetIdx - 1 // -1 because loop will i++
			}
		}
	}

	// Steps exhausted
	if requireEnd {
		return &RunResult{Status: "error", Error: fmt.Errorf("execution completed without reaching an end step")}
	}
	return nil
}

// executeStep dispatches a single step by type.
// Returns nil to continue to next step, or a RunResult to terminate.
func (e *Engine) executeStep(step schema.Step, stepID string) *RunResult {
	start := time.Now()

	// Track visited steps for test harness
	e.VisitedSteps = append(e.VisitedSteps, stepID)

	// Resolve contract and evaluate governance for executable steps
	resolvedContract := e.resolveContract(step)
	if resolvedContract != nil {
		// Emit contract_evaluated
		if e.trace != nil {
			e.trace.EmitContractEvaluated(stepID, contractToMap(resolvedContract))
		}

		// Evaluate governance
		decision := governance.Evaluate(resolvedContract, e.rb.Meta.Governance)
		if e.trace != nil {
			e.trace.EmitGovernanceDecision(stepID, string(decision.RiskLevel), string(decision.Action), decision.MinApprovers)
		}

		switch decision.Action {
		case schema.DecisionDeny:
			if e.trace != nil {
				e.trace.EmitStepStart(stepID, string(step.Type), nil)
				e.trace.EmitStepComplete(stepID, trace.StatusSkipped, nil, time.Since(start), &trace.Failure{
					Kind: "denied", Message: "governance denied execution",
				})
			}
			return &RunResult{
				Status: "failed",
				Error:  fmt.Errorf("step %s: governance denied", stepID),
			}

		case schema.DecisionRequireApproval:
			approved := e.requestApproval(stepID, decision)
			if !approved {
				if e.trace != nil {
					e.trace.EmitStepStart(stepID, string(step.Type), nil)
					e.trace.EmitStepComplete(stepID, trace.StatusSkipped, nil, time.Since(start), &trace.Failure{
						Kind: "denied", Message: "approval rejected",
					})
				}
				return &RunResult{
					Status: "failed",
					Error:  fmt.Errorf("step %s: approval rejected", stepID),
				}
			}
		}
	}

	switch step.Type {
	case schema.StepTool:
		return e.executeTool(step, stepID, start)
	case schema.StepManual:
		return e.executeManual(step, stepID, start)
	case schema.StepAssert:
		return e.executeAssert(step, stepID, start)
	case schema.StepBranch:
		return e.executeBranch(step, stepID)
	case schema.StepParallel:
		return e.executeParallel(step, stepID)
	case schema.StepEnd:
		return e.executeEnd(step, stepID, start)
	case schema.StepExtension:
		return e.executeExtension(step, stepID, start)
	default:
		return &RunResult{Status: "error", Error: fmt.Errorf("step %s: unsupported type %q", stepID, step.Type)}
	}
}

// ---------------------------------------------------------------------------
// Step type executors
// ---------------------------------------------------------------------------

func (e *Engine) executeTool(step schema.Step, stepID string, start time.Time) *RunResult {
	if e.trace != nil {
		e.trace.EmitStepStart(stepID, "tool", nil)
	}

	// Resolve inputs
	resolvedInputs, err := e.resolveInputs(step)
	if err != nil {
		e.emitStepError(stepID, start, "template", err.Error())
		return &RunResult{Status: "error", Error: fmt.Errorf("step %s: %w", stepID, err)}
	}

	// Dry-run mode: report what would execute, skip actual execution
	if e.cfg.Mode == "dry-run" {
		fmt.Fprintf(e.cfg.Stdout, "  [dry-run] tool %s:%s\n", step.Tool, step.Action)
		fmt.Fprintf(e.cfg.Stdout, "    inputs: %v\n", resolvedInputs)
		td := e.tools[step.Tool]
		if td != nil && len(td.Meta.Platform) > 0 {
			fmt.Fprintf(e.cfg.Stdout, "    platform: %v\n", td.Meta.Platform)
		}
		if td != nil && td.Meta.Binary != "" {
			fmt.Fprintf(e.cfg.Stdout, "    binary: %s\n", td.Meta.Binary)
		}
		c := e.resolveContract(step)
		if c != nil {
			r := c.Resolved()
			fmt.Fprintf(e.cfg.Stdout, "    contract: side_effects=%v deterministic=%v idempotent=%v risk=%s\n",
				*r.SideEffects, *r.Deterministic, *r.Idempotent, r.Risk())
			if len(r.Reads) > 0 {
				fmt.Fprintf(e.cfg.Stdout, "    reads: %v\n", r.Reads)
			}
			if len(r.Writes) > 0 {
				fmt.Fprintf(e.cfg.Stdout, "    writes: %v\n", r.Writes)
			}
		}
		if e.trace != nil {
			e.trace.EmitStepComplete(stepID, trace.StatusSkipped, resolvedInputs, time.Since(start), nil)
		}
		return nil
	}

	// Load tool definition
	td := e.tools[step.Tool]
	if td == nil {
		e.emitStepError(stepID, start, "tool_not_found", fmt.Sprintf("tool %q not loaded", step.Tool))
		return &RunResult{Status: "error", Error: fmt.Errorf("step %s: tool %q not found", stepID, step.Tool)}
	}

	// Execute via tool executor (default or replay)
	result, err := e.toolExec.Execute(td, step.Action, resolvedInputs, e.vars)
	if err != nil {
		e.emitStepError(stepID, start, "exec", err.Error())
		return &RunResult{Status: "error", Error: fmt.Errorf("step %s: %w", stepID, err)}
	}

	// Store outputs
	outputs := make(map[string]any)
	for k, v := range result.Outputs {
		outputs[k] = v
		e.vars[k] = v
	}
	// Store under step ID namespace
	if stepID != "" {
		e.vars[stepID] = result.Outputs
	}

	duration := time.Since(start)

	if result.ExitCode != 0 {
		if e.trace != nil {
			e.trace.EmitStepComplete(stepID, trace.StatusFailed, outputs, duration, &trace.Failure{
				Kind: "exit_code", Message: fmt.Sprintf("exit code %d", result.ExitCode),
			})
		}
		if step.ContinueOnFail {
			return nil
		}
		return &RunResult{Status: "failed", Error: fmt.Errorf("step %s: tool exited with code %d", stepID, result.ExitCode)}
	}

	if e.trace != nil {
		e.trace.EmitStepComplete(stepID, trace.StatusSuccess, outputs, duration, nil)
	}
	return nil
}

func (e *Engine) executeManual(step schema.Step, stepID string, start time.Time) *RunResult {
	if e.trace != nil {
		e.trace.EmitStepStart(stepID, "manual", nil)
	}

	// Resolve instructions template
	instructions, err := eval.Resolve(step.Instructions, e.vars)
	if err != nil {
		e.emitStepError(stepID, start, "template", err.Error())
		return &RunResult{Status: "error", Error: fmt.Errorf("step %s: %w", stepID, err)}
	}

	fmt.Fprintf(e.cfg.Stdout, "\n  [manual] %s\n", instructions)

	if e.cfg.Mode == "dry-run" {
		fmt.Fprintf(e.cfg.Stdout, "  (dry-run: skipping manual input)\n")
		if e.trace != nil {
			e.trace.EmitStepComplete(stepID, trace.StatusSuccess, nil, time.Since(start), nil)
		}
		return nil
	}

	// Collect evidence
	outputs := make(map[string]any)
	for _, ev := range step.RequiredEvidence {
		fmt.Fprintf(e.cfg.Stdout, "  [evidence] %s (%s): ", ev.Name, ev.Kind)
		scanner := bufio.NewScanner(e.cfg.Stdin)
		if scanner.Scan() {
			outputs[ev.Name] = scanner.Text()
		}
	}

	// If no evidence required, ask for confirmation
	if len(step.RequiredEvidence) == 0 {
		fmt.Fprintf(e.cfg.Stdout, "  Press Enter to continue...")
		scanner := bufio.NewScanner(e.cfg.Stdin)
		scanner.Scan()
	}

	if stepID != "" {
		e.vars[stepID] = outputs
	}

	if e.trace != nil {
		e.trace.EmitStepComplete(stepID, trace.StatusSuccess, outputs, time.Since(start), nil)
	}
	return nil
}

func (e *Engine) executeAssert(step schema.Step, stepID string, start time.Time) *RunResult {
	if e.trace != nil {
		e.trace.EmitStepStart(stepID, "assert", nil)
	}

	allPassed := true
	var failMessages []string

	for _, assertion := range step.Assert {
		passed, msg := e.evaluateAssertion(assertion)
		if !passed {
			allPassed = false
			failMessages = append(failMessages, msg)
		}
	}

	duration := time.Since(start)
	outputs := map[string]any{"passed": allPassed}
	if stepID != "" {
		e.vars[stepID] = outputs
	}

	if !allPassed {
		failure := &trace.Failure{
			Kind:    "assertion",
			Message: strings.Join(failMessages, "; "),
		}
		if e.trace != nil {
			e.trace.EmitStepComplete(stepID, trace.StatusFailed, outputs, duration, failure)
		}
		if step.ContinueOnFail {
			return nil
		}
		return &RunResult{
			Status: "failed",
			Error:  fmt.Errorf("step %s: assertion failed: %s", stepID, failure.Message),
		}
	}

	if e.trace != nil {
		e.trace.EmitStepComplete(stepID, trace.StatusSuccess, outputs, duration, nil)
	}
	return nil
}

func (e *Engine) executeBranch(step schema.Step, stepID string) *RunResult {
	for _, br := range step.Branches {
		matches, err := eval.EvalBool(br.Condition, e.vars)
		if err != nil {
			return &RunResult{Status: "error", Error: fmt.Errorf("step %s: branch condition: %w", stepID, err)}
		}
		if matches {
			if e.trace != nil {
				e.trace.EmitBranchEnter(br.Label, br.Condition)
			}
			result := e.executeSteps(br.Steps, false)
			if e.trace != nil {
				e.trace.EmitBranchExit(br.Label)
			}
			return result
		}
	}
	return &RunResult{Status: "error", Error: fmt.Errorf("step %s: no branch condition matched", stepID)}
}

// ---------------------------------------------------------------------------
// Parallel execution (Phase 4)
// ---------------------------------------------------------------------------

// executeParallel runs parallel branches concurrently with state isolation.
// Contract conflicts cause serialization (sequential fallback).
func (e *Engine) executeParallel(step schema.Step, stepID string) *RunResult {
	if len(step.Branches) < 2 {
		return &RunResult{Status: "error", Error: fmt.Errorf("step %s: parallel requires at least 2 branches", stepID)}
	}

	// Compute per-branch aggregate contracts for conflict detection
	type branchInfo struct {
		index    int
		label    string
		contract contract.Contract
	}

	var branchInfos []branchInfo
	for i, br := range step.Branches {
		var allReads, allWrites []string
		walkBranchContracts(br.Steps, e, &allReads, &allWrites)
		branchInfos = append(branchInfos, branchInfo{
			index: i,
			label: br.Label,
			contract: contract.Contract{
				Reads:  allReads,
				Writes: allWrites,
			},
		})
	}

	// Build conflict graph — which pairs conflict?
	conflictsWith := make(map[int]map[int]bool)
	for i := 0; i < len(branchInfos); i++ {
		for j := i + 1; j < len(branchInfos); j++ {
			if contract.HasConflict(&branchInfos[i].contract, &branchInfos[j].contract) {
				if conflictsWith[i] == nil {
					conflictsWith[i] = make(map[int]bool)
				}
				if conflictsWith[j] == nil {
					conflictsWith[j] = make(map[int]bool)
				}
				conflictsWith[i][j] = true
				conflictsWith[j][i] = true
			}
		}
	}

	// If any conflicts exist, serialize all branches (simple strategy per §8)
	hasConflicts := len(conflictsWith) > 0

	// Emit parallel_fork
	if e.trace != nil {
		labels := make([]any, len(step.Branches))
		for i, br := range step.Branches {
			labels[i] = br.Label
		}
		e.trace.Emit(trace.EventParallelFork, map[string]any{
			"step_id":      stepID,
			"branch_count": len(step.Branches),
			"branches":     labels,
			"serialized":   hasConflicts,
		})
	}

	if hasConflicts {
		// Serialized execution — run branches sequentially
		return e.executeParallelSerialized(step, stepID)
	}

	// Concurrent execution — fork state per branch, run in goroutines
	results := make([]branchResult, len(step.Branches))
	var wg sync.WaitGroup

	for i, br := range step.Branches {
		wg.Add(1)
		go func(idx int, branch schema.Branch) {
			defer wg.Done()

			// Fork state — each branch gets a snapshot
			forkedVars := e.forkVars()
			branchEngine := e.forkEngine(forkedVars)

			res := branchEngine.executeSteps(branch.Steps, false)
			results[idx] = branchResult{
				index:   idx,
				label:   branch.Label,
				result:  res,
				outputs: branchEngine.collectNewVars(e.vars),
			}
		}(i, br)
	}

	wg.Wait()

	return e.mergeParallelResults(stepID, results)
}

// executeParallelSerialized runs parallel branches sequentially due to conflicts.
func (e *Engine) executeParallelSerialized(step schema.Step, stepID string) *RunResult {
	results := make([]branchResult, len(step.Branches))
	for i, br := range step.Branches {
		forkedVars := e.forkVars()
		branchEngine := e.forkEngine(forkedVars)

		res := branchEngine.executeSteps(br.Steps, false)
		results[i] = branchResult{
			index:   i,
			label:   br.Label,
			result:  res,
			outputs: branchEngine.collectNewVars(e.vars),
		}
	}

	return e.mergeParallelResults(stepID, results)
}

type branchResult struct {
	index   int
	label   string
	result  *RunResult
	outputs map[string]any
}

// mergeParallelResults merges branch results in declaration order.
func (e *Engine) mergeParallelResults(stepID string, results []branchResult) *RunResult {
	var anyFailed bool
	var firstFailure *RunResult
	branchOutcomes := make([]map[string]any, len(results))

	for i, br := range results {
		status := "success"
		if br.result != nil {
			status = br.result.Status
		}

		branchOutcomes[i] = map[string]any{
			"label":  br.label,
			"status": status,
		}

		if br.result != nil && (br.result.Status == "failed" || br.result.Status == "error") {
			anyFailed = true
			if firstFailure == nil {
				firstFailure = br.result
			}
		} else {
			// Merge outputs from successful branches into parent state
			for k, v := range br.outputs {
				e.vars[k] = v
			}
		}
	}

	// Emit parallel_merge
	if e.trace != nil {
		e.trace.Emit(trace.EventParallelMerge, map[string]any{
			"step_id":         stepID,
			"branch_outcomes": branchOutcomes,
		})
	}

	// If any branch ended with an outcome (completed), that's the overall result
	for _, br := range results {
		if br.result != nil && br.result.Outcome != nil {
			// Merge all successful outputs first
			return br.result
		}
	}

	if anyFailed {
		return firstFailure
	}

	// All branches completed without an end step — continue after parallel block
	return nil
}

// forkVars creates a deep copy of the variable scope for state isolation.
func (e *Engine) forkVars() map[string]any {
	forked := make(map[string]any, len(e.vars))
	for k, v := range e.vars {
		forked[k] = v
	}
	return forked
}

// forkEngine creates a child engine with forked state.
func (e *Engine) forkEngine(vars map[string]any) *Engine {
	return &Engine{
		cfg:       e.cfg,
		rb:        e.rb,
		vars:      vars,
		trace:     e.trace,
		tools:     e.tools,
		toolExec:  e.toolExec,
		startTime: e.startTime,
	}
}

// collectNewVars returns variables that were added or changed relative to parentVars.
func (e *Engine) collectNewVars(parentVars map[string]any) map[string]any {
	new_ := make(map[string]any)
	for k, v := range e.vars {
		if pv, ok := parentVars[k]; !ok || pv != v {
			new_[k] = v
		}
	}
	return new_
}

// walkBranchContracts aggregates reads/writes from all steps in a branch.
func walkBranchContracts(steps []schema.Step, e *Engine, reads, writes *[]string) {
	for _, s := range steps {
		c := e.resolveContract(s)
		if c != nil {
			resolved := c.Resolved()
			*reads = append(*reads, resolved.Reads...)
			*writes = append(*writes, resolved.Writes...)
		}
		// Also check inline contract on the step (for extension, or step-level overrides)
		if s.Contract != nil {
			*reads = append(*reads, s.Contract.Reads...)
			*writes = append(*writes, s.Contract.Writes...)
		}
		for _, br := range s.Branches {
			walkBranchContracts(br.Steps, e, reads, writes)
		}
	}
}

// ---------------------------------------------------------------------------
// for_each expansion (Phase 4)
// ---------------------------------------------------------------------------

// executeForEach expands a step with a for_each modifier.
func (e *Engine) executeForEach(step schema.Step, stepID string) *RunResult {
	fe := step.ForEach

	// Resolve the `over` expression to get the list.
	// First, try to extract a variable name from the template pattern {{ .varname }}
	var items []any
	varName := extractVarName(fe.Over)
	if varName != "" {
		if raw, ok := e.vars[varName]; ok {
			if list, ok := raw.([]any); ok {
				items = list
			}
		}
	}

	// Fallback: resolve as template and try to interpret the result
	if items == nil {
		overVal, err := eval.Resolve(fe.Over, e.vars)
		if err != nil {
			return &RunResult{Status: "error", Error: fmt.Errorf("step %s: for_each over: %w", stepID, err)}
		}
		// Split comma-separated string as fallback
		if overVal != "" {
			parts := strings.Split(overVal, ",")
			for _, p := range parts {
				p = strings.TrimSpace(p)
				if p != "" {
					items = append(items, p)
				}
			}
		}
	}

	if len(items) == 0 {
		return nil // empty list — skip
	}

	// Emit for_each_start
	if e.trace != nil {
		e.trace.Emit(trace.EventForEachStart, map[string]any{
			"step_id":    stepID,
			"over":       fe.Over,
			"item_count": len(items),
			"parallel":   fe.Parallel,
		})
	}

	// Make a copy of the step without for_each to avoid infinite recursion
	innerStep := step
	innerStep.ForEach = nil

	if fe.Parallel {
		return e.executeForEachParallel(innerStep, stepID, fe.As, items)
	}
	return e.executeForEachSequential(innerStep, stepID, fe.As, items)
}

// executeForEachSequential runs the step once per item, sequentially.
func (e *Engine) executeForEachSequential(step schema.Step, stepID, asVar string, items []any) *RunResult {
	accumulated := make([]any, 0, len(items))

	for i, item := range items {
		// Emit for_each_item
		if e.trace != nil {
			e.trace.Emit(trace.EventForEachItem, map[string]any{
				"step_id": stepID,
				"index":   i,
				"value":   item,
			})
		}

		// Set the iteration variable
		e.vars[asVar] = item

		iterID := fmt.Sprintf("%s[%d]", stepID, i)
		result := e.executeStep(step, iterID)

		// Collect outputs for accumulation
		if iterID != "" {
			if outputs, ok := e.vars[iterID]; ok {
				accumulated = append(accumulated, outputs)
			}
		}

		if result != nil {
			// Store partial accumulation
			e.vars[stepID] = accumulated
			return result
		}
	}

	// Store accumulated results under step ID
	e.vars[stepID] = accumulated

	// Remove the iteration variable (no longer in scope)
	delete(e.vars, asVar)

	return nil
}

// executeForEachParallel runs the step once per item, concurrently.
func (e *Engine) executeForEachParallel(step schema.Step, stepID, asVar string, items []any) *RunResult {
	type iterResult struct {
		index   int
		result  *RunResult
		outputs any
	}

	results := make([]iterResult, len(items))
	var wg sync.WaitGroup

	for i, item := range items {
		wg.Add(1)
		go func(idx int, itemVal any) {
			defer wg.Done()

			// Fork state for isolation
			forkedVars := e.forkVars()
			forkedVars[asVar] = itemVal

			iterEngine := e.forkEngine(forkedVars)
			iterID := fmt.Sprintf("%s[%d]", stepID, idx)

			// Emit for_each_item
			if e.trace != nil {
				e.trace.Emit(trace.EventForEachItem, map[string]any{
					"step_id": stepID,
					"index":   idx,
					"value":   itemVal,
				})
			}

			res := iterEngine.executeStep(step, iterID)
			var outputs any
			if val, ok := iterEngine.vars[iterID]; ok {
				outputs = val
			}

			results[idx] = iterResult{index: idx, result: res, outputs: outputs}
		}(i, item)
	}

	wg.Wait()

	// Merge results in declaration order (deterministic)
	accumulated := make([]any, 0, len(items))
	for _, ir := range results {
		if ir.outputs != nil {
			accumulated = append(accumulated, ir.outputs)
		}
		if ir.result != nil && (ir.result.Status == "failed" || ir.result.Status == "error") {
			// Store partial accumulation
			e.vars[stepID] = accumulated
			return ir.result
		}
		// If an iteration returned an outcome (end step), propagate it
		if ir.result != nil && ir.result.Outcome != nil {
			e.vars[stepID] = accumulated
			return ir.result
		}
	}

	// Store accumulated results
	e.vars[stepID] = accumulated
	return nil
}

func (e *Engine) executeEnd(step schema.Step, stepID string, start time.Time) *RunResult {
	if e.trace != nil {
		e.trace.EmitStepStart(stepID, "end", nil)
	}

	// Resolve outcome meta templates
	outcome := step.Outcome
	if outcome != nil && outcome.Meta != nil {
		resolvedMeta, err := eval.ResolveMap(outcome.Meta, e.vars)
		if err != nil {
			e.emitStepError(stepID, start, "template", err.Error())
			return &RunResult{Status: "error", Error: fmt.Errorf("step %s: %w", stepID, err)}
		}
		// Make a copy to avoid mutating the schema
		resolved := *outcome
		resolved.Meta = resolvedMeta
		outcome = &resolved
	}

	if e.trace != nil {
		e.trace.EmitStepComplete(stepID, trace.StatusSuccess, nil, time.Since(start), nil)
		if outcome != nil {
			e.trace.EmitOutcomeResolved(string(outcome.Category), outcome.Code, outcome.Meta)
		}
	}

	return &RunResult{
		Outcome: outcome,
		Status:  "completed",
	}
}

func (e *Engine) executeExtension(step schema.Step, stepID string, start time.Time) *RunResult {
	// Extensions are dispatched to external executors — out of scope for Phase 3.
	// For now, emit a trace event and skip.
	if e.trace != nil {
		e.trace.EmitStepStart(stepID, "extension", nil)
		e.trace.EmitStepComplete(stepID, trace.StatusSkipped, nil, time.Since(start), &trace.Failure{
			Kind: "not_implemented", Message: "extension execution not yet implemented",
		})
	}
	if step.ContinueOnFail {
		return nil
	}
	return &RunResult{Status: "error", Error: fmt.Errorf("step %s: extension execution not yet implemented", stepID)}
}

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

func (e *Engine) resolveContract(step schema.Step) *contract.Contract {
	switch step.Type {
	case schema.StepTool:
		// From tool definition, merged with step-level
		td := e.tools[step.Tool]
		if td != nil {
			base := td.Contract
			if step.Action != "" {
				if action, ok := td.Actions[step.Action]; ok && action.Contract != nil {
					base = contract.Merge(&td.Contract, action.Contract)
				}
			}
			if step.Contract != nil {
				merged := contract.Merge(&base, step.Contract)
				return &merged
			}
			return &base
		}
		if step.Contract != nil {
			return step.Contract
		}
		return nil
	case schema.StepManual:
		defaults := contract.ManualDefaults()
		if step.Contract != nil {
			merged := contract.Merge(&defaults, step.Contract)
			return &merged
		}
		return &defaults
	case schema.StepAssert:
		c := contract.AssertContract()
		return &c
	case schema.StepExtension:
		return step.Contract
	default:
		return nil
	}
}

func (e *Engine) resolveInputs(step schema.Step) (map[string]any, error) {
	resolved := make(map[string]any)

	// inputs_from spreading
	if step.InputsFrom != nil {
		sources := normalizeInputsFrom(step.InputsFrom)
		for _, src := range sources {
			if obj, ok := e.vars[src]; ok {
				if m, ok := obj.(map[string]any); ok {
					for k, v := range m {
						resolved[k] = v
					}
				}
			}
		}
	}

	// Explicit inputs override inputs_from
	for k, v := range step.Inputs {
		resolved[k] = v
	}

	// Resolve templates in all values
	return eval.ResolveMap(resolved, e.vars)
}

func normalizeInputsFrom(raw any) []string {
	switch v := raw.(type) {
	case string:
		return []string{v}
	case []any:
		var out []string
		for _, item := range v {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	case []string:
		return v
	}
	return nil
}

func (e *Engine) evaluateAssertion(a schema.Assertion) (bool, string) {
	switch a.Type {
	case "equals":
		val, err := eval.Resolve(a.Value, e.vars)
		if err != nil {
			return false, fmt.Sprintf("value template: %s", err)
		}
		exp, err := eval.Resolve(a.Expected, e.vars)
		if err != nil {
			return false, fmt.Sprintf("expected template: %s", err)
		}
		if val != exp {
			return false, fmt.Sprintf("expected %q, got %q", exp, val)
		}
		return true, ""

	case "not_equals":
		val, err := eval.Resolve(a.Value, e.vars)
		if err != nil {
			return false, fmt.Sprintf("value template: %s", err)
		}
		exp, err := eval.Resolve(a.Expected, e.vars)
		if err != nil {
			return false, fmt.Sprintf("expected template: %s", err)
		}
		if val == exp {
			return false, fmt.Sprintf("expected not %q, got %q", exp, val)
		}
		return true, ""

	case "contains":
		val, err := eval.Resolve(a.Value, e.vars)
		if err != nil {
			return false, fmt.Sprintf("value template: %s", err)
		}
		exp, err := eval.Resolve(a.Expected, e.vars)
		if err != nil {
			return false, fmt.Sprintf("expected template: %s", err)
		}
		if !strings.Contains(val, exp) {
			return false, fmt.Sprintf("%q does not contain %q", val, exp)
		}
		return true, ""

	case "matches":
		val, err := eval.Resolve(a.Value, e.vars)
		if err != nil {
			return false, fmt.Sprintf("value template: %s", err)
		}
		matched, err := matchPattern(a.Pattern, val)
		if err != nil {
			return false, fmt.Sprintf("pattern: %s", err)
		}
		if !matched {
			return false, fmt.Sprintf("%q does not match pattern %q", val, a.Pattern)
		}
		return true, ""

	default:
		return false, fmt.Sprintf("unknown assertion type %q", a.Type)
	}
}

func matchPattern(pattern, value string) (bool, error) {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return false, err
	}
	return re.MatchString(value), nil
}

func (e *Engine) requestApproval(stepID string, decision governance.Decision) bool {
	if e.cfg.Mode == "dry-run" {
		return true
	}
	min := decision.MinApprovers
	if min == 0 {
		min = 1
	}
	fmt.Fprintf(e.cfg.Stdout, "\n  ⚠ Step %s requires approval (risk: %s, min approvers: %d)\n", stepID, decision.RiskLevel, min)
	fmt.Fprintf(e.cfg.Stdout, "  Approve? [y/N]: ")
	scanner := bufio.NewScanner(e.cfg.Stdin)
	if scanner.Scan() {
		answer := strings.TrimSpace(strings.ToLower(scanner.Text()))
		return answer == "y" || answer == "yes"
	}
	return false
}

func (e *Engine) loadTools() {
	for _, toolName := range e.rb.Tools {
		path := validate.ResolveToolPath(toolName, e.cfg.BaseDir, e.cfg.ProjectRoot)
		if path == "" {
			continue
		}
		td, err := schema.LoadToolFile(path)
		if err != nil {
			continue
		}
		e.tools[toolName] = td
	}
}

// Vars returns the current variable scope (for test harness inspection).
func (e *Engine) Vars() map[string]any {
	return e.vars
}

// SetToolDef injects a tool definition directly (for testing/replay without disk loading).
func (e *Engine) SetToolDef(name string, td *schema.ToolDefinition) {
	e.tools[name] = td
}

func (e *Engine) emitStepError(stepID string, start time.Time, kind, msg string) {
	if e.trace != nil {
		e.trace.EmitStepComplete(stepID, trace.StatusError, nil, time.Since(start), &trace.Failure{
			Kind: kind, Message: msg,
		})
	}
}

// extractVarName extracts a variable name from {{ .varname }} template pattern.
var varRefRe = regexp.MustCompile(`^\{\{\s*\.(\w+)\s*\}\}$`)

func extractVarName(tmpl string) string {
	m := varRefRe.FindStringSubmatch(strings.TrimSpace(tmpl))
	if len(m) == 2 {
		return m[1]
	}
	return ""
}

func contractToMap(c *contract.Contract) map[string]any {
	resolved := c.Resolved()
	m := map[string]any{
		"side_effects":  *resolved.SideEffects,
		"deterministic": *resolved.Deterministic,
		"idempotent":    *resolved.Idempotent,
		"risk":          string(resolved.Risk()),
	}
	if len(resolved.Reads) > 0 {
		m["reads"] = resolved.Reads
	}
	if len(resolved.Writes) > 0 {
		m["writes"] = resolved.Writes
	}
	return m
}
