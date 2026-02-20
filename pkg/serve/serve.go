// Package serve implements the JSON-RPC server for the gert VS Code extension.
// It communicates over stdio (stdin/stdout) using newline-delimited JSON messages.
package serve

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/ormasoftchile/gert/pkg/inputs"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/replay"
	"github.com/ormasoftchile/gert/pkg/runtime"
	"github.com/ormasoftchile/gert/pkg/schema"
	"github.com/ormasoftchile/gert/pkg/tools"
)

// Message is a JSON-RPC 2.0 message (request or notification).
type Message struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      *int            `json:"id,omitempty"` // nil for notifications
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}

// RPCError is a JSON-RPC error.
type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// ExecStartParams are the parameters for exec/start.
type ExecStartParams struct {
	Runbook     string            `json:"runbook"`
	Mode        string            `json:"mode"`
	Vars        map[string]string `json:"vars,omitempty"`
	ICMID       string            `json:"icmId,omitempty"`
	ScenarioDir string            `json:"scenarioDir,omitempty"`
	RebaseTime  string            `json:"rebaseTime,omitempty"`
	Actor       string            `json:"actor,omitempty"`
}

// SubmitEvidenceParams are the parameters for exec/submitEvidence.
type SubmitEvidenceParams struct {
	StepID   string                              `json:"stepId"`
	Evidence map[string]*providers.EvidenceValue `json:"evidence"`
}

// Server is the JSON-RPC server that wraps the gert engine.
type Server struct {
	reader  *bufio.Reader
	writer  io.Writer
	mu      sync.Mutex
	engine  *runtime.Engine
	runbook *schema.Runbook
	ctx     context.Context
	cancel  context.CancelFunc

	// Input resolution manager — resolves from: bindings before execution
	InputManager *inputs.Manager

	// Channel-based step control for interactive mode
	nextCh     chan struct{}             // signal to advance to next step
	evidenceCh chan SubmitEvidenceParams // evidence submission

	// Tree cursor for step-by-step tree execution
	treeCursor       *treeCursor
	pendingManual    *pendingNode // manual step awaiting user acknowledgment
	pendingManualMsg *Message     // the exec/next message that presented the manual step

	// Invoke stack for nested runbook execution
	invokeStack []invokeFrame
}

// invokeFrame stores parent context when entering a child invoke runbook.
type invokeFrame struct {
	parentEngine  *runtime.Engine
	parentCursor  *treeCursor
	parentRunbook *schema.Runbook
	invokeStepID  string            // the invoke step's ID
	gate          *schema.Gate      // gate config from the invoke step
	capture       map[string]string // capture mapping (parent key → child key)
	msg           *Message          // the exec/next message that triggered the invoke
}

// treeCursor walks a tree one step at a time.
// After each step, it evaluates outcomes/branches and queues the next steps.
type treeCursor struct {
	pending []pendingNode // queue of nodes yet to execute
	stepIdx int           // global step counter
}

type pendingNode struct {
	node  schema.TreeNode
	depth int
}

func newTreeCursor(nodes []schema.TreeNode) *treeCursor {
	tc := &treeCursor{}
	for _, n := range nodes {
		tc.pending = append(tc.pending, pendingNode{node: n, depth: 0})
	}
	return tc
}

func (tc *treeCursor) hasNext() bool {
	return len(tc.pending) > 0
}

func (tc *treeCursor) pop() pendingNode {
	n := tc.pending[0]
	tc.pending = tc.pending[1:]
	return n
}

// insertBranchSteps adds branch steps to the front of the queue
func (tc *treeCursor) insertBranchSteps(nodes []schema.TreeNode, depth int) {
	var items []pendingNode
	for _, n := range nodes {
		items = append(items, pendingNode{node: n, depth: depth})
	}
	tc.pending = append(items, tc.pending...)
}

// New creates a new server reading from stdin and writing to stdout.
func New() *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		reader:     bufio.NewReader(os.Stdin),
		writer:     os.Stdout,
		ctx:        ctx,
		cancel:     cancel,
		nextCh:     make(chan struct{}, 1),
		evidenceCh: make(chan SubmitEvidenceParams, 1),
	}
}

// Run starts the server main loop — reads messages from stdin and dispatches them.
func (s *Server) Run() error {
	defer s.cancel()

	scanner := bufio.NewScanner(s.reader)
	// Increase buffer for large messages
	scanner.Buffer(make([]byte, 0, 1024*1024), 1024*1024)

	for scanner.Scan() {
		line := scanner.Bytes()
		if len(line) == 0 {
			continue
		}

		var msg Message
		if err := json.Unmarshal(line, &msg); err != nil {
			s.sendError(nil, -32700, fmt.Sprintf("parse error: %v", err))
			continue
		}

		s.dispatch(&msg)
	}

	return scanner.Err()
}

// dispatch routes a message to the appropriate handler.
func (s *Server) dispatch(msg *Message) {
	switch msg.Method {
	case "exec/start":
		s.handleExecStart(msg)
	case "exec/next":
		s.handleExecNext(msg)
	case "exec/chooseOutcome":
		s.handleChooseOutcome(msg)
	case "exec/submitChoice":
		s.handleSubmitChoice(msg)
	case "exec/submitEvidence":
		s.handleSubmitEvidence(msg)
	case "exec/getVariables":
		s.handleGetVariables(msg)
	case "exec/getManifest":
		s.handleGetManifest(msg)
	case "exec/saveScenario":
		s.handleSaveScenario(msg)
	case "shutdown":
		s.cancel()
		s.sendResult(msg.ID, map[string]string{"status": "shutting down"})
	default:
		s.sendError(msg.ID, -32601, fmt.Sprintf("unknown method: %s", msg.Method))
	}
}

