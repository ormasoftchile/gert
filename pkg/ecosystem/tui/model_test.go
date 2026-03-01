package tui

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/schema"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
)

// T100: TUI model initializes from runbook
func TestModel_InitFromRunbook(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "test-rb"},
		Steps: []schema.Step{
			{ID: "step1", Type: schema.StepTool},
			{ID: "step2", Type: schema.StepAssert},
			{ID: "step3", Type: schema.StepEnd},
		},
	}

	m := NewModel(rb)
	if len(m.steps) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(m.steps))
	}
	if m.steps[0].ID != "step1" {
		t.Errorf("step[0].ID = %q, want step1", m.steps[0].ID)
	}
	if m.status != "idle" {
		t.Errorf("status = %q, want idle", m.status)
	}
}

// T101: TUI updates step status on trace events
func TestModel_TracksStepStatus(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "test-rb"},
		Steps: []schema.Step{
			{ID: "check", Type: schema.StepTool},
			{ID: "end", Type: schema.StepEnd},
		},
	}

	m := NewModel(rb)

	// Simulate step_start
	m.applyTraceEvent(trace.Event{
		Type: trace.EventStepStart,
		Data: map[string]any{"step_id": "check"},
	})
	if m.steps[0].Status != "running" {
		t.Errorf("after step_start: status = %q, want running", m.steps[0].Status)
	}

	// Simulate step_complete
	m.applyTraceEvent(trace.Event{
		Type: trace.EventStepComplete,
		Data: map[string]any{"step_id": "check", "status": "success", "duration": "100ms"},
	})
	if m.steps[0].Status != "success" {
		t.Errorf("after step_complete: status = %q, want success", m.steps[0].Status)
	}
	if m.steps[0].Duration.Milliseconds() != 100 {
		t.Errorf("duration = %v, want 100ms", m.steps[0].Duration)
	}
}
