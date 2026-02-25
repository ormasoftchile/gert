package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/governance"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// executeStdio runs a tool action by spawning the tool binary with resolved argv.
// This is the default transport: one process per action call.
func (m *Manager) executeStdio(ctx context.Context, td *schema.ToolDefinition, actionName string, act schema.ToolAction, args map[string]string, vars map[string]string) (*ActionResult, error) {
	// Build template data: vars + args (args take precedence)
	data := make(map[string]string)
	for k, v := range vars {
		data[k] = v
	}
	for k, v := range args {
		data[k] = v
	}

	var resolvedArgv []string

	if len(act.Argv) == 0 {
		return nil, fmt.Errorf("action has no argv for stdio transport")
	}
	resolvedArgv, err := resolveArgvTemplates(act.Argv, data)
	if err != nil {
		return nil, fmt.Errorf("resolve argv: %w", err)
	}

	// Build full command: binary + resolved argv
	binary := td.Meta.Binary
	if td.Transport.Binary != "" {
		binary = td.Transport.Binary
	}

	// Apply per-arg redaction: mask arg values where redact: true
	redactedArgs := make(map[string]string)
	for k, v := range args {
		redactedArgs[k] = v
	}
	for argName, argDef := range act.Args {
		if argDef.Redact {
			if val, ok := redactedArgs[argName]; ok && val != "" {
				redactedArgs[argName] = "[REDACTED]"
			}
		}
	}

	start := time.Now()

	// Execute via shared executor (works with real, replay, and dry-run)
	cmdResult, usedBinary, err := m.executeWithBinaryFallback(ctx, binary, resolvedArgv)
	if err != nil {
		return nil, fmt.Errorf("execute %s: %w", usedBinary, err)
	}

	duration := time.Since(start)

	// Apply redaction
	stdout := string(cmdResult.Stdout)
	stderr := string(cmdResult.Stderr)
	if len(m.redact) > 0 {
		stdout = governance.RedactOutput(stdout, m.redact)
		stderr = governance.RedactOutput(stderr, m.redact)
	}

	// Apply tool-level redaction rules
	if td.Governance != nil && len(td.Governance.Redact) > 0 {
		toolRedact, err := governance.CompileRedactionRules(td.Governance.Redact)
		if err == nil && len(toolRedact) > 0 {
			stdout = governance.RedactOutput(stdout, toolRedact)
			stderr = governance.RedactOutput(stderr, toolRedact)
		}
	}

	// Extract captures
	captures := make(map[string]string)
	for name, capDef := range act.Capture {
		source := capDef.From
		if source == "" {
			// Default: capture name is the source (e.g. "stdout" → stdout)
			source = name
		}
		switch source {
		case "stdout":
			captures[name] = strings.TrimSpace(stdout)
		case "stderr":
			captures[name] = strings.TrimSpace(stderr)
		default:
			// JSON path extraction from stdout for named fields
			extracted, err := ExtractJSONPath(json.RawMessage(stdout), source)
			if err == nil {
				captures[name] = extracted
			}
			// If extraction fails, leave capture unset — the engine will
			// treat it as nil (via AllowUndefinedVariables) rather than
			// polluting it with the full stdout (which is often an error
			// message when the tool exits non-zero).
		}
	}

	return &ActionResult{
		Stdout:       stdout,
		Stderr:       stderr,
		ExitCode:     cmdResult.ExitCode,
		Captures:     captures,
		Duration:     duration,
		RedactedArgs: redactedArgs,
		Usage:        parseUsageFromCaptures(captures),
	}, nil
}

func (m *Manager) executeWithBinaryFallback(ctx context.Context, binary string, argv []string) (*providers.CommandResult, string, error) {
	candidates := []string{binary}

	seen := make(map[string]bool)
	var lastErr error
	lastBin := binary
	for _, candidate := range candidates {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		lastBin = candidate
		result, err := m.executor.Execute(ctx, candidate, argv, nil)
		if err == nil {
			return result, candidate, nil
		}
		lastErr = err
		errText := strings.ToLower(err.Error())
		if strings.Contains(errText, "executable file not found") || strings.Contains(errText, "not found in %path%") || strings.Contains(errText, "file not found") {
			continue
		}
		return nil, candidate, err
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("tool binary %q not found in PATH", binary)
	}
	return nil, lastBin, lastErr
}
