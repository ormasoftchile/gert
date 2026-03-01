package mcp

import (
	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"
)

// NewServer creates a new MCP server with gert tools registered.
func NewServer(version string) *server.MCPServer {
	s := server.NewMCPServer(
		"gert",
		version,
		server.WithToolCapabilities(true),
	)

	// Register tools
	s.AddTool(
		mcp.NewTool("gert/validate",
			mcp.WithDescription("Validate a gert runbook or tool definition YAML file"),
			mcp.WithString("path", mcp.Required(), mcp.Description("Path to the runbook or tool YAML file")),
		),
		HandleValidate,
	)

	s.AddTool(
		mcp.NewTool("gert/exec",
			mcp.WithDescription("Execute a gert runbook (defaults to dry-run mode for safety)"),
			mcp.WithString("path", mcp.Required(), mcp.Description("Path to the runbook YAML file")),
			mcp.WithString("mode", mcp.Description("Execution mode: real, dry-run, or probe")),
		),
		HandleExec,
	)

	s.AddTool(
		mcp.NewTool("gert/test",
			mcp.WithDescription("Run scenario replay tests for a gert runbook"),
			mcp.WithString("path", mcp.Required(), mcp.Description("Path to the runbook YAML file")),
			mcp.WithString("scenario", mcp.Description("Run only the named scenario (optional)")),
		),
		HandleTest,
	)

	s.AddTool(
		mcp.NewTool("gert/schema",
			mcp.WithDescription("Export gert JSON Schema (runbook or tool)"),
			mcp.WithString("type", mcp.Required(), mcp.Description("Schema type: 'runbook' or 'tool'")),
		),
		HandleSchema,
	)

	return s
}
