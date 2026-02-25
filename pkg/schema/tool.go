// Package schema — tool.go defines Go struct types for the tool definition
// YAML schema (.tool.yaml files) and provides strict YAML parsing.
package schema

import (
	"fmt"
	"io"
	"os"
	"regexp"

	"gopkg.in/yaml.v3"
)

// ToolDefinition represents a .tool.yaml file that declares a tool's binary,
// transport, actions, typed arguments, capture rules, governance constraints,
// and optional capabilities (e.g. input resolution).
type ToolDefinition struct {
	APIVersion   string                `yaml:"apiVersion"  json:"apiVersion"  jsonschema:"required,const=tool/v0"`
	Meta         ToolMeta              `yaml:"meta"        json:"meta"        jsonschema:"required"`
	Transport    ToolTransport         `yaml:"transport,omitempty" json:"transport,omitempty"`
	Governance   *ToolGovernance       `yaml:"governance,omitempty" json:"governance,omitempty"`
	Capabilities *ToolCapabilities     `yaml:"capabilities,omitempty" json:"capabilities,omitempty"`
	Actions      map[string]ToolAction `yaml:"actions,omitempty"     json:"actions,omitempty"`
}

// ToolCapabilities declares implicit integration capabilities.
// A tool with capabilities is dispatched by the engine based on declared
// prefixes or context, rather than explicit step invocation.
type ToolCapabilities struct {
	ResolveInputs *ResolveInputsCap `yaml:"resolve_inputs,omitempty" json:"resolve_inputs,omitempty"`
}

// ResolveInputsCap describes the input resolution capability.
// Tools declaring this are automatically dispatched when inputs use matching prefixes.
type ResolveInputsCap struct {
	Prefixes      []string `yaml:"prefixes"                json:"prefixes"      jsonschema:"required,minItems=1"`
	ContextFields []string `yaml:"context_fields,omitempty" json:"context_fields,omitempty"`
}

// ToolMeta holds the tool's identity and the binary used for execution.
type ToolMeta struct {
	Name        string `yaml:"name"                 json:"name"        jsonschema:"required"`
	Version     string `yaml:"version,omitempty"     json:"version,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Binary      string `yaml:"binary"               json:"binary"      jsonschema:"required"`
}

// ToolTransport specifies how gert communicates with the tool process.
type ToolTransport struct {
	Mode    string       `yaml:"mode,omitempty"    json:"mode,omitempty"    jsonschema:"enum=stdio,enum=jsonrpc,enum=mcp,default=stdio"`
	Binary  string       `yaml:"binary,omitempty"  json:"binary,omitempty"`
	Connect string       `yaml:"connect,omitempty" json:"connect,omitempty"`
	Startup *ToolStartup `yaml:"startup,omitempty" json:"startup,omitempty"`
}

// ToolStartup configures how a persistent tool process is launched.
type ToolStartup struct {
	Argv           []string `yaml:"argv,omitempty"            json:"argv,omitempty"`
	ReadySignal    string   `yaml:"ready_signal,omitempty"    json:"ready_signal,omitempty"`
	Timeout        string   `yaml:"timeout,omitempty"         json:"timeout,omitempty"        jsonschema:"pattern=^[0-9]+(s|m|h)$"`
	ShutdownMethod string   `yaml:"shutdown_method,omitempty" json:"shutdown_method,omitempty"`
}

// ToolGovernance declares tool-wide governance defaults.
type ToolGovernance struct {
	ReadOnly bool            `yaml:"read_only,omitempty" json:"read_only,omitempty"`
	Redact   []RedactionRule `yaml:"redact,omitempty"    json:"redact,omitempty"`
}

// ToolAction defines a single invocable operation on a tool.
type ToolAction struct {
	Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
	Argv        []string               `yaml:"argv,omitempty"        json:"argv,omitempty"`
	Method      string                 `yaml:"method,omitempty"      json:"method,omitempty"`
	MCPTool     string                 `yaml:"mcp_tool,omitempty"    json:"mcp_tool,omitempty"`
	Args        map[string]ToolArg     `yaml:"args,omitempty"        json:"args,omitempty"`
	Capture     map[string]ToolCapture `yaml:"capture,omitempty"     json:"capture,omitempty"`
	Governance  *ActionGovernance      `yaml:"governance,omitempty"  json:"governance,omitempty"`
}

// ToolArg defines a single typed argument for a tool action.
type ToolArg struct {
	Type        string   `yaml:"type"                  json:"type"        jsonschema:"required,enum=string,enum=int,enum=bool,enum=float"`
	Required    bool     `yaml:"required,omitempty"    json:"required,omitempty"`
	Default     string   `yaml:"default,omitempty"     json:"default,omitempty"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Enum        []string `yaml:"enum,omitempty"        json:"enum,omitempty"`
	Redact      bool     `yaml:"redact,omitempty"      json:"redact,omitempty"`
}

