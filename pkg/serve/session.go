package serve

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// ─── Serializable types ─────────────────────────────────────────────

// SessionState captures the full serializable state of a serve-mode tree execution.
// Written to .runbook/runs/<root_run_id>/session.json after each mutation.
type SessionState struct {
	RunID             string                  `json:"run_id"`
	RunbookPath       string                  `json:"runbook_path"`
	ActiveRunbookPath string                  `json:"active_runbook_path,omitempty"` // differs when inside invoke
	Mode              string                  `json:"mode"`
	Actor             string                  `json:"actor,omitempty"`
	StartedAt         time.Time               `json:"started_at"`
	Cwd               string                  `json:"cwd,omitempty"`
	Vars              map[string]string       `json:"vars"`
	Captures          map[string]string       `json:"captures"`
	History           []*providers.StepResult `json:"history"`

	// Tree cursor
	StepIdx       int              `json:"step_idx"`
	Pending       []PendingNodeRef `json:"pending"`
	PendingManual *PendingNodeRef  `json:"pending_manual,omitempty"`

	// Invoke stack (outermost parent first)
	InvokeStack []InvokeFrameRef `json:"invoke_stack,omitempty"`
}

// PendingNodeRef is a serializable reference to a pending tree cursor node.
// Step nodes are identified by step ID; iterate watchpoints by the first
// child step ID of their block.
type PendingNodeRef struct {
	Kind   string `json:"kind"` // "step", "convergence_wp", "over_wp"
	StepID string `json:"step_id,omitempty"`
	Depth  int    `json:"depth"`

	// Iterate block key (first child step ID)
	IterateFirstStepID string `json:"iterate_fsid,omitempty"`

	// Convergence watchpoint
	Pass int `json:"pass,omitempty"`
	Max  int `json:"max,omitempty"`

	// Over-watchpoint
	Items []string `json:"items,omitempty"`
	Index int      `json:"idx,omitempty"`
	AsVar string   `json:"as_var,omitempty"`
}

// InvokeFrameRef is a serializable invoke stack frame capturing parent state.
type InvokeFrameRef struct {
	RunbookPath  string            `json:"runbook_path"`
	RunID        string            `json:"run_id"`
	BaseDir      string            `json:"base_dir"`
	InvokeStepID string            `json:"invoke_step_id"`
	Gate         *schema.Gate      `json:"gate,omitempty"`
	Capture      map[string]string `json:"capture,omitempty"`
	ChainDepth   int               `json:"chain_depth"`
	Vars         map[string]string `json:"vars"`
	Captures     map[string]string `json:"captures"`
	StepIdx      int               `json:"step_idx"`
	Pending      []PendingNodeRef  `json:"pending"`
}

// ─── Session save ───────────────────────────────────────────────────

// saveSession persists the current server state to session.json in the root
// run directory. Called after each mutation so the run can be resumed later.
func (s *Server) saveSession() {
	if s.engine == nil {
		return
	}
	if s.treeCursor == nil && s.pendingManual == nil {
		return
	}
	baseDir := s.rootBaseDir
	if baseDir == "" {
		return
	}
	session := s.buildSessionState()
	path := filepath.Join(baseDir, "session.json")
	if err := writeSessionFile(session, path); err != nil {
		fmt.Fprintf(os.Stderr, "serve: session save error: %v\n", err)
	}
}

func (s *Server) buildSessionState() *SessionState {
	session := &SessionState{
		RunID:       s.engine.GetRunID(),
		RunbookPath: s.engine.RunbookPath,
		Mode:        s.engine.State.Mode,
		Actor:       s.engine.State.Actor,
		StartedAt:   s.engine.State.StartedAt,
		Vars:        cloneMap(s.engine.State.Vars),
		Captures:    cloneMap(s.engine.State.Captures),
		History:     s.engine.State.History,
	}

	if cwd, err := os.Getwd(); err == nil {
		session.Cwd = cwd
	}

	if s.treeCursor != nil {
		session.StepIdx = s.treeCursor.stepIdx
		session.Pending = serializePendingQueue(s.treeCursor.pending)
	}

	if s.pendingManual != nil {
		ref := serializePendingNode(*s.pendingManual)
		session.PendingManual = &ref
	}

	for _, frame := range s.invokeStack {
		session.InvokeStack = append(session.InvokeStack, InvokeFrameRef{
			RunbookPath:  frame.parentEngine.RunbookPath,
			RunID:        frame.parentEngine.GetRunID(),
			BaseDir:      frame.parentEngine.GetBaseDir(),
			InvokeStepID: frame.invokeStepID,
			Gate:         frame.gate,
			Capture:      frame.capture,
			ChainDepth:   frame.parentEngine.ChainDepth,
			Vars:         cloneMap(frame.parentEngine.State.Vars),
			Captures:     cloneMap(frame.parentEngine.State.Captures),
			StepIdx:      frame.parentCursor.stepIdx,
			Pending:      serializePendingQueue(frame.parentCursor.pending),
		})
	}

	// When inside an invoke, adjust top-level IDs to root run
	if len(s.invokeStack) > 0 {
		session.ActiveRunbookPath = s.engine.RunbookPath
		session.RunID = s.invokeStack[0].parentEngine.GetRunID()
		session.RunbookPath = s.invokeStack[0].parentEngine.RunbookPath
	}

	return session
}

