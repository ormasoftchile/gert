package replay

import (
	"context"
	"testing"
)

// TestScenarioParsing verifies valid scenario files load correctly.
func TestScenarioParsing(t *testing.T) {
	data := []byte(`
commands:
  - argv: ["echo", "hello"]
    stdout: "hello\n"
    stderr: ""
    exit_code: 0
  - argv: ["kubectl", "get", "pods"]
    stdout: "NAME  READY\npod1  1/1\n"
    stderr: ""
    exit_code: 0
evidence:
  manual_step:
    observation:
      kind: text
      value: "looks good"
`)
	s, err := ParseScenario(data)
	if err != nil {
		t.Fatalf("ParseScenario() error: %v", err)
	}
	if len(s.Commands) != 2 {
		t.Errorf("expected 2 commands, got %d", len(s.Commands))
	}
	if len(s.Evidence) != 1 {
		t.Errorf("expected 1 evidence entry, got %d", len(s.Evidence))
	}
	if s.Evidence["manual_step"]["observation"].Value != "looks good" {
		t.Errorf("unexpected evidence value: %q", s.Evidence["manual_step"]["observation"].Value)
	}
}

// TestScenarioParsingEmpty verifies empty scenario is rejected.
func TestScenarioParsingEmpty(t *testing.T) {
	data := []byte(`{}`)
	_, err := ParseScenario(data)
	if err == nil {
		t.Fatal("expected error for empty scenario")
	}
}

// TestScenarioParsingInvalidYAML verifies invalid YAML is rejected.
func TestScenarioParsingInvalidYAML(t *testing.T) {
	data := []byte(`{{{invalid`)
	_, err := ParseScenario(data)
	if err == nil {
		t.Fatal("expected error for invalid YAML")
	}
}

// TestReplayExecutorCommandMatching verifies correct command matching.
func TestReplayExecutorCommandMatching(t *testing.T) {
	s := &Scenario{
		Commands: []ScenarioCommand{
			{Argv: []string{"echo", "hello"}, Stdout: "hello\n", ExitCode: 0},
			{Argv: []string{"kubectl", "get", "pods"}, Stdout: "pod1\n", ExitCode: 0},
		},
	}
	exec := NewReplayExecutor(s)
	ctx := context.Background()

	// First command
	result, err := exec.Execute(ctx, "echo", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result.Stdout) != "hello\n" {
		t.Errorf("stdout = %q, want %q", string(result.Stdout), "hello\n")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}

	// Second command
	result, err = exec.Execute(ctx, "kubectl", []string{"get", "pods"}, nil)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if string(result.Stdout) != "pod1\n" {
		t.Errorf("stdout = %q, want %q", string(result.Stdout), "pod1\n")
	}
}

// TestReplayExecutorFailClosed verifies unknown commands are rejected.
func TestReplayExecutorFailClosed(t *testing.T) {
	s := &Scenario{
		Commands: []ScenarioCommand{
			{Argv: []string{"echo", "hello"}, Stdout: "hello\n", ExitCode: 0},
		},
	}
	exec := NewReplayExecutor(s)
	ctx := context.Background()

	_, err := exec.Execute(ctx, "rm", []string{"-rf", "/"}, nil)
	if err == nil {
		t.Fatal("expected error for unmatched command")
	}
}

// TestReplayExecutorDeterministic verifies same inputs produce same outputs.
func TestReplayExecutorDeterministic(t *testing.T) {
	s := &Scenario{
		Commands: []ScenarioCommand{
			{Argv: []string{"echo", "hello"}, Stdout: "hello\n", ExitCode: 0},
		},
	}
	ctx := context.Background()

	// Run twice with fresh executors
	exec1 := NewReplayExecutor(s)
	r1, err := exec1.Execute(ctx, "echo", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("run 1 error: %v", err)
	}

	exec2 := NewReplayExecutor(s)
	r2, err := exec2.Execute(ctx, "echo", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("run 2 error: %v", err)
	}

	if string(r1.Stdout) != string(r2.Stdout) {
		t.Errorf("stdout mismatch: %q vs %q", string(r1.Stdout), string(r2.Stdout))
	}
	if r1.ExitCode != r2.ExitCode {
		t.Errorf("exit code mismatch: %d vs %d", r1.ExitCode, r2.ExitCode)
	}
}

// TestReplayExecutorNonZeroExit verifies non-zero exit codes are returned.
func TestReplayExecutorNonZeroExit(t *testing.T) {
	s := &Scenario{
		Commands: []ScenarioCommand{
			{Argv: []string{"false"}, Stdout: "", Stderr: "error\n", ExitCode: 1},
		},
	}
	exec := NewReplayExecutor(s)
	ctx := context.Background()

	result, err := exec.Execute(ctx, "false", nil, nil)
	if err != nil {
		t.Fatalf("Execute() error: %v", err)
	}
	if result.ExitCode != 1 {
		t.Errorf("exit code = %d, want 1", result.ExitCode)
	}
	if string(result.Stderr) != "error\n" {
		t.Errorf("stderr = %q, want %q", string(result.Stderr), "error\n")
	}
}

// TestReplayExecutorUsedOnce verifies commands are consumed once (ordered matching).
func TestReplayExecutorUsedOnce(t *testing.T) {
	s := &Scenario{
		Commands: []ScenarioCommand{
			{Argv: []string{"echo", "first"}, Stdout: "first\n", ExitCode: 0},
			{Argv: []string{"echo", "first"}, Stdout: "second\n", ExitCode: 0},
		},
	}
	exec := NewReplayExecutor(s)
	ctx := context.Background()

	r1, err := exec.Execute(ctx, "echo", []string{"first"}, nil)
	if err != nil {
		t.Fatalf("first call error: %v", err)
	}
	if string(r1.Stdout) != "first\n" {
		t.Errorf("first call stdout = %q, want %q", string(r1.Stdout), "first\n")
	}

	r2, err := exec.Execute(ctx, "echo", []string{"first"}, nil)
	if err != nil {
		t.Fatalf("second call error: %v", err)
	}
	if string(r2.Stdout) != "second\n" {
		t.Errorf("second call stdout = %q, want %q", string(r2.Stdout), "second\n")
	}
}

// TestLoadScenarioFile verifies loading from a test fixture file.
func TestLoadScenarioFile(t *testing.T) {
	s, err := LoadScenario("../../testdata/scenarios/minimal-scenario.yaml")
	if err != nil {
		t.Fatalf("LoadScenario() error: %v", err)
	}
	if len(s.Commands) != 1 {
		t.Errorf("expected 1 command, got %d", len(s.Commands))
	}
	if s.Commands[0].Argv[0] != "echo" {
		t.Errorf("expected command 'echo', got %q", s.Commands[0].Argv[0])
	}
}
