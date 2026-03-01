package mcp

import (
	"context"
	"testing"

	"github.com/mark3labs/mcp-go/mcp"
)

// T110: MCP validate handler returns correct result
func TestHandleValidate_MissingPath(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{}

	result, err := HandleValidate(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for missing path")
	}
}

// T111: MCP schema handler returns result
func TestHandleSchema_Runbook(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"type": "runbook"}

	result, err := HandleSchema(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Error("expected success for runbook schema")
	}
	if len(result.Content) == 0 {
		t.Error("expected schema content")
	}
}

func TestHandleSchema_UnknownType(t *testing.T) {
	req := mcp.CallToolRequest{}
	req.Params.Arguments = map[string]any{"type": "foo"}

	result, err := HandleSchema(context.Background(), req)
	if err != nil {
		t.Fatal(err)
	}
	if !result.IsError {
		t.Error("expected error for unknown schema type")
	}
}
