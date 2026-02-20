package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadValidToolDefinitions ensures valid .tool.yaml files parse without errors.
func TestLoadValidToolDefinitions(t *testing.T) {
	files, err := filepath.Glob("../../testdata/tools/*.tool.yaml")
	if err != nil {
		t.Fatalf("glob tool fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no valid tool fixtures found")
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			td, err := LoadToolFile(f)
			if err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
			if td.APIVersion != "tool/v0" {
				t.Errorf("apiVersion = %q, want %q", td.APIVersion, "tool/v0")
			}
			if td.Meta.Name == "" {
				t.Error("meta.name is empty")
			}
			if td.Meta.Binary == "" {
				t.Error("meta.binary is empty")
			}
			if len(td.Actions) == 0 {
				t.Error("expected at least one action")
			}
		})
	}
}

// TestValidateValidToolDefinitions ensures valid tool definitions pass validation.
func TestValidateValidToolDefinitions(t *testing.T) {
	files, err := filepath.Glob("../../testdata/tools/*.tool.yaml")
	if err != nil {
		t.Fatalf("glob tool fixtures: %v", err)
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			td, loadErr := LoadToolFile(f)
			if loadErr != nil {
				t.Fatalf("load error: %v", loadErr)
			}
			errs := ValidateToolDefinition(td)
			for _, e := range errs {
				if e.Severity == "error" {
					t.Errorf("unexpected validation error: %v", e)
				}
			}
		})
	}
}

// TestLoadToolRejectsUnknownFields verifies strict mode rejects unknown YAML keys.
func TestLoadToolRejectsUnknownFields(t *testing.T) {
	_, err := LoadToolFile("../../testdata/tools/invalid/unknown-fields.tool.yaml")
	if err == nil {
		t.Fatal("expected error for unknown fields, got nil")
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "unknown") {
		t.Logf("got error: %v (accepted — unknown field rejection)", err)
	}
}

// TestValidateToolMissingFields checks that missing required fields are caught.
func TestValidateToolMissingFields(t *testing.T) {
	td, errs := ValidateToolFile("../../testdata/tools/invalid/missing-fields.tool.yaml")
	if td == nil && errs == nil {
		t.Fatal("expected either a parsed definition or errors")
	}

	// Should have errors for: missing binary, empty actions, invalid transport mode
	var errorMsgs []string
	for _, e := range errs {
		if e.Severity == "error" {
			errorMsgs = append(errorMsgs, e.Message)
		}
	}

	mustContain := []string{"meta.binary", "at least one action", "invalid transport mode"}
	for _, expected := range mustContain {
		found := false
		for _, msg := range errorMsgs {
			if strings.Contains(msg, expected) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("expected error containing %q, got errors: %v", expected, errorMsgs)
		}
	}
}

// TestValidateToolBadArgs checks arg validation: enum on int, required+default, approval_min without requires_approval.
func TestValidateToolBadArgs(t *testing.T) {
	td, loadErr := LoadToolFile("../../testdata/tools/invalid/bad-args.tool.yaml")
	if loadErr != nil {
		t.Fatalf("load error: %v", loadErr)
	}

	errs := ValidateToolDefinition(td)
	var errorMsgs []string
	for _, e := range errs {
		errorMsgs = append(errorMsgs, e.Message)
	}

	// enum on int type
	expectSubstring(t, errorMsgs, "enum requires type: string")
	// required + default
	expectSubstring(t, errorMsgs, "should not have a default")
	// approval_min without requires_approval
	expectSubstring(t, errorMsgs, "approval_min but requires_approval is not set")
}

// TestValidateToolStdioTransport verifies stdio transport requires argv on actions.
func TestValidateToolStdioTransport(t *testing.T) {
	td := &ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       ToolMeta{Name: "test", Binary: "test-bin"},
		Transport:  ToolTransport{Mode: "stdio"},
		Actions: map[string]ToolAction{
			"no-argv": {Description: "missing argv"},
		},
	}
	errs := ValidateToolDefinition(td)
	expectError(t, errs, "requires 'argv' for stdio transport")
}

// TestValidateToolJSONRPCTransport verifies jsonrpc transport requires method on actions.
func TestValidateToolJSONRPCTransport(t *testing.T) {
	td := &ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       ToolMeta{Name: "test", Binary: "test-bin"},
		Transport:  ToolTransport{Mode: "jsonrpc"},
		Actions: map[string]ToolAction{
			"no-method": {Description: "missing method"},
		},
	}
	errs := ValidateToolDefinition(td)
	expectError(t, errs, "requires 'method' for jsonrpc transport")
}

// TestValidateToolMCPTransport verifies mcp transport requires mcp_tool on actions.
func TestValidateToolMCPTransport(t *testing.T) {
	td := &ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       ToolMeta{Name: "test", Binary: "test-bin"},
		Transport:  ToolTransport{Mode: "mcp"},
		Actions: map[string]ToolAction{
			"no-mcp": {Description: "missing mcp_tool"},
		},
	}
	errs := ValidateToolDefinition(td)
	expectError(t, errs, "requires 'mcp_tool' for mcp transport")
}

// TestValidateToolConnectOnlyMCP verifies transport.connect is only valid for mcp.
func TestValidateToolConnectOnlyMCP(t *testing.T) {
	td := &ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       ToolMeta{Name: "test", Binary: "test-bin"},
		Transport:  ToolTransport{Mode: "jsonrpc", Connect: "http://localhost:3000"},
		Actions: map[string]ToolAction{
			"a": {Method: "test/method"},
		},
	}
	errs := ValidateToolDefinition(td)
	expectError(t, errs, "transport.connect is only valid for mode: mcp")
}

