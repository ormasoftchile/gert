package replay

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

func TestParseScenario(t *testing.T) {
	yaml := `
inputs:
  hostname: srv1.example.com
tool_responses:
  "health-check:check":
    - exit_code: 0
      stdout: "200"
      outputs:
        status_code: "200"
  "restart-service:restart":
    - exit_code: 0
      stdout: "restarted"
evidence:
  check_manual:
    investigation_notes: "DNS looks fine"
`
	s, err := ParseScenario([]byte(yaml))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if s.Inputs["hostname"] != "srv1.example.com" {
		t.Errorf("hostname = %q", s.Inputs["hostname"])
	}
	if len(s.ToolResponses["health-check:check"]) != 1 {
		t.Errorf("tool_responses count = %d", len(s.ToolResponses["health-check:check"]))
	}
	if s.Evidence["check_manual"]["investigation_notes"] != "DNS looks fine" {
		t.Error("evidence not loaded")
	}
}

func TestReplayExecutor_Execute(t *testing.T) {
	s := &Scenario{
		ToolResponses: map[string][]ToolResponse{
			"my-tool:check": {
				{ExitCode: 0, Stdout: "200", Outputs: map[string]any{"status": "200"}},
				{ExitCode: 1, Stdout: "503"},
			},
		},
	}
	re := NewReplayExecutor(s)

	td := &schema.ToolDefinition{Meta: schema.ToolMeta{Name: "my-tool"}}

	// First call
	r1, err := re.Execute(td, "check", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r1.ExitCode != 0 {
		t.Errorf("exit_code = %d", r1.ExitCode)
	}
	if r1.Outputs["status"] != "200" {
		t.Errorf("status = %v", r1.Outputs["status"])
	}

	// Second call — consumes next response
	r2, err := re.Execute(td, "check", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if r2.ExitCode != 1 {
		t.Errorf("exit_code = %d", r2.ExitCode)
	}

	// Third call — exhausted
	_, err = re.Execute(td, "check", nil, nil)
	if err == nil {
		t.Error("expected exhausted error")
	}
}

func TestReplayExecutor_NoMatch(t *testing.T) {
	s := &Scenario{
		ToolResponses: map[string][]ToolResponse{},
	}
	re := NewReplayExecutor(s)
	td := &schema.ToolDefinition{Meta: schema.ToolMeta{Name: "unknown"}}

	_, err := re.Execute(td, "action", nil, nil)
	if err == nil {
		t.Error("expected no-match error")
	}
}

func TestReplayExecutor_EvidenceForStep(t *testing.T) {
	s := &Scenario{
		Evidence: map[string]map[string]string{
			"manual_step": {
				"notes": "looked good",
			},
		},
	}
	re := NewReplayExecutor(s)

	ev := re.EvidenceForStep("manual_step")
	if ev["notes"] != "looked good" {
		t.Errorf("evidence = %v", ev)
	}

	ev2 := re.EvidenceForStep("nonexistent")
	if ev2 != nil {
		t.Error("expected nil for nonexistent step")
	}
}