// handleExecStart loads a runbook and starts execution in a goroutine.
func (s *Server) handleExecStart(msg *Message) {
	var params ExecStartParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.sendError(msg.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	fmt.Fprintf(os.Stderr, "serve: exec/start runbook=%q mode=%q scenarioDir=%q\n", params.Runbook, params.Mode, params.ScenarioDir)

	// Validate runbook
	rb, errs := schema.ValidateFile(params.Runbook)
	if hasServeValidationErrors(errs) {
		s.sendError(msg.ID, -32603, fmt.Sprintf("validation failed: %v", firstServeError(errs)))
		return
	}
	s.runbook = rb

	// Check source hash for staleness
	if rb.Meta.Source != nil && rb.Meta.Source.SourceHash != "" && rb.Meta.Source.File != "" {
		if srcData, err := os.ReadFile(rb.Meta.Source.File); err == nil {
			currentHash := fmt.Sprintf("%x", sha256.Sum256(srcData))
			if currentHash != rb.Meta.Source.SourceHash {
				fmt.Fprintf(os.Stderr, "serve: WARNING source TSG has changed since compilation (compiled=%s, current=%s)\n",
					rb.Meta.Source.SourceHash[:12], currentHash[:12])
				s.sendEvent("runbook/staleSource", map[string]interface{}{
					"sourceFile":   rb.Meta.Source.File,
					"compiledAt":   rb.Meta.Source.CompiledAt,
					"compiledHash": rb.Meta.Source.SourceHash,
					"currentHash":  currentHash,
				})
			}
		} else {
			fmt.Fprintf(os.Stderr, "serve: could not read source file for hash check: %v\n", err)
		}
	}

	// Merge vars into runbook
	if rb.Meta.Vars == nil {
		rb.Meta.Vars = make(map[string]string)
	}
	for k, v := range params.Vars {
		rb.Meta.Vars[k] = v
	}

	// Resolve inputs
	if rb.Meta.Inputs != nil {
		for name, input := range rb.Meta.Inputs {
			if _, ok := rb.Meta.Vars[name]; !ok {
				if input.Default != "" {
					rb.Meta.Vars[name] = input.Default
				}
			}
		}
	}

	// Input resolution: dispatch to registered input providers.
	if rb.Meta.Inputs != nil && s.InputManager != nil {
		execCtx := make(map[string]string)
		if params.ICMID != "" {
			execCtx["icmId"] = params.ICMID
		}

		resolved, warnings, err := s.InputManager.Resolve(s.ctx, rb.Meta.Inputs, execCtx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: input resolution error: %v\n", err)
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "serve: input warning: %s\n", w)
			s.sendEvent("event/icmWarning", map[string]interface{}{
				"icmId":   params.ICMID,
				"warning": w,
			})
		}
		for k, v := range resolved {
			if _, already := rb.Meta.Vars[k]; !already {
				rb.Meta.Vars[k] = v
				fmt.Fprintf(os.Stderr, "serve: input resolved %s = %q\n", k, v)
			}
		}
		if len(resolved) > 0 {
			fmt.Fprintf(os.Stderr, "serve: resolved %d inputs\n", len(resolved))
		}
	}

	// Set up executor/collector based on mode
	var executor providers.CommandExecutor
	var collector providers.EvidenceCollector
	var xtsScenario *replay.XTSScenario

	switch params.Mode {
	case "real":
		executor = &providers.RealExecutor{}
		collector = &ServeCollector{server: s}
	case "dry-run":
		executor = &DryRunExecutor{}
		collector = &providers.DryRunCollector{}
	case "replay":
		if params.ScenarioDir != "" {
			fmt.Fprintf(os.Stderr, "serve: loading scenario from %q\n", params.ScenarioDir)
			var err error
			xtsScenario, err = replay.LoadXTSScenario(params.ScenarioDir, parseTimeOrZero(params.RebaseTime))
			if err != nil {
				s.sendError(msg.ID, -32604, fmt.Sprintf("load scenario: %v", err))
				return
			}
			executor = replay.NewReplayExecutor(xtsScenario.Scenario)
			collector = &providers.DryRunCollector{}
		}
	default:
		s.sendError(msg.ID, -32605, fmt.Sprintf("unknown mode: %s", params.Mode))
		return
	}

	// Create engine
	engine, err := runtime.NewEngine(rb, executor, collector, params.Mode, params.Actor)
	if err != nil {
		s.sendError(msg.ID, -32606, fmt.Sprintf("create engine: %v", err))
		return
	}
	engine.ICMID = params.ICMID
	engine.RunbookPath = params.Runbook
	if xtsScenario != nil {
		engine.XTSScenario = xtsScenario
	}

	// Load tool definitions if the runbook declares tools:
	if len(rb.Tools) > 0 {
		tm := tools.NewManager(executor, engine.Redact)
		baseDir := ""
		if params.Runbook != "" {
			baseDir = filepath.Dir(params.Runbook)
		}
		for alias, path := range rb.Tools {
			if err := tm.Load(alias, path, baseDir); err != nil {
				fmt.Fprintf(os.Stderr, "serve: WARNING failed to load tool %q: %v\n", alias, err)
			}
		}
		engine.ToolManager = tm
	}

	// Register built-in XTS tool when meta.xts is present
	if rb.Meta.XTS != nil {
		if engine.ToolManager == nil {
			engine.ToolManager = tools.NewManager(executor, engine.Redact)
		}
		cliPath := ""
		if engine.GetXTSCLIPath() != "" {
			cliPath = engine.GetXTSCLIPath()
		}
		engine.ToolManager.RegisterBuiltin("__xts", tools.BuildXTSToolDef(cliPath))
	}

	s.engine = engine

	// Return run info
	result := map[string]interface{}{
		"runId":     engine.GetRunID(),
		"baseDir":   engine.GetBaseDir(),
		"stepCount": len(rb.Steps),
		"steps":     buildStepSummaries(rb.Steps),
		"kind":      string(rb.Meta.Kind),
	}
	if rb.Meta.Prose != nil {
		result["prose"] = rb.Meta.Prose
	}
	if rb.Meta.Description != "" {
		result["description"] = rb.Meta.Description
	}
	if len(rb.Tree) > 0 {
		result["tree"] = s.resolveTreeForDisplay(rb.Tree)
		s.treeCursor = newTreeCursor(rb.Tree)
	}
	s.sendResult(msg.ID, result)
}

// handleExecNext advances execution by one step.
func (s *Server) handleExecNext(msg *Message) {
	if s.engine == nil {
		s.sendError(msg.ID, -32607, "no active execution — call exec/start first")
		return
	}

	// Tree mode: step-by-step via cursor
	if s.treeCursor != nil {
		s.handleTreeNext(msg)
		return
	}

	idx := s.engine.State.CurrentStepIndex
	if idx >= len(s.runbook.Steps) {
		s.sendResult(msg.ID, map[string]string{"status": "completed"})
		return
	}

	step := s.runbook.Steps[idx]

	// Evaluate when: guard
	if step.When != "" {
		if !s.engine.EvalConditionPublic(step.When) {
			// Skip this step
			s.sendEvent("event/stepSkipped", map[string]interface{}{
				"stepId": step.ID,
				"index":  idx,
				"reason": fmt.Sprintf("when: %s → false", step.When),
			})
			s.engine.State.CurrentStepIndex = idx + 1
			s.sendResult(msg.ID, map[string]interface{}{
				"stepId": step.ID,
				"status": "skipped",
				"reason": fmt.Sprintf("when: %s → false", step.When),
			})
			return
		}
	}

	// Send stepStarted event with full step info
	stepEvent := map[string]interface{}{
		"stepId":       step.ID,
		"index":        idx,
		"type":         step.Type,
		"title":        step.Title,
		"instructions": s.engine.ResolveTemplatePublic(step.Instructions),
		"outcomes":     s.buildOutcomeSummaries(step.Outcomes),
	}
	if step.XTS != nil && step.XTS.Query != "" {
		stepEvent["query"] = s.engine.ResolveTemplatePublic(step.XTS.Query)
		stepEvent["queryType"] = step.XTS.QueryType
	}
	if step.With != nil && len(step.With.Argv) > 0 {
		stepEvent["command"] = s.resolveArgv(step.With.Argv)
	}
	s.sendEvent("event/stepStarted", stepEvent)

	// Execute the step
	result, err := s.engine.ExecuteStep(s.ctx, idx)
	if err != nil {
		s.sendEvent("event/stepCompleted", map[string]interface{}{
			"stepId": step.ID,
			"status": "failed",
			"error":  err.Error(),
		})
		s.sendResult(msg.ID, map[string]interface{}{"stepId": step.ID, "status": "failed", "error": err.Error()})
		return
	}

	// Send stepCompleted with captures
	s.sendEvent("event/stepCompleted", map[string]interface{}{
		"stepId":   step.ID,
		"status":   result.Status,
		"captures": result.Captures,
		"error":    result.Error,
	})

	// Evaluate outcomes
	if len(step.Outcomes) > 0 && result.Status == "passed" {
		for _, outcome := range step.Outcomes {
			triggered := s.engine.EvalConditionPublic(outcome.When)
			if triggered {
				rec := s.engine.ResolveTemplatePublic(outcome.Recommendation)
				if rec == "" {
					rec = outcome.Recommendation
				}
				s.sendEvent("event/outcomeReached", map[string]interface{}{
					"stepId":         step.ID,
					"state":          outcome.State,
					"recommendation": strings.TrimSpace(rec),
				})
				s.sendResult(msg.ID, map[string]interface{}{
					"stepId":         step.ID,
					"status":         "outcome",
					"outcomeState":   outcome.State,
					"recommendation": strings.TrimSpace(rec),
					"captures":       result.Captures,
				})
				return
			}
		}
	}

	s.sendResult(msg.ID, map[string]interface{}{
		"stepId":   step.ID,
		"status":   result.Status,
		"captures": result.Captures,
	})
}

