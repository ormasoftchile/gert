package serve

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// ─── buildTreeIndex tests ───────────────────────────────────────────

func TestBuildTreeIndex(t *testing.T) {
	tree := []schema.TreeNode{
		{Step: schema.Step{ID: "step_a", Type: "cli", Title: "A"}},
		{Step: schema.Step{ID: "step_b", Type: "manual", Title: "B"},
			Branches: []schema.Branch{
				{Condition: "true", Label: "branch1",
					Steps: []schema.TreeNode{
						{Step: schema.Step{ID: "step_c", Type: "cli", Title: "C"}},
					}},
			}},
		{Iterate: &schema.IterateBlock{
			Max: 3, Until: "converged == 'true'",
			Steps: []schema.TreeNode{
				{Step: schema.Step{ID: "iter_step", Type: "cli", Title: "Iter"}},
			},
		}},
	}

	idx := buildTreeIndex(tree)

	// Verify step nodes
	for _, id := range []string{"step_a", "step_b", "step_c", "iter_step"} {
		if _, ok := idx.nodes[id]; !ok {
			t.Errorf("step %q not found in tree index", id)
		}
	}

	// Verify iterate block indexed by first step ID
	if _, ok := idx.iterates["iter_step"]; !ok {
		t.Error("iterate block (key 'iter_step') not found in index")
	}

	// Branch step should have correct title
	if idx.nodes["step_c"].Step.Title != "C" {
		t.Errorf("step_c title = %q, want %q", idx.nodes["step_c"].Step.Title, "C")
	}
}

// ─── Serialize/Deserialize round-trip tests ─────────────────────────

func TestSerializeDeserializePendingQueue(t *testing.T) {
	tree := []schema.TreeNode{
		{Step: schema.Step{ID: "s1", Type: "cli", Title: "S1"}},
		{Step: schema.Step{ID: "s2", Type: "manual", Title: "S2"}},
		{Step: schema.Step{ID: "s3", Type: "cli", Title: "S3"},
			Branches: []schema.Branch{
				{Condition: "x == 'y'", Steps: []schema.TreeNode{
					{Step: schema.Step{ID: "s4", Type: "cli", Title: "S4"}},
				}},
			},
		},
	}

	tidx := buildTreeIndex(tree)

	// Build a pending queue: s2, s3, s4 (from branch)
	pending := []pendingNode{
		{node: tree[1], depth: 0},
		{node: tree[2], depth: 0},
		{node: tree[2].Branches[0].Steps[0], depth: 1},
	}

	// Serialize
	refs := serializePendingQueue(pending)
	if len(refs) != 3 {
		t.Fatalf("serialized %d refs, want 3", len(refs))
	}
	if refs[0].Kind != "step" || refs[0].StepID != "s2" {
		t.Errorf("refs[0] = {kind=%q, stepID=%q}, want {step, s2}", refs[0].Kind, refs[0].StepID)
	}
	if refs[2].StepID != "s4" || refs[2].Depth != 1 {
		t.Errorf("refs[2] = {stepID=%q, depth=%d}, want {s4, 1}", refs[2].StepID, refs[2].Depth)
	}

	// Deserialize
	restored, err := deserializePendingQueue(refs, tidx)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	if len(restored) != 3 {
		t.Fatalf("restored %d nodes, want 3", len(restored))
	}
	if restored[0].node.Step.ID != "s2" {
		t.Errorf("restored[0].node.Step.ID = %q, want s2", restored[0].node.Step.ID)
	}
	if restored[2].node.Step.ID != "s4" || restored[2].depth != 1 {
		t.Errorf("restored[2] = {ID=%q, depth=%d}, want {s4, 1}", restored[2].node.Step.ID, restored[2].depth)
	}
}

