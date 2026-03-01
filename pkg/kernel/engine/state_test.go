package engine

import (
	"os"
	"testing"
	"time"
)

func TestSaveLoadState(t *testing.T) {
	// Clean up
	defer os.RemoveAll("runs")

	state := &RunState{
		RunID:       "test-run-123",
		RunbookPath: "runbooks/test.yaml",
		StepIndex:   3,
		Vars:        map[string]any{"hostname": "srv1", "status": "200"},
		TracePath:   "traces/test.jsonl",
		PendingTicket: &ApprovalTicket{
			TicketID: "ticket-abc",
			Status:   "pending",
			Created:  time.Now().UTC().Truncate(time.Second),
		},
	}

	// Save
	if err := SaveState(state); err != nil {
		t.Fatalf("SaveState: %v", err)
	}

	// Load
	loaded, err := LoadState("test-run-123")
	if err != nil {
		t.Fatalf("LoadState: %v", err)
	}

	if loaded.RunID != state.RunID {
		t.Errorf("RunID = %q, want %q", loaded.RunID, state.RunID)
	}
	if loaded.StepIndex != state.StepIndex {
		t.Errorf("StepIndex = %d, want %d", loaded.StepIndex, state.StepIndex)
	}
	if loaded.Vars["hostname"] != "srv1" {
		t.Errorf("Vars[hostname] = %v", loaded.Vars["hostname"])
	}
	if loaded.PendingTicket == nil {
		t.Fatal("PendingTicket = nil")
	}
	if loaded.PendingTicket.TicketID != "ticket-abc" {
		t.Errorf("TicketID = %q", loaded.PendingTicket.TicketID)
	}
}

func TestLoadState_NotFound(t *testing.T) {
	_, err := LoadState("nonexistent-run")
	if err == nil {
		t.Error("expected error for nonexistent state")
	}
}
