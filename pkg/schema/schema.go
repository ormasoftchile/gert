// Package schema defines the Go struct types for the runbook YAML schema
// and provides strict YAML parsing.
package schema

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/invopop/jsonschema"
	"gopkg.in/yaml.v3"
)

// Runbook is the top-level document defining an incident response procedure.
type Runbook struct {
	APIVersion string            `yaml:"apiVersion" json:"apiVersion" jsonschema:"required,enum=runbook/v0,enum=runbook/v1"`
	Imports    map[string]string `yaml:"imports,omitempty" json:"imports,omitempty"`
	Tools      []string          `yaml:"tools,omitempty"   json:"tools,omitempty"`
	ToolPaths  map[string]string `yaml:"-" json:"-"`
	Meta       Meta              `yaml:"meta"       json:"meta"       jsonschema:"required"`
	Steps      []Step            `yaml:"steps,omitempty" json:"steps,omitempty"`
	Tree       []TreeNode        `yaml:"tree,omitempty"  json:"tree,omitempty"`
}

func (rb *Runbook) ResolveToolPath(name string) string {
	if rb != nil && rb.ToolPaths != nil {
		if p := strings.TrimSpace(rb.ToolPaths[name]); p != "" {
			return p
		}
	}
	return filepath.Join("tools", name+".tool.yaml")
}

// TreeNode is a node in the runbook execution tree.
// It contains either a step (with optional branches) or an iterate block.
type TreeNode struct {
	Step     Step           `yaml:"step"               json:"step"`
	Iterate  *IterateBlock  `yaml:"iterate,omitempty"  json:"iterate,omitempty"`
	Branches []Branch       `yaml:"branches,omitempty" json:"branches,omitempty"`
}

// JSONSchemaExtend customizes the generated JSON Schema for TreeNode.
// Step is not required at the schema level because iterate nodes omit it.
// Domain validation enforces that each node has exactly one of step or iterate.
func (TreeNode) JSONSchemaExtend(schema *jsonschema.Schema) {
	var filtered []string
	for _, r := range schema.Required {
		if r != "step" {
			filtered = append(filtered, r)
		}
	}
	schema.Required = filtered
}

// MarshalJSON implements custom JSON marshaling for TreeNode.
// When a node is an iterate node (Step is zero-value), the empty step is
// omitted so that JSON Schema validation does not reject the zero-value
// Step's required/enum fields.
func (n TreeNode) MarshalJSON() ([]byte, error) {
	if n.Iterate != nil && n.Step.ID == "" {
		return json.Marshal(struct {
			Iterate  *IterateBlock `json:"iterate,omitempty"`
			Branches []Branch      `json:"branches,omitempty"`
		}{
			Iterate:  n.Iterate,
			Branches: n.Branches,
		})
	}
	type alias TreeNode
	return json.Marshal(alias(n))
}