// handleTreeNext executes the next step in the tree cursor.
func (s *Server) handleTreeNext(msg *Message) {
	// Phase 2: If there's a pending manual step, execute it now (user clicked "Mark Complete")
	if s.pendingManual != nil {
		pn := s.pendingManual
		s.pendingManual = nil
		step := pn.node.Step
		stepIdx := s.treeCursor.stepIdx

		// Execute the step
		origStdout := os.Stdout
		os.Stdout = os.Stderr
		result, err := s.engine.ExecuteTreeStep(s.ctx, stepIdx, step)
		os.Stdout = origStdout

		if err != nil {
			errEvt := map[string]interface{}{
				"stepId": step.ID, "status": "failed", "error": err.Error(),
			}
			if len(s.invokeStack) > 0 {
				errEvt["invokeChild"] = true
			}
			s.sendEvent("event/stepCompleted", errEvt)
			s.sendResult(msg.ID, map[string]interface{}{
				"stepId": step.ID, "status": "failed", "error": err.Error(),
			})
			return
		}
		s.treeCursor.stepIdx++

		compEvt := map[string]interface{}{
			"stepId": step.ID, "status": result.Status,
			"captures": result.Captures, "error": result.Error,
		}
		if len(s.invokeStack) > 0 {
			compEvt["invokeChild"] = true
		}
		s.sendEvent("event/stepCompleted", compEvt)

		// Evaluate outcomes — if one triggers, stop the tree
		if len(step.Outcomes) > 0 && result.Status == "passed" {
			for _, outcome := range step.Outcomes {
				triggered := s.engine.EvalConditionPublic(outcome.When)
				if triggered {
					rec := s.engine.ResolveTemplatePublic(outcome.Recommendation)
					if rec == "" || rec == "<no value>" {
						rec = outcome.Recommendation
					}
					s.engine.SetOutcome(outcome.State, step.ID, strings.TrimSpace(rec))
					s.treeCursor.pending = nil

					// If inside an invoke context, pop back to parent
					// instead of ending the whole run.
					if len(s.invokeStack) > 0 {
						s.sendEvent("event/stepCompleted", map[string]interface{}{
							"stepId":       step.ID,
							"status":       "passed",
							"captures":     result.Captures,
							"childOutcome": outcome.State,
							"invokeChild":  true,
						})
						if gateStop := s.exitInvoke(msg); gateStop {
							return // gate stopped parent — exitInvoke sent result
						}
						// Gate didn't stop — continue parent tree
						s.handleTreeNext(msg)
						return
					}

					s.sendEvent("event/outcomeReached", map[string]interface{}{
						"stepId": step.ID, "state": outcome.State,
						"recommendation": strings.TrimSpace(rec),
					})
					s.emitSkippedSteps()
					s.sendResult(msg.ID, map[string]interface{}{
						"stepId": step.ID, "status": "outcome",
						"outcomeState": outcome.State, "recommendation": strings.TrimSpace(rec),
					})
					return
				}
			}
		}

		// Evaluate branches
		if len(pn.node.Branches) > 0 {
			for _, branch := range pn.node.Branches {
				if s.engine.EvalConditionPublic(branch.Condition) {
					s.treeCursor.insertBranchSteps(branch.Steps, pn.depth+1)
					break
				}
			}
		}

		// Fall through to present the next step
	}

	if !s.treeCursor.hasNext() {
		s.completeTree(msg)
		return
	}

	// ── Auto-advance loop ────────────────────────────────────────────
	// Pop steps and auto-advance routing/passthrough manual steps without
	// returning to the extension. Only stop when we hit a step that needs
	// user interaction (evidence, approval, real decision) or an xts/cli step.
	for {
		if !s.treeCursor.hasNext() {
			s.completeTree(msg)
			return
		}

		pn := s.treeCursor.pop()
		step := pn.node.Step
		stepIdx := s.treeCursor.stepIdx

		// Evaluate precondition
		if step.Precondition != nil && step.Precondition.SkipIfSucceeds && len(step.Precondition.Check) > 0 {
			resolvedCheck := make([]string, len(step.Precondition.Check))
			canCheck := true
			for i, arg := range step.Precondition.Check {
				r := s.engine.ResolveTemplatePublic(arg)
				if r == "<no value>" {
					canCheck = false
					break
				}
				resolvedCheck[i] = r
			}
			if canCheck {
				probeResult, probeErr := s.engine.Executor.Execute(s.ctx, resolvedCheck[0], resolvedCheck[1:], nil)
				if probeErr == nil && probeResult.ExitCode == 0 {
					precondMsg := step.Precondition.Message
					if precondMsg == "" {
						precondMsg = fmt.Sprintf("precondition satisfied: %s", strings.Join(resolvedCheck, " "))
					}
					s.treeCursor.stepIdx++
					s.sendEvent("event/stepSkipped", map[string]interface{}{
						"stepId": step.ID, "index": stepIdx, "reason": precondMsg,
					})
					continue // skip to next step in the loop
				}
			}
		}

		// Send stepStarted event
		resolvedInstructions := s.engine.ResolveTemplatePublic(step.Instructions)
		resolvedTitle := s.engine.ResolveTemplatePublic(step.Title)
		if resolvedTitle == "" || resolvedTitle == "<no value>" {
			resolvedTitle = step.Title
		}
		treeStepEvent := map[string]interface{}{
			"stepId":       step.ID,
			"index":        stepIdx,
			"type":         step.Type,
			"title":        resolvedTitle,
			"instructions": resolvedInstructions,
			"outcomes":     s.buildOutcomeSummaries(step.Outcomes),
		}
		if len(s.invokeStack) > 0 {
			treeStepEvent["invokeChild"] = true
		}
		if step.XTS != nil && step.XTS.Query != "" {
			treeStepEvent["query"] = s.engine.ResolveTemplatePublic(step.XTS.Query)
			treeStepEvent["queryType"] = step.XTS.QueryType
		}
		// Desugar XTS steps to include tool metadata for structured rendering
		if step.Type == "xts" && step.XTS != nil {
			defaultEnv := ""
			if s.runbook.Meta.XTS != nil {
				defaultEnv = s.runbook.Meta.XTS.Environment
			}
			if toolInfo := tools.DesugarXTSStep(&step, s.engine.ResolveTemplatePublic(defaultEnv)); toolInfo != nil {
				// Resolve template vars in the desugared args
				if args, ok := toolInfo["args"].(map[string]string); ok {
					for k, v := range args {
						args[k] = s.engine.ResolveTemplatePublic(v)
					}
				}
				treeStepEvent["tool"] = toolInfo
			}
		}
		if step.With != nil && len(step.With.Argv) > 0 {
			treeStepEvent["command"] = s.resolveArgv(step.With.Argv)
		}
		if step.Tool != nil {
			toolInfo := map[string]interface{}{
				"name":   step.Tool.Name,
				"action": step.Tool.Action,
			}
			if len(step.Tool.Args) > 0 {
				resolvedArgs := make(map[string]string)
				for k, v := range step.Tool.Args {
					resolvedArgs[k] = s.engine.ResolveTemplatePublic(v)
				}
				toolInfo["args"] = resolvedArgs
			}
			// Include governance info if available
			if s.engine.ToolManager != nil {
				if td := s.engine.ToolManager.GetDef(step.Tool.Name); td != nil {
					if act, ok := td.Actions[step.Tool.Action]; ok && act.Governance != nil {
						toolInfo["governance"] = map[string]interface{}{
							"read_only":         act.Governance.ReadOnly,
							"requires_approval": act.Governance.RequiresApproval,
						}
					}
				}
			}
			treeStepEvent["tool"] = toolInfo
			// Build command display for the extension
			if s.engine.ToolManager != nil {
				if td := s.engine.ToolManager.GetDef(step.Tool.Name); td != nil {
					treeStepEvent["command"] = td.Meta.Binary + " " + step.Tool.Action
				}
			}
		}
		if step.Choices != nil {
			options := make([]map[string]interface{}, len(step.Choices.Options))
			for j, opt := range step.Choices.Options {
				options[j] = map[string]interface{}{
					"value":       opt.Value,
					"label":       opt.Label,
					"description": opt.Description,
				}
			}
			treeStepEvent["choices"] = map[string]interface{}{
				"variable": step.Choices.Variable,
				"prompt":   s.engine.ResolveTemplatePublic(step.Choices.Prompt),
				"options":  options,
			}
		}
		s.sendEvent("event/stepStarted", treeStepEvent)

		// ── Check if manual step can auto-advance ────────────────────
		// Never auto-advance steps with choices — the user must select an option first.
		if step.Type == "manual" && step.Choices == nil && len(step.RequiredEvidence) == 0 && (step.Approvals == nil || step.Approvals.Min == 0) {
			hasBranches := len(pn.node.Branches) > 0
			hasOnlyBranchOutcome := len(step.Outcomes) == 0 && hasBranches
			hasSingleAutoOutcome := false
			if len(step.Outcomes) == 1 && step.Outcomes[0].When == "" {
				hasSingleAutoOutcome = true
			}

			if hasOnlyBranchOutcome || hasSingleAutoOutcome {
				fmt.Fprintf(os.Stderr, "serve: auto-advancing manual step %q (routing=%v, autoOutcome=%v)\n",
					step.ID, hasOnlyBranchOutcome, hasSingleAutoOutcome)
				s.executeTreeStep(msg, pn)
				// executeTreeStep may have inserted branch steps or triggered an outcome.
				// If it triggered an outcome, it already sent the result — we're done.
				if s.treeCursor.pending == nil {
					return // outcome was reached
				}
				continue // loop to pick up the next step
			}
		}

		// ── Non-auto-advanceable step: present to user or execute ────
		// For manual steps without evidence: wait for user
		if step.Type == "manual" && len(step.RequiredEvidence) == 0 {
			s.pendingManual = &pn
			hasOutcomes := len(step.Outcomes) > 0
			resultMap := map[string]interface{}{
				"stepId":       step.ID,
				"status":       "awaiting_user",
				"title":        resolvedTitle,
				"type":         step.Type,
				"instructions": resolvedInstructions,
				"hasOutcomes":  hasOutcomes,
			}
			if hasOutcomes {
				resultMap["outcomes"] = s.buildOutcomeSummaries(step.Outcomes)
			}
			if step.Choices != nil {
				options := make([]map[string]interface{}, len(step.Choices.Options))
				for j, opt := range step.Choices.Options {
					options[j] = map[string]interface{}{
						"value":       opt.Value,
						"label":       opt.Label,
						"description": opt.Description,
					}
				}
				resultMap["choices"] = map[string]interface{}{
					"variable": step.Choices.Variable,
					"prompt":   s.engine.ResolveTemplatePublic(step.Choices.Prompt),
					"options":  options,
				}
			}
			s.sendResult(msg.ID, resultMap)
			return
		}

		// ── Invoke step: push parent context and switch to child tree ──
		if step.Type == "invoke" {
			if err := s.enterInvoke(msg, step); err != nil {
				s.sendEvent("event/stepCompleted", map[string]interface{}{
					"stepId": step.ID,
					"status": "failed",
					"error":  err.Error(),
				})
				s.sendResult(msg.ID, map[string]interface{}{
					"stepId": step.ID,
					"status": "failed",
					"error":  err.Error(),
				})
				return
			}
			// enterInvoke replaced the cursor with child tree nodes.
			// Continue the loop so auto-advance picks up child steps.
			continue
		}

		// For CLI / XTS / manual-with-evidence: execute immediately
		s.executeTreeStep(msg, pn)

		// Inside invoke context, executeTreeStep skips sendResult so we can
		// keep driving child steps in-loop. If an outcome or gate already
		// terminated execution (pending cleared), stop.
		if len(s.invokeStack) > 0 {
			if s.treeCursor == nil || s.treeCursor.pending == nil {
				return // outcome/gate sent result
			}
			continue
		}
		return
	} // end auto-advance loop
}

