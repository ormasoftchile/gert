// Package runtime defines execution state types used by the runtime engine.
package runtime

import (
	"time"

	"github.com/ormasoftchile/gert/pkg/providers"
)

// RunState is the complete execution state at a point in time.
// Serialized to JSON for snapshot persistence.
type RunState struct {
	RunID            string                  `json:"run_id"`
	RunbookPath      string                  `json:"runbook_path"`
	Mode             string                  `json:"mode"` // real, replay, dry-run
	StartedAt        time.Time               `json:"started_at"`
	Actor            string                  `json:"actor"`
	CurrentStepIndex int                     `json:"current_step_index"`
	Vars             map[string]string       `json:"vars"`
	Captures         map[string]string       `json:"captures"`
	History          []*providers.StepResult `json:"history"`
}

// TraceEvent wraps a StepResult for JSONL trace output with extra metadata.
type TraceEvent struct {
	Type      string                `json:"type"` // step_result
	Timestamp time.Time             `json:"timestamp"`
	RunID     string                `json:"run_id"`
	Result    *providers.StepResult `json:"result"`
}

// RunManifest records the complete metadata for a runbook execution.
// Written as run.yaml after a run completes (or fails).
type RunManifest struct {
	RunID          string            `yaml:"run_id"            json:"run_id"`
	Runbook        string            `yaml:"runbook"           json:"runbook"`
	Actor          string            `yaml:"actor,omitempty"   json:"actor,omitempty"`
	Mode           string            `yaml:"mode"              json:"mode"`
	StartedAt      string            `yaml:"started_at"        json:"started_at"`
	EndedAt        string            `yaml:"ended_at"          json:"ended_at"`
	Outcome        *OutcomeRecord    `yaml:"outcome,omitempty" json:"outcome,omitempty"`
	InputsResolved map[string]string `yaml:"inputs_resolved,omitempty" json:"inputs_resolved,omitempty"`
	StepsSummary   StepsSummary      `yaml:"steps_summary"     json:"steps_summary"`
	ParentRunID    string            `yaml:"parent_run_id,omitempty" json:"parent_run_id,omitempty"`
	ChildRuns      []ChildRunRef     `yaml:"child_runs,omitempty"    json:"child_runs,omitempty"`
}

// OutcomeRecord captures the terminal outcome of a run.
type OutcomeRecord struct {
	State          string `yaml:"state"                    json:"state"`
	StepID         string `yaml:"step_id"                  json:"step_id"`
	Recommendation string `yaml:"recommendation,omitempty" json:"recommendation,omitempty"`
}

// StepsSummary counts step results by status.
type StepsSummary struct {
	Total   int `yaml:"total"   json:"total"`
	Passed  int `yaml:"passed"  json:"passed"`
	Failed  int `yaml:"failed"  json:"failed"`
	Skipped int `yaml:"skipped" json:"skipped"`
}

// ChildRunRef is a reference to a chained child run.
type ChildRunRef struct {
	RunID   string `yaml:"run_id"   json:"run_id"`
	Runbook string `yaml:"runbook"  json:"runbook"`
	Outcome string `yaml:"outcome"  json:"outcome"`
}
