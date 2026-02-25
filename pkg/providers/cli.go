package providers

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"time"
)

// RealExecutor runs commands via os/exec with timeout support.
type RealExecutor struct{}

// Execute runs a command with the given arguments and environment.
// On Windows, if the command is not found directly it is retried through
// cmd.exe /C so that shell builtins (echo, set, …) work transparently.
func (r *RealExecutor) Execute(ctx context.Context, command string, args []string, env []string) (*CommandResult, error) {
	start := time.Now()
	cmd := exec.CommandContext(ctx, command, args...)
	if len(env) > 0 {
		cmd.Env = env
	}

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()

	// On Windows, retry through cmd.exe when the executable is not found.
	// This handles shell builtins like echo, set, type, etc.
	// We pass the entire command line as a single string after /C so that
	// Go's exec doesn't add extra quoting around individual arguments.
	if err != nil && runtime.GOOS == "windows" && isExecNotFound(err) {
		stdout.Reset()
		stderr.Reset()
		cmdLine := command
		for _, a := range args {
			cmdLine += " " + a
		}
		cmd = exec.CommandContext(ctx, "cmd.exe", "/C", cmdLine)
		if len(env) > 0 {
			cmd.Env = env
		}
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		err = cmd.Run()
	}

	duration := time.Since(start)

	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("execute command %q: %w", command, err)
		}
	}

	return &CommandResult{
		Stdout:   stdout.Bytes(),
		Stderr:   stderr.Bytes(),
		ExitCode: exitCode,
		Duration: duration,
	}, nil
}

// isExecNotFound returns true when the error indicates the executable was not found.
func isExecNotFound(err error) bool {
	if err == exec.ErrNotFound {
		return true
	}
	// exec.Error wraps ErrNotFound for the specific binary name
	var execErr *exec.Error
	if ok := errors.As(err, &execErr); ok {
		return true
	}
	return false
}
