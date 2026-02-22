// Package schema defines the Go struct types for the runbook YAML schema
// and provides strict YAML parsing.
package schema

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// Runbook is the top-level document defining an incident response procedure.
type Runbook struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion" jsonschema:"required,enum=runbook/v0,enum=runbook/v1"`
	Imports    map[string]string `yaml:"imports,omitempty" json:"imports,omitempty"`
	Tools      []string          `yaml:"tools,omitempty"   json:"tools,omitempty"`
	Meta       Meta              `yaml:"meta"       json:"meta"       jsonschema:"required"`
	Steps      []Step            `yaml:"steps,omitempty" json:"steps,omitempty"`
	Tree       []TreeNode        `yaml:"tree,omitempty"  json:"tree,omitempty"`
}

// TreeNode is a node in the runbook execution tree.
// It contains a step and optional branches that fork based on conditions.
type TreeNode struct {
	Step     Step     `yaml:"step"               json:"step"`
	Branches []Branch `yaml:"branches,omitempty"  json:"branches,omitempty"`
}

// Branch represents a conditional fork in the tree.
// Steps inside a branch only execute if the condition is true.
type Branch struct {
	Condition string     `yaml:"condition"          json:"condition"          jsonschema:"required"`
	Label     string     `yaml:"label,omitempty"    json:"label,omitempty"`
	Steps     []TreeNode `yaml:"steps,omitempty"    json:"steps,omitempty"`
}

// Meta contains runbook metadata, variables, defaults and governance.
type Meta struct {
	Name        string               `yaml:"name"                json:"name"        jsonschema:"required"`
	Kind        string               `yaml:"kind,omitempty"       json:"kind,omitempty" jsonschema:"enum=mitigation,enum=reference,enum=composable,enum=rca"`
	Description string               `yaml:"description,omitempty" json:"description,omitempty"`
	Source      *SourceMeta          `yaml:"source,omitempty"     json:"source,omitempty"`
	Vars        map[string]string    `yaml:"vars,omitempty"        json:"vars,omitempty"`
	Inputs      map[string]*InputDef `yaml:"inputs,omitempty"      json:"inputs,omitempty"`
	Defaults    *Defaults            `yaml:"defaults,omitempty"    json:"defaults,omitempty"`
	Governance  *GovernancePolicy    `yaml:"governance,omitempty"  json:"governance,omitempty"`
	XTS         *XTSMeta             `yaml:"xts,omitempty"         json:"xts,omitempty"`
	Prose       *Prose               `yaml:"prose,omitempty"       json:"prose,omitempty"`
}

// SourceMeta tracks provenance — where this runbook was compiled from.
type SourceMeta struct {
	File       string `yaml:"file"                json:"file"                jsonschema:"required"`
	CompiledAt string `yaml:"compiled_at"         json:"compiled_at"         jsonschema:"required"`
	CompiledBy string `yaml:"compiled_by,omitempty" json:"compiled_by,omitempty"`
	Model      string `yaml:"model,omitempty"      json:"model,omitempty"`
	SourceHash string `yaml:"source_hash,omitempty" json:"source_hash,omitempty"`
}

// Prose contains the human-readable documentation sections of a TSG.
// These are rendered by `gert render` to produce a publishable markdown/HTML document
// and displayed in the VS Code extension's TSG prose panel.
type Prose struct {
	Background     string           `yaml:"background,omitempty"      json:"background,omitempty"`
	Safety         string           `yaml:"safety,omitempty"          json:"safety,omitempty"`
	Prerequisites  string           `yaml:"prerequisites,omitempty"   json:"prerequisites,omitempty"`
	PostMitigation string           `yaml:"post_mitigation,omitempty" json:"post_mitigation,omitempty"`
	References     []ProseReference `yaml:"references,omitempty"      json:"references,omitempty"`
	Ownership      *ProseOwnership  `yaml:"ownership,omitempty"       json:"ownership,omitempty"`
	Notes          string           `yaml:"notes,omitempty"           json:"notes,omitempty"`
}

