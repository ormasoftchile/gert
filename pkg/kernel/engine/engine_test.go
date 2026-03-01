package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
	"github.com/ormasoftchile/gert/pkg/kernel/executor"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
)

func TestEngine_SimpleEndStep(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta: schema.Meta{
			Name: "test",
		},
		Steps: []schema.Step{
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "test-run")

	eng := New(rb, RunConfig{
		RunID: "test-run",
		Mode:  "real",
		Trace: tw,
	})

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, want completed", result.Status)
	}
	if result.Outcome == nil {
		t.Fatal("expected outcome")
	}
	if result.Outcome.Category != schema.OutcomeResolved {
		t.Errorf("category = %q", result.Outcome.Category)
	}
	if result.Outcome.Code != "done" {
		t.Errorf("code = %q", result.Outcome.Code)
	}

	// Verify trace contains events
	traceStr := traceBuf.String()
	if !strings.Contains(traceStr, "run_start") {
		t.Error("trace missing run_start")
	}
	if !strings.Contains(traceStr, "outcome_resolved") {
		t.Error("trace missing outcome_resolved")
	}
	if !strings.Contains(traceStr, "run_complete") {
		t.Error("trace missing run_complete")
	}
}

func TestEngine_AssertPass(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta: schema.Meta{
			Name:   "test",
			Inputs: map[string]contract.ParamDef{"status": {Type: "string"}},
		},
		Steps: []schema.Step{
			{
				ID:   "check",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .status }}", Expected: "200"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "ok",
				},
			},
		},
	}

	// Supply vars directly.

	eng := New(rb, RunConfig{
		RunID: "test-run",
		Mode:  "real",
		Vars:  map[string]string{"status": "200"},
	})

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
}

func TestEngine_AssertFail_Halts(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "check",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .status }}", Expected: "200"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "ok",
				},
			},
		},
	}

	eng := New(rb, RunConfig{
		RunID: "test-run",
		Mode:  "real",
		Vars:  map[string]string{"status": "503"},
	})

	result := eng.Run(context.Background())
	if result.Status != "failed" {
		t.Errorf("status = %q, want failed", result.Status)
	}
}

func TestEngine_AssertFail_ContinueOnFail(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:             "check",
				Type:           schema.StepAssert,
				ContinueOnFail: true,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .status }}", Expected: "200"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeEscalated,
					Code:     "failed_check",
				},
			},
		},
	}

	eng := New(rb, RunConfig{
		RunID: "test-run",
		Mode:  "real",
		Vars:  map[string]string{"status": "503"},
	})

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, want completed (continue_on_fail)", result.Status)
	}
	if result.Outcome.Category != schema.OutcomeEscalated {
		t.Errorf("category = %q", result.Outcome.Category)
	}
}

func TestEngine_BranchExecution(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				Type: schema.StepBranch,
				Branches: []schema.Branch{
					{
						Condition: `{{ eq .status "200" }}`,
						Label:     "healthy",
						Steps: []schema.Step{
							{Type: schema.StepEnd, Outcome: &schema.Outcome{Category: schema.OutcomeNoAction, Code: "healthy"}},
						},
					},
					{
						Condition: "default",
						Label:     "unhealthy",
						Steps: []schema.Step{
							{Type: schema.StepEnd, Outcome: &schema.Outcome{Category: schema.OutcomeEscalated, Code: "broken"}},
						},
					},
				},
			},
		},
	}

	// Test healthy branch
	eng := New(rb, RunConfig{RunID: "r1", Mode: "real", Vars: map[string]string{"status": "200"}})
	result := eng.Run(context.Background())
	if result.Outcome.Code != "healthy" {
		t.Errorf("expected healthy branch, got %q", result.Outcome.Code)
	}

	// Test default branch
	eng2 := New(rb, RunConfig{RunID: "r2", Mode: "real", Vars: map[string]string{"status": "503"}})
	result2 := eng2.Run(context.Background())
	if result2.Outcome.Code != "broken" {
		t.Errorf("expected default branch, got %q", result2.Outcome.Code)
	}
}

