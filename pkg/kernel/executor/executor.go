// Package executor dispatches tool steps to their transport implementations.
package executor

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"regexp"
	"strings"

	"github.com/ormasoftchile/gert/pkg/kernel/eval"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// Result is the output of executing a tool action.
type Result struct {
	ExitCode int
	Stdout   string
	Stderr   string
	Outputs  map[string]any // extracted outputs mapped to contract
}

// RunTool executes a tool action via its declared transport.
// Currently supports stdio only. jsonrpc and mcp are Phase 3+ / ecosystem.
func RunTool(td *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*Result, error) {
	action, ok := td.Actions[actionName]
	if !ok {
		return nil, fmt.Errorf("action %q not found in tool %q", actionName, td.Meta.Name)
	}

	transport := td.Meta.Transport
	if transport == "" {
		transport = "stdio"
	}

	switch transport {
	case "stdio":
		return runStdio(td, &action, inputs, vars)
	case "jsonrpc":
		return nil, fmt.Errorf("jsonrpc transport not yet implemented")
	case "mcp":
		return nil, fmt.Errorf("mcp transport not yet implemented")
	default:
		return nil, fmt.Errorf("unknown transport %q", transport)
	}
}

// runStdio executes a tool action by spawning a process.
func runStdio(td *schema.ToolDefinition, action *schema.ToolAction, inputs map[string]any, vars map[string]any) (*Result, error) {
	if len(action.Argv) == 0 {
		return nil, fmt.Errorf("stdio action has no argv")
	}

	// Merge inputs into vars for template resolution
	merged := mergeVars(vars, inputs)

	// Resolve argv templates
	argv := make([]string, len(action.Argv))
	for i, arg := range action.Argv {
		resolved, err := eval.Resolve(arg, merged)
		if err != nil {
			return nil, fmt.Errorf("argv[%d] template: %w", i, err)
		}
		argv[i] = resolved
	}

	// Resolve binary: meta.binary overrides argv[0] for process lookup
	binaryName := argv[0]
	if td.Meta.Binary != "" {
		binaryName = td.Meta.Binary
	}

	// Execute
	cmd := exec.Command(binaryName, argv[1:]...) //#nosec G204 -- argv comes from tool definition authored by runbook owner
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	err := cmd.Run()
	exitCode := 0
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			exitCode = exitErr.ExitCode()
		} else {
			return nil, fmt.Errorf("exec %q: %w", argv[0], err)
		}
	}

	result := &Result{
		ExitCode: exitCode,
		Stdout:   normalizeLineEndings(stdout.String()),
		Stderr:   normalizeLineEndings(stderr.String()),
		Outputs:  make(map[string]any),
	}

	// Apply extract rules to map stdout/stderr to contract outputs
	if err := applyExtract(action.Extract, result); err != nil {
		return result, fmt.Errorf("extract: %w", err)
	}

	return result, nil
}

// applyExtract maps tool output to declared contract outputs using extract rules.
func applyExtract(extracts map[string]schema.Extract, result *Result) error {
	for name, ext := range extracts {
		var source string
		switch ext.From {
		case "stdout":
			source = result.Stdout
		case "stderr":
			source = result.Stderr
		case "json":
			// Parse stdout as JSON and extract via path
			var parsed map[string]any
			if err := json.Unmarshal([]byte(result.Stdout), &parsed); err != nil {
				return fmt.Errorf("extract %q: json parse: %w", name, err)
			}
			val := jsonPath(parsed, ext.Path)
			result.Outputs[name] = val
			continue
		default:
			source = result.Stdout
		}

		if ext.Pattern != "" {
			re, err := regexp.Compile(ext.Pattern)
			if err != nil {
				return fmt.Errorf("extract %q: invalid pattern: %w", name, err)
			}
			match := re.FindStringSubmatch(strings.TrimSpace(source))
			if len(match) > 1 {
				result.Outputs[name] = match[1]
			} else if len(match) == 1 {
				result.Outputs[name] = match[0]
			}
		} else {
			result.Outputs[name] = strings.TrimSpace(source)
		}
	}
	return nil
}

// jsonPath does a simple dot-path traversal of a JSON object.
func jsonPath(obj map[string]any, path string) any {
	if path == "" {
		return obj
	}
	parts := strings.Split(path, ".")
	var current any = obj
	for _, part := range parts {
		m, ok := current.(map[string]any)
		if !ok {
			return nil
		}
		current = m[part]
	}
	return current
}

func mergeVars(base, overlay map[string]any) map[string]any {
	merged := make(map[string]any, len(base)+len(overlay))
	for k, v := range base {
		merged[k] = v
	}
	for k, v := range overlay {
		merged[k] = v
	}
	return merged
}

// normalizeLineEndings replaces \r\n with \n for cross-platform consistency.
func normalizeLineEndings(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}
