package tools

import (
	"context"
	"testing"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

func TestStdioArgvResolution(t *testing.T) {
	executor := &mockExecutor{stdout: "ok", exitCode: 0}
	mgr := NewManager(executor, nil)
	mgr.defs["test"] = &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "test", Binary: "echo"},
		Transport:  schema.ToolTransport{Mode: "stdio"},
		Actions: map[string]schema.ToolAction{
			"greet": {
				Argv: []string{"hello", "{{ .name }}", "--flag={{ .flag }}"},
				Args: map[string]schema.ToolArg{
					"name": {Type: "string", Required: true},
					"flag": {Type: "string", Default: "default-val"},
				},
				Capture: map[string]schema.ToolCapture{
					"stdout": {Format: "text"},
				},
			},
		},
	}

	t.Run("templates resolved in argv", func(t *testing.T) {
		result, err := mgr.Execute(context.Background(), "test", "greet",
			map[string]string{"name": "world"},
			nil,
		)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.ExitCode != 0 {
			t.Errorf("exit code = %d", result.ExitCode)
		}
	})

	t.Run("default args applied", func(t *testing.T) {
		// flag should get default "default-val" since not provided
		result, err := mgr.Execute(context.Background(), "test", "greet",
			map[string]string{"name": "test"},
			nil,
		)
		if err != nil {
			t.Fatalf("Execute: %v", err)
		}
		if result.Stdout != "ok" {
			t.Errorf("stdout = %q, want %q", result.Stdout, "ok")
		}
	})
}

func TestStdioCaptureExtraction(t *testing.T) {
	executor := &mockExecutor{stdout: "  captured-value  \n", stderr: "err-output", exitCode: 0}
	mgr := NewManager(executor, nil)
	mgr.defs["cap"] = &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "cap", Binary: "test-bin"},
		Transport:  schema.ToolTransport{Mode: "stdio"},
		Actions: map[string]schema.ToolAction{
			"run": {
				Argv: []string{"run"},
				Capture: map[string]schema.ToolCapture{
					"out":    {From: "stdout", Format: "text"},
					"errout": {From: "stderr", Format: "text"},
				},
			},
		},
	}

	result, err := mgr.Execute(context.Background(), "cap", "run", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Captures["out"] != "captured-value" {
		t.Errorf("capture out = %q, want %q", result.Captures["out"], "captured-value")
	}
	if result.Captures["errout"] != "err-output" {
		t.Errorf("capture errout = %q, want %q", result.Captures["errout"], "err-output")
	}
}

func TestStdioJSONPathCapture(t *testing.T) {
	jsonOutput := `{"success": true, "rowCount": 42, "data": [{"name": "test"}]}`
	executor := &mockExecutor{stdout: jsonOutput, exitCode: 0}
	mgr := NewManager(executor, nil)
	mgr.defs["querytool"] = &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "querytool", Binary: "query-cli"},
		Transport:  schema.ToolTransport{Mode: "stdio"},
		Actions: map[string]schema.ToolAction{
			"query": {
				Argv: []string{"query"},
				Capture: map[string]schema.ToolCapture{
					"stdout":    {Format: "json"},
					"row_count": {From: "rowCount", Format: "json"},
				},
			},
		},
	}

	result, err := mgr.Execute(context.Background(), "querytool", "query", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.Captures["stdout"] != jsonOutput {
		t.Errorf("capture stdout = %q, want full JSON", result.Captures["stdout"])
	}
	if result.Captures["row_count"] != "42" {
		t.Errorf("capture row_count = %q, want %q", result.Captures["row_count"], "42")
	}
}

func TestStdioPerArgRedaction(t *testing.T) {
	executor := &mockExecutor{stdout: "ok", exitCode: 0}
	mgr := NewManager(executor, nil)
	mgr.defs["redact"] = &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "redact", Binary: "test-bin"},
		Transport:  schema.ToolTransport{Mode: "stdio"},
		Actions: map[string]schema.ToolAction{
			"secret": {
				Argv: []string{"--token={{ .token }}", "--name={{ .name }}"},
				Args: map[string]schema.ToolArg{
					"token": {Type: "string", Required: true, Redact: true},
					"name":  {Type: "string", Required: true},
				},
				Capture: map[string]schema.ToolCapture{
					"stdout": {Format: "text"},
				},
			},
		},
	}

	result, err := mgr.Execute(context.Background(), "redact", "secret",
		map[string]string{"token": "super-secret-123", "name": "visible"},
		nil,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}

	// RedactedArgs should have token masked but name visible
	if result.RedactedArgs["token"] != "[REDACTED]" {
		t.Errorf("redacted token = %q, want '[REDACTED]'", result.RedactedArgs["token"])
	}
	if result.RedactedArgs["name"] != "visible" {
		t.Errorf("redacted name = %q, want 'visible'", result.RedactedArgs["name"])
	}
}

func TestStdioToolLevelRedaction(t *testing.T) {
	executor := &mockExecutor{stdout: "Bearer abc123xyz output", exitCode: 0}
	mgr := NewManager(executor, nil)
	mgr.defs["redact-out"] = &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "redact-out", Binary: "test-bin"},
		Transport:  schema.ToolTransport{Mode: "stdio"},
		Governance: &schema.ToolGovernance{
			Redact: []schema.RedactionRule{
				{Pattern: "Bearer [A-Za-z0-9]+", Replace: "Bearer [REDACTED]"},
			},
		},
		Actions: map[string]schema.ToolAction{
			"run": {
				Argv: []string{"run"},
				Capture: map[string]schema.ToolCapture{
					"stdout": {Format: "text"},
				},
			},
		},
	}

	result, err := mgr.Execute(context.Background(), "redact-out", "run", nil, nil)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if contains(result.Stdout, "abc123xyz") {
		t.Errorf("stdout still contains secret: %q", result.Stdout)
	}
	if !contains(result.Stdout, "Bearer [REDACTED]") {
		t.Errorf("stdout not redacted: %q", result.Stdout)
	}
}

func TestStdioApprovalBlocks(t *testing.T) {
	executor := &mockExecutor{stdout: "should not run", exitCode: 0}
	mgr := NewManager(executor, nil)
	mgr.defs["destructive"] = &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "destructive", Binary: "kubectl"},
		Transport:  schema.ToolTransport{Mode: "stdio"},
		Actions: map[string]schema.ToolAction{
			"delete": {
				Argv: []string{"delete", "pod", "{{ .pod }}"},
				Args: map[string]schema.ToolArg{
					"pod": {Type: "string", Required: true},
				},
				Governance: &schema.ActionGovernance{
					RequiresApproval: true,
					ApprovalMin:      1,
				},
			},
		},
	}

	result, err := mgr.Execute(context.Background(), "destructive", "delete",
		map[string]string{"pod": "web-123"},
		nil,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !result.RequiresApproval {
		t.Error("expected RequiresApproval=true")
	}
	if result.ApprovalMin != 1 {
		t.Errorf("ApprovalMin = %d, want 1", result.ApprovalMin)
	}
	// Stdout should be empty — action was not executed
	if result.Stdout != "" {
		t.Errorf("expected empty stdout (not executed), got %q", result.Stdout)
	}
}

// mockExecutor is shared with manager_test.go via the same package
var _ providers.CommandExecutor = (*mockExecutor)(nil)