func TestEngine_WhenGuard(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "guarded",
				Type: schema.StepAssert,
				When: `{{ eq .run "yes" }}`,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "a", Expected: "a"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	// Guard is false → step skipped, execution continues to end
	eng := New(rb, RunConfig{RunID: "r1", Mode: "real", Vars: map[string]string{"run": "no"}})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, want completed", result.Status)
	}
}

func TestEngine_Constants(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta: schema.Meta{
			Name:      "test",
			Constants: map[string]any{"endpoint": "/healthz"},
		},
		Steps: []schema.Step{
			{
				ID:   "check",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .endpoint }}", Expected: "/healthz"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
}

func TestEngine_GovernanceDeny(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta: schema.Meta{
			Name: "test",
			Governance: &schema.GovernancePolicy{
				Rules: []schema.GovernanceRule{
					{Risk: "critical", Action: "deny"},
				},
			},
		},
		Steps: []schema.Step{
			{
				ID:           "dangerous",
				Type:         schema.StepManual,
				Instructions: "Do something dangerous",
				// Manual defaults: side_effects=true, deterministic=false, idempotent=false → critical risk
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "failed" {
		t.Errorf("status = %q, want failed (governance deny)", result.Status)
	}
}

func TestEngine_DryRun(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:           "manual",
				Type:         schema.StepManual,
				Instructions: "Would need input in real mode",
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	var out bytes.Buffer
	eng := New(rb, RunConfig{
		RunID:  "r1",
		Mode:   "dry-run",
		Stdout: &out,
	})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
}

func TestEngine_OutcomeMeta_TemplateResolution(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
					Meta:     map[string]any{"host": "{{ .hostname }}"},
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real", Vars: map[string]string{"hostname": "srv1"}})
	result := eng.Run(context.Background())
	if result.Outcome.Meta["host"] != "srv1" {
		t.Errorf("meta.host = %v, want srv1", result.Outcome.Meta["host"])
	}
}

// Test that the Meta.Inputs field uses contract.ParamDef properly
func TestEngine_InputDefaults(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta: schema.Meta{
			Name: "test",
			Inputs: map[string]contract.ParamDef{
				"threshold": {Type: "int", Default: "200"},
			},
		},
		Steps: []schema.Step{
			{
				ID:   "check",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .threshold }}", Expected: "200"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
}

// ---------------------------------------------------------------------------
// Phase 4 Tests: Parallel, ForEach, Next/Goto
// ---------------------------------------------------------------------------

func TestEngine_ParallelExecution(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "par",
				Type: schema.StepParallel,
				Branches: []schema.Branch{
					{
						Label: "branch_a",
						Steps: []schema.Step{
							{
								ID:   "a_check",
								Type: schema.StepAssert,
								Assert: []schema.Assertion{
									{Type: "equals", Value: "a", Expected: "a"},
								},
							},
						},
					},
					{
						Label: "branch_b",
						Steps: []schema.Step{
							{
								ID:   "b_check",
								Type: schema.StepAssert,
								Assert: []schema.Assertion{
									{Type: "equals", Value: "b", Expected: "b"},
								},
							},
						},
					},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "both_passed",
				},
			},
		},
	}

	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "r1")
	eng := New(rb, RunConfig{RunID: "r1", Mode: "real", Trace: tw})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
	if result.Outcome.Code != "both_passed" {
		t.Errorf("code = %q", result.Outcome.Code)
	}

	traceStr := traceBuf.String()
	if !strings.Contains(traceStr, "parallel_fork") {
		t.Error("trace missing parallel_fork")
	}
	if !strings.Contains(traceStr, "parallel_merge") {
		t.Error("trace missing parallel_merge")
	}
}

