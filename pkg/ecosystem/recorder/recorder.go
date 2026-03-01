package recorder

import (
	"context"
	"os"
	"strings"

	"github.com/ormasoftchile/gert/pkg/kernel/executor"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// ToolExecutor matches the engine.ToolExecutor interface.
type ToolExecutor interface {
	Execute(ctx context.Context, toolDef *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*executor.Result, error)
}

// CapturedResponse records a single tool response.
type CapturedResponse struct {
	Tool     string         `yaml:"tool"`
	Action   string         `yaml:"action"`
	ExitCode int            `yaml:"exit_code"`
	Stdout   string         `yaml:"stdout"`
	Outputs  map[string]any `yaml:"outputs,omitempty"`
}

// Recorder wraps a ToolExecutor and captures all tool responses.
type Recorder struct {
	inner     ToolExecutor
	Responses []CapturedResponse
	secrets   []string // env var names whose values should be redacted
}

// New creates a recording wrapper around an existing tool executor.
func New(inner ToolExecutor) *Recorder {
	return &Recorder{inner: inner}
}

// SetSecrets configures secret env var names whose values are redacted in captured output.
func (r *Recorder) SetSecrets(envVars []string) {
	r.secrets = envVars
}

// Execute delegates to the inner executor and records the response.
func (r *Recorder) Execute(ctx context.Context, toolDef *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*executor.Result, error) {
	result, err := r.inner.Execute(ctx, toolDef, actionName, inputs, vars)
	if err != nil {
		return nil, err
	}

	captured := CapturedResponse{
		Tool:     toolDef.Meta.Name,
		Action:   actionName,
		ExitCode: result.ExitCode,
		Stdout:   r.redact(result.Stdout),
		Outputs:  r.redactMap(result.Outputs),
	}
	r.Responses = append(r.Responses, captured)
	return result, nil
}

// redact replaces secret values with <REDACTED>.
func (r *Recorder) redact(s string) string {
	for _, envVar := range r.secrets {
		val := os.Getenv(envVar)
		if val != "" {
			s = strings.ReplaceAll(s, val, "<REDACTED>")
		}
	}
	return s
}

// redactMap redacts secret values in a map.
func (r *Recorder) redactMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			out[k] = r.redact(s)
		} else {
			out[k] = v
		}
	}
	return out
}
