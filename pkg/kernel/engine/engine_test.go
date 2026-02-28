package engine

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
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
					As:  "item",
					Over: "{{ .items }}",
					Key: "{{ .item }}",
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
					As:  "item",
					Over: "{{ .items }}",
					Key: "same_key",
				},
				Assert: []schema.Assertion{
					{Type: "equals", Value: "a", Expected: "a"},
				},
			},
			{
				Type: schema.StepEnd,
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
