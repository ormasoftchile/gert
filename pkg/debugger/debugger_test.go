package debugger

import (
	"bytes"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/runtime"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// mockExecutor is a test executor that returns canned responses.
type mockExecutor struct{}

func (m *mockExecutor) Execute(ctx interface{}, command string, args []string, env []string) (*providers.CommandResult, error) {
	return &providers.CommandResult{
		Stdout:   []byte("mock output"),
		ExitCode: 0,
	}, nil
}

// TestDebuggerCommandHelp verifies help output lists all commands.
func TestDebuggerCommandHelp(t *testing.T) {
	var buf bytes.Buffer
	d := &Debugger{
		output: &buf,
	}
	d.handleHelp()
	out := buf.String()
	cmds := []string{"next", "continue", "print", "history", "evidence", "approve", "snapshot", "dump", "help", "quit"}
	for _, cmd := range cmds {
		if !strings.Contains(out, cmd) {
			t.Errorf("help output missing command %q", cmd)
		}
	}
}

// TestDebuggerPrintVars verifies print vars output.
func TestDebuggerPrintVars(t *testing.T) {
	var buf bytes.Buffer
	d := &Debugger{
		output: &buf,
		state: &runtime.RunState{
			Vars: map[string]string{"namespace": "prod", "service": "api"},
		},
	}
	d.handlePrint([]string{"print", "vars"})
	out := buf.String()
	if !strings.Contains(out, "namespace") || !strings.Contains(out, "prod") {
		t.Errorf("print vars missing expected content: %s", out)
	}
}

// TestDebuggerPrintCaptures verifies print captures output.
func TestDebuggerPrintCaptures(t *testing.T) {
	var buf bytes.Buffer
	d := &Debugger{
		output: &buf,
		state: &runtime.RunState{
			Captures: map[string]string{"pods": "pod1 Running"},
		},
	}
	d.handlePrint([]string{"print", "captures"})
	out := buf.String()
	if !strings.Contains(out, "pods") {
		t.Errorf("print captures missing expected content: %s", out)
	}
}

// TestDebuggerHistory verifies history output.
func TestDebuggerHistory(t *testing.T) {
	var buf bytes.Buffer
	d := &Debugger{
		output: &buf,
		state: &runtime.RunState{
			History: []*providers.StepResult{
				{StepID: "check_pods", StepIndex: 0, Status: "passed"},
				{StepID: "get_logs", StepIndex: 1, Status: "failed"},
			},
		},
	}
	d.handleHistory()
	out := buf.String()
	if !strings.Contains(out, "check_pods") || !strings.Contains(out, "passed") {
		t.Errorf("history missing expected content: %s", out)
	}
}

// TestDebuggerPromptFormat verifies prompt shows step info.
func TestDebuggerPromptFormat(t *testing.T) {
	rb := &schema.Runbook{
		Steps: []schema.Step{
			{ID: "step_one"},
			{ID: "step_two"},
		},
	}
	d := &Debugger{
		runbook: rb,
		state: &runtime.RunState{
			CurrentStepIndex: 0,
		},
	}
	prompt := d.buildPrompt()
	if !strings.Contains(prompt, "1/2") || !strings.Contains(prompt, "step_one") {
		t.Errorf("prompt format unexpected: %q", prompt)
	}
}