// ─── Serialization helpers ──────────────────────────────────────────

func serializePendingQueue(pending []pendingNode) []PendingNodeRef {
	refs := make([]PendingNodeRef, 0, len(pending))
	for _, pn := range pending {
		refs = append(refs, serializePendingNode(pn))
	}
	return refs
}

func serializePendingNode(pn pendingNode) PendingNodeRef {
	if pn.watchpoint != nil {
		return PendingNodeRef{
			Kind:               "convergence_wp",
			Depth:              pn.depth,
			IterateFirstStepID: iterateBlockKey(pn.watchpoint.block),
			Pass:               pn.watchpoint.pass,
			Max:                pn.watchpoint.max,
		}
	}
	if pn.overWatchpoint != nil {
		return PendingNodeRef{
			Kind:               "over_wp",
			Depth:              pn.depth,
			IterateFirstStepID: iterateBlockKey(pn.overWatchpoint.block),
			Items:              pn.overWatchpoint.items,
			Index:              pn.overWatchpoint.index,
			AsVar:              pn.overWatchpoint.asVar,
		}
	}
	return PendingNodeRef{
		Kind:   "step",
		StepID: pn.node.Step.ID,
		Depth:  pn.depth,
	}
}

func iterateBlockKey(block *schema.IterateBlock) string {
	if block != nil && len(block.Steps) > 0 && block.Steps[0].Step.ID != "" {
		return block.Steps[0].Step.ID
	}
	return ""
}

// ─── Deserialization ────────────────────────────────────────────────

// treeIndex maps step IDs to their TreeNode values and iterate block first-step
// IDs to their IterateBlock pointers, enabling cursor reconstruction from a
// serialized session.
type treeIndex struct {
	nodes    map[string]schema.TreeNode
	iterates map[string]*schema.IterateBlock
}

func buildTreeIndex(tree []schema.TreeNode) *treeIndex {
	idx := &treeIndex{
		nodes:    make(map[string]schema.TreeNode),
		iterates: make(map[string]*schema.IterateBlock),
	}
	idx.walk(tree)
	return idx
}

func (idx *treeIndex) walk(nodes []schema.TreeNode) {
	for i := range nodes {
		n := &nodes[i]
		if n.Iterate != nil {
			key := iterateBlockKey(n.Iterate)
			if key != "" {
				idx.iterates[key] = n.Iterate
			}
			idx.walk(n.Iterate.Steps)
			continue
		}
		if n.Step.ID != "" {
			idx.nodes[n.Step.ID] = *n
		}
		for _, b := range n.Branches {
			idx.walk(b.Steps)
		}
	}
}

func deserializePendingQueue(refs []PendingNodeRef, tidx *treeIndex) ([]pendingNode, error) {
	nodes := make([]pendingNode, 0, len(refs))
	for _, ref := range refs {
		pn, err := deserializePendingNode(ref, tidx)
		if err != nil {
			return nil, err
		}
		nodes = append(nodes, pn)
	}
	return nodes, nil
}

func deserializePendingNode(ref PendingNodeRef, tidx *treeIndex) (pendingNode, error) {
	switch ref.Kind {
	case "step":
		n, ok := tidx.nodes[ref.StepID]
		if !ok {
			return pendingNode{}, fmt.Errorf("step %q not found in tree", ref.StepID)
		}
		return pendingNode{node: n, depth: ref.Depth}, nil

	case "convergence_wp":
		block, ok := tidx.iterates[ref.IterateFirstStepID]
		if !ok {
			return pendingNode{}, fmt.Errorf("iterate block (key %q) not found", ref.IterateFirstStepID)
		}
		return pendingNode{
			depth:      ref.Depth,
			watchpoint: &iterateWatchpoint{block: block, pass: ref.Pass, max: ref.Max},
		}, nil

	case "over_wp":
		block, ok := tidx.iterates[ref.IterateFirstStepID]
		if !ok {
			return pendingNode{}, fmt.Errorf("iterate block (key %q) not found", ref.IterateFirstStepID)
		}
		return pendingNode{
			depth: ref.Depth,
			overWatchpoint: &iterateOverWatchpoint{
				block: block, items: ref.Items, index: ref.Index, asVar: ref.AsVar,
			},
		}, nil

	default:
		return pendingNode{}, fmt.Errorf("unknown pending node kind %q", ref.Kind)
	}
}

// ─── File I/O ───────────────────────────────────────────────────────

func writeSessionFile(session *SessionState, path string) error {
	data, err := json.MarshalIndent(session, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal session: %w", err)
	}
	return os.WriteFile(path, data, 0644)
}

func loadSessionFile(path string) (*SessionState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read session: %w", err)
	}
	var session SessionState
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, fmt.Errorf("parse session: %w", err)
	}
	return &session, nil
}

// ─── Helpers ────────────────────────────────────────────────────────

func cloneMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	cp := make(map[string]string, len(m))
	for k, v := range m {
		cp[k] = v
	}
	return cp
}