// executeTreeStep runs a single tree step and evaluates outcomes/branches.
func (s *Server) executeTreeStep(msg *Message, pn pendingNode) {
	step := pn.node.Step
	stepIdx := s.treeCursor.stepIdx

	// Redirect stdout to stderr during execution
	origStdout := os.Stdout
	os.Stdout = os.Stderr

	// Execute the step
	result, err := s.engine.ExecuteTreeStep(s.ctx, stepIdx, step)

	os.Stdout = origStdout

	if err != nil {
		errEvt := map[string]interface{}{
			"stepId": step.ID,
			"status": "failed",
			"error":  err.Error(),
		}
		if len(s.invokeStack) > 0 {
			errEvt["invokeChild"] = true
		}
		s.sendEvent("event/stepCompleted", errEvt)
		// Inside invoke context, don't send result — let the auto-advance
		// loop continue driving child steps. Increment stepIdx so the next
		// child step gets the correct index.
		if len(s.invokeStack) > 0 {
			s.treeCursor.stepIdx++
			return
		}
		s.sendResult(msg.ID, map[string]interface{}{
			"stepId":       step.ID,
			"status":       "failed",
			"error":        err.Error(),
			"title":        step.Title,
			"type":         step.Type,
			"instructions": step.Instructions,
		})
		return
	}

	s.treeCursor.stepIdx++

	// Send stepCompleted
	stepCompletedEvt := map[string]interface{}{
		"stepId":   step.ID,
		"status":   result.Status,
		"captures": result.Captures,
		"error":    result.Error,
	}
	if len(s.invokeStack) > 0 {
		stepCompletedEvt["invokeChild"] = true
	}
	s.sendEvent("event/stepCompleted", stepCompletedEvt)

	// Evaluate outcomes — if one triggers, stop the tree
	outcomeTriggered := false
	if len(step.Outcomes) > 0 && result.Status == "passed" {
		for _, outcome := range step.Outcomes {
			triggered := s.engine.EvalConditionPublic(outcome.When)
			if triggered {
				rec := s.engine.ResolveTemplatePublic(outcome.Recommendation)
				if rec == "" {
					rec = outcome.Recommendation
				}
				// Set outcome on engine
				s.engine.SetOutcome(outcome.State, step.ID, strings.TrimSpace(rec))

				// Clear remaining cursor — this outcome terminates the tree
				s.treeCursor.pending = nil
				outcomeTriggered = true

				// If inside an invoke context, pop back to parent instead of
				// sending outcomeReached to the extension.
				if len(s.invokeStack) > 0 {
					s.sendEvent("event/stepCompleted", map[string]interface{}{
						"stepId":       step.ID,
						"status":       "passed",
						"captures":     result.Captures,
						"childOutcome": outcome.State,
						"invokeChild":  true,
					})
					if gateStop := s.exitInvoke(msg); gateStop {
						return // gate stopped parent — exitInvoke sent result
					}
					// Gate didn't stop — continue parent tree
					s.handleTreeNext(msg)
					return
				}

				s.sendEvent("event/outcomeReached", map[string]interface{}{
					"stepId":         step.ID,
					"state":          outcome.State,
					"recommendation": strings.TrimSpace(rec),
					"nextRunbook":    s.buildNextRunbookInfo(&outcome),
				})

				// Emit skipped for remaining steps
				s.emitSkippedSteps()

				s.sendResult(msg.ID, map[string]interface{}{
					"stepId":         step.ID,
					"status":         "outcome",
					"outcomeState":   outcome.State,
					"recommendation": strings.TrimSpace(rec),
					"captures":       result.Captures,
					"title":          step.Title,
					"type":           step.Type,
					"instructions":   step.Instructions,
					"nextRunbook":    s.buildNextRunbookInfo(&outcome),
				})
				return
			}
		}
	}

	// Evaluate branches — if no outcome triggered
	if !outcomeTriggered && len(pn.node.Branches) > 0 {
		for _, branch := range pn.node.Branches {
			if s.engine.EvalConditionPublic(branch.Condition) {
				s.treeCursor.insertBranchSteps(branch.Steps, pn.depth+1)
				break
			}
		}
	}

	// Inside invoke context, don't send result — the auto-advance loop
	// in handleTreeNext will continue driving child steps.
	if len(s.invokeStack) > 0 {
		return
	}

	s.sendResult(msg.ID, map[string]interface{}{
		"stepId":       step.ID,
		"status":       result.Status,
		"captures":     result.Captures,
		"title":        step.Title,
		"type":         step.Type,
		"instructions": step.Instructions,
	})
}