func TestSerializeDeserializeWatchpoints(t *testing.T) {
	iterBlock := &schema.IterateBlock{
		Max: 5, Until: "done == 'true'",
		Steps: []schema.TreeNode{
			{Step: schema.Step{ID: "it1", Type: "cli", Title: "IterStep"}},
		},
	}
	tree := []schema.TreeNode{
		{Iterate: iterBlock},
		{Step: schema.Step{ID: "after_iter", Type: "manual", Title: "After"}},
	}

	tidx := buildTreeIndex(tree)

	// Convergence watchpoint
	pending := []pendingNode{
		{node: tree[0].Iterate.Steps[0], depth: 1},
		{watchpoint: &iterateWatchpoint{block: iterBlock, pass: 2, max: 5}, depth: 0},
		{node: tree[1], depth: 0},
	}

	refs := serializePendingQueue(pending)
	if refs[1].Kind != "convergence_wp" {
		t.Errorf("refs[1].Kind = %q, want convergence_wp", refs[1].Kind)
	}
	if refs[1].Pass != 2 || refs[1].Max != 5 {
		t.Errorf("refs[1] pass=%d max=%d, want 2 and 5", refs[1].Pass, refs[1].Max)
	}
	if refs[1].IterateFirstStepID != "it1" {
		t.Errorf("refs[1].IterateFirstStepID = %q, want it1", refs[1].IterateFirstStepID)
	}

	restored, err := deserializePendingQueue(refs, tidx)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	wp := restored[1].watchpoint
	if wp == nil {
		t.Fatal("restored[1].watchpoint is nil")
	}
	if wp.pass != 2 || wp.max != 5 {
		t.Errorf("watchpoint pass=%d max=%d, want 2 and 5", wp.pass, wp.max)
	}
	if wp.block != iterBlock {
		t.Error("watchpoint.block doesn't point to the original iterate block")
	}
}

func TestSerializeDeserializeOverWatchpoint(t *testing.T) {
	iterBlock := &schema.IterateBlock{
		Over: "a,b,c", As: "db",
		Steps: []schema.TreeNode{
			{Step: schema.Step{ID: "ov1", Type: "tool", Title: "OverStep"}},
		},
	}
	tree := []schema.TreeNode{{Iterate: iterBlock}}
	tidx := buildTreeIndex(tree)

	pending := []pendingNode{
		{overWatchpoint: &iterateOverWatchpoint{
			block: iterBlock, items: []string{"a", "b", "c"}, index: 1, asVar: "db",
		}, depth: 0},
	}

	refs := serializePendingQueue(pending)
	if refs[0].Kind != "over_wp" {
		t.Fatalf("refs[0].Kind = %q, want over_wp", refs[0].Kind)
	}
	if refs[0].AsVar != "db" || refs[0].Index != 1 {
		t.Errorf("refs[0] asVar=%q index=%d, want db and 1", refs[0].AsVar, refs[0].Index)
	}

	restored, err := deserializePendingQueue(refs, tidx)
	if err != nil {
		t.Fatalf("deserialize: %v", err)
	}
	ow := restored[0].overWatchpoint
	if ow == nil {
		t.Fatal("restored[0].overWatchpoint is nil")
	}
	if ow.asVar != "db" || ow.index != 1 || len(ow.items) != 3 {
		t.Errorf("overWatchpoint asVar=%q index=%d items=%v, want db/1/[a b c]", ow.asVar, ow.index, ow.items)
	}
}

// ─── Session file I/O tests ────────────────────────────────────────

func TestWriteLoadSessionFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "session.json")

	session := &SessionState{
		RunID:       "20260224T100000-abcd",
		RunbookPath: "/path/to/runbook.yaml",
		Mode:        "real",
		Actor:       "test-user",
		StartedAt:   time.Date(2026, 2, 24, 10, 0, 0, 0, time.UTC),
		Vars:        map[string]string{"server": "sql01", "db": "mydb"},
		Captures:    map[string]string{"cause": "HasDumps"},
		History: []*providers.StepResult{
			{StepID: "s1", Status: "passed", Captures: map[string]string{"x": "1"}},
			{StepID: "s2", Status: "skipped"},
		},
		StepIdx: 5,
		Pending: []PendingNodeRef{
			{Kind: "step", StepID: "s3", Depth: 0},
			{Kind: "step", StepID: "s4", Depth: 1},
		},
		PendingManual: &PendingNodeRef{Kind: "step", StepID: "s5", Depth: 0},
		InvokeStack: []InvokeFrameRef{
			{
				RunbookPath:  "/parent.yaml",
				RunID:        "run-parent",
				BaseDir:      ".runbook/runs/run-parent",
				InvokeStepID: "invoke_child",
				ChainDepth:   0,
				Vars:         map[string]string{"a": "b"},
				Captures:     map[string]string{"c": "d"},
				StepIdx:      3,
				Pending:      []PendingNodeRef{{Kind: "step", StepID: "ps1", Depth: 0}},
			},
		},
	}

	if err := writeSessionFile(session, path); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Verify file exists
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("stat: %v", err)
	}

	// Load back
	loaded, err := loadSessionFile(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}

	if loaded.RunID != session.RunID {
		t.Errorf("RunID = %q, want %q", loaded.RunID, session.RunID)
	}
	if loaded.Mode != "real" {
		t.Errorf("Mode = %q, want real", loaded.Mode)
	}
	if loaded.Vars["server"] != "sql01" {
		t.Errorf("Vars[server] = %q, want sql01", loaded.Vars["server"])
	}
	if len(loaded.History) != 2 {
		t.Errorf("len(History) = %d, want 2", len(loaded.History))
	}
	if loaded.StepIdx != 5 {
		t.Errorf("StepIdx = %d, want 5", loaded.StepIdx)
	}
	if len(loaded.Pending) != 2 {
		t.Errorf("len(Pending) = %d, want 2", len(loaded.Pending))
	}
	if loaded.PendingManual == nil || loaded.PendingManual.StepID != "s5" {
		t.Error("PendingManual not restored correctly")
	}
	if len(loaded.InvokeStack) != 1 {
		t.Fatalf("len(InvokeStack) = %d, want 1", len(loaded.InvokeStack))
	}
	if loaded.InvokeStack[0].InvokeStepID != "invoke_child" {
		t.Errorf("InvokeStack[0].InvokeStepID = %q, want invoke_child", loaded.InvokeStack[0].InvokeStepID)
	}
}

// ─── cloneMap test ──────────────────────────────────────────────────

func TestCloneMap(t *testing.T) {
	orig := map[string]string{"a": "1", "b": "2"}
	cp := cloneMap(orig)

	if len(cp) != 2 || cp["a"] != "1" || cp["b"] != "2" {
		t.Errorf("clone = %v, want %v", cp, orig)
	}

	// Mutate clone — original should be unchanged
	cp["a"] = "changed"
	if orig["a"] != "1" {
		t.Error("mutating clone affected original")
	}

	// nil input
	if cloneMap(nil) != nil {
		t.Error("cloneMap(nil) should return nil")
	}
}

// ─── JSON round-trip test ───────────────────────────────────────────

func TestSessionStateJSON(t *testing.T) {
	// Verify the session state survives a full JSON round-trip
	session := &SessionState{
		RunID:       "run-1",
		RunbookPath: "test.yaml",
		Mode:        "replay",
		StartedAt:   time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC),
		Vars:        map[string]string{"x": "1"},
		Captures:    map[string]string{"y": "2"},
		StepIdx:     10,
		Pending: []PendingNodeRef{
			{Kind: "convergence_wp", Depth: 0, IterateFirstStepID: "it1", Pass: 3, Max: 5},
			{Kind: "over_wp", Depth: 1, IterateFirstStepID: "ov1", Items: []string{"a", "b"}, Index: 1, AsVar: "item"},
		},
	}

	data, err := json.Marshal(session)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}

	var restored SessionState
	if err := json.Unmarshal(data, &restored); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}

	if restored.StepIdx != 10 {
		t.Errorf("StepIdx = %d, want 10", restored.StepIdx)
	}
	if len(restored.Pending) != 2 {
		t.Fatalf("len(Pending) = %d, want 2", len(restored.Pending))
	}
	if restored.Pending[0].Pass != 3 || restored.Pending[0].Max != 5 {
		t.Error("convergence watchpoint not preserved")
	}
	if restored.Pending[1].AsVar != "item" || restored.Pending[1].Index != 1 {
		t.Error("over watchpoint not preserved")
	}
}
