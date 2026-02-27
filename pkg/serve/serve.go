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

	"github.com/ormasoftchile/gert/pkg/diagram"
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

// DisplayConfig controls which debug/trace UI sections to show.
// All fields are optional pointers so callers can omit them (nil = use default).
type DisplayConfig struct {
	DebugTrace        *bool `json:"debugTrace,omitempty"`        // master toggle — hides captures + outcome conditions
	Captures          *bool `json:"captures,omitempty"`          // show the Captures section
	OutcomeConditions *bool `json:"outcomeConditions,omitempty"` // show when-expressions on outcomes
	CopySummary       *bool `json:"copySummary,omitempty"`       // show "Copy Summary" button
	SaveForReplay     *bool `json:"saveForReplay,omitempty"`     // show "Save for Replay" button
}

// ExecStartParams are the parameters for exec/start.
type ExecStartParams struct {
	Runbook     string            `json:"runbook"`
	Mode        string            `json:"mode"`
	Vars        map[string]string `json:"vars,omitempty"`
	Cwd         string            `json:"cwd,omitempty"`
	ScenarioDir string            `json:"scenarioDir,omitempty"`
	RebaseTime  string            `json:"rebaseTime,omitempty"`
	Actor       string            `json:"actor,omitempty"`
	ResumeRunID string            `json:"resumeRunId,omitempty"` // if set, resume an existing run
	Display     *DisplayConfig    `json:"display,omitempty"`     // UI display preferences
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

	// Session persistence — root run's base dir for session.json
	rootBaseDir string

	// Display preferences from exec/start (echoed back to client)
	display *DisplayConfig
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
	node               schema.TreeNode
	depth              int
	watchpoint         *iterateWatchpoint     // non-nil for convergence iterate checkpoints
	overWatchpoint     *iterateOverWatchpoint // non-nil for list-mode iterate checkpoints
}

// iterateWatchpoint is a synthetic node inserted after each iterate pass's
// steps. When popped, the until condition is evaluated to decide whether
// to re-queue another pass or continue past the iterate.
type iterateWatchpoint struct {
	block *schema.IterateBlock
	pass  int // 0-indexed pass that just completed
	max   int
}