// ToolCapture declares how output is extracted from a tool action.
type ToolCapture struct {
	From   string `yaml:"from,omitempty"   json:"from,omitempty"`
	Format string `yaml:"format,omitempty" json:"format,omitempty" jsonschema:"enum=text,enum=json"`
}

// ActionGovernance declares per-action governance overrides.
type ActionGovernance struct {
	ReadOnly         bool `yaml:"read_only,omitempty"         json:"read_only,omitempty"`
	RequiresApproval bool `yaml:"requires_approval,omitempty" json:"requires_approval,omitempty"`
	ApprovalMin      int  `yaml:"approval_min,omitempty"      json:"approval_min,omitempty"`
}

// ToolStepConfig configures a type:tool step in a runbook.
type ToolStepConfig struct {
	Name   string            `yaml:"name"           json:"name"   jsonschema:"required"`
	Action string            `yaml:"action"         json:"action" jsonschema:"required"`
	Args   map[string]string `yaml:"args,omitempty" json:"args,omitempty"`
}

// LoadToolFile reads and parses a .tool.yaml file with strict unknown-field rejection.
func LoadToolFile(path string) (*ToolDefinition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open tool definition: %w", err)
	}
	defer f.Close()
	return LoadTool(f)
}

// LoadTool parses a tool definition from an io.Reader with strict unknown-field rejection.
func LoadTool(r io.Reader) (*ToolDefinition, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var td ToolDefinition
	if err := dec.Decode(&td); err != nil {
		return nil, fmt.Errorf("decode tool definition: %w", err)
	}
	return &td, nil
}

