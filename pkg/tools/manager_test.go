package tools

import (
	"context"
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