func TestEngine_ParallelBranchFailure(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "par",
				Type: schema.StepParallel,
				Branches: []schema.Branch{
					{
						Label: "good",
						Steps: []schema.Step{
							{
								ID:   "ok",
								Type: schema.StepAssert,
								Assert: []schema.Assertion{
									{Type: "equals", Value: "a", Expected: "a"},
								},
							},
						},
					},
					{
						Label: "bad",
						Steps: []schema.Step{
							{
								ID:   "fail",
								Type: schema.StepAssert,
								Assert: []schema.Assertion{
									{Type: "equals", Value: "x", Expected: "y"},
								},
							},
						},
					},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "failed" {
		t.Errorf("status = %q, want failed", result.Status)
	}
}

func TestEngine_ParallelConflictSerialization(t *testing.T) {
	boolTrue := true
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "par",
				Type: schema.StepParallel,
				Branches: []schema.Branch{
					{
						Label: "writer_a",
						Steps: []schema.Step{
							{
								ID:   "a",
								Type: schema.StepAssert,
								Contract: &contract.Contract{
									SideEffects: &boolTrue,
									Writes:      []string{"service"},
								},
								Assert: []schema.Assertion{
									{Type: "equals", Value: "1", Expected: "1"},
								},
							},
						},
					},
					{
						Label: "writer_b",
						Steps: []schema.Step{
							{
								ID:   "b",
								Type: schema.StepAssert,
								Contract: &contract.Contract{
									SideEffects: &boolTrue,
									Writes:      []string{"service"},
								},
								Assert: []schema.Assertion{
									{Type: "equals", Value: "2", Expected: "2"},
								},
							},
						},
					},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "r1")
	eng := New(rb, RunConfig{RunID: "r1", Mode: "real", Trace: tw})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	if !strings.Contains(traceBuf.String(), `"serialized":true`) {
		t.Error("expected serialized=true in trace for conflicting branches")
	}
}

func TestEngine_ForEachSequential(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "check_all",
				Type: schema.StepAssert,
				ForEach: &schema.ForEach{
					As:   "item",
					Over: "{{ .items }}",
				},
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .item }}", Expected: "{{ .item }}"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	eng.vars["items"] = []any{"a", "b", "c"}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	accumulated, ok := eng.vars["check_all"].([]any)
	if !ok {
		t.Fatalf("expected accumulated list, got %T", eng.vars["check_all"])
	}
	if len(accumulated) != 3 {
		t.Errorf("accumulated %d items, want 3", len(accumulated))
	}
}

func TestEngine_ForEachParallel(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "check_par",
				Type: schema.StepAssert,
				ForEach: &schema.ForEach{
					As:       "item",
					Over:     "{{ .items }}",
					Parallel: true,
				},
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .item }}", Expected: "{{ .item }}"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	eng.vars["items"] = []any{"x", "y", "z"}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	accumulated, ok := eng.vars["check_par"].([]any)
	if !ok {
		t.Fatalf("expected accumulated list, got %T", eng.vars["check_par"])
	}
	if len(accumulated) != 3 {
		t.Errorf("accumulated %d items, want 3", len(accumulated))
	}
}

func TestEngine_NextBackwardMaxEnforced(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "target",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "a", Expected: "a"},
				},
			},
			{
				ID:   "jumper",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "b", Expected: "b"},
				},
				Next: map[string]any{"step": "target", "max": 2},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
	if result.Outcome.Code != "done" {
		t.Errorf("code = %q", result.Outcome.Code)
	}

	rc, ok := eng.vars["target.retry_count"].(int)
	if !ok {
		t.Fatalf("expected retry_count int, got %T", eng.vars["target.retry_count"])
	}
	if rc < 2 {
		t.Errorf("retry_count = %d, expected >= 2", rc)
	}
}

func TestEngine_NextForwardJump(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "skipper",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "a", Expected: "a"},
				},
				Next: "finish",
			},
			{
				ID:   "should_skip",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "x", Expected: "y"},
				},
			},
			{
				ID:   "finish",
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "skipped_ahead",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
	if result.Outcome.Code != "skipped_ahead" {
		t.Errorf("code = %q, want skipped_ahead", result.Outcome.Code)
	}
}

