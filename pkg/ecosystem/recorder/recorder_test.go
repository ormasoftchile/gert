package recorder

import (
	"context"
	"os"
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/executor"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

type mockExecutor struct {
	result *executor.Result
}

func (m *mockExecutor) Execute(ctx context.Context, toolDef *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*executor.Result, error) {
	return m.result, nil
}

func TestRecorder_CapturesResponse(t *testing.T) {
	inner := &mockExecutor{
		result: &executor.Result{
			ExitCode: 0,
			Stdout:   "200",
			Outputs:  map[string]any{"status_code": "200"},
		},
	}

	rec := New(inner)
	td := &schema.ToolDefinition{
		Meta: schema.ToolMeta{Name: "curl"},
	}

	_, err := rec.Execute(context.Background(), td, "get", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(rec.Responses) != 1 {
		t.Fatalf("expected 1 response, got %d", len(rec.Responses))
	}

	r := rec.Responses[0]
	if r.Tool != "curl" || r.Action != "get" {
		t.Errorf("tool=%q action=%q, want curl:get", r.Tool, r.Action)
	}
	if r.Stdout != "200" {
		t.Errorf("stdout=%q, want 200", r.Stdout)
	}
}

func TestRecorder_RedactsSecrets(t *testing.T) {
	os.Setenv("TEST_SECRET_KEY", "supersecret123")
	defer os.Unsetenv("TEST_SECRET_KEY")

	inner := &mockExecutor{
		result: &executor.Result{
			ExitCode: 0,
			Stdout:   "auth: supersecret123",
			Outputs:  map[string]any{"token": "supersecret123", "status": "ok"},
		},
	}

	rec := New(inner)
	rec.SetSecrets([]string{"TEST_SECRET_KEY"})

	td := &schema.ToolDefinition{
		Meta: schema.ToolMeta{Name: "curl"},
	}

	_, err := rec.Execute(context.Background(), td, "get", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	r := rec.Responses[0]
	if r.Stdout != "auth: <REDACTED>" {
		t.Errorf("stdout=%q, want redacted", r.Stdout)
	}
	if r.Outputs["token"] != "<REDACTED>" {
		t.Errorf("token=%q, want <REDACTED>", r.Outputs["token"])
	}
	if r.Outputs["status"] != "ok" {
		t.Errorf("status=%q, want ok", r.Outputs["status"])
	}
}
