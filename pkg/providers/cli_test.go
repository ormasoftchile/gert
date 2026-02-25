package providers

import (
	"context"
	"os/exec"
	"runtime"
	"strings"
	"testing"
)

func TestRealExecutorEcho(t *testing.T) {
	r := &RealExecutor{}
	result, err := r.Execute(context.Background(), "echo", []string{"hello"}, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := strings.TrimSpace(string(result.Stdout))
	if out != "hello" {
		t.Errorf("stdout = %q, want %q", out, "hello")
	}
	if result.ExitCode != 0 {
		t.Errorf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestIsExecNotFound(t *testing.T) {
	if !isExecNotFound(exec.ErrNotFound) {
		t.Error("expected ErrNotFound to be detected")
	}
	err := &exec.Error{Name: "bogus", Err: exec.ErrNotFound}
	if !isExecNotFound(err) {
		t.Error("expected exec.Error wrapping ErrNotFound to be detected")
	}
}

func TestRealExecutorShellBuiltin(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("shell builtin fallback is Windows-only")
	}
	r := &RealExecutor{}
	// "ver" is a cmd.exe builtin that prints the Windows version — no ver.exe exists.
	result, err := r.Execute(context.Background(), "ver", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error running shell builtin 'ver': %v", err)
	}
	out := strings.TrimSpace(string(result.Stdout))
	if out == "" {
		t.Error("expected non-empty output from 'ver'")
	}
}