// ValidateToolDefinition performs domain-level validation on a parsed tool definition.
func ValidateToolDefinition(td *ToolDefinition) []*ValidationError {
	var errs []*ValidationError

	// apiVersion
	if td.APIVersion != "tool/v0" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "apiVersion",
			Message:  fmt.Sprintf("unrecognized apiVersion %q, expected %q", td.APIVersion, "tool/v0"),
			Severity: "error",
		})
	}

	// meta.name required
	if td.Meta.Name == "" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "meta.name",
			Message:  "tool definition requires meta.name",
			Severity: "error",
		})
	}

	// meta.binary required
	if td.Meta.Binary == "" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "meta.binary",
			Message:  "tool definition requires meta.binary",
			Severity: "error",
		})
	}

	// At least one of actions or capabilities
	hasActions := len(td.Actions) > 0
	hasCapabilities := td.Capabilities != nil && td.Capabilities.ResolveInputs != nil
	if !hasActions && !hasCapabilities {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "actions",
			Message:  "tool definition requires at least one action or capability",
			Severity: "error",
		})
	}

	// Validate capabilities if present
	if td.Capabilities != nil && td.Capabilities.ResolveInputs != nil {
		if len(td.Capabilities.ResolveInputs.Prefixes) == 0 {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     "capabilities.resolve_inputs.prefixes",
				Message:  "resolve_inputs requires at least one prefix",
				Severity: "error",
			})
		}
	}

	// Determine effective transport mode
	mode := td.Transport.Mode
	if mode == "" {
		mode = "stdio"
	}

	// Transport-specific validation
	validModes := map[string]bool{"stdio": true, "jsonrpc": true, "mcp": true}
	if !validModes[mode] {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "transport.mode",
			Message:  fmt.Sprintf("invalid transport mode %q: must be stdio, jsonrpc, or mcp", mode),
			Severity: "error",
		})
	}

	if td.Transport.Connect != "" && mode != "mcp" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "transport.connect",
			Message:  "transport.connect is only valid for mode: mcp",
			Severity: "error",
		})
	}

	if td.Transport.Startup != nil && mode == "stdio" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "transport.startup",
			Message:  "transport.startup is not valid for mode: stdio (processes are spawned per call)",
			Severity: "warning",
		})
	}

	// Validate governance redaction patterns
	if td.Governance != nil {
		for i, rule := range td.Governance.Redact {
			if _, err := regexp.Compile(rule.Pattern); err != nil {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("governance.redact[%d].pattern", i),
					Message:  fmt.Sprintf("invalid regex pattern %q: %v", rule.Pattern, err),
					Severity: "error",
				})
			}
		}
	}

	// Skip action validation if no actions (capabilities-only tool)
	if !hasActions {
		return errs
	}

	// Validate each action
	for name, action := range td.Actions {
		prefix := fmt.Sprintf("actions.%s", name)

		// Action must have the right dispatch field for the transport mode
		switch mode {
		case "stdio":
			if len(action.Argv) == 0 {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     prefix + ".argv",
					Message:  fmt.Sprintf("action %q requires 'argv' for stdio transport", name),
					Severity: "error",
				})
			}
		case "jsonrpc":
			if action.Method == "" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     prefix + ".method",
					Message:  fmt.Sprintf("action %q requires 'method' for jsonrpc transport", name),
					Severity: "error",
				})
			}
		case "mcp":
			if action.MCPTool == "" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     prefix + ".mcp_tool",
					Message:  fmt.Sprintf("action %q requires 'mcp_tool' for mcp transport", name),
					Severity: "error",
				})
			}
		}

		// Validate args
		validArgTypes := map[string]bool{"string": true, "int": true, "bool": true, "float": true}
		for argName, arg := range action.Args {
			argPath := fmt.Sprintf("%s.args.%s", prefix, argName)

			if !validArgTypes[arg.Type] {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     argPath + ".type",
					Message:  fmt.Sprintf("arg %q has invalid type %q: must be string, int, bool, or float", argName, arg.Type),
					Severity: "error",
				})
			}

			// Required args should not have default
			if arg.Required && arg.Default != "" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     argPath,
					Message:  fmt.Sprintf("arg %q is required and should not have a default value", argName),
					Severity: "warning",
				})
			}

			// Enum only valid for string type
			if len(arg.Enum) > 0 && arg.Type != "string" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     argPath + ".enum",
					Message:  fmt.Sprintf("arg %q has enum but type is %q (enum requires type: string)", argName, arg.Type),
					Severity: "error",
				})
			}
		}

		// Validate governance
		if action.Governance != nil {
			if action.Governance.ApprovalMin > 0 && !action.Governance.RequiresApproval {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     prefix + ".governance.approval_min",
					Message:  fmt.Sprintf("action %q has approval_min but requires_approval is not set", name),
					Severity: "error",
				})
			}
		}

		// Validate capture format
		for capName, cap := range action.Capture {
			if cap.Format != "" && cap.Format != "text" && cap.Format != "json" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("%s.capture.%s.format", prefix, capName),
					Message:  fmt.Sprintf("capture %q has invalid format %q: must be text or json", capName, cap.Format),
					Severity: "error",
				})
			}
		}
	}

	return errs
}

// ValidateToolFile loads and validates a .tool.yaml file in one call.
func ValidateToolFile(path string) (*ToolDefinition, []*ValidationError) {
	td, err := LoadToolFile(path)
	if err != nil {
		return nil, []*ValidationError{{
			Phase:    "structural",
			Path:     "",
			Message:  err.Error(),
			Severity: "error",
		}}
	}
	errs := ValidateToolDefinition(td)
	if len(errs) > 0 {
		return td, errs
	}
	return td, nil
}
