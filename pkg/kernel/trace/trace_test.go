package trace

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestWriter_Emit(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "test-run-1")

	err := tw.Emit(EventStepStart, map[string]any{
		"step_id": "s1",
		"type":    "tool",
	})
	if err != nil {
		t.Fatalf("Emit error: %v", err)
	}

	// Parse the JSONL line
	var evt Event
	if err := json.Unmarshal(buf.Bytes(), &evt); err != nil {
		t.Fatalf("JSON unmarshal: %v (raw: %s)", err, buf.String())
	}
	if evt.Type != EventStepStart {
		t.Errorf("type = %q, want step_start", evt.Type)
	}
	if evt.RunID != "test-run-1" {
		t.Errorf("run_id = %q", evt.RunID)
	}
	if evt.Data["step_id"] != "s1" {
		t.Errorf("step_id = %v", evt.Data["step_id"])
	}
}

func TestWriter_EmitStepComplete(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "run-1")

	err := tw.EmitStepComplete("s1", StatusSuccess, map[string]any{"code": 200}, 100*time.Millisecond, nil)
	if err != nil {
		t.Fatal(err)
	}

	var evt Event
	json.Unmarshal(buf.Bytes(), &evt)
	if evt.Data["status"] != "success" {
		t.Errorf("status = %v", evt.Data["status"])
	}
}

func TestWriter_EmitStepComplete_WithFailure(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "run-1")

	err := tw.EmitStepComplete("s1", StatusFailed, nil, 50*time.Millisecond, &Failure{
		Kind: "exit_code", Message: "exit code 1",
	})
	if err != nil {
		t.Fatal(err)
	}

	var evt Event
	json.Unmarshal(buf.Bytes(), &evt)
	if evt.Data["status"] != "failed" {
		t.Errorf("status = %v", evt.Data["status"])
	}
	failure, ok := evt.Data["failure"].(map[string]any)
	if !ok {
		t.Fatal("expected failure object")
	}
	if failure["kind"] != "exit_code" {
		t.Errorf("failure.kind = %v", failure["kind"])
	}
}

func TestWriter_MultipleEvents_JSONL(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "run-1")

	tw.EmitStepStart("s1", "tool", nil)
	tw.EmitStepComplete("s1", StatusSuccess, nil, 0, nil)
	tw.EmitStepStart("s2", "assert", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 JSONL lines, got %d", len(lines))
	}

	// Each line should be valid JSON
	for i, line := range lines {
		var evt Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Errorf("line %d: invalid JSON: %v", i, err)
		}
	}
}

func TestWriter_EmitGovernanceDecision(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "run-1")

	tw.EmitGovernanceDecision("s1", "critical", "require-approval", 2)

	var evt Event
	json.Unmarshal(buf.Bytes(), &evt)
	if evt.Type != EventGovernanceDecision {
		t.Errorf("type = %q", evt.Type)
	}
	if evt.Data["risk_level"] != "critical" {
		t.Errorf("risk_level = %v", evt.Data["risk_level"])
	}
}

func TestWriter_EmitOutcomeResolved(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "run-1")

	tw.EmitOutcomeResolved("resolved", "service_restarted", map[string]any{"attempts": 2})

	var evt Event
	json.Unmarshal(buf.Bytes(), &evt)
	if evt.Type != EventOutcomeResolved {
		t.Errorf("type = %q", evt.Type)
	}
	outcome, _ := evt.Data["structured_outcome"].(map[string]any)
	if outcome["category"] != "resolved" {
		t.Errorf("category = %v", outcome["category"])
	}
}

// T087: Every trace event includes prev_hash
func TestWriter_HashChaining(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "run-1")

	tw.EmitStepStart("s1", "tool", nil)
	tw.EmitStepComplete("s1", StatusSuccess, nil, 0, nil)
	tw.EmitStepStart("s2", "assert", nil)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 lines, got %d", len(lines))
	}

	for i, line := range lines {
		var evt Event
		if err := json.Unmarshal([]byte(line), &evt); err != nil {
			t.Fatalf("line %d: JSON unmarshal: %v", i, err)
		}
		if evt.PrevHash == "" {
			t.Errorf("line %d: prev_hash is empty", i)
		}
	}

	// First event should have zero-hash genesis
	var first Event
	json.Unmarshal([]byte(lines[0]), &first)
	if first.PrevHash != strings.Repeat("0", 64) {
		t.Errorf("first event prev_hash = %q, want 64 zeros", first.PrevHash)
	}

	// Second event should have different prev_hash from first
	var second Event
	json.Unmarshal([]byte(lines[1]), &second)
	if second.PrevHash == first.PrevHash {
		t.Error("second event should have different prev_hash from first")
	}
}

// T090: run_complete includes chain_hash
func TestWriter_RunComplete_ChainHash(t *testing.T) {
	var buf bytes.Buffer
	tw := NewWriter(&buf, "run-1")

	tw.EmitStepStart("s1", "tool", nil)
	tw.EmitRunComplete(nil, "completed", time.Second)

	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	lastLine := lines[len(lines)-1]

	var evt Event
	json.Unmarshal([]byte(lastLine), &evt)

	chainHash, ok := evt.Data["chain_hash"].(string)
	if !ok || chainHash == "" {
		t.Error("run_complete missing chain_hash")
	}
	if len(chainHash) != 64 {
		t.Errorf("chain_hash length = %d, want 64 hex chars", len(chainHash))
	}
}
