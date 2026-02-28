// Package schema defines the kernel/v0 runbook and tool definition types.
package schema

import (
	"fmt"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
)

// API version constant for kernel/v0.
const APIVersionKernel = "kernel/v0"

// ---------------------------------------------------------------------------
// Runbook
// ---------------------------------------------------------------------------

// Runbook is the top-level kernel/v0 document.
type Runbook struct {
	APIVersion string         `yaml:"apiVersion" json:"apiVersion"`
	Meta       Meta           `yaml:"meta"       json:"meta"`
	Tools      []string       `yaml:"tools,omitempty" json:"tools,omitempty"`
	Steps      []Step         `yaml:"steps"      json:"steps"`
	Extensions map[string]any `yaml:"extensions,omitempty" json:"extensions,omitempty"`
}

// ---------------------------------------------------------------------------
// Meta
// ---------------------------------------------------------------------------

// Meta contains runbook metadata, inputs, constants, and governance.
type Meta struct {
	Name        string                       `yaml:"name"        json:"name"`
	Description string                       `yaml:"description,omitempty" json:"description,omitempty"`
	Inputs      map[string]contract.ParamDef `yaml:"inputs,omitempty"    json:"inputs,omitempty"`
	Constants   map[string]any               `yaml:"constants,omitempty" json:"constants,omitempty"`
	Governance  *GovernancePolicy            `yaml:"governance,omitempty" json:"governance,omitempty"`
	Extensions  map[string]any               `yaml:"extensions,omitempty" json:"extensions,omitempty"`
}

// ---------------------------------------------------------------------------
// Governance
// ---------------------------------------------------------------------------

// GovernancePolicy defines risk-based and contract-based governance rules.
type GovernancePolicy struct {
	Rules []GovernanceRule `yaml:"rules" json:"rules"`
}

// GovernanceRule is a single governance policy rule.
type GovernanceRule struct {
	Risk         string              `yaml:"risk,omitempty"     json:"risk,omitempty"`
	Contract     *GovernanceContract `yaml:"contract,omitempty" json:"contract,omitempty"`
	Default      string              `yaml:"default,omitempty"  json:"default,omitempty"`
	Action       string              `yaml:"action,omitempty"   json:"action,omitempty"`
	MinApprovers int                 `yaml:"min_approvers,omitempty" json:"min_approvers,omitempty"`
}

// GovernanceContract matches steps by contract properties.
type GovernanceContract struct {
	Writes []string `yaml:"writes,omitempty" json:"writes,omitempty"`
	Reads  []string `yaml:"reads,omitempty"  json:"reads,omitempty"`
}

// GovernanceDecision is the result of evaluating governance for a step.
type GovernanceDecision string

const (
	DecisionAllow           GovernanceDecision = "allow"
	DecisionRequireApproval GovernanceDecision = "require-approval"
	DecisionDeny            GovernanceDecision = "deny"
)

// ---------------------------------------------------------------------------
// Step
// ---------------------------------------------------------------------------

// StepType enumerates the seven kernel step types.
type StepType string

const (
	StepTool      StepType = "tool"
	StepManual    StepType = "manual"
	StepAssert    StepType = "assert"
	StepBranch    StepType = "branch"
	StepParallel  StepType = "parallel"
	StepEnd       StepType = "end"
	StepExtension StepType = "extension"
)

// Step is the universal step structure. Fields are populated based on Type.
type Step struct {
	// Common fields
	ID             string         `yaml:"id,omitempty"   json:"id,omitempty"`
	Type           StepType       `yaml:"type"           json:"type"`
	When           string         `yaml:"when,omitempty" json:"when,omitempty"`
	Next           any            `yaml:"next,omitempty" json:"next,omitempty"` // string or NextBounded
	ContinueOnFail bool           `yaml:"continue_on_fail,omitempty" json:"continue_on_fail,omitempty"`
	Extensions     map[string]any `yaml:"extensions,omitempty" json:"extensions,omitempty"`

	// ForEach modifier (any step type)
	ForEach *ForEach `yaml:"for_each,omitempty" json:"for_each,omitempty"`

	// Tool step
	Tool       string         `yaml:"tool,omitempty"   json:"tool,omitempty"`
	Action     string         `yaml:"action,omitempty" json:"action,omitempty"`
	Inputs     map[string]any `yaml:"inputs,omitempty" json:"inputs,omitempty"`
	InputsFrom any            `yaml:"inputs_from,omitempty" json:"inputs_from,omitempty"` // string or []string

	// Manual step
	Instructions     string                `yaml:"instructions,omitempty"      json:"instructions,omitempty"`
	RequiredEvidence []EvidenceRequirement `yaml:"required_evidence,omitempty" json:"required_evidence,omitempty"`

	// Assert step
	Assert []Assertion `yaml:"assert,omitempty" json:"assert,omitempty"`

	// Branch step
	Branches []Branch `yaml:"branches,omitempty" json:"branches,omitempty"`

	// Parallel step  (reuses Branches with parallel semantics)

	// End step
	Outcome *Outcome `yaml:"outcome,omitempty" json:"outcome,omitempty"`

	// Extension step
	Extension string             `yaml:"extension,omitempty" json:"extension,omitempty"`
	Contract  *contract.Contract `yaml:"contract,omitempty"  json:"contract,omitempty"`
}