// ProseReference is a link in the References section.
type ProseReference struct {
	Title string `yaml:"title" json:"title" jsonschema:"required"`
	URL   string `yaml:"url"   json:"url"   jsonschema:"required"`
}

// ProseOwnership identifies the team responsible for this TSG.
type ProseOwnership struct {
	Team  string `yaml:"team"            json:"team"  jsonschema:"required"`
	Email string `yaml:"email,omitempty" json:"email,omitempty"`
}

// InputDef describes a variable that must be resolved from an external source
// before execution begins. The From field specifies the source path.
//
// Supported sources:
//   - icm.<field>                  — direct ICM incident field (e.g. icm.impactStartTime)
//   - icm.occuringLocation.<field> — ICM location (e.g. icm.occuringLocation.instance)
//   - icm.customFields.<name>      — ICM custom field by name (e.g. icm.customFields.ServerName)
//   - icm.title                    — ICM title (use pattern for regex extraction)
//   - icm.location.<field>         — classified location (e.g. icm.location.Region)
//   - prompt                       — ask the engineer at runtime
//   - enrichment                   — requires a lookup step (future)
type InputDef struct {
	From        string `yaml:"from"                 json:"from"                 jsonschema:"required"`
	Pattern     string `yaml:"pattern,omitempty"     json:"pattern,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Default     string `yaml:"default,omitempty"     json:"default,omitempty"`
	Example     string `yaml:"example,omitempty"     json:"example,omitempty"`
}

// XTSMeta holds global XTS configuration shared by all xts steps.
type XTSMeta struct {
	Environment string `yaml:"environment" json:"environment" jsonschema:"required"`
	ViewsRoot   string `yaml:"views_root,omitempty" json:"views_root,omitempty"`
	CLIPath     string `yaml:"cli_path,omitempty"   json:"cli_path,omitempty"`
}

// Defaults specifies default execution settings applied to all steps.
type Defaults struct {
	Timeout string `yaml:"timeout,omitempty" json:"timeout,omitempty" jsonschema:"pattern=^[0-9]+(s|m|h)$"`
}

// GovernancePolicy defines safety rules evaluated before and during execution.
type GovernancePolicy struct {
	AllowedCommands []string        `yaml:"allowed_commands,omitempty" json:"allowed_commands,omitempty"`
	DeniedCommands  []string        `yaml:"denied_commands,omitempty"  json:"denied_commands,omitempty"`
	DenyEnvVars     []string        `yaml:"deny_env_vars,omitempty"    json:"deny_env_vars,omitempty"`
	Redact          []RedactionRule `yaml:"redact,omitempty"           json:"redact,omitempty"`
	Evidence        *EvidencePolicy `yaml:"evidence,omitempty"         json:"evidence,omitempty"`
}

// RedactionRule is a regex pattern-replacement pair for sanitizing output.
type RedactionRule struct {
	Pattern string `yaml:"pattern" json:"pattern" jsonschema:"required"`
	Replace string `yaml:"replace" json:"replace" jsonschema:"required"`
}

// EvidencePolicy defines global settings for evidence collection.
type EvidencePolicy struct {
	RequireForManual bool `yaml:"require_for_manual" json:"require_for_manual,omitempty"`
	StoreFullStdout  bool `yaml:"store_full_stdout"  json:"store_full_stdout,omitempty"`
}

// Step is a single unit of work. Dispatched to a Provider based on Type.
type Step struct {
	ID               string                `yaml:"id"                json:"id"                jsonschema:"required"`
	Type             string                `yaml:"type"              json:"type"              jsonschema:"required,enum=cli,enum=manual,enum=xts,enum=invoke,enum=tool"`
	Title            string                `yaml:"title,omitempty"   json:"title,omitempty"`
	When             string                `yaml:"when,omitempty"    json:"when,omitempty"`
	Precondition     *Precondition         `yaml:"precondition,omitempty" json:"precondition,omitempty"`
	Outcomes         []Outcome             `yaml:"outcomes,omitempty" json:"outcomes,omitempty"`
	With             *CLIStepConfig        `yaml:"with,omitempty"    json:"with,omitempty"`
	XTS              *XTSStepConfig        `yaml:"xts,omitempty"     json:"xts,omitempty"`
	Instructions     string                `yaml:"instructions,omitempty"  json:"instructions,omitempty"`
	RequiredEvidence []EvidenceRequirement `yaml:"required_evidence,omitempty" json:"required_evidence,omitempty"`
	Approvals        *ApprovalRequirement  `yaml:"approvals,omitempty"   json:"approvals,omitempty"`
	Choices          *ChoiceConfig         `yaml:"choices,omitempty"     json:"choices,omitempty"`
	Capture          map[string]string     `yaml:"capture,omitempty"     json:"capture,omitempty"`
	Assertions       []Assertion           `yaml:"assertions,omitempty"  json:"assertions,omitempty"`
	Timeout          string                `yaml:"timeout,omitempty"     json:"timeout,omitempty"  jsonschema:"pattern=^[0-9]+(s|m|h)$"`
	ReplayMode       string                `yaml:"replay_mode,omitempty" json:"replay_mode,omitempty" jsonschema:"enum=reuse_evidence"`
	Invoke           *InvokeConfig         `yaml:"invoke,omitempty"      json:"invoke,omitempty"`
	Gate             *Gate                 `yaml:"gate,omitempty"        json:"gate,omitempty"`
	Tool             *ToolStepConfig       `yaml:"tool,omitempty"        json:"tool,omitempty"`
}

// Outcome defines a terminal state that a step can reach after execution.
// A step may have multiple outcomes with different conditions — the first
// whose When evaluates to true (or has no When) triggers the terminal state.
type Outcome struct {
	When           string       `yaml:"when,omitempty"           json:"when,omitempty"`
	State          string       `yaml:"state"                    json:"state"          jsonschema:"required,enum=resolved,enum=escalated,enum=no_action,enum=needs_rca"`
	Recommendation string       `yaml:"recommendation,omitempty" json:"recommendation,omitempty"`
	NextRunbook    *NextRunbook `yaml:"next_runbook,omitempty"   json:"next_runbook,omitempty"`
}

// NextRunbook specifies a child runbook to invoke when this outcome triggers.
type NextRunbook struct {
	File   string            `yaml:"file"             json:"file"             jsonschema:"required"`
	Inputs map[string]string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// CLIStepConfig is the configuration for a CLI step's command execution.
type CLIStepConfig struct {
	Argv []string `yaml:"argv" json:"argv" jsonschema:"required,minItems=1"`
}

// InvokeConfig specifies a child runbook to run inline as a sub-procedure.
type InvokeConfig struct {
	Runbook string            `yaml:"runbook"          json:"runbook"          jsonschema:"required"`
	Inputs  map[string]string `yaml:"inputs,omitempty" json:"inputs,omitempty"`
}

// Gate controls parent execution based on the child runbook's outcome.
type Gate struct {
	StopIf  []string `yaml:"stop_if,omitempty"  json:"stop_if,omitempty"`
	OnError string   `yaml:"on_error,omitempty" json:"on_error,omitempty" jsonschema:"enum=skip"`
}

// Precondition defines a probe command that runs before a step.
// If the probe succeeds (exit code 0), the step is auto-skipped with status "already_satisfied".
// This makes steps idempotent — useful for installation or setup steps.
type Precondition struct {
	Check          []string `yaml:"check"             json:"check"             jsonschema:"required,minItems=1"`
	SkipIfSucceeds bool     `yaml:"skip_if_succeeds"  json:"skip_if_succeeds"`
	Message        string   `yaml:"message,omitempty" json:"message,omitempty"`
}

// XTSStepConfig is the configuration for an XTS step.
type XTSStepConfig struct {
	Mode        string            `yaml:"mode"                   json:"mode"                   jsonschema:"required,enum=query,enum=activity,enum=view"`
	Environment string            `yaml:"environment,omitempty"  json:"environment,omitempty"`
	File        string            `yaml:"file,omitempty"         json:"file,omitempty"`
	Activity    string            `yaml:"activity,omitempty"     json:"activity,omitempty"`
	QueryType   string            `yaml:"query_type,omitempty"   json:"query_type,omitempty"   jsonschema:"enum=sql,enum=kusto,enum=cms,enum=mds"`
	Query       string            `yaml:"query,omitempty"        json:"query,omitempty"`
	Params      map[string]string `yaml:"params,omitempty"       json:"params,omitempty"`
	AutoSelect  bool              `yaml:"auto_select,omitempty"  json:"auto_select,omitempty"`
	SQLTimeout  int               `yaml:"sql_timeout,omitempty"  json:"sql_timeout,omitempty"`
}

// EvidenceRequirement specifies a single evidence item required for a step.
type EvidenceRequirement struct {
	Kind  string   `yaml:"kind"  json:"kind"  jsonschema:"required,enum=text,enum=checklist,enum=attachment"`
	Name  string   `yaml:"name"  json:"name"  jsonschema:"required"`
	Items []string `yaml:"items" json:"items,omitempty"`
}

// ApprovalRequirement defines approval gates for manual steps.
type ApprovalRequirement struct {
	Min   int      `yaml:"min"   json:"min,omitempty"`
	Roles []string `yaml:"roles" json:"roles,omitempty"`
}

// ChoiceConfig presents a list of options for the user to select from.
// The selected value is stored in the variable named by Variable.
type ChoiceConfig struct {
	Variable string         `yaml:"variable" json:"variable" jsonschema:"required"`
	Prompt   string         `yaml:"prompt,omitempty" json:"prompt,omitempty"`
	Options  []ChoiceOption `yaml:"options"  json:"options"  jsonschema:"required,minItems=2"`
}

// ChoiceOption is a single selectable option in a choice.
type ChoiceOption struct {
	Value       string `yaml:"value"                json:"value"       jsonschema:"required"`
	Label       string `yaml:"label,omitempty"       json:"label,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// Assertion is a post-execution check on captured output.
// Exactly one field must be set per Assertion object.
type Assertion struct {
	Contains    string             `yaml:"contains"     json:"contains,omitempty"`
	NotContains string             `yaml:"not_contains" json:"not_contains,omitempty"`
	Matches     string             `yaml:"matches"      json:"matches,omitempty"`
	ExitCode    *int               `yaml:"exit_code"    json:"exit_code,omitempty"`
	Equals      string             `yaml:"equals"       json:"equals,omitempty"`
	NotEquals   string             `yaml:"not_equals"   json:"not_equals,omitempty"`
	JSONPath    *JSONPathAssertion `yaml:"json_path"    json:"json_path,omitempty"`
}

// JSONPathAssertion is a structured query into JSON output.
type JSONPathAssertion struct {
	Path   string `yaml:"path"   json:"path"   jsonschema:"required"`
	Equals string `yaml:"equals" json:"equals" jsonschema:"required"`
}

// LoadFile reads and parses a runbook YAML file with strict unknown-field
// rejection (yaml.v3 KnownFields). Returns the parsed Runbook or an error.
func LoadFile(path string) (*Runbook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open runbook: %w", err)
	}
	defer f.Close()
	return Load(f)
}

// Load parses a runbook from an io.Reader with strict unknown-field rejection.
func Load(r io.Reader) (*Runbook, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true) // reject unknown fields (FR-001)

	var rb Runbook
	if err := dec.Decode(&rb); err != nil {
		return nil, fmt.Errorf("decode runbook: %w", err)
	}
	return &rb, nil
}
