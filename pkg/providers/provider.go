// Package providers defines the Provider, CommandExecutor, and
// EvidenceCollector interfaces and their shared types.
package providers

import (
	"context"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// ValidationResult is returned by Provider.Validate.
type ValidationResult struct {
	Valid    bool     `json:"valid"`
	Errors   []string `json:"errors,omitempty"`
	Warnings []string `json:"warnings,omitempty"`
}

// CommandResult holds the output of a single command execution.
type CommandResult struct {
	Stdout   []byte        `json:"stdout"`
	Stderr   []byte        `json:"stderr"`
	ExitCode int           `json:"exit_code"`
	Duration time.Duration `json:"duration"`
}

// AttachmentInfo describes a file attachment provided as evidence.
type AttachmentInfo struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

// Approval records a single approval from an authorized actor.
type Approval struct {
	Actor     string    `json:"actor"`
	Role      string    `json:"role"`
	Timestamp time.Time `json:"timestamp"`
}

// CommandExecutor abstracts real vs replay command execution.
// Implementations: RealExecutor, ReplayExecutor.
type CommandExecutor interface {
	Execute(ctx context.Context, command string, args []string, env []string) (*CommandResult, error)
}

// EvidenceCollector abstracts interactive vs pre-recorded evidence collection.
// Implementations: InteractiveCollector, ScenarioCollector, DryRunCollector.
type EvidenceCollector interface {
	PromptText(name string, instructions string) (string, error)
	PromptChecklist(name string, items []string) (map[string]bool, error)
	PromptAttachment(name string, instructions string) (*AttachmentInfo, error)
	PromptApproval(roles []string, min int) ([]Approval, error)
}

// ExecutionContext is provided to Provider.Execute by the runtime engine.
type ExecutionContext struct {
	RunID             string
	Mode              string // "real", "replay", "dry-run"
	Vars              map[string]string
	Captures          map[string]string
	CommandExecutor   CommandExecutor
	EvidenceCollector EvidenceCollector
	Governance        *schema.GovernancePolicy
}

// Provider handles a specific step type (cli or manual).
// Every provider implements a strict Validate + Execute interface.
type Provider interface {
	// Validate checks step-type-specific fields during schema validation.
	// MUST NOT perform side effects.
	Validate(step schema.Step) ValidationResult

	// Execute runs the step and returns the result.
	// MUST NOT mutate global state outside the returned StepResult.
	// MUST NOT alter execution flow.
	// MUST return a result for every invocation.
	Execute(ctx context.Context, execCtx *ExecutionContext, step schema.Step) (*StepResult, error)
}

// StepResult is the outcome of executing a single step.
// Uniform envelope for all step types, written to trace.
type StepResult struct {
	RunID       string                    `json:"run_id"`
	StepID      string                    `json:"step_id"`
	StepIndex   int                       `json:"step_index"`
	Status      string                    `json:"status"` // passed, failed, skipped
	Actor       string                    `json:"actor"`  // engine, human
	StartedAt   time.Time                 `json:"started_at"`
	EndedAt     time.Time                 `json:"ended_at"`
	Evidence    map[string]*EvidenceValue `json:"evidence,omitempty"`
	Captures    map[string]string         `json:"captures,omitempty"`
	Assertions  []*AssertionResult        `json:"assertions,omitempty"`
	Error       string                    `json:"error,omitempty"`
	RawResponse []byte                    `json:"-"` // raw provider response (not serialized to trace, used for auto-save)
}

// EvidenceValue represents a single piece of collected evidence.
type EvidenceValue struct {
	Kind   string          `json:"kind"` // text, checklist, attachment
	Value  string          `json:"value,omitempty"`
	Items  map[string]bool `json:"items,omitempty"`
	Path   string          `json:"path,omitempty"`
	SHA256 string          `json:"sha256,omitempty"`
	Size   int64           `json:"size,omitempty"`
}

// AssertionResult is the outcome of evaluating a single assertion.
type AssertionResult struct {
	Type     string `json:"type"` // contains, not_contains, matches, exit_code, equals, not_equals, json_path
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message"`
}