// IterateBlock represents a bounded iteration in the tree.
// Two modes:
//   - Convergence: max + until — re-execute steps until the condition is true or max passes.
//   - List: over + as — iterate over a comma-separated list, running steps once per item.
//
// The modes are mutually exclusive. {{ .iteration }} (0-indexed) is always available.
// In list mode, {{ .<as> }} (default "item") holds the current element.
type IterateBlock struct {
	Max   int        `yaml:"max,omitempty"   json:"max,omitempty"   jsonschema:"minimum=1"`
	Until string     `yaml:"until,omitempty" json:"until,omitempty"`
	Over  string     `yaml:"over,omitempty"  json:"over,omitempty"`
	As    string     `yaml:"as,omitempty"    json:"as,omitempty"`
	Steps []TreeNode `yaml:"steps"           json:"steps"           jsonschema:"required,minItems=1"`
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
	Scenarios   map[string]string    `yaml:"scenarios,omitempty"  json:"scenarios,omitempty"`
	Vars        map[string]string    `yaml:"vars,omitempty"        json:"vars,omitempty"`
	Inputs      map[string]*InputDef `yaml:"inputs,omitempty"      json:"inputs,omitempty"`
	Defaults    *Defaults            `yaml:"defaults,omitempty"    json:"defaults,omitempty"`
	Governance  *GovernancePolicy    `yaml:"governance,omitempty"  json:"governance,omitempty"`
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
//   - prompt                       — ask the engineer at runtime
//   - enrichment                   — requires a lookup step (future)
//   - <provider>.<field>           — resolved by an external input provider
type InputDef struct {
	From        string `yaml:"from"                 json:"from"                 jsonschema:"required"`
	Pattern     string `yaml:"pattern,omitempty"     json:"pattern,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Default     string `yaml:"default,omitempty"     json:"default,omitempty"`
	Example     string `yaml:"example,omitempty"     json:"example,omitempty"`
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
	Type             string                `yaml:"type"              json:"type"              jsonschema:"required,enum=cli,enum=manual,enum=invoke,enum=tool"`
	Title            string                `yaml:"title,omitempty"   json:"title,omitempty"`
	When             string                `yaml:"when,omitempty"    json:"when,omitempty"`
	Precondition     *Precondition         `yaml:"precondition,omitempty" json:"precondition,omitempty"`
	Outcomes         []Outcome             `yaml:"outcomes,omitempty" json:"outcomes,omitempty"`
	With             *CLIStepConfig        `yaml:"with,omitempty"    json:"with,omitempty"`
	Instructions     string                `yaml:"instructions,omitempty"  json:"instructions,omitempty"`
	RequiredEvidence []EvidenceRequirement `yaml:"required_evidence,omitempty" json:"required_evidence,omitempty"`
	Approvals        *ApprovalRequirement  `yaml:"approvals,omitempty"   json:"approvals,omitempty"`
	Choices          *ChoiceConfig         `yaml:"choices,omitempty"     json:"choices,omitempty"`
	Capture          map[string]string     `yaml:"capture,omitempty"     json:"capture,omitempty"`
	Assertions       []Assertion           `yaml:"assertions,omitempty"  json:"assertions,omitempty"`
	Timeout          string                `yaml:"timeout,omitempty"     json:"timeout,omitempty"  jsonschema:"pattern=^[0-9]+(s|m|h)$"`
	Delay            string                `yaml:"delay,omitempty"       json:"delay,omitempty"    jsonschema:"pattern=^[0-9]+(ms|s|m|h)$"`
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
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read runbook: %w", err)
	}

	rb, strictErr := decodeRunbookStrict(data)
	if strictErr == nil {
		return rb, nil
	}

	// Fallback for shorthand/verbose imports/tools forms.
	flexRB, flexErr := decodeRunbookFlexible(data)
	if flexErr != nil {
		return nil, fmt.Errorf("decode runbook: %w", strictErr)
	}
	return flexRB, nil
}

func decodeRunbookStrict(data []byte) (*Runbook, error) {
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true) // reject unknown fields (FR-001)

	var rb Runbook
	if err := dec.Decode(&rb); err != nil {
		return nil, err
	}
	return &rb, nil
}

func decodeRunbookFlexible(data []byte) (*Runbook, error) {
	type runbookFlex struct {
		APIVersion string      `yaml:"apiVersion"`
		Imports    interface{} `yaml:"imports,omitempty"`
		Tools      interface{} `yaml:"tools,omitempty"`
		Meta       Meta        `yaml:"meta"`
		Steps      []Step      `yaml:"steps,omitempty"`
		Tree       []TreeNode  `yaml:"tree,omitempty"`
	}

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	var wire runbookFlex
	if err := dec.Decode(&wire); err != nil {
		return nil, err
	}

	imports, err := normalizeImportsValue(wire.Imports)
	if err != nil {
		return nil, fmt.Errorf("decode imports: %w", err)
	}
	tools, toolPaths, err := normalizeToolsValue(wire.Tools)
	if err != nil {
		return nil, fmt.Errorf("decode tools: %w", err)
	}

	return &Runbook{
		APIVersion: wire.APIVersion,
		Imports:    imports,
		Tools:      tools,
		ToolPaths:  toolPaths,
		Meta:       wire.Meta,
		Steps:      wire.Steps,
		Tree:       wire.Tree,
	}, nil
}

func normalizeImportsValue(value interface{}) (map[string]string, error) {
	if value == nil {
		return nil, nil
	}

	imports := make(map[string]string)
	add := func(alias, path string) {
		alias = strings.TrimSpace(alias)
		path = strings.TrimSpace(path)
		if alias == "" {
			return
		}
		if path == "" {
			path = filepath.Join("..", alias, alias+".runbook.yaml")
		} else {
			path = normalizeRunbookRefPath(path)
		}
		imports[alias] = path
	}

	var parseSpec func(alias string, spec interface{}) error
	parseSpec = func(alias string, spec interface{}) error {
		if spec == nil {
			add(alias, "")
			return nil
		}
		switch typed := spec.(type) {
		case string:
			if alias == "" {
				add(typed, "")
			} else {
				add(alias, typed)
			}
			return nil
		case map[string]interface{}:
			if alias == "" {
				if foundAlias, ok := asString(typed["alias"]); ok {
					foundPath, _ := asString(typed["path"])
					if foundPath == "" {
						foundPath, _ = asString(typed["runbook"])
					}
					add(foundAlias, foundPath)
					return nil
				}
				if foundAlias, ok := asString(typed["name"]); ok {
					foundPath, _ := asString(typed["path"])
					if foundPath == "" {
						foundPath, _ = asString(typed["runbook"])
					}
					add(foundAlias, foundPath)
					return nil
				}
				if len(typed) == 1 {
					for k, v := range typed {
						return parseSpec(k, v)
					}
				}
				return fmt.Errorf("expected import alias in object form")
			}

			path := ""
			if v, ok := asString(typed["path"]); ok {
				path = v
			}
			if path == "" {
				if v, ok := asString(typed["runbook"]); ok {
					path = v
				}
			}
			add(alias, path)
			return nil
		case map[interface{}]interface{}:
			converted := make(map[string]interface{}, len(typed))
			for k, v := range typed {
				converted[fmt.Sprint(k)] = v
			}
			return parseSpec(alias, converted)
		default:
			if s, ok := asString(spec); ok {
				if alias == "" {
					add(s, "")
				} else {
					add(alias, s)
				}
				return nil
			}
			return fmt.Errorf("unsupported imports value type %T", spec)
		}
	}

	switch typed := value.(type) {
	case string:
		add(typed, "")
	case []interface{}:
		for _, item := range typed {
			if err := parseSpec("", item); err != nil {
				return nil, err
			}
		}
	case map[string]interface{}:
		for alias, spec := range typed {
			if err := parseSpec(strings.TrimSpace(alias), spec); err != nil {
				return nil, err
			}
		}
	case map[interface{}]interface{}:
		for rawAlias, spec := range typed {
			if err := parseSpec(strings.TrimSpace(fmt.Sprint(rawAlias)), spec); err != nil {
				return nil, err
			}
		}
	default:
		return nil, fmt.Errorf("unsupported imports value type %T", value)
	}

	if len(imports) == 0 {
		return nil, nil
	}
	return imports, nil
}

func normalizeToolsValue(value interface{}) ([]string, map[string]string, error) {
	if value == nil {
		return nil, nil, nil
	}

	seen := make(map[string]struct{})
	tools := make([]string, 0)
	toolPaths := make(map[string]string)
	add := func(name, path string) {
		name = strings.TrimSpace(name)
		path = strings.TrimSpace(path)
		if name == "" {
			return
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			tools = append(tools, name)
		}
		if path != "" {
			toolPaths[name] = path
		}
	}

	var parseSpec func(name string, spec interface{}) error
	parseSpec = func(name string, spec interface{}) error {
		if spec == nil {
			add(name, "")
			return nil
		}
		switch typed := spec.(type) {
		case string:
			if name == "" {
				add(typed, "")
			} else {
				add(name, typed)
			}
			return nil
		case map[string]interface{}:
			if name == "" {
				if foundName, ok := asString(typed["name"]); ok {
					foundPath, _ := asString(typed["path"])
					if foundPath == "" {
						foundPath, _ = asString(typed["file"])
					}
					add(foundName, foundPath)
					return nil
				}
				if foundName, ok := asString(typed["tool"]); ok {
					foundPath, _ := asString(typed["path"])
					if foundPath == "" {
						foundPath, _ = asString(typed["file"])
					}
					add(foundName, foundPath)
					return nil
				}
				if len(typed) == 1 {
					for k, v := range typed {
						return parseSpec(k, v)
					}
				}
				return fmt.Errorf("expected tool name in object form")
			}

			path := ""
			if v, ok := asString(typed["path"]); ok {
				path = v
			}
			if path == "" {
				if v, ok := asString(typed["file"]); ok {
					path = v
				}
			}
			add(name, path)
			return nil
		case map[interface{}]interface{}:
			converted := make(map[string]interface{}, len(typed))
			for k, v := range typed {
				converted[fmt.Sprint(k)] = v
			}
			return parseSpec(name, converted)
		default:
			if s, ok := asString(spec); ok {
				if name == "" {
					add(s, "")
				} else {
					add(name, s)
				}
				return nil
			}
			return fmt.Errorf("unsupported tools value type %T", spec)
		}
	}

	switch typed := value.(type) {
	case string:
		add(typed, "")
	case []interface{}:
		for _, item := range typed {
			if err := parseSpec("", item); err != nil {
				return nil, nil, err
			}
		}
	case map[string]interface{}:
		for name, spec := range typed {
			if err := parseSpec(strings.TrimSpace(name), spec); err != nil {
				return nil, nil, err
			}
		}
	case map[interface{}]interface{}:
		for rawName, spec := range typed {
			if err := parseSpec(strings.TrimSpace(fmt.Sprint(rawName)), spec); err != nil {
				return nil, nil, err
			}
		}
	default:
		return nil, nil, fmt.Errorf("unsupported tools value type %T", value)
	}

	if len(tools) == 0 {
		return nil, nil, nil
	}
	if len(toolPaths) == 0 {
		toolPaths = nil
	}
	return tools, toolPaths, nil
}

func asString(v interface{}) (string, bool) {
	if v == nil {
		return "", false
	}
	s := strings.TrimSpace(fmt.Sprint(v))
	if s == "" || s == "<nil>" {
		return "", false
	}
	return s, true
}

func normalizeRunbookRefPath(path string) string {
	p := strings.TrimSpace(path)
	if p == "" {
		return p
	}
	lower := strings.ToLower(p)
	if strings.HasSuffix(lower, ".runbook.yaml") || strings.HasSuffix(lower, ".runbook.yml") {
		return p
	}
	if strings.HasSuffix(lower, ".runbook") {
		return p + ".yaml"
	}
	ext := strings.ToLower(filepath.Ext(p))
	if ext == ".yaml" || ext == ".yml" {
		return p
	}
	return p + ".runbook.yaml"
}
