package runtime

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/providers"
)

// TestTraceWriteAndRead verifies writing and reading JSONL trace events.
func TestTraceWriteAndRead(t *testing.T) {
	dir := t.TempDir()
	tracePath := filepath.Join(dir, "trace.jsonl")

	w, err := NewTraceWriter(tracePath)
	if err != nil {
		t.Fatalf("create trace writer: %v", err)
	}

	result := &providers.StepResult{
		RunID:     "20260211T153042-a7f3",
		StepID:    "check_pods",
		StepIndex: 0,
		Status:    "passed",
		Actor:     "engine",
	}
	if err := w.Write(result); err != nil {
		t.Fatalf("write: %v", err)
	}

	result2 := &providers.StepResult{
		RunID:     "20260211T153042-a7f3",
		StepID:    "get_logs",
		StepIndex: 1,
		Status:    "passed",
		Actor:     "engine",
	}
	if err := w.Write(result2); err != nil {
		t.Fatalf("write: %v", err)
	}

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	// Read back and verify valid JSONL
	data, err := os.ReadFile(tracePath)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 2 {
		t.Fatalf("expected 2 lines, got %d", len(lines))
	}
	for i, line := range lines {
		var event TraceEvent
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Errorf("line %d is not valid JSON: %v", i, err)
		}
		if event.Type != "step_result" {
			t.Errorf("line %d type = %q, want %q", i, event.Type, "step_result")
		}
	}
}