func TestEngine_ForEachWithTrace(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "iter",
				Type: schema.StepAssert,
				ForEach: &schema.ForEach{
					As:   "n",
					Over: "{{ .nums }}",
				},
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .n }}", Expected: "{{ .n }}"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "r1")
	eng := New(rb, RunConfig{RunID: "r1", Mode: "real", Trace: tw})
	eng.vars["nums"] = []any{"1", "2"}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	traceStr := traceBuf.String()
	if !strings.Contains(traceStr, "for_each_start") {
		t.Error("trace missing for_each_start")
	}
	if !strings.Contains(traceStr, "for_each_item") {
		t.Error("trace missing for_each_item")
	}
}

// T056: for_each.key produces map-structured outputs
func TestEngine_ForEachKeyed(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "keyed",
				Type: schema.StepAssert,
				ForEach: &schema.ForEach{
					As:   "item",
					Over: "{{ .items }}",
					Key:  "{{ .item }}",
				},
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .item }}", Expected: "{{ .item }}"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	eng.vars["items"] = []any{"alpha", "beta", "gamma"}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	// Keyed output should be a map
	keyed, ok := eng.vars["keyed"].(map[string]any)
	if !ok {
		t.Fatalf("expected map output, got %T: %v", eng.vars["keyed"], eng.vars["keyed"])
	}
	if len(keyed) != 3 {
		t.Errorf("keyed map has %d entries, want 3", len(keyed))
	}
	if _, ok := keyed["alpha"]; !ok {
		t.Error("missing key 'alpha'")
	}
	if _, ok := keyed["beta"]; !ok {
		t.Error("missing key 'beta'")
	}
}

// T057: for_each.key duplicate keys → runtime error
func TestEngine_ForEachKey_DuplicateError(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "duped",
				Type: schema.StepAssert,
				ForEach: &schema.ForEach{
					As:   "item",
					Over: "{{ .items }}",
					Key:  "same_key",
				},
				Assert: []schema.Assertion{
					{Type: "equals", Value: "a", Expected: "a"},
				},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	eng.vars["items"] = []any{"a", "b"}

	result := eng.Run(context.Background())
	if result.Status != "error" {
		t.Errorf("status = %q, want error for duplicate keys", result.Status)
	}
}

// mockToolExecutor implements ToolExecutor for tests.
type mockToolExecutor struct {
	result *executor.Result
	err    error
}

func (m *mockToolExecutor) Execute(ctx context.Context, toolDef *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*executor.Result, error) {
	return m.result, m.err
}

// T126: Contract violation detection — undeclared outputs
func TestEngine_ContractViolation_UndeclaredOutput(t *testing.T) {
	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "r1")

	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:     "check",
				Type:   schema.StepTool,
				Tool:   "test-tool",
				Action: "run",
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}

	eng := New(rb, RunConfig{
		RunID: "r1",
		Mode:  "real",
		Trace: tw,
		ToolExec: &mockToolExecutor{
			result: &executor.Result{
				ExitCode: 0,
				Outputs:  map[string]any{"status": "ok", "extra": "undeclared"},
			},
		},
	})
	// Register tool with contract that only declares "status" output
	eng.tools["test-tool"] = &schema.ToolDefinition{
		Meta: schema.ToolMeta{Name: "test-tool"},
		Actions: map[string]schema.ToolAction{
			"run": {},
		},
		Contract: contract.Contract{
			Outputs: map[string]contract.ParamDef{
				"status": {Type: "string"},
			},
		},
	}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	// Check trace for contract_violation event
	traceOutput := traceBuf.String()
	if !strings.Contains(traceOutput, "contract_violation") {
		t.Error("expected contract_violation event in trace")
	}
	if !strings.Contains(traceOutput, "undeclared_output") {
		t.Error("expected undeclared_output violation in trace")
	}
}

