package runtime

import (
	"context"
	"regexp"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// TestRunIDFormat validates the run ID format: timestamp+short random suffix.
func TestRunIDFormat(t *testing.T) {
	id := GenerateRunID()
	// Expected format: YYYYMMDDTHHmmss-xxxxxxxx (24 chars: 15 timestamp + 1 dash + 8 hex)
	re := regexp.MustCompile(`^\d{8}T\d{6}-[a-f0-9]{8}$`)
	if !re.MatchString(id) {
		t.Errorf("RunID %q does not match expected format YYYYMMDDTHHmmss-xxxx", id)
	}
}

// TestRunIDUniqueness verifies consecutive IDs differ.
func TestRunIDUniqueness(t *testing.T) {
	ids := make(map[string]bool)
	for i := 0; i < 100; i++ {
		id := GenerateRunID()
		if ids[id] {
			t.Fatalf("duplicate RunID: %q", id)
		}
		ids[id] = true
	}
}

// dryRunExecutor is a test executor that reports commands without executing.
type dryRunExecutor struct {
	commands []string
}

func (d *dryRunExecutor) Execute(ctx context.Context, command string, args []string, env []string) (*providers.CommandResult, error) {
	d.commands = append(d.commands, command+" "+strings.Join(args, " "))
	return &providers.CommandResult{
		Stdout:   []byte("<dry-run>"),
		ExitCode: 0,
	}, nil
}

// TestDryRunZeroSideEffects verifies dry-run mode executes no real commands
// and produces placeholder output.
func TestDryRunZeroSideEffects(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v0",
		Meta: schema.Meta{
			Name: "dry-run-test",
			Vars: map[string]string{"ns": "default"},
		},
		Steps: []schema.Step{
			{
				ID:    "step1",
				Type:  "cli",
				Title: "echo test",
				With:  &schema.CLIStepConfig{Argv: []string{"echo", "hello"}},
				Assertions: []schema.Assertion{
					{Contains: "dry-run"}, // will match "<dry-run>" output
				},
			},
			{
				ID:    "step2",
				Type:  "cli",
				Title: "list files",
				With:  &schema.CLIStepConfig{Argv: []string{"ls", "-la"}},
				Capture: map[string]string{
					"files": "stdout",
				},
			},
		},
	}

	executor := &dryRunExecutor{}
	collector := &providers.DryRunCollector{}

	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer engine.Trace.Close()

	ctx := context.Background()
	err = engine.Run(ctx)
	if err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Verify executor was called (recorded commands)
	if len(executor.commands) != 2 {
		t.Errorf("expected 2 commands recorded, got %d", len(executor.commands))
	}

	// Verify captures contain placeholder
	if v, ok := engine.State.Captures["files"]; !ok || v != "<dry-run>" {
		t.Errorf("expected capture 'files' = '<dry-run>', got %q", v)
	}

	// Verify all steps passed
	if len(engine.State.History) != 2 {
		t.Errorf("expected 2 history entries, got %d", len(engine.State.History))
	}
	for _, h := range engine.State.History {
		if h.Status != "passed" {
			t.Errorf("step %q status = %q, want passed", h.StepID, h.Status)
		}
	}
}

// TestDryRunVariableResolution verifies variables are resolved in dry-run mode.
func TestDryRunVariableResolution(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v0",
		Meta: schema.Meta{
			Name: "var-resolution-test",
			Vars: map[string]string{"env": "staging"},
		},
		Steps: []schema.Step{
			{
				ID:    "check",
				Type:  "cli",
				Title: "check env",
				With:  &schema.CLIStepConfig{Argv: []string{"echo", "{{ .env }}"}},
				Capture: map[string]string{
					"output": "stdout",
				},
			},
		},
	}

	executor := &dryRunExecutor{}
	collector := &providers.DryRunCollector{}

	engine, err := NewEngine(rb, executor, collector, "dry-run", "")
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer engine.Trace.Close()

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	// Verify the executor received the resolved variable
	if len(executor.commands) != 1 {
		t.Fatalf("expected 1 command, got %d", len(executor.commands))
	}
	if !strings.Contains(executor.commands[0], "staging") {
		t.Errorf("command should contain resolved var 'staging': %q", executor.commands[0])
	}
}

// TestDryRunManualSteps verifies manual steps get placeholder evidence.
func TestDryRunManualSteps(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v0",
		Meta: schema.Meta{
			Name: "manual-dryrun-test",
		},
		Steps: []schema.Step{
			{
				ID:           "manual_step",
				Type:         "manual",
				Title:        "verify dashboard",
				Instructions: "Check the dashboard",
				RequiredEvidence: []schema.EvidenceRequirement{
					{Kind: "text", Name: "observation"},
					{Kind: "checklist", Name: "checks", Items: []string{"item1", "item2"}},
				},
			},
		},
	}

	executor := &dryRunExecutor{}
	collector := &providers.DryRunCollector{}

	engine, err := NewEngine(rb, executor, collector, "dry-run", "")
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer engine.Trace.Close()

	ctx := context.Background()
	if err := engine.Run(ctx); err != nil {
		t.Fatalf("Run error: %v", err)
	}

	if len(engine.State.History) != 1 {
		t.Fatalf("expected 1 history entry, got %d", len(engine.State.History))
	}

	result := engine.State.History[0]
	if result.Status != "passed" {
		t.Errorf("manual step status = %q, want passed", result.Status)
	}

	// Check placeholder evidence was collected
	if ev, ok := result.Evidence["observation"]; !ok || ev.Kind != "text" {
		t.Error("expected text evidence 'observation'")
	}
	if ev, ok := result.Evidence["checks"]; !ok || ev.Kind != "checklist" {
		t.Error("expected checklist evidence 'checks'")
	}
}

// TestDryRunGovernanceReported verifies governance violations are still reported
// in dry-run mode (prevents denied commands).
func TestDryRunGovernanceReported(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v0",
		Meta: schema.Meta{
			Name: "gov-dryrun-test",
			Governance: &schema.GovernancePolicy{
				AllowedCommands: []string{"kubectl"},
			},
		},
		Steps: []schema.Step{
			{
				ID:    "blocked",
				Type:  "cli",
				Title: "run rm",
				With:  &schema.CLIStepConfig{Argv: []string{"rm", "-rf", "/"}},
			},
		},
	}

	executor := &dryRunExecutor{}
	collector := &providers.DryRunCollector{}

	engine, err := NewEngine(rb, executor, collector, "dry-run", "")
	if err != nil {
		t.Fatalf("NewEngine error: %v", err)
	}
	defer engine.Trace.Close()

	ctx := context.Background()
	err = engine.Run(ctx)
	// Should fail because governance blocked the command
	if err == nil {
		t.Fatal("expected governance error in dry-run mode")
	}
}
