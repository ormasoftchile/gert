package main

import (
	"bytes"
	"os"
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/trace"
)

func TestTraceVerify_ValidChain(t *testing.T) {
	var buf bytes.Buffer
	tw := trace.NewWriter(&buf, "verify-test")

	// Emit a few events
	tw.EmitRunStart("test-runbook", nil, nil)
	tw.Emit(trace.EventStepStart, map[string]any{"step_id": "s1"})
	tw.Emit(trace.EventStepComplete, map[string]any{"step_id": "s1", "status": "success"})

	result, err := trace.Verify(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Errorf("expected valid chain, got broken at event %d: %s", result.BrokenAt, result.Error)
	}
	if result.EventCount != 3 {
		t.Errorf("EventCount = %d, want 3", result.EventCount)
	}
}

func TestTraceVerify_BrokenChain(t *testing.T) {
	// Write valid first event, then a tampered second event
	var buf bytes.Buffer
	tw := trace.NewWriter(&buf, "verify-test")
	tw.EmitRunStart("test-runbook", nil, nil)

	// Tamper: write a second line that doesn't have the correct prev_hash
	buf.WriteString(`{"type":"step_start","timestamp":"2026-01-01T00:00:00Z","run_id":"verify-test","prev_hash":"0000000000000000000000000000000000000000000000000000000000000000","data":{"step_id":"tampered"}}` + "\n")

	result, err := trace.Verify(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if result.Valid {
		t.Error("expected broken chain, got valid")
	}
	if result.BrokenAt != 2 {
		t.Errorf("BrokenAt = %d, want 2", result.BrokenAt)
	}
}

func TestTraceVerify_WithSignature(t *testing.T) {
	os.Setenv("GERT_TRACE_SIGNING_KEY", "test-secret-key")
	os.Setenv("GERT_TRACE_SIGNING_KEY_ID", "test-key")
	defer os.Unsetenv("GERT_TRACE_SIGNING_KEY")
	defer os.Unsetenv("GERT_TRACE_SIGNING_KEY_ID")

	var buf bytes.Buffer
	tw := trace.NewWriter(&buf, "sig-test")

	tw.EmitRunStart("test-runbook", nil, nil)
	tw.EmitRunComplete(map[string]any{"category": "no_action", "code": "test"}, "completed", 0)

	// Copy buf before verify consumes it
	data := buf.Bytes()
	reader := bytes.NewReader(data)

	result, err := trace.Verify(reader)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Valid {
		t.Errorf("chain not valid: %s", result.Error)
	}
	if !result.SignatureOK {
		t.Error("expected signature to be valid")
	}
	if result.SigningKeyID != "test-key" {
		t.Errorf("SigningKeyID = %q, want test-key", result.SigningKeyID)
	}
}