// handleChooseOutcome handles the user picking an outcome for a manual step.
func (s *Server) handleChooseOutcome(msg *Message) {
	if s.pendingManual == nil {
		s.sendError(msg.ID, -32608, "no pending manual step")
		return
	}

	var params struct {
		StepID string `json:"stepId"`
		State  string `json:"state"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.sendError(msg.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	pn := s.pendingManual
	s.pendingManual = nil
	step := pn.node.Step
	stepIdx := s.treeCursor.stepIdx

	// Execute the step (records it in history)
	origStdout := os.Stdout
	os.Stdout = os.Stderr
	result, err := s.engine.ExecuteTreeStep(s.ctx, stepIdx, step)
	os.Stdout = origStdout

	if err != nil {
		s.sendEvent("event/stepCompleted", map[string]interface{}{
			"stepId": step.ID,
			"status": "failed",
			"error":  err.Error(),
		})
		s.sendResult(msg.ID, map[string]interface{}{"stepId": step.ID, "status": "failed", "error": err.Error()})
		return
	}
	s.treeCursor.stepIdx++

	s.sendEvent("event/stepCompleted", map[string]interface{}{
		"stepId":   step.ID,
		"status":   result.Status,
		"captures": result.Captures,
	})

	// Find the chosen outcome to get its recommendation and next_runbook
	var rec string
	var chosenOutcome *schema.Outcome
	for i, o := range step.Outcomes {
		if o.State == params.State {
			rec = s.engine.ResolveTemplatePublic(o.Recommendation)
			if rec == "" || rec == "<no value>" {
				rec = o.Recommendation
			}
			rec = strings.TrimSpace(rec)
			chosenOutcome = &step.Outcomes[i]
			break
		}
	}

	// Set the chosen outcome on the engine
	s.engine.SetOutcome(params.State, step.ID, rec)

	// Clear remaining cursor — outcome terminates the tree
	s.treeCursor.pending = nil

	s.sendEvent("event/outcomeReached", map[string]interface{}{
		"stepId":         step.ID,
		"state":          params.State,
		"recommendation": rec,
		"nextRunbook":    s.buildNextRunbookInfo(chosenOutcome),
	})

	s.emitSkippedSteps()

	s.sendResult(msg.ID, map[string]interface{}{
		"stepId":         step.ID,
		"status":         "outcome",
		"outcomeState":   params.State,
		"recommendation": rec,
		"nextRunbook":    s.buildNextRunbookInfo(chosenOutcome),
	})
}

// completeTree finalizes tree execution — emits skipped steps and outcome.
// handleSubmitChoice stores a user's choice selection as a capture variable.
func (s *Server) handleSubmitChoice(msg *Message) {
	var params struct {
		StepID   string `json:"stepId"`
		Variable string `json:"variable"`
		Value    string `json:"value"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.sendError(msg.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}

	// Store the choice as a capture/var in the engine
	s.engine.SetVar(params.Variable, params.Value)
	fmt.Fprintf(os.Stderr, "serve: choice %s = %q (step %s)\n", params.Variable, params.Value, params.StepID)

	s.sendResult(msg.ID, map[string]interface{}{
		"stepId":   params.StepID,
		"variable": params.Variable,
		"value":    params.Value,
	})
}

// enterInvoke loads a child runbook, pushes the parent context onto the invoke
// stack, and replaces the server's engine/cursor/runbook with the child's.
// The auto-advance loop in handleTreeNext then continues with child steps.
func (s *Server) enterInvoke(msg *Message, step schema.Step) error {
	if step.Invoke == nil {
		return fmt.Errorf("invoke step %q missing invoke config", step.ID)
	}

	// Resolve the runbook alias → file path via imports
	resolvedFile := step.Invoke.Runbook
	if s.runbook.Imports != nil {
		if path, ok := s.runbook.Imports[resolvedFile]; ok {
			resolvedFile = path
		}
	}

	// Resolve template vars in the file path
	resolvedFile = s.engine.ResolveTemplatePublic(resolvedFile)

	// Resolve relative to the parent runbook's directory
	if s.engine.RunbookPath != "" && !filepath.IsAbs(resolvedFile) {
		resolvedFile = filepath.Join(filepath.Dir(s.engine.RunbookPath), resolvedFile)
	}

	fmt.Fprintf(os.Stderr, "serve: entering invoke %q → %s\n", step.ID, resolvedFile)

	// Send event for the invoke step itself
	s.sendEvent("event/invokeStarted", map[string]interface{}{
		"stepId":  step.ID,
		"runbook": resolvedFile,
	})

	// Load and validate child runbook
	childRB, errs := schema.ValidateFile(resolvedFile)
	if len(errs) > 0 {
		return fmt.Errorf("child runbook validation: %v", errs[0])
	}

	// Resolve inputs
	if childRB.Meta.Vars == nil {
		childRB.Meta.Vars = make(map[string]string)
	}
	for k, v := range step.Invoke.Inputs {
		childRB.Meta.Vars[k] = s.engine.ResolveTemplatePublic(v)
	}

	// Check chain depth
	depth := s.engine.ChainDepth + 1
	if depth > runtime.MaxChainDepth {
		return fmt.Errorf("invoke chain depth %d exceeds maximum %d", depth, runtime.MaxChainDepth)
	}

	// Create child engine with same executor/collector
	childEngine, err := runtime.NewEngine(childRB, s.engine.Executor, s.engine.Collector,
		s.engine.State.Mode, s.engine.State.Actor)
	if err != nil {
		return fmt.Errorf("create child engine: %v", err)
	}
	childEngine.ICMID = s.engine.ICMID
	childEngine.RunbookPath = resolvedFile
	childEngine.ChainDepth = depth
	childEngine.ParentRunID = s.engine.State.RunID

	// Load tool definitions for child runbook if it declares tools:
	if len(childRB.Tools) > 0 {
		tm := tools.NewManager(s.engine.Executor, childEngine.Redact)
		childBaseDir := filepath.Dir(resolvedFile)
		for alias, path := range childRB.Tools {
			if err := tm.Load(alias, path, childBaseDir); err != nil {
				fmt.Fprintf(os.Stderr, "serve: WARNING failed to load child tool %q: %v\n", alias, err)
			}
		}
		childEngine.ToolManager = tm
	}

	// Register built-in XTS tool for child if it has meta.xts
	if childRB.Meta.XTS != nil {
		if childEngine.ToolManager == nil {
			childEngine.ToolManager = tools.NewManager(s.engine.Executor, childEngine.Redact)
		}
		childEngine.ToolManager.RegisterBuiltin("__xts", tools.BuildXTSToolDef(childEngine.GetXTSCLIPath()))
	}

	fmt.Fprintf(os.Stderr, "serve: child engine %s (depth %d)\n", childEngine.GetRunID(), depth)

	// Push parent context onto invoke stack
	s.invokeStack = append(s.invokeStack, invokeFrame{
		parentEngine:  s.engine,
		parentCursor:  s.treeCursor,
		parentRunbook: s.runbook,
		invokeStepID:  step.ID,
		gate:          step.Gate,
		capture:       step.Capture,
		msg:           msg,
	})

	// Switch server to child context
	s.engine = childEngine
	s.runbook = childRB
	s.treeCursor = newTreeCursor(childRB.Tree)

	return nil
}

// exitInvoke pops the invoke stack, restoring the parent engine/cursor/runbook.
// It evaluates the gate, maps captures, and returns whether the parent should stop.
func (s *Server) exitInvoke(msg *Message) (gateStop bool) {
	n := len(s.invokeStack)
	if n == 0 {
		return false
	}

	frame := s.invokeStack[n-1]
	s.invokeStack = s.invokeStack[:n-1]

	childEngine := s.engine
	childOutcome := ""
	if childEngine.GetOutcome() != nil {
		childOutcome = childEngine.GetOutcome().State
	}

	fmt.Fprintf(os.Stderr, "serve: exiting invoke %q, child outcome=%q\n", frame.invokeStepID, childOutcome)

	// Record child run in parent
	frame.parentEngine.ChildRuns = append(frame.parentEngine.ChildRuns, runtime.ChildRunRef{
		RunID:   childEngine.GetRunID(),
		Runbook: childEngine.RunbookPath,
		Outcome: childOutcome,
	})

	// Map child captures back to parent
	for parentKey, childKey := range frame.capture {
		if val, ok := childEngine.State.Captures[childKey]; ok {
			frame.parentEngine.SetVar(parentKey, val)
		} else if val, ok := childEngine.State.Vars[childKey]; ok {
			frame.parentEngine.SetVar(parentKey, val)
		}
	}

	// Restore parent context
	s.engine = frame.parentEngine
	s.runbook = frame.parentRunbook
	s.treeCursor = frame.parentCursor

	// Send invoke completed event
	s.sendEvent("event/invokeCompleted", map[string]interface{}{
		"stepId":       frame.invokeStepID,
		"childOutcome": childOutcome,
	})

	// Evaluate gate
	if frame.gate != nil && len(frame.gate.StopIf) > 0 {
		for _, stopState := range frame.gate.StopIf {
			if childOutcome == stopState {
				fmt.Fprintf(os.Stderr, "serve: gate triggered for %q: child %q matches stop_if\n",
					frame.invokeStepID, childOutcome)

				// Propagate child outcome to parent
				if childEngine.GetOutcome() != nil {
					o := childEngine.GetOutcome()
					s.engine.SetOutcome(o.State, frame.invokeStepID, o.Recommendation)
				}

				// Clear remaining cursor — gate stops the parent tree
				s.treeCursor.pending = nil

				s.sendEvent("event/outcomeReached", map[string]interface{}{
					"stepId":         frame.invokeStepID,
					"state":          childOutcome,
					"recommendation": childEngine.GetOutcome().Recommendation,
					"gateTriggered":  true,
				})
				s.emitSkippedSteps()
				s.sendEvent("event/runCompleted", map[string]interface{}{
					"status": "completed",
				})
				s.sendResult(msg.ID, map[string]interface{}{
					"stepId":       frame.invokeStepID,
					"status":       "outcome",
					"outcomeState": childOutcome,
					"gateTriggered": true,
				})
				return true
			}
		}
	}

	// No gate triggered — increment step index and continue parent
	s.treeCursor.stepIdx++
	return false
}

func (s *Server) completeTree(msg *Message) {
	// If we're inside an invoke context, pop back to parent instead of ending
	if len(s.invokeStack) > 0 {
		if gateStop := s.exitInvoke(msg); gateStop {
			return // gate stopped the parent — exitInvoke already sent result
		}
		// Parent restored — continue with next parent step
		s.handleTreeNext(msg)
		return
	}

	// Emit skipped for remaining steps
	s.emitSkippedSteps()

	// Check for outcome
	manifest := s.engine.BuildManifest()
	if manifest.Outcome != nil {
		s.sendEvent("event/outcomeReached", map[string]interface{}{
			"stepId":         manifest.Outcome.StepID,
			"state":          manifest.Outcome.State,
			"recommendation": manifest.Outcome.Recommendation,
		})
	}

	// Always emit runCompleted so the extension can clear the spinner
	s.sendEvent("event/runCompleted", map[string]interface{}{
		"status": "completed",
	})

	s.sendResult(msg.ID, map[string]interface{}{
		"status":  "completed",
		"outcome": manifest.Outcome,
	})
}

// emitSkippedSteps sends event/stepSkipped for all tree steps not in engine history.
func (s *Server) emitSkippedSteps() {
	var collectAll func(nodes []schema.TreeNode) []string
	collectAll = func(nodes []schema.TreeNode) []string {
		var ids []string
		for _, n := range nodes {
			ids = append(ids, n.Step.ID)
			for _, b := range n.Branches {
				ids = append(ids, collectAll(b.Steps)...)
			}
		}
		return ids
	}
	executed := make(map[string]bool)
	for _, h := range s.engine.State.History {
		executed[h.StepID] = true
	}
	allIDs := collectAll(s.runbook.Tree)
	for _, id := range allIDs {
		if !executed[id] {
			s.sendEvent("event/stepSkipped", map[string]interface{}{"stepId": id})
		}
	}
}

// handleExecRunTree runs the entire tree and emits step events (used for batch replay).
func (s *Server) handleExecRunTree(msg *Message) {
	// Emit events for each step as the tree walks
	// We use the engine's history to track what happened
	beforeCount := len(s.engine.State.History)

	// Redirect stdout to stderr during tree execution to prevent
	// engine fmt.Printf from corrupting the JSON-RPC stream
	origStdout := os.Stdout
	os.Stdout = os.Stderr
	err := s.engine.Run(s.ctx)
	os.Stdout = origStdout

	// Emit events for all steps that executed
	for i := beforeCount; i < len(s.engine.State.History); i++ {
		result := s.engine.State.History[i]
		s.sendEvent("event/stepCompleted", map[string]interface{}{
			"stepId":   result.StepID,
			"status":   result.Status,
			"captures": result.Captures,
			"error":    result.Error,
		})
	}

	// Collect executed step IDs
	executedIDs := make(map[string]bool)
	for i := beforeCount; i < len(s.engine.State.History); i++ {
		executedIDs[s.engine.State.History[i].StepID] = true
	}

	// Emit stepSkipped for all tree steps that didn't execute
	var collectTreeIDs func(nodes []schema.TreeNode) []string
	collectTreeIDs = func(nodes []schema.TreeNode) []string {
		var ids []string
		for _, n := range nodes {
			ids = append(ids, n.Step.ID)
			for _, b := range n.Branches {
				ids = append(ids, collectTreeIDs(b.Steps)...)
			}
		}
		return ids
	}
	allIDs := collectTreeIDs(s.runbook.Tree)
	for _, id := range allIDs {
		if !executedIDs[id] {
			s.sendEvent("event/stepSkipped", map[string]interface{}{
				"stepId": id,
			})
		}
	}

	// Check for outcome
	manifest := s.engine.BuildManifest()
	if manifest.Outcome != nil {
		s.sendEvent("event/outcomeReached", map[string]interface{}{
			"stepId":         manifest.Outcome.StepID,
			"state":          manifest.Outcome.State,
			"recommendation": manifest.Outcome.Recommendation,
		})
	}

	if err != nil {
		s.sendResult(msg.ID, map[string]interface{}{
			"status": "failed",
			"error":  err.Error(),
		})
		return
	}

	s.sendResult(msg.ID, map[string]interface{}{
		"status":  "completed",
		"outcome": manifest.Outcome,
		"steps":   manifest.StepsSummary,
	})
}

// handleSubmitEvidence receives evidence for a manual step.
func (s *Server) handleSubmitEvidence(msg *Message) {
	var params SubmitEvidenceParams
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.sendError(msg.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}
	// Signal the evidence channel
	select {
	case s.evidenceCh <- params:
		s.sendResult(msg.ID, map[string]string{"status": "accepted"})
	default:
		s.sendError(msg.ID, -32608, "no step waiting for evidence")
	}
}

// handleGetVariables returns current vars and captures.
func (s *Server) handleGetVariables(msg *Message) {
	if s.engine == nil {
		s.sendError(msg.ID, -32607, "no active execution")
		return
	}
	s.sendResult(msg.ID, map[string]interface{}{
		"vars":     s.engine.State.Vars,
		"captures": s.engine.State.Captures,
	})
}

// handleGetManifest returns the current run manifest.
func (s *Server) handleGetManifest(msg *Message) {
	if s.engine == nil {
		s.sendError(msg.ID, -32607, "no active execution")
		return
	}
	s.sendResult(msg.ID, s.engine.BuildManifest())
}

// handleSaveScenario saves the current run's inputs and XTS step responses
// as a replay scenario folder.
func (s *Server) handleSaveScenario(msg *Message) {
	if s.engine == nil {
		s.sendError(msg.ID, -32607, "no active execution")
		return
	}
	var params struct {
		OutputDir string `json:"outputDir"`
	}
	if err := json.Unmarshal(msg.Params, &params); err != nil {
		s.sendError(msg.ID, -32602, fmt.Sprintf("invalid params: %v", err))
		return
	}
	if params.OutputDir == "" {
		s.sendError(msg.ID, -32602, "outputDir is required")
		return
	}
	// Create the output directory
	if err := os.MkdirAll(params.OutputDir, 0755); err != nil {
		s.sendError(msg.ID, -32603, fmt.Sprintf("create output dir: %v", err))
		return
	}
	if err := s.engine.SaveScenario(params.OutputDir); err != nil {
		s.sendError(msg.ID, -32603, fmt.Sprintf("save scenario: %v", err))
		return
	}
	s.sendResult(msg.ID, map[string]string{
		"status":    "saved",
		"outputDir": params.OutputDir,
	})
}

// --- Message sending ---

func (s *Server) sendResult(id *int, result interface{}) {
	data, _ := json.Marshal(result)
	msg := Message{
		JSONRPC: "2.0",
		ID:      id,
		Result:  json.RawMessage(data),
	}
	s.send(&msg)
}

func (s *Server) sendError(id *int, code int, message string) {
	msg := Message{
		JSONRPC: "2.0",
		ID:      id,
		Error:   &RPCError{Code: code, Message: message},
	}
	s.send(&msg)
}

func (s *Server) sendEvent(method string, params interface{}) {
	data, _ := json.Marshal(params)
	msg := Message{
		JSONRPC: "2.0",
		Method:  method,
		Params:  json.RawMessage(data),
	}
	s.send(&msg)
}

func (s *Server) send(msg *Message) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, _ := json.Marshal(msg)
	fmt.Fprintf(s.writer, "%s\n", data)
}

