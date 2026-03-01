package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/ormasoftchile/gert/pkg/kernel/engine"
	kschema "github.com/ormasoftchile/gert/pkg/kernel/schema"
	ktesting "github.com/ormasoftchile/gert/pkg/kernel/testing"
	kvalidate "github.com/ormasoftchile/gert/pkg/kernel/validate"
)

// HandleValidate implements the gert/validate MCP tool.
func HandleValidate(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult("path argument is required"), nil
	}

	// Detect tool vs runbook
	if isToolFile(path) {
		td, errs := kvalidate.ValidateToolFile(path)
		if hasErrors(errs) {
			return errorResult(formatErrors(errs)), nil
		}
		name := ""
		if td != nil {
			name = td.Meta.Name
		}
		return textResult(fmt.Sprintf("✓ tool %s is valid (%d actions)", name, len(td.Actions))), nil
	}

	rb, errs := kvalidate.ValidateFile(path)
	if hasErrors(errs) {
		return errorResult(formatErrors(errs)), nil
	}
	return textResult(fmt.Sprintf("✓ %s is valid (%d steps)", rb.Meta.Name, len(rb.Steps))), nil
}

// HandleSchema implements the gert/schema MCP tool.
func HandleSchema(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	schemaType, _ := args["type"].(string)

	var data []byte
	var err error

	switch schemaType {
	case "runbook":
		data, err = kschema.GenerateRunbookJSONSchema()
	case "tool":
		data, err = kschema.GenerateToolJSONSchema()
	default:
		return errorResult(fmt.Sprintf("unknown schema type %q — use 'runbook' or 'tool'", schemaType)), nil
	}

	if err != nil {
		return errorResult(err.Error()), nil
	}
	return textResult(string(data)), nil
}

// HandleExec implements the gert/exec MCP tool.
func HandleExec(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult("path argument is required"), nil
	}
	mode, _ := args["mode"].(string)
	if mode == "" {
		mode = "dry-run" // safe default for AI agents
	}

	// Validate
	rb, errs := kvalidate.ValidateFile(path)
	if hasErrors(errs) {
		return errorResult(formatErrors(errs)), nil
	}

	// Parse vars
	vars := make(map[string]string)
	if rawVars, ok := args["vars"].(map[string]any); ok {
		for k, v := range rawVars {
			vars[k] = fmt.Sprint(v)
		}
	}

	// Resolve inputs
	resolved, err := engine.ResolveInputs(ctx, rb, vars, nil)
	if err != nil {
		return errorResult(fmt.Sprintf("input resolution: %s", err)), nil
	}

	// Execute
	var out bytes.Buffer
	cfg := engine.RunConfig{
		RunID:   "mcp-run-1",
		Mode:    mode,
		Vars:    resolved.Vars,
		BaseDir: filepath.Dir(path),
		Stdout:  &out,
	}

	eng := engine.New(rb, cfg)
	result := eng.Run(ctx)

	// Build response
	response := map[string]any{
		"status":   result.Status,
		"duration": result.Duration.String(),
		"mode":     mode,
	}
	if result.Outcome != nil {
		response["outcome"] = map[string]any{
			"category": string(result.Outcome.Category),
			"code":     result.Outcome.Code,
		}
	}
	if result.Error != nil {
		response["error"] = result.Error.Error()
	}
	if out.Len() > 0 {
		response["output"] = out.String()
	}

	data, _ := json.MarshalIndent(response, "", "  ")

	isErr := result.Status == "failed" || result.Status == "error"
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(string(data))},
		IsError: isErr,
	}, nil
}

// HandleTest implements the gert/test MCP tool.
func HandleTest(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := req.GetArguments()
	path, _ := args["path"].(string)
	if path == "" {
		return errorResult("path argument is required"), nil
	}

	scenarioName, _ := args["scenario"].(string)

	runner := &ktesting.Runner{
		Timeout:  30 * time.Second,
		FailFast: false,
	}

	var output *ktesting.TestOutput
	var err error

	if scenarioName != "" {
		result, e := runner.RunScenario(path, scenarioName)
		if e != nil {
			return errorResult(fmt.Sprintf("run scenario: %s", e)), nil
		}
		output = &ktesting.TestOutput{
			Runbook:   filepath.Base(path),
			Scenarios: []ktesting.TestResult{*result},
			Summary:   ktesting.TestSummary{Total: 1},
		}
		switch result.Status {
		case "passed":
			output.Summary.Passed = 1
		case "failed":
			output.Summary.Failed = 1
		default:
			output.Summary.Errors = 1
		}
	} else {
		output, err = runner.RunAll(path)
		if err != nil {
			return errorResult(fmt.Sprintf("run tests: %s", err)), nil
		}
	}

	data, _ := json.MarshalIndent(output, "", "  ")

	isErr := output.Summary.Failed > 0 || output.Summary.Errors > 0
	return &mcp.CallToolResult{
		Content: []mcp.Content{mcp.NewTextContent(string(data))},
		IsError: isErr,
	}, nil
}

// isToolFile checks if a file is a tool definition.
func isToolFile(path string) bool {
	return strings.Contains(path, ".tool.")
}

func hasErrors(errs []*kvalidate.ValidationError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}

func formatErrors(errs []*kvalidate.ValidationError) string {
	var msgs []string
	for _, e := range errs {
		if e.Severity == "error" {
			msgs = append(msgs, fmt.Sprintf("[%s] %s", e.Phase, e.Message))
		}
	}
	return strings.Join(msgs, "; ")
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(text),
		},
	}
}

func errorResult(msg string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{
			mcp.NewTextContent(msg),
		},
		IsError: true,
	}
}
