package providers

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"time"
)

// RealExecutor runs commands via os/exec with timeout support.
type RealExecutor struct{}

// Execute runs a command with the given arguments and environment.
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
