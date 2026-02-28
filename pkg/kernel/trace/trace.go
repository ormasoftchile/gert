// Package trace implements the kernel's append-only JSONL audit trail.
package trace

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"time"
)

// EventType enumerates all kernel trace event types.
type EventType string

const (
	EventRunStart           EventType = "run_start"
	EventRunComplete        EventType = "run_complete"
	EventStepStart          EventType = "step_start"
	EventStepComplete       EventType = "step_complete"
	EventBranchEnter        EventType = "branch_enter"
	EventBranchExit         EventType = "branch_exit"
	EventParallelFork       EventType = "parallel_fork"
	EventParallelMerge      EventType = "parallel_merge"
	EventOutcomeResolved    EventType = "outcome_resolved"
	EventContractEvaluated  EventType = "contract_evaluated"
	EventGovernanceDecision EventType = "governance_decision"
	EventRedactionApplied   EventType = "redaction_applied"
	EventForEachStart       EventType = "for_each_start"
	EventForEachItem        EventType = "for_each_item"
	EventApprovalSubmitted  EventType = "approval_submitted"
	EventApprovalResolved   EventType = "approval_resolved"
	EventScopeExport        EventType = "scope_export"
	EventVisibilityApplied  EventType = "visibility_applied"
	EventRepeatStart        EventType = "repeat_start"
	EventRepeatIteration    EventType = "repeat_iteration"
	EventContractViolation  EventType = "contract_violation"
	EventInputResolved      EventType = "input_resolved"
)

// StepStatus is the execution status of a step.
type StepStatus string

const (
	StatusSuccess StepStatus = "success"
	StatusFailed  StepStatus = "failed"
	StatusSkipped StepStatus = "skipped"
	StatusError   StepStatus = "error"
)

// Event is a single trace event written to the JSONL stream.
type Event struct {
	Type      EventType      `json:"type"`
	Timestamp time.Time      `json:"timestamp"`
	RunID     string         `json:"run_id"`
	Data      map[string]any `json:"data,omitempty"`
}

// Failure describes why a step failed or errored.
type Failure struct {
	Kind    string `json:"kind"` // exit_code, assertion, denied, contract_violation, timeout, ...
	Message string `json:"message"`
}

// Writer writes trace events to an append-only JSONL stream.
type Writer struct {
	mu         sync.Mutex
	w          io.Writer
	runID      string
	enc        *json.Encoder
	secretVars []string // env var names whose values should be redacted
}

// NewWriter creates a trace writer that writes to the given io.Writer.
func NewWriter(w io.Writer, runID string) *Writer {
	return &Writer{
		w:     w,
		runID: runID,
		enc:   json.NewEncoder(w),
	}
}

// SetSecrets configures the writer to redact values of the given env vars from trace output.
func (tw *Writer) SetSecrets(envVars []string) {
	tw.mu.Lock()
	defer tw.mu.Unlock()
	tw.secretVars = envVars
}

// RedactSecrets replaces secret values in a string with "<REDACTED>".
func (tw *Writer) RedactSecrets(s string) string {
	for _, envVar := range tw.secretVars {
		val := os.Getenv(envVar)
		if val != "" && len(val) > 0 {
			s = strings.ReplaceAll(s, val, "<REDACTED>")
		}
	}
	return s
}

// NewFileWriter creates a trace writer that appends to a JSONL file.
func NewFileWriter(path, runID string) (*Writer, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return nil, fmt.Errorf("open trace file: %w", err)
	}
	return NewWriter(f, runID), nil
}

// Emit writes a single trace event.
func (tw *Writer) Emit(eventType EventType, data map[string]any) error {
	tw.mu.Lock()
	defer tw.mu.Unlock()

	evt := Event{
		Type:      eventType,
		Timestamp: time.Now().UTC(),
		RunID:     tw.runID,
		Data:      data,
	}
	return tw.enc.Encode(evt)
}

// EmitStepStart emits a step_start event.
func (tw *Writer) EmitStepStart(stepID, stepType string, contractSummary map[string]any) error {
	data := map[string]any{
		"step_id": stepID,
		"type":    stepType,
	}
	if contractSummary != nil {
		data["contract_summary"] = contractSummary
	}
	return tw.Emit(EventStepStart, data)
}

// EmitStepComplete emits a step_complete event.
func (tw *Writer) EmitStepComplete(stepID string, status StepStatus, outputs map[string]any, duration time.Duration, failure *Failure) error {
	data := map[string]any{
		"step_id":  stepID,
		"status":   string(status),
		"duration": duration.String(),
	}
	if outputs != nil {
		data["outputs"] = outputs
	}
	if failure != nil {
		data["failure"] = map[string]any{
			"kind":    failure.Kind,
			"message": failure.Message,
		}
	}
	return tw.Emit(EventStepComplete, data)
}

// EmitGovernanceDecision emits a governance_decision event.
func (tw *Writer) EmitGovernanceDecision(stepID, riskLevel, decision string, minApprovers int) error {
	data := map[string]any{
		"step_id":    stepID,
		"risk_level": riskLevel,
		"decision":   decision,
	}
	if minApprovers > 0 {
		data["min_approvers"] = minApprovers
	}
	return tw.Emit(EventGovernanceDecision, data)
}

// EmitContractEvaluated emits a contract_evaluated event.
func (tw *Writer) EmitContractEvaluated(stepID string, resolved map[string]any) error {
	return tw.Emit(EventContractEvaluated, map[string]any{
		"step_id":           stepID,
		"resolved_contract": resolved,
	})
}

// EmitBranchEnter emits a branch_enter event.
func (tw *Writer) EmitBranchEnter(label, condition string) error {
	return tw.Emit(EventBranchEnter, map[string]any{
		"branch_label": label,
		"condition":    condition,
	})
}

// EmitBranchExit emits a branch_exit event.
func (tw *Writer) EmitBranchExit(label string) error {
	return tw.Emit(EventBranchExit, map[string]any{
		"branch_label": label,
	})
}

// EmitOutcomeResolved emits an outcome_resolved event.
func (tw *Writer) EmitOutcomeResolved(category, code string, meta map[string]any) error {
	outcome := map[string]any{
		"category": category,
		"code":     code,
	}
	if meta != nil {
		outcome["meta"] = meta
	}
	return tw.Emit(EventOutcomeResolved, map[string]any{
		"structured_outcome": outcome,
	})
}

// EmitRunStart emits a run_start event with runbook info and constants.
func (tw *Writer) EmitRunStart(runbook string, inputs, constants map[string]any) error {
	data := map[string]any{
		"runbook": runbook,
	}
	if inputs != nil {
		data["inputs"] = inputs
	}
	if constants != nil {
		data["constants"] = constants
	}
	return tw.Emit(EventRunStart, data)
}

// EmitRunComplete emits a run_complete event.
func (tw *Writer) EmitRunComplete(outcome map[string]any, status string, duration time.Duration) error {
	data := map[string]any{
		"status":   status,
		"duration": duration.String(),
	}
	if outcome != nil {
		data["outcome"] = outcome
	}
	return tw.Emit(EventRunComplete, data)
}