// iterateOverWatchpoint is a synthetic node for list-mode iterate.
// It fires after each item's steps complete to advance to the next item.
type iterateOverWatchpoint struct {
	block *schema.IterateBlock
	items []string // resolved list items
	index int      // 0-indexed item that just completed
	asVar string   // variable name for current item
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

// pushIteratePass inserts the iterate steps followed by a watchpoint into
// the front of the cursor queue. The watchpoint fires after all steps in
// the pass complete, triggering convergence evaluation.
func (tc *treeCursor) pushIteratePass(block *schema.IterateBlock, pass, max, depth int) {
	var items []pendingNode
	for _, n := range block.Steps {
		items = append(items, pendingNode{node: n, depth: depth + 1})
	}
	items = append(items, pendingNode{
		watchpoint: &iterateWatchpoint{block: block, pass: pass, max: max},
		depth:      depth,
	})
	tc.pending = append(items, tc.pending...)
}

// pushIterateOverPass inserts the iterate steps followed by a list-mode
// watchpoint for the given item index. The watchpoint triggers advancement
// to the next item or completion.
func (tc *treeCursor) pushIterateOverPass(block *schema.IterateBlock, items []string, index int, asVar string, depth int) {
	var nodes []pendingNode
	for _, n := range block.Steps {
		nodes = append(nodes, pendingNode{node: n, depth: depth + 1})
	}
	nodes = append(nodes, pendingNode{
		overWatchpoint: &iterateOverWatchpoint{block: block, items: items, index: index, asVar: asVar},
		depth:          depth,
	})
	tc.pending = append(nodes, tc.pending...)
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

// NewWithIO creates a server reading from r and writing to w instead of stdio.
// Used by the TUI to communicate over in-memory pipes.
func NewWithIO(r io.Reader, w io.Writer) *Server {
	ctx, cancel := context.WithCancel(context.Background())
	return &Server{
		reader:     bufio.NewReader(r),
		writer:     w,
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
		s.saveSession()
	case "exec/next":
		s.handleExecNext(msg)
		s.saveSession()
	case "exec/chooseOutcome":
		s.handleChooseOutcome(msg)
		s.saveSession()
	case "exec/submitChoice":
		s.handleSubmitChoice(msg)
		s.saveSession()
	case "exec/submitEvidence":
		s.handleSubmitEvidence(msg)
		s.saveSession()
	case "exec/getVariables":
		s.handleGetVariables(msg)
	case "exec/getManifest":
		s.handleGetManifest(msg)
	case "exec/saveScenario":
		s.handleSaveScenario(msg)
	case "runbook/diagram":
		s.handleDiagram(msg)
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

	// Resume an existing run if resumeRunId is specified
	if params.ResumeRunID != "" {
		s.handleExecResume(msg, params)
		return
	}

	fmt.Fprintf(os.Stderr, "serve: exec/start runbook=%q mode=%q scenarioDir=%q\n", params.Runbook, params.Mode, params.ScenarioDir)

	// Change working directory if specified (so child commands resolve relative paths correctly)
	if params.Cwd != "" {
		if err := os.Chdir(params.Cwd); err != nil {
			fmt.Fprintf(os.Stderr, "serve: WARNING failed to chdir to %q: %v\n", params.Cwd, err)
		} else {
			fmt.Fprintf(os.Stderr, "serve: chdir to %q\n", params.Cwd)
		}
	}

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

		resolved, warnings, err := s.InputManager.Resolve(s.ctx, rb.Meta.Inputs, execCtx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: input resolution error: %v\n", err)
		}
		for _, w := range warnings {
			fmt.Fprintf(os.Stderr, "serve: input warning: %s\n", w)
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
	var stepScenario *replay.StepScenario

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
			stepScenario, err = replay.LoadStepScenario(params.ScenarioDir, parseTimeOrZero(params.RebaseTime))
			if err != nil {
				s.sendError(msg.ID, -32604, fmt.Sprintf("load scenario: %v", err))
				return
			}
			executor = replay.NewReplayExecutor(stepScenario.Scenario)
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
	engine.RunbookPath = params.Runbook
	if stepScenario != nil {
		engine.StepScenario = stepScenario
	}

	// Discover project context for package resolution
	var proj *schema.Project
	if params.Runbook != "" {
		proj, _ = schema.DiscoverProject(params.Runbook)
	}
	if proj == nil && params.Runbook != "" {
		proj = schema.FallbackProject(filepath.Dir(params.Runbook))
	}
	engine.Project = proj

	// Load tool definitions if the runbook declares tools:
	if len(rb.Tools) > 0 {
		tm := tools.NewManager(executor, engine.Redact)
		baseDir := ""
		if params.Runbook != "" {
			baseDir = filepath.Dir(params.Runbook)
		}
		for _, name := range rb.Tools {
			resolved := schema.ResolveToolPathCompat(proj, rb, name, baseDir)
			if err := tm.Load(name, resolved, ""); err != nil {
				fmt.Fprintf(os.Stderr, "serve: WARNING failed to load tool %q: %v\n", name, err)
			}
		}
		engine.ToolManager = tm
	}

	s.engine = engine
	s.rootBaseDir = engine.GetBaseDir()

	// Store display preferences
	s.display = params.Display

	// Build step summaries: prefer flat steps, fall back to flattened tree
	stepSummaries := buildStepSummaries(rb.Steps)
	stepCount := len(rb.Steps)
	if len(rb.Steps) == 0 && len(rb.Tree) > 0 {
		stepSummaries = buildStepSummaries(flattenTreeSteps(rb.Tree))
		stepCount = len(stepSummaries)
	}

	// Return run info
	result := map[string]interface{}{
		"runId":     engine.GetRunID(),
		"baseDir":   engine.GetBaseDir(),
		"stepCount": stepCount,
		"steps":     stepSummaries,
		"kind":      string(rb.Meta.Kind),
	}
	if rb.Meta.Prose != nil {
		result["prose"] = rb.Meta.Prose
	}
	if rb.Meta.Description != "" {
		result["description"] = rb.Meta.Description
	}
	if s.display != nil {
		result["display"] = s.display
	}
	if len(rb.Tree) > 0 {
		result["tree"] = s.resolveTreeForDisplay(rb.Tree)
		s.treeCursor = newTreeCursor(rb.Tree)
	}
	s.sendResult(msg.ID, result)
}

// handleExecResume resumes a previously saved session by its run ID.
// It loads the session file, rebuilds the engine/cursor/invoke stack, and
// returns the run info with history of already-completed steps.
func (s *Server) handleExecResume(msg *Message, params ExecStartParams) {
	fmt.Fprintf(os.Stderr, "serve: exec/start resumeRunId=%q\n", params.ResumeRunID)

	sessionPath := filepath.Join(".runbook", "runs", params.ResumeRunID, "session.json")
	session, err := loadSessionFile(sessionPath)
	if err != nil {
		s.sendError(msg.ID, -32610, fmt.Sprintf("load session: %v", err))
		return
	}

	// Restore working directory
	if session.Cwd != "" {
		if err := os.Chdir(session.Cwd); err != nil {
			fmt.Fprintf(os.Stderr, "serve: WARNING failed to chdir to %q: %v\n", session.Cwd, err)
		}
	}

	// Determine the root and active runbook paths
	rootRunbookPath := session.RunbookPath
	activeRunbookPath := session.RunbookPath
	if session.ActiveRunbookPath != "" {
		activeRunbookPath = session.ActiveRunbookPath
	}

	// Load the root runbook (needed for tree index of root level)
	rootRB, errs := schema.ValidateFile(rootRunbookPath)
	if hasServeValidationErrors(errs) {
		s.sendError(msg.ID, -32603, fmt.Sprintf("validate root runbook: %v", firstServeError(errs)))
		return
	}

	// Determine which runbook is active (may be child if inside invoke)
	activeRB := rootRB
	if activeRunbookPath != rootRunbookPath {
		activeRB, errs = schema.ValidateFile(activeRunbookPath)
		if hasServeValidationErrors(errs) {
			s.sendError(msg.ID, -32603, fmt.Sprintf("validate active runbook: %v", firstServeError(errs)))
			return
		}
	}
	s.runbook = activeRB

	// Set up executor/collector based on mode
	var executor providers.CommandExecutor
	var collector providers.EvidenceCollector

	switch session.Mode {
	case "real":
		executor = &providers.RealExecutor{}
		collector = &ServeCollector{server: s}
	case "dry-run":
		executor = &DryRunExecutor{}
		collector = &providers.DryRunCollector{}
	case "replay":
		if params.ScenarioDir != "" {
			stepScenario, err := replay.LoadStepScenario(params.ScenarioDir, parseTimeOrZero(params.RebaseTime))
			if err != nil {
				s.sendError(msg.ID, -32604, fmt.Sprintf("load scenario: %v", err))
				return
			}
			executor = replay.NewReplayExecutor(stepScenario.Scenario)
			collector = &providers.DryRunCollector{}
		} else {
			executor = &DryRunExecutor{}
			collector = &providers.DryRunCollector{}
		}
	default:
		s.sendError(msg.ID, -32605, fmt.Sprintf("unknown mode: %s", session.Mode))
		return
	}

	// Create the active engine from the session's current state
	engine, err := runtime.ResumeForServe(activeRB, executor, collector,
		session.RunID, session.Vars, session.Captures, session.History,
		session.Mode, session.Actor, session.StartedAt)
	if err != nil {
		s.sendError(msg.ID, -32610, fmt.Sprintf("rebuild engine: %v", err))
		return
	}
	engine.RunbookPath = activeRunbookPath

	// Discover project context
	var proj *schema.Project
	if rootRunbookPath != "" {
		proj, _ = schema.DiscoverProject(rootRunbookPath)
	}
	if proj == nil && rootRunbookPath != "" {
		proj = schema.FallbackProject(filepath.Dir(rootRunbookPath))
	}
	engine.Project = proj

	// Load tool definitions for the active runbook
	if len(activeRB.Tools) > 0 {
		tm := tools.NewManager(executor, engine.Redact)
		baseDir := filepath.Dir(activeRunbookPath)
		for _, name := range activeRB.Tools {
			resolved := schema.ResolveToolPathCompat(proj, activeRB, name, baseDir)
			if err := tm.Load(name, resolved, ""); err != nil {
				fmt.Fprintf(os.Stderr, "serve: WARNING failed to load tool %q on resume: %v\n", name, err)
			}
		}
		engine.ToolManager = tm
	}
	s.engine = engine
	s.rootBaseDir = filepath.Join(".runbook", "runs", session.RunID)

	// Rebuild tree cursor from the active runbook's tree
	activeTidx := buildTreeIndex(activeRB.Tree)
	pending, err := deserializePendingQueue(session.Pending, activeTidx)
	if err != nil {
		s.sendError(msg.ID, -32610, fmt.Sprintf("rebuild cursor: %v", err))
		return
	}
	s.treeCursor = &treeCursor{
		pending: pending,
		stepIdx: session.StepIdx,
	}

	// Rebuild pending manual step
	s.pendingManual = nil
	if session.PendingManual != nil {
		pn, err := deserializePendingNode(*session.PendingManual, activeTidx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: WARNING couldn't restore pending manual: %v\n", err)
		} else {
			s.pendingManual = &pn
		}
	}

	// Rebuild invoke stack
	s.invokeStack = nil
	for _, frameRef := range session.InvokeStack {
		parentRB, errs := schema.ValidateFile(frameRef.RunbookPath)
		if hasServeValidationErrors(errs) {
			fmt.Fprintf(os.Stderr, "serve: WARNING couldn't restore invoke frame for %q: %v\n", frameRef.RunbookPath, firstServeError(errs))
			continue
		}
		parentTidx := buildTreeIndex(parentRB.Tree)
		parentPending, err := deserializePendingQueue(frameRef.Pending, parentTidx)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: WARNING couldn't restore invoke cursor: %v\n", err)
			continue
		}
		parentEngine, err := runtime.ResumeForServe(parentRB, executor, collector,
			frameRef.RunID, frameRef.Vars, frameRef.Captures, nil,
			session.Mode, session.Actor, session.StartedAt)
		if err != nil {
			fmt.Fprintf(os.Stderr, "serve: WARNING couldn't restore invoke engine: %v\n", err)
			continue
		}
		parentEngine.RunbookPath = frameRef.RunbookPath
		parentEngine.ChainDepth = frameRef.ChainDepth
		parentEngine.Project = proj

		// Load tool definitions for parent runbook
		if len(parentRB.Tools) > 0 {
			tm := tools.NewManager(executor, parentEngine.Redact)
			baseDir := filepath.Dir(frameRef.RunbookPath)
			for _, name := range parentRB.Tools {
				resolved := schema.ResolveToolPathCompat(proj, parentRB, name, baseDir)
				if err := tm.Load(name, resolved, ""); err != nil {
					fmt.Fprintf(os.Stderr, "serve: WARNING failed to load parent tool %q on resume: %v\n", name, err)
				}
			}
			parentEngine.ToolManager = tm
		}

		s.invokeStack = append(s.invokeStack, invokeFrame{
			parentEngine:  parentEngine,
			parentCursor:  &treeCursor{pending: parentPending, stepIdx: frameRef.StepIdx},
			parentRunbook: parentRB,
			invokeStepID:  frameRef.InvokeStepID,
			gate:          frameRef.Gate,
			capture:       frameRef.Capture,
			msg:           msg,
		})
	}

	fmt.Fprintf(os.Stderr, "serve: resumed run %s (%d steps completed, %d pending, %d invoke frames)\n",
		session.RunID, len(session.History), len(session.Pending), len(session.InvokeStack))

	// Build step summaries: prefer flat steps, fall back to flattened tree
	resumeStepSummaries := buildStepSummaries(activeRB.Steps)
	resumeStepCount := len(activeRB.Steps)
	if len(activeRB.Steps) == 0 && len(activeRB.Tree) > 0 {
		resumeStepSummaries = buildStepSummaries(flattenTreeSteps(activeRB.Tree))
		resumeStepCount = len(resumeStepSummaries)
	}

	// Build response — same shape as exec/start but with resumed flag and history
	result := map[string]interface{}{
		"runId":     session.RunID,
		"baseDir":   s.rootBaseDir,
		"resumed":   true,
		"stepCount": resumeStepCount,
		"steps":     resumeStepSummaries,
		"kind":      string(rootRB.Meta.Kind),
	}
	if rootRB.Meta.Prose != nil {
		result["prose"] = rootRB.Meta.Prose
	}
	if rootRB.Meta.Description != "" {
		result["description"] = rootRB.Meta.Description
	}
	if len(rootRB.Tree) > 0 {
		result["tree"] = s.resolveTreeForDisplay(rootRB.Tree)
	}

	// Include completed step history so the extension can mark them done
	completedSteps := make([]map[string]interface{}, 0, len(session.History))
	for _, h := range session.History {
		completedSteps = append(completedSteps, map[string]interface{}{
			"stepId":   h.StepID,
			"status":   h.Status,
			"captures": h.Captures,
		})
	}
	result["history"] = completedSteps

	if s.pendingManual != nil {
		result["pendingManual"] = map[string]interface{}{
			"stepId": s.pendingManual.node.Step.ID,
			"title":  s.pendingManual.node.Step.Title,
		}
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
	// Extract query/queryType from tool args for syntax highlighting
	if step.Tool != nil {
		if q, ok := step.Tool.Args["query"]; ok {
			stepEvent["query"] = s.engine.ResolveTemplatePublic(q)
			if qt, ok := step.Tool.Args["query_type"]; ok {
				stepEvent["queryType"] = qt
			}
		}
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
			"type":   step.Type,
			"error":  err.Error(),
		})
		s.sendResult(msg.ID, map[string]interface{}{"stepId": step.ID, "status": "failed", "error": err.Error()})
		return
	}

	// Send stepCompleted with captures
	s.sendEvent("event/stepCompleted", map[string]interface{}{
		"stepId":   step.ID,
		"status":   result.Status,
		"type":     step.Type,
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
				"stepId": step.ID, "status": "failed", "type": step.Type, "error": err.Error(),
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
			"stepId": step.ID, "status": result.Status, "type": step.Type,
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
							"type":         step.Type,
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
	// user interaction (evidence, approval, real decision) or a cli step.
	for {
		if !s.treeCursor.hasNext() {
			s.completeTree(msg)
			return
		}

		pn := s.treeCursor.pop()

		// ── Handle iterate watchpoints (convergence check) ───────────
		if pn.watchpoint != nil {
			wp := pn.watchpoint
			converged := s.engine.EvalConditionPublic(wp.block.Until)
			if converged {
				fmt.Fprintf(os.Stderr, "serve: iterate converged at pass %d/%d\n", wp.pass+1, wp.max)
				s.sendEvent("event/iterateConverged", map[string]interface{}{
					"pass": wp.pass + 1,
					"max":  wp.max,
				})
				continue // done — move past the iterate
			}
			nextPass := wp.pass + 1
			if nextPass >= wp.max {
				errMsg := fmt.Sprintf("iterate did not converge after %d passes (until: %s)", wp.max, wp.block.Until)
				fmt.Fprintf(os.Stderr, "serve: %s\n", errMsg)
				s.sendEvent("event/iterateFailed", map[string]interface{}{
					"error": errMsg,
					"max":   wp.max,
				})
				s.treeCursor.pending = nil
				s.sendResult(msg.ID, map[string]interface{}{
					"status": "failed",
					"error":  errMsg,
				})
				return
			}
			// Not converged — start next pass
			s.engine.State.Vars["iteration"] = fmt.Sprintf("%d", nextPass)
			fmt.Fprintf(os.Stderr, "serve: iterate pass %d/%d starting\n", nextPass+1, wp.max)
			s.sendEvent("event/iteratePass", map[string]interface{}{
				"pass": nextPass + 1,
				"max":  wp.max,
			})
			s.treeCursor.pushIteratePass(wp.block, nextPass, wp.max, pn.depth)
			continue
		}

		// ── Handle list-mode iterate watchpoints (advance to next item) ──
		if pn.overWatchpoint != nil {
			ow := pn.overWatchpoint
			nextIdx := ow.index + 1
			if nextIdx >= len(ow.items) {
				// All items processed — done
				fmt.Fprintf(os.Stderr, "serve: iterate over completed (%d items)\n", len(ow.items))
				s.sendEvent("event/iterateConverged", map[string]interface{}{
					"mode":  "over",
					"pass":  len(ow.items),
					"total": len(ow.items),
				})
				continue
			}
			// Advance to next item
			s.engine.State.Vars["iteration"] = fmt.Sprintf("%d", nextIdx)
			s.engine.State.Vars[ow.asVar] = ow.items[nextIdx]
			fmt.Fprintf(os.Stderr, "serve: iterate over [%d/%d] %s = %s\n", nextIdx+1, len(ow.items), ow.asVar, ow.items[nextIdx])
			s.sendEvent("event/iteratePass", map[string]interface{}{
				"mode":  "over",
				"pass":  nextIdx + 1,
				"total": len(ow.items),
				"item":  ow.items[nextIdx],
			})
			s.treeCursor.pushIterateOverPass(ow.block, ow.items, nextIdx, ow.asVar, pn.depth)
			continue
		}

		// ── Handle iterate nodes (expand into steps + watchpoint) ────
		if pn.node.Iterate != nil {
			iter := pn.node.Iterate

			// List mode: iterate over comma-separated items
			if iter.Over != "" {
				resolved := s.engine.ResolveTemplatePublic(iter.Over)
				var listItems []string
				for _, part := range strings.Split(resolved, ",") {
					part = strings.TrimSpace(part)
					if part != "" {
						listItems = append(listItems, part)
					}
				}
				asVar := iter.As
				if asVar == "" {
					asVar = "item"
				}

				if len(listItems) == 0 {
					fmt.Fprintf(os.Stderr, "serve: iterate over: empty list, skipping\n")
					s.sendEvent("event/iterateStarted", map[string]interface{}{
						"mode":  "over",
						"as":    asVar,
						"total": 0,
						"pass":  0,
					})
					s.sendEvent("event/iterateConverged", map[string]interface{}{
						"mode":  "over",
						"pass":  0,
						"total": 0,
					})
					continue
				}

				fmt.Fprintf(os.Stderr, "serve: iterate over %d items (as: %s)\n", len(listItems), asVar)
				s.engine.State.Vars["iteration"] = "0"
				s.engine.State.Vars[asVar] = listItems[0]
				s.sendEvent("event/iterateStarted", map[string]interface{}{
					"mode":  "over",
					"as":    asVar,
					"total": len(listItems),
					"pass":  1,
					"item":  listItems[0],
				})
				s.treeCursor.pushIterateOverPass(iter, listItems, 0, asVar, pn.depth)
				continue
			}

			// Convergence mode: retry until condition
			s.engine.State.Vars["iteration"] = "0"
			fmt.Fprintf(os.Stderr, "serve: iterate started (max=%d, until=%q)\n", iter.Max, iter.Until)
			s.sendEvent("event/iterateStarted", map[string]interface{}{
				"max":   iter.Max,
				"until": iter.Until,
			})
			s.sendEvent("event/iteratePass", map[string]interface{}{
				"pass": 1,
				"max":  iter.Max,
			})
			s.treeCursor.pushIteratePass(iter, 0, iter.Max, pn.depth)
			continue
		}

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
			// Extract query/queryType from tool args for syntax highlighting
			if q, ok := step.Tool.Args["query"]; ok {
				treeStepEvent["query"] = s.engine.ResolveTemplatePublic(q)
				if qt, ok := step.Tool.Args["query_type"]; ok {
					treeStepEvent["queryType"] = qt
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
					"type":   step.Type,
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

		// For CLI / tool / manual-with-evidence: execute immediately
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
			"type":   step.Type,
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
		"type":     step.Type,
		"captures": result.Captures,
		"error":    result.Error,
	}
	if len(s.invokeStack) > 0 {
		stepCompletedEvt["invokeChild"] = true
	}
	s.sendEvent("event/stepCompleted", stepCompletedEvt)

	// Evaluate outcomes — if one triggers, stop the tree.
	// Outcomes are evaluated when the step passed OR when the step failed
	// but captures were extracted (e.g., tool exited non-zero but produced
	// valid output that was captured).
	outcomeTriggered := false
	hasCapturedData := len(result.Captures) > 0
	if len(step.Outcomes) > 0 && (result.Status == "passed" || hasCapturedData) {
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
						"type":         step.Type,
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
		Index  *int   `json:"index,omitempty"`
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
			"type":   step.Type,
			"error":  err.Error(),
		})
		s.sendResult(msg.ID, map[string]interface{}{"stepId": step.ID, "status": "failed", "error": err.Error()})
		return
	}
	s.treeCursor.stepIdx++

	s.sendEvent("event/stepCompleted", map[string]interface{}{
		"stepId":   step.ID,
		"status":   result.Status,
		"type":     step.Type,
		"captures": result.Captures,
	})

	// Find the chosen outcome — prefer index (disambiguates duplicate states),
	// fall back to matching by state string for backward compatibility.
	var rec string
	var chosenOutcome *schema.Outcome
	var chosenState string
	if params.Index != nil && *params.Index >= 0 && *params.Index < len(step.Outcomes) {
		i := *params.Index
		chosenOutcome = &step.Outcomes[i]
		chosenState = chosenOutcome.State
		rec = s.engine.ResolveTemplatePublic(chosenOutcome.Recommendation)
		if rec == "" || rec == "<no value>" {
			rec = chosenOutcome.Recommendation
		}
		rec = strings.TrimSpace(rec)
	} else {
		chosenState = params.State
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
	}

	// Set the chosen outcome on the engine
	s.engine.SetOutcome(chosenState, step.ID, rec)

	// Clear remaining cursor — outcome terminates the tree
	s.treeCursor.pending = nil

	s.sendEvent("event/outcomeReached", map[string]interface{}{
		"stepId":         step.ID,
		"state":          chosenState,
		"recommendation": rec,
		"nextRunbook":    s.buildNextRunbookInfo(chosenOutcome),
	})

	s.emitSkippedSteps()

	s.sendResult(msg.ID, map[string]interface{}{
		"stepId":         step.ID,
		"status":         "outcome",
		"outcomeState":   chosenState,
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

	// Resolve the runbook reference: imports → project → relative path
	resolvedFile := step.Invoke.Runbook
	if s.runbook.Imports != nil {
		if path, ok := s.runbook.Imports[resolvedFile]; ok {
			resolvedFile = path
		}
	}

	// Resolve template vars in the file path
	resolvedFile = s.engine.ResolveTemplatePublic(resolvedFile)

	// Try project-aware resolution, then fall back to relative path
	if s.engine.Project != nil && !filepath.IsAbs(resolvedFile) {
		if projResolved, err := s.engine.Project.ResolveRunbookRef(resolvedFile); err == nil {
			resolvedFile = projResolved
		} else if s.engine.RunbookPath != "" {
			resolvedFile = filepath.Join(filepath.Dir(s.engine.RunbookPath), resolvedFile)
		}
	} else if s.engine.RunbookPath != "" && !filepath.IsAbs(resolvedFile) {
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
	childEngine.RunbookPath = resolvedFile
	childEngine.ChainDepth = depth
	childEngine.ParentRunID = s.engine.State.RunID

	// Inherit project context from parent
	childEngine.Project = s.engine.Project

	// Load tool definitions for child runbook if it declares tools:
	if len(childRB.Tools) > 0 {
		tm := tools.NewManager(s.engine.Executor, childEngine.Redact)
		childBaseDir := filepath.Dir(resolvedFile)
		for _, name := range childRB.Tools {
			resolved := schema.ResolveToolPathCompat(s.engine.Project, childRB, name, childBaseDir)
			if err := tm.Load(name, resolved, ""); err != nil {
				fmt.Fprintf(os.Stderr, "serve: WARNING failed to load child tool %q: %v\n", name, err)
			}
		}
		childEngine.ToolManager = tm
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
			if n.Iterate != nil {
				ids = append(ids, collectAll(n.Iterate.Steps)...)
				continue
			}
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
			if n.Iterate != nil {
				ids = append(ids, collectTreeIDs(n.Iterate.Steps)...)
				continue
			}
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

// handleSaveScenario saves the current run's inputs and step responses
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

// handleDiagram generates a diagram from a runbook file or the currently loaded runbook.
func (s *Server) handleDiagram(msg *Message) {
	var params struct {
		File   string `json:"file"`
		Format string `json:"format"`
	}
	if msg.Params != nil {
		if err := json.Unmarshal(msg.Params, &params); err != nil {
			s.sendError(msg.ID, -32602, fmt.Sprintf("invalid params: %v", err))
			return
		}
	}

	// Determine runbook source
	var rb *schema.Runbook
	if params.File != "" {
		var err error
		rb, err = schema.LoadFile(params.File)
		if err != nil {
			s.sendError(msg.ID, -32603, fmt.Sprintf("load runbook: %v", err))
			return
		}
	} else if s.runbook != nil {
		rb = s.runbook
	} else {
		s.sendError(msg.ID, -32604, "no runbook specified or loaded")
		return
	}

	format := diagram.FormatMermaid
	if params.Format != "" {
		format = diagram.Format(params.Format)
	}

	out, err := diagram.Generate(rb, format)
	if err != nil {
		s.sendError(msg.ID, -32603, fmt.Sprintf("generate diagram: %v", err))
		return
	}

	s.sendResult(msg.ID, map[string]string{
		"format":  string(format),
		"diagram": out,
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

// flattenTreeSteps collects all steps from tree nodes into a flat list.
// This enables the TUI (and other clients) to show a step list for tree-based runbooks.
// Branch/iterate sub-steps are included recursively; the runtime order may differ
// but having them visible up-front is better than an empty panel.
func flattenTreeSteps(nodes []schema.TreeNode) []schema.Step {
	var steps []schema.Step
	for _, n := range nodes {
		if n.Step.ID != "" {
			steps = append(steps, n.Step)
		}
		for _, b := range n.Branches {
			steps = append(steps, flattenTreeSteps(b.Steps)...)
		}
		if n.Iterate != nil {
			steps = append(steps, flattenTreeSteps(n.Iterate.Steps)...)
		}
	}
	return steps
}

// resolveTreeForDisplay creates a display copy of the tree with templates resolved.
func (s *Server) resolveTreeForDisplay(nodes []schema.TreeNode) []map[string]interface{} {
	result := make([]map[string]interface{}, len(nodes))
	for i, node := range nodes {
		// Handle iterate nodes
		if node.Iterate != nil {
			iterMap := map[string]interface{}{
				"steps": s.resolveTreeForDisplay(node.Iterate.Steps),
			}
			if node.Iterate.Over != "" {
				// List mode
				resolved := s.resolve(node.Iterate.Over)
				var count int
				for _, part := range strings.Split(resolved, ",") {
					if strings.TrimSpace(part) != "" {
						count++
					}
				}
				asVar := node.Iterate.As
				if asVar == "" {
					asVar = "item"
				}
				iterMap["mode"] = "over"
				iterMap["over"] = resolved
				iterMap["as"] = asVar
				iterMap["total"] = count
			} else {
				// Convergence mode
				iterMap["max"] = node.Iterate.Max
				iterMap["until"] = s.resolve(node.Iterate.Until)
			}
			result[i] = map[string]interface{}{
				"iterate": iterMap,
			}
			continue
		}

		step := node.Step
		stepMap := map[string]interface{}{
			"id":    step.ID,
			"type":  step.Type,
			"title": step.Title,
		}
		if step.Instructions != "" {
			stepMap["instructions"] = s.resolve(step.Instructions)
		}
		// Extract query/queryType from tool args for syntax highlighting
		if step.Tool != nil {
			if q, ok := step.Tool.Args["query"]; ok {
				stepMap["query"] = s.resolve(q)
				if qt, ok := step.Tool.Args["query_type"]; ok {
					stepMap["queryType"] = qt
				}
			}
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