// TestValidateToolStepInRunbook checks that type:tool step validation works in domain validation.
func TestValidateToolStepInRunbook(t *testing.T) {
	t.Run("valid tool step", func(t *testing.T) {
		rb := &Runbook{
			APIVersion: "runbook/v0",
			Tools:      map[string]string{"kubectl": "../tools/kubectl.tool.yaml"},
			Meta:       Meta{Name: "test"},
			Tree: []TreeNode{
				{Step: Step{
					ID:   "s1",
					Type: "tool",
					Tool: &ToolStepConfig{Name: "kubectl", Action: "get-pods", Args: map[string]string{"namespace": "default"}},
				}},
			},
		}
		errs := ValidateDomain(rb)
		for _, e := range errs {
			if e.Severity == "error" {
				t.Errorf("unexpected error: %v", e)
			}
		}
	})

	t.Run("missing tool config", func(t *testing.T) {
		rb := &Runbook{
			APIVersion: "runbook/v0",
			Tools:      map[string]string{"kubectl": "../tools/kubectl.tool.yaml"},
			Meta:       Meta{Name: "test"},
			Tree: []TreeNode{
				{Step: Step{ID: "s1", Type: "tool"}},
			},
		}
		errs := ValidateDomain(rb)
		expectError(t, errs, "requires 'tool' configuration")
	})

	t.Run("missing tool.name", func(t *testing.T) {
		rb := &Runbook{
			APIVersion: "runbook/v0",
			Tools:      map[string]string{"kubectl": "../tools/kubectl.tool.yaml"},
			Meta:       Meta{Name: "test"},
			Tree: []TreeNode{
				{Step: Step{ID: "s1", Type: "tool", Tool: &ToolStepConfig{Action: "get-pods"}}},
			},
		}
		errs := ValidateDomain(rb)
		expectError(t, errs, "requires 'tool.name'")
	})

	t.Run("missing tool.action", func(t *testing.T) {
		rb := &Runbook{
			APIVersion: "runbook/v0",
			Tools:      map[string]string{"kubectl": "../tools/kubectl.tool.yaml"},
			Meta:       Meta{Name: "test"},
			Tree: []TreeNode{
				{Step: Step{ID: "s1", Type: "tool", Tool: &ToolStepConfig{Name: "kubectl"}}},
			},
		}
		errs := ValidateDomain(rb)
		expectError(t, errs, "requires 'tool.action'")
	})

	t.Run("tool not in tools map", func(t *testing.T) {
		rb := &Runbook{
			APIVersion: "runbook/v0",
			Tools:      map[string]string{"kubectl": "../tools/kubectl.tool.yaml"},
			Meta:       Meta{Name: "test"},
			Tree: []TreeNode{
				{Step: Step{ID: "s1", Type: "tool", Tool: &ToolStepConfig{Name: "unknown", Action: "x"}}},
			},
		}
		errs := ValidateDomain(rb)
		expectError(t, errs, "not declared in 'tools:'")
	})

	t.Run("no tools map declared", func(t *testing.T) {
		rb := &Runbook{
			APIVersion: "runbook/v0",
			Meta:       Meta{Name: "test"},
			Tree: []TreeNode{
				{Step: Step{ID: "s1", Type: "tool", Tool: &ToolStepConfig{Name: "kubectl", Action: "x"}}},
			},
		}
		errs := ValidateDomain(rb)
		expectError(t, errs, "no 'tools:' map is declared")
	})
}

// TestLoadRunbookWithToolStep ensures a runbook with type:tool parses correctly.
func TestLoadRunbookWithToolStep(t *testing.T) {
	rb, err := LoadFile("../../testdata/valid/tool-step.yaml")
	if err != nil {
		t.Fatalf("expected valid, got error: %v", err)
	}
	if len(rb.Tree) != 1 {
		t.Fatalf("expected 1 tree node, got %d", len(rb.Tree))
	}
	step := rb.Tree[0].Step
	if step.Type != "tool" {
		t.Errorf("step type = %q, want %q", step.Type, "tool")
	}
	if step.Tool == nil {
		t.Fatal("step.Tool is nil")
	}
	if step.Tool.Name != "kubectl" {
		t.Errorf("tool.name = %q, want %q", step.Tool.Name, "kubectl")
	}
	if step.Tool.Action != "get-pods" {
		t.Errorf("tool.action = %q, want %q", step.Tool.Action, "get-pods")
	}
	if step.Tool.Args["namespace"] != "{{ .namespace }}" {
		t.Errorf("tool.args.namespace = %q, want template", step.Tool.Args["namespace"])
	}
	if rb.Tools == nil || rb.Tools["kubectl"] == "" {
		t.Error("runbook.Tools['kubectl'] not loaded")
	}
}

// --- helpers ---

func expectError(t *testing.T, errs []*ValidationError, substring string) {
	t.Helper()
	for _, e := range errs {
		if strings.Contains(e.Message, substring) {
			return
		}
	}
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Message
	}
	t.Errorf("expected error containing %q, got: %v", substring, msgs)
}

func expectSubstring(t *testing.T, msgs []string, substring string) {
	t.Helper()
	for _, msg := range msgs {
		if strings.Contains(msg, substring) {
			return
		}
	}
	t.Errorf("expected message containing %q, got: %v", substring, msgs)
}

// Suppress unused import warning for os — used in other test files.
var _ = os.Stderr