// T128: Probe mode — writes skipped, read-only executed
func TestEngine_ProbeMode_SkipsWrites(t *testing.T) {
	var out bytes.Buffer

	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:     "write_step",
				Type:   schema.StepTool,
				Tool:   "write-tool",
				Action: "write",
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}

	eng := New(rb, RunConfig{
		RunID:  "r1",
		Mode:   "probe",
		Stdout: &out,
		ToolExec: &mockToolExecutor{
			result: &executor.Result{ExitCode: 0, Outputs: map[string]any{}},
		},
	})
	// Tool with writes (non-read-only)
	eng.tools["write-tool"] = &schema.ToolDefinition{
		Meta: schema.ToolMeta{Name: "write-tool"},
		Actions: map[string]schema.ToolAction{
			"write": {},
		},
		Contract: contract.Contract{
			Effects: []string{"filesystem"},
			Writes:  []string{"files"},
		},
	}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
	if !strings.Contains(out.String(), "[probe] SKIP") {
		t.Errorf("expected probe skip output, got: %s", out.String())
	}
}

// T053: Scope field normalizes `/` to `.` (tested via loader, verified here via engine)
func TestEngine_ScopeNormalization(t *testing.T) {
	// The loader normalizes scope paths. Verify scoped step runs and vars are tagged.
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:    "scoped",
				Type:  schema.StepAssert,
				Scope: "round.0", // already normalized (loader converts / to .)
				Assert: []schema.Assertion{
					{Type: "equals", Value: "a", Expected: "a"},
				},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}
	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
}

// T054: Export promotes scope-local outputs to global
func TestEngine_ExportPromotion(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:     "producer",
				Type:   schema.StepTool,
				Tool:   "test-tool",
				Action: "run",
				Scope:  "local",
				Export: []string{"status"},
			},
			{
				// This step uses the exported value
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .status }}", Expected: "promoted"},
				},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}
	eng := New(rb, RunConfig{
		RunID: "r1",
		Mode:  "real",
		ToolExec: &mockToolExecutor{
			result: &executor.Result{
				ExitCode: 0,
				Outputs:  map[string]any{"status": "promoted"},
			},
		},
	})
	eng.tools["test-tool"] = &schema.ToolDefinition{
		Meta:    schema.ToolMeta{Name: "test-tool"},
		Actions: map[string]schema.ToolAction{"run": {}},
		Contract: contract.Contract{
			Outputs: map[string]contract.ParamDef{"status": {Type: "string"}},
		},
	}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
	// Verify the exported var is accessible globally
	if eng.Vars()["status"] != "promoted" {
		t.Errorf("exported status = %v, want 'promoted'", eng.Vars()["status"])
	}
}

// T055: Export collision with existing global → value is overwritten (scope exit restores, then export re-sets)
func TestEngine_ExportCollision(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:     "producer",
				Type:   schema.StepTool,
				Tool:   "test-tool",
				Action: "run",
				Scope:  "local",
				Export: []string{"status"},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}
	eng := New(rb, RunConfig{
		RunID: "r1",
		Mode:  "real",
		ToolExec: &mockToolExecutor{
			result: &executor.Result{ExitCode: 0, Outputs: map[string]any{"status": "new"}},
		},
	})
	eng.tools["test-tool"] = &schema.ToolDefinition{
		Meta:    schema.ToolMeta{Name: "test-tool"},
		Actions: map[string]schema.ToolAction{"run": {}},
		Contract: contract.Contract{
			Outputs: map[string]contract.ParamDef{"status": {Type: "string"}},
		},
	}
	// Pre-set the global var
	eng.vars["status"] = "old"

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
	// Export should overwrite the old value
	if eng.Vars()["status"] != "new" {
		t.Errorf("exported status = %v, want 'new'", eng.Vars()["status"])
	}
}