// NextBounded is the structured form of `next` for backward jumps.
type NextBounded struct {
	Step string `yaml:"step" json:"step"`
	Max  int    `yaml:"max"  json:"max"`
}

// ParseNext extracts the next target from Step.Next.
// Returns (target, max, isBounded).
//   - Simple string: ("step_id", 0, false)
//   - Map: ("step_id", max, true)
func ParseNext(raw any) (target string, max int, bounded bool, err error) {
	switch v := raw.(type) {
	case nil:
		return "", 0, false, nil
	case string:
		return v, 0, false, nil
	case map[string]any:
		s, _ := v["step"].(string)
		m, _ := v["max"].(int)
		if fm, ok := v["max"].(float64); ok {
			m = int(fm)
		}
		// Also handle string-valued max (template expression)
		if ms, ok := v["max"].(string); ok && ms != "" {
			// At validation time we can't resolve templates — treat as bounded
			return s, 1, true, nil
		}
		return s, m, m > 0 || s != "", nil
	default:
		return "", 0, false, fmt.Errorf("invalid next value: %T", raw)
	}
}

// ForEach is the iteration modifier.
type ForEach struct {
	As       string `yaml:"as"       json:"as"`
	Over     string `yaml:"over"     json:"over"`
	Parallel bool   `yaml:"parallel,omitempty" json:"parallel,omitempty"`
}

// ---------------------------------------------------------------------------
// Branch (used by branch + parallel steps)
// ---------------------------------------------------------------------------

// Branch is one arm of a branch or parallel step.
type Branch struct {
	Condition string `yaml:"condition,omitempty" json:"condition,omitempty"`
	Label     string `yaml:"label,omitempty"     json:"label,omitempty"`
	Steps     []Step `yaml:"steps"               json:"steps"`
}

// ---------------------------------------------------------------------------
// Assertion
// ---------------------------------------------------------------------------

// Assertion is a single assertion expression within an assert step.
type Assertion struct {
	Type     string `yaml:"type"               json:"type"`
	Value    string `yaml:"value,omitempty"     json:"value,omitempty"`
	Expected string `yaml:"expected,omitempty"  json:"expected,omitempty"`
	Pattern  string `yaml:"pattern,omitempty"   json:"pattern,omitempty"`
}

// ---------------------------------------------------------------------------
// Outcome
// ---------------------------------------------------------------------------

// OutcomeCategory is the fixed enum for structured outcomes.
type OutcomeCategory string

const (
	OutcomeResolved  OutcomeCategory = "resolved"
	OutcomeEscalated OutcomeCategory = "escalated"
	OutcomeNoAction  OutcomeCategory = "no_action"
	OutcomeNeedsRCA  OutcomeCategory = "needs_rca"
)

// Outcome is the structured outcome carried by an end step.
type Outcome struct {
	Category OutcomeCategory `yaml:"category" json:"category"`
	Code     string          `yaml:"code"     json:"code"`
	Meta     map[string]any  `yaml:"meta,omitempty" json:"meta,omitempty"`
}

// ---------------------------------------------------------------------------
// Evidence
// ---------------------------------------------------------------------------

// EvidenceRequirement specifies what evidence a manual step collects.
type EvidenceRequirement struct {
	Kind  string   `yaml:"kind"             json:"kind"` // text, checklist, attachment
	Name  string   `yaml:"name"             json:"name"`
	Items []string `yaml:"items,omitempty"  json:"items,omitempty"`
}
