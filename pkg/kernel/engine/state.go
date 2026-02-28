package engine

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// RunState captures the engine state at a point in time for resume.
type RunState struct {
	RunID         string         `json:"run_id"`
	RunbookPath   string         `json:"runbook_path"`
	StepIndex     int            `json:"step_index"`
	Vars          map[string]any `json:"vars"`
	TracePath     string         `json:"trace_path"`
	PendingTicket *ApprovalTicket `json:"pending_ticket,omitempty"`
}

// SaveState persists the run state to a JSON file for later resume.
func SaveState(state *RunState) error {
	dir := filepath.Join("runs", state.RunID)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create state dir: %w", err)
	}

	path := filepath.Join(dir, "state.json")
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal state: %w", err)
	}

	if err := os.WriteFile(path, data, 0o644); err != nil {
		return fmt.Errorf("write state: %w", err)
	}
	return nil
}

// LoadState reads a persisted run state from disk.
func LoadState(runID string) (*RunState, error) {
	path := filepath.Join("runs", runID, "state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read state: %w", err)
	}

	var state RunState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, fmt.Errorf("unmarshal state: %w", err)
	}
	return &state, nil
}
