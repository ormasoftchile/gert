package schema

import (
	"github.com/ormasoftchile/gert/pkg/kernel/contract"
)

// API version constant for tool definitions.
const APIVersionTool = "tool/v0"

// ---------------------------------------------------------------------------
// Tool Definition
// ---------------------------------------------------------------------------

// ToolDefinition is the top-level tool/v0 document.
type ToolDefinition struct {
	APIVersion string                `yaml:"apiVersion" json:"apiVersion"`
	Meta       ToolMeta              `yaml:"meta"       json:"meta"`
	Contract   contract.Contract     `yaml:"contract"   json:"contract"`
	Actions    map[string]ToolAction `yaml:"actions"    json:"actions"`
}

// ToolMeta describes a tool's identity and transport.
type ToolMeta struct {
	Name        string   `yaml:"name"        json:"name"`
	Description string   `yaml:"description,omitempty" json:"description,omitempty"`
	Transport   string   `yaml:"transport,omitempty"    json:"transport,omitempty"` // stdio, jsonrpc, mcp
	Binary      string   `yaml:"binary,omitempty"       json:"binary,omitempty"`
	Platform    []string `yaml:"platform,omitempty"     json:"platform,omitempty"` // e.g. ["linux","darwin"] or ["windows"]
}

// ToolAction is one named action within a tool definition.
type ToolAction struct {
	Description string             `yaml:"description,omitempty" json:"description,omitempty"`
	Argv        []string           `yaml:"argv,omitempty"        json:"argv,omitempty"`
	Method      string             `yaml:"method,omitempty"      json:"method,omitempty"`
	MCPTool     string             `yaml:"mcp_tool,omitempty"    json:"mcp_tool,omitempty"`
	Contract    *contract.Contract `yaml:"contract,omitempty"    json:"contract,omitempty"`
	Extract     map[string]Extract `yaml:"extract,omitempty"     json:"extract,omitempty"`
}

// Extract maps a tool output to a declared contract output.
type Extract struct {
	From    string `yaml:"from"              json:"from"` // stdout, stderr, json
	Pattern string `yaml:"pattern,omitempty" json:"pattern,omitempty"`
	Path    string `yaml:"path,omitempty"    json:"path,omitempty"` // for json format
}