// T058: visibility_applied trace event emitted
func TestEngine_VisibilityApplied_TraceEvent(t *testing.T) {
	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "r1")

	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "visible_step",
				Type: schema.StepAssert,
				Visibility: &schema.Visibility{
					Allow: []string{"question"},
					Deny:  []string{"scope.round.0.*"},
				},
				Assert: []schema.Assertion{
					{Type: "equals", Value: "a", Expected: "a"},
				},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}
	eng := New(rb, RunConfig{RunID: "r1", Mode: "real", Trace: tw})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	traceOutput := traceBuf.String()
	if !strings.Contains(traceOutput, "visibility_applied") {
		t.Error("expected visibility_applied event in trace")
	}
	if !strings.Contains(traceOutput, "question") {
		t.Error("expected allow pattern 'question' in trace event")
	}
}

// T059: scope_export trace event emitted
func TestEngine_ScopeExport_TraceEvent(t *testing.T) {
	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "r1")

	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:     "producer",
				Type:   schema.StepTool,
				Tool:   "test-tool",
				Action: "run",
				Scope:  "local",
				Export: []string{"status"},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}
	eng := New(rb, RunConfig{
		RunID: "r1",
		Mode:  "real",
		Trace: tw,
		ToolExec: &mockToolExecutor{
			result: &executor.Result{ExitCode: 0, Outputs: map[string]any{"status": "ok"}},
		},
	})
	eng.tools["test-tool"] = &schema.ToolDefinition{
		Meta:    schema.ToolMeta{Name: "test-tool"},
		Actions: map[string]schema.ToolAction{"run": {}},
		Contract: contract.Contract{
			Outputs: map[string]contract.ParamDef{"status": {Type: "string"}},
		},
	}

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	traceOutput := traceBuf.String()
	if !strings.Contains(traceOutput, "scope_export") {
		t.Error("expected scope_export event in trace")
	}
}

// T060: Repeat block iterates max times
func TestEngine_RepeatBlock_MaxIterations(t *testing.T) {
	callCount := 0
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "loop",
				Type: schema.StepAssert, // just a container for repeat
				Repeat: &schema.RepeatBlock{
					Max: 3,
					Steps: []schema.Step{
						{
							ID:   "inner",
							Type: schema.StepAssert,
							Assert: []schema.Assertion{
								{Type: "equals", Value: "a", Expected: "a"},
							},
						},
					},
				},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}
	_ = callCount

	// Verify repeat.index was set on last iteration
	if rep, ok := eng.Vars()["repeat"].(map[string]any); ok {
		if idx, ok := rep["index"].(int); ok {
			if idx != 2 { // 0-indexed, last iteration is 2
				t.Errorf("repeat.index = %d, want 2 (0-indexed last of 3)", idx)
			}
		} else {
			t.Error("repeat.index not set")
		}
	} else {
		t.Error("repeat var not set")
	}
}

// T061: Repeat block stops on until condition
func TestEngine_RepeatBlock_StopsOnUntil(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta:       schema.Meta{Name: "test"},
		Steps: []schema.Step{
			{
				ID:   "loop",
				Type: schema.StepAssert,
				Repeat: &schema.RepeatBlock{
					Max:   10,
					Until: `{{ eq .done "true" }}`,
					Steps: []schema.Step{
						{
							ID:   "inner",
							Type: schema.StepAssert,
							Assert: []schema.Assertion{
								{Type: "equals", Value: "a", Expected: "a"},
							},
						},
					},
				},
			},
			{
				Type:    schema.StepEnd,
				Outcome: &schema.Outcome{Category: schema.OutcomeResolved, Code: "done"},
			},
		},
	}

	eng := New(rb, RunConfig{RunID: "r1", Mode: "real"})
	// Set done=true before first iteration so it stops after iteration 0
	eng.vars["done"] = "true"

	result := eng.Run(context.Background())
	if result.Status != "completed" {
		t.Errorf("status = %q, error = %v", result.Status, result.Error)
	}

	// Should have stopped after first iteration
	if rep, ok := eng.Vars()["repeat"].(map[string]any); ok {
		if idx, ok := rep["index"].(int); ok {
			if idx != 0 {
				t.Errorf("repeat.index = %d, want 0 (should stop on first iteration)", idx)
			}
		}
	}
}
