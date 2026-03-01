package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/engine"
)

func TestResumeCmd_LoadsState(t *testing.T) {
	// Create a temporary state directory
	tmpDir := t.TempDir()
	origDir, _ := os.Getwd()
	os.Chdir(tmpDir)
	defer os.Chdir(origDir)

	// Create a dummy runbook
	rbContent := `apiVersion: kernel/v0
meta:
  name: test-resume
  inputs:
    hostname: { type: string, required: true }
steps:
  - id: done
    type: end
    outcome:
      category: no_action
      code: resumed_ok
`
	rbPath := filepath.Join(tmpDir, "test.yaml")
	os.WriteFile(rbPath, []byte(rbContent), 0o644)

	// Create persisted state
	state := &engine.RunState{
		RunID:       "test-run-1",
		RunbookPath: rbPath,
		StepIndex:   0,
		Vars:        map[string]any{"hostname": "srv1"},
		TracePath:   "",
	}

	dir := filepath.Join("runs", "test-run-1")
	os.MkdirAll(dir, 0o755)
	data, _ := json.MarshalIndent(state, "", "  ")
	os.WriteFile(filepath.Join(dir, "state.json"), data, 0o644)

	// Verify state loads correctly
	loaded, err := engine.LoadState("test-run-1")
	if err != nil {
		t.Fatal(err)
	}
	if loaded.RunID != "test-run-1" {
		t.Errorf("RunID = %q, want test-run-1", loaded.RunID)
	}
	if loaded.RunbookPath != rbPath {
		t.Errorf("RunbookPath = %q, want %q", loaded.RunbookPath, rbPath)
	}
}