// hasServeValidationErrors returns true if any non-warning validation error exists.
func hasServeValidationErrors(errs []*schema.ValidationError) bool {
	for _, e := range errs {
		if e.Severity != "warning" {
			return true
		}
	}
	return false
}

// firstServeError returns the first non-warning error.
func firstServeError(errs []*schema.ValidationError) *schema.ValidationError {
	for _, e := range errs {
		if e.Severity != "warning" {
			return e
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}

// --- Helper types ---

// DryRunExecutor for serve mode.
type DryRunExecutor struct{}

func (d *DryRunExecutor) Execute(ctx context.Context, command string, args []string, env []string) (*providers.CommandResult, error) {
	return &providers.CommandResult{
		Stdout:   []byte("<dry-run>"),
		Stderr:   nil,
		ExitCode: 0,
	}, nil
}

// ServeCollector implements EvidenceCollector by waiting for messages from the extension.
type ServeCollector struct {
	server *Server
}

func (c *ServeCollector) PromptText(name string, instructions string) (string, error) {
	// If the variable was already set (e.g. via submitChoice), return it immediately
	// instead of blocking on the evidence channel.
	if val, ok := c.server.engine.State.Vars[name]; ok {
		if val != "" {
			return val, nil
		}
	}
	c.server.sendEvent("event/inputRequired", map[string]interface{}{
		"kind":         "text",
		"name":         name,
		"instructions": instructions,
	})
	// Wait for evidence submission
	ev := <-c.server.evidenceCh
	if val, ok := ev.Evidence[name]; ok {
		return val.Value, nil
	}
	return "", fmt.Errorf("no evidence received for %q", name)
}

func (c *ServeCollector) PromptChecklist(name string, items []string) (map[string]bool, error) {
	c.server.sendEvent("event/inputRequired", map[string]interface{}{
		"kind":  "checklist",
		"name":  name,
		"items": items,
	})
	ev := <-c.server.evidenceCh
	if val, ok := ev.Evidence[name]; ok && val.Items != nil {
		return val.Items, nil
	}
	// Default: all checked
	result := make(map[string]bool)
	for _, item := range items {
		result[item] = true
	}
	return result, nil
}

func (c *ServeCollector) PromptAttachment(name string, instructions string) (*providers.AttachmentInfo, error) {
	c.server.sendEvent("event/inputRequired", map[string]interface{}{
		"kind":         "attachment",
		"name":         name,
		"instructions": instructions,
	})
	ev := <-c.server.evidenceCh
	if val, ok := ev.Evidence[name]; ok {
		return &providers.AttachmentInfo{
			Path:   val.Path,
			SHA256: val.SHA256,
			Size:   val.Size,
		}, nil
	}
	return &providers.AttachmentInfo{}, nil
}

func (c *ServeCollector) PromptApproval(roles []string, min int) ([]providers.Approval, error) {
	c.server.sendEvent("event/inputRequired", map[string]interface{}{
		"kind":  "approval",
		"roles": roles,
		"min":   min,
	})
	// For now, auto-approve
	return []providers.Approval{{Actor: "serve-mode", Role: "any"}}, nil
}

// --- Helpers ---

func buildStepSummaries(steps []schema.Step) []map[string]interface{} {
	summaries := make([]map[string]interface{}, len(steps))
	for i, s := range steps {
		summaries[i] = map[string]interface{}{
			"id":    s.ID,
			"type":  s.Type,
			"title": s.Title,
			"index": i,
		}
		if s.When != "" {
			summaries[i]["when"] = s.When
		}
		if len(s.Outcomes) > 0 {
			summaries[i]["hasOutcomes"] = true
		}
	}
	return summaries
}

// resolveTreeForDisplay creates a display copy of the tree with templates resolved.
func (s *Server) resolveTreeForDisplay(nodes []schema.TreeNode) []map[string]interface{} {
	result := make([]map[string]interface{}, len(nodes))
	for i, node := range nodes {
		step := node.Step
		stepMap := map[string]interface{}{
			"id":    step.ID,
			"type":  step.Type,
			"title": step.Title,
		}
		if step.Instructions != "" {
			stepMap["instructions"] = s.resolve(step.Instructions)
		}
		if step.XTS != nil && step.XTS.Query != "" {
			stepMap["query"] = s.resolve(step.XTS.Query)
			stepMap["queryType"] = step.XTS.QueryType
		}
		if step.With != nil && len(step.With.Argv) > 0 {
			stepMap["command"] = s.resolveArgvDisplay(step.With.Argv)
		}
		if step.When != "" {
			stepMap["when"] = step.When
		}

		// Resolve outcomes
		if len(step.Outcomes) > 0 {
			outcomes := make([]map[string]interface{}, len(step.Outcomes))
			for j, o := range step.Outcomes {
				outcomes[j] = map[string]interface{}{
					"state": o.State,
				}
				if o.When != "" {
					outcomes[j]["when"] = s.resolve(o.When)
				}
				if o.Recommendation != "" {
					outcomes[j]["recommendation"] = s.resolve(o.Recommendation)
				}
			}
			stepMap["outcomes"] = outcomes
		}

		nodeMap := map[string]interface{}{
			"step": stepMap,
		}

		// Resolve branches recursively
		if len(node.Branches) > 0 {
			branches := make([]map[string]interface{}, len(node.Branches))
			for j, b := range node.Branches {
				branches[j] = map[string]interface{}{
					"condition": s.resolve(b.Condition),
				}
				if b.Label != "" {
					branches[j]["label"] = b.Label
				}
				if len(b.Steps) > 0 {
					branches[j]["steps"] = s.resolveTreeForDisplay(b.Steps)
				}
			}
			nodeMap["branches"] = branches
		}

		result[i] = nodeMap
	}
	return result
}

// resolve resolves a template string, returning the original if resolution fails.
func (s *Server) resolve(tmpl string) string {
	if s.engine == nil {
		return tmpl
	}
	val := s.engine.ResolveTemplatePublic(tmpl)
	if val == "<no value>" {
		return tmpl
	}
	return val
}

// resolveArgv resolves template vars in argv and joins into a command string.
func (s *Server) resolveArgv(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = s.engine.ResolveTemplatePublic(arg)
	}
	return strings.Join(parts, " ")
}

// resolveArgvDisplay resolves argv using the display resolver (for tree display).
func (s *Server) resolveArgvDisplay(argv []string) string {
	parts := make([]string, len(argv))
	for i, arg := range argv {
		parts[i] = s.resolve(arg)
	}
	return strings.Join(parts, " ")
}

func (s *Server) buildOutcomeSummaries(outcomes []schema.Outcome) []map[string]interface{} {
	if len(outcomes) == 0 {
		return nil
	}
	result := make([]map[string]interface{}, len(outcomes))
	for i, o := range outcomes {
		rec := o.Recommendation
		if s.engine != nil && rec != "" {
			resolved := s.engine.ResolveTemplatePublic(rec)
			if resolved != "<no value>" {
				rec = resolved
			}
		}
		when := o.When
		if s.engine != nil && when != "" {
			// Try to resolve for display; keep original if it errors
			resolved := s.engine.ResolveTemplatePublic(when)
			if resolved != "<no value>" {
				when = resolved
			}
		}
		result[i] = map[string]interface{}{
			"state":          o.State,
			"recommendation": rec,
		}
		if o.When != "" {
			result[i]["when"] = when
		}
		if o.NextRunbook != nil {
			result[i]["nextRunbook"] = o.NextRunbook.File
		}
	}
	return result
}

func parseTimeOrZero(s string) time.Time {
	if s == "now" {
		return time.Now().UTC()
	}
	t, _ := time.Parse(time.RFC3339, s)
	return t

}

// buildNextRunbookInfo returns a JSON-friendly map for an outcome's next_runbook,
// with template variables resolved. Returns nil if the outcome has no next_runbook.
func (s *Server) buildNextRunbookInfo(outcome *schema.Outcome) interface{} {
	if outcome == nil || outcome.NextRunbook == nil {
		return nil
	}
	nr := outcome.NextRunbook
	// Resolve templates in file path (e.g. login-errors/error-{{ .top_error }}-state-{{ .top_state }}.md)
	resolvedFile := s.engine.ResolveTemplatePublic(nr.File)
	if resolvedFile == "" || resolvedFile == "<no value>" {
		resolvedFile = nr.File
	}
	info := map[string]interface{}{
		"file": resolvedFile,
	}
	if len(nr.Inputs) > 0 {
		resolvedInputs := make(map[string]string, len(nr.Inputs))
		for k, v := range nr.Inputs {
			resolved := s.engine.ResolveTemplatePublic(v)
			if resolved == "<no value>" {
				resolved = v
			}
			resolvedInputs[k] = resolved
		}
		info["inputs"] = resolvedInputs
	}
	return info
}
