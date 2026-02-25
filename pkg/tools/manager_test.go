package tools

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// mockExecutor returns canned responses for testing.
type mockExecutor struct {
	stdout   string
	stderr   string
	exitCode int
}

func (m *mockExecutor) Execute(ctx context.Context, command string, args []string, env []string) (*providers.CommandResult, error) {
	return &providers.CommandResult{
		Stdout:   []byte(m.stdout),
		Stderr:   []byte(m.stderr),
		ExitCode: m.exitCode,
	}, nil
}

func TestManagerLoadAndExecute(t *testing.T) {
	executor := &mockExecutor{stdout: "pod-list-json", exitCode: 0}
	mgr := NewManager(executor, nil)

	err := mgr.Load("kubectl", "../../testdata/tools/kubectl.tool.yaml", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Verify loaded
	td := mgr.GetDef("kubectl")
	if td == nil {
		t.Fatal("expected kubectl def, got nil")
	}
	if td.Meta.Name != "kubectl" {
		t.Errorf("meta.name = %q, want %q", td.Meta.Name, "kubectl")
	}

	// Execute get-pods
	result, err := mgr.Execute(context.Background(), "kubectl", "get-pods",
		map[string]string{"namespace": "default"},
		nil,
	)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
	if result.Stdout != "pod-list-json" {
		t.Errorf("stdout = %q, want %q", result.Stdout, "pod-list-json")
	}
}

func TestManagerExecuteMissingRequiredArg(t *testing.T) {
	executor := &mockExecutor{stdout: "", exitCode: 0}
	mgr := NewManager(executor, nil)

	err := mgr.Load("kubectl", "../../testdata/tools/kubectl.tool.yaml", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	// Execute without required namespace arg
	_, err = mgr.Execute(context.Background(), "kubectl", "get-pods",
		map[string]string{},
		nil,
	)
	if err == nil {
		t.Fatal("expected error for missing required arg, got nil")
	}
	if !contains(err.Error(), "required") {
		t.Errorf("error %q should mention 'required'", err.Error())
	}
}

func TestManagerExecuteUnknownTool(t *testing.T) {
	mgr := NewManager(&mockExecutor{}, nil)

	_, err := mgr.Execute(context.Background(), "nonexistent", "action", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown tool, got nil")
	}
}

func TestManagerExecuteUnknownAction(t *testing.T) {
	executor := &mockExecutor{stdout: "", exitCode: 0}
	mgr := NewManager(executor, nil)

	err := mgr.Load("kubectl", "../../testdata/tools/kubectl.tool.yaml", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err = mgr.Execute(context.Background(), "kubectl", "nonexistent-action", nil, nil)
	if err == nil {
		t.Fatal("expected error for unknown action")
	}
}

func TestManagerValidateStep(t *testing.T) {
	executor := &mockExecutor{}
	mgr := NewManager(executor, nil)

	err := mgr.Load("kubectl", "../../testdata/tools/kubectl.tool.yaml", "")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}

	t.Run("valid step", func(t *testing.T) {
		errs := mgr.ValidateStep(&schema.ToolStepConfig{
			Name:   "kubectl",
			Action: "get-pods",
			Args:   map[string]string{"namespace": "default"},
		})
		if len(errs) > 0 {
			t.Errorf("unexpected errors: %v", errs)
		}
	})

	t.Run("missing required arg", func(t *testing.T) {
		errs := mgr.ValidateStep(&schema.ToolStepConfig{
			Name:   "kubectl",
			Action: "get-pods",
			Args:   map[string]string{},
		})
		if len(errs) == 0 {
			t.Error("expected error for missing required arg")
		}
	})

	t.Run("unknown arg", func(t *testing.T) {
		errs := mgr.ValidateStep(&schema.ToolStepConfig{
			Name:   "kubectl",
			Action: "get-pods",
			Args:   map[string]string{"namespace": "default", "bogus": "x"},
		})
		hasUnknown := false
		for _, e := range errs {
			if contains(e, "unknown arg") {
				hasUnknown = true
			}
		}
		if !hasUnknown {
			t.Error("expected warning about unknown arg")
		}
	})

	t.Run("unknown tool", func(t *testing.T) {
		errs := mgr.ValidateStep(&schema.ToolStepConfig{
			Name:   "nonexistent",
			Action: "x",
		})
		if len(errs) == 0 {
			t.Error("expected error for unknown tool")
		}
	})

	t.Run("unknown action", func(t *testing.T) {
		errs := mgr.ValidateStep(&schema.ToolStepConfig{
			Name:   "kubectl",
			Action: "nonexistent",
		})
		if len(errs) == 0 {
			t.Error("expected error for unknown action")
		}
	})
}

func TestResolveArgvTemplates(t *testing.T) {
	argv := []string{"get", "pods", "-n", "{{ .namespace }}", "-l", "{{ .selector }}"}
	data := map[string]string{
		"namespace": "production",
		"selector":  "app=web",
	}
	resolved, err := resolveArgvTemplates(argv, data)
	if err != nil {
		t.Fatalf("resolveArgvTemplates: %v", err)
	}
	expected := []string{"get", "pods", "-n", "production", "-l", "app=web"}
	for i, want := range expected {
		if resolved[i] != want {
			t.Errorf("argv[%d] = %q, want %q", i, resolved[i], want)
		}
	}
}

func TestApplyDefaults(t *testing.T) {
	act := schema.ToolAction{
		Args: map[string]schema.ToolArg{
			"namespace": {Type: "string", Required: true},
			"selector":  {Type: "string", Default: "app=default"},
		},
	}
	merged := applyDefaults(act, map[string]string{"namespace": "prod"})
	if merged["namespace"] != "prod" {
		t.Errorf("namespace = %q, want %q", merged["namespace"], "prod")
	}
	if merged["selector"] != "app=default" {
		t.Errorf("selector = %q, want %q", merged["selector"], "app=default")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsCheck(s, substr))
}

func containsCheck(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

// --- UsageReport parsing tests ---

func TestParseUsageFromCaptures_Present(t *testing.T) {
	captures := map[string]string{
		"patch":  "some-diff",
		"_usage": `{"prompt_tokens":150,"completion_tokens":80,"total_tokens":230,"model":"gpt-4o","estimated_cost":0.0023}`,
	}
	usage := parseUsageFromCaptures(captures)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PromptTokens != 150 {
		t.Errorf("PromptTokens = %d, want 150", usage.PromptTokens)
	}
	if usage.CompletionTokens != 80 {
		t.Errorf("CompletionTokens = %d, want 80", usage.CompletionTokens)
	}
	if usage.TotalTokens != 230 {
		t.Errorf("TotalTokens = %d, want 230", usage.TotalTokens)
	}
	if usage.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", usage.Model, "gpt-4o")
	}
	if usage.EstimatedCost != 0.0023 {
		t.Errorf("EstimatedCost = %f, want 0.0023", usage.EstimatedCost)
	}
	// _usage key should be removed from captures
	if _, ok := captures["_usage"]; ok {
		t.Error("_usage key should be deleted from captures")
	}
	// other captures should remain
	if captures["patch"] != "some-diff" {
		t.Error("non-usage capture should be preserved")
	}
}

func TestParseUsageFromCaptures_Absent(t *testing.T) {
	captures := map[string]string{"patch": "some-diff"}
	usage := parseUsageFromCaptures(captures)
	if usage != nil {
		t.Errorf("expected nil usage, got %+v", usage)
	}
}

func TestParseUsageFromCaptures_InvalidJSON(t *testing.T) {
	captures := map[string]string{"_usage": "not-json{"}
	usage := parseUsageFromCaptures(captures)
	if usage != nil {
		t.Errorf("expected nil for invalid JSON, got %+v", usage)
	}
}

func TestParseUsageFromCaptures_Empty(t *testing.T) {
	captures := map[string]string{"_usage": ""}
	usage := parseUsageFromCaptures(captures)
	if usage != nil {
		t.Errorf("expected nil for empty _usage, got %+v", usage)
	}
}

func TestParseUsageFromJSON_Present(t *testing.T) {
	raw := json.RawMessage(`{
		"patch": "--- a/file\n+++ b/file\n",
		"reasoning": "added null check",
		"_usage": {"prompt_tokens":200,"completion_tokens":100,"total_tokens":300,"model":"gpt-4o","estimated_cost":0.005}
	}`)
	usage := parseUsageFromJSON(raw)
	if usage == nil {
		t.Fatal("expected non-nil usage")
	}
	if usage.PromptTokens != 200 {
		t.Errorf("PromptTokens = %d, want 200", usage.PromptTokens)
	}
	if usage.CompletionTokens != 100 {
		t.Errorf("CompletionTokens = %d, want 100", usage.CompletionTokens)
	}
	if usage.TotalTokens != 300 {
		t.Errorf("TotalTokens = %d, want 300", usage.TotalTokens)
	}
	if usage.Model != "gpt-4o" {
		t.Errorf("Model = %q, want %q", usage.Model, "gpt-4o")
	}
}

func TestParseUsageFromJSON_Absent(t *testing.T) {
	raw := json.RawMessage(`{"patch": "diff", "reasoning": "ok"}`)
	usage := parseUsageFromJSON(raw)
	if usage != nil {
		t.Errorf("expected nil usage, got %+v", usage)
	}
}

func TestParseUsageFromJSON_NotObject(t *testing.T) {
	raw := json.RawMessage(`"just a string"`)
	usage := parseUsageFromJSON(raw)
	if usage != nil {
		t.Errorf("expected nil for non-object JSON, got %+v", usage)
	}
}

func TestParseUsageFromJSON_InvalidUsage(t *testing.T) {
	raw := json.RawMessage(`{"_usage": "not-an-object"}`)
	usage := parseUsageFromJSON(raw)
	if usage != nil {
		t.Errorf("expected nil for invalid _usage, got %+v", usage)
	}
}

func TestActionResultUsageNilByDefault(t *testing.T) {
	// Verify that a plain ActionResult has nil Usage — backward compatibility
	r := ActionResult{Stdout: "hello", ExitCode: 0}
	if r.Usage != nil {
		t.Error("expected nil Usage for default ActionResult")
	}
}
