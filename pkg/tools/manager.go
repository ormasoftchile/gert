// Package tools manages tool lifecycle: loading definitions, validating steps,
// dispatching action calls, and (in future phases) managing persistent processes.
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"text/template"
	"time"

	"github.com/ormasoftchile/gert/pkg/governance"
	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// ActionResult holds the output of a tool action execution.
type ActionResult struct {
	Stdout           string
	Stderr           string
	ExitCode         int
	Captures         map[string]string
	Duration         time.Duration
	RequiresApproval bool              // true if action governance requires approval before execution
	ApprovalMin      int               // minimum approvals needed
	RedactedArgs     map[string]string // arg values with redact:true masked for evidence
}

// Manager handles tool lifecycle: loading definitions, spawning processes,
// routing action calls, and shutdown.
type Manager struct {
	defs         map[string]*schema.ToolDefinition // loaded tool defs by alias
	paths        map[string]string                 // alias → resolved file path
	processes    map[string]*jsonrpcProcess         // live jsonrpc processes by alias
	mcpProcesses map[string]*mcpProcess             // live MCP processes by alias
	executor     providers.CommandExecutor
	redact       []*governance.CompiledRedaction
	mu           sync.Mutex
}

// NewManager creates a tool manager that shares the given command executor
// (real, replay, or dry-run) and redaction rules.
func NewManager(executor providers.CommandExecutor, redact []*governance.CompiledRedaction) *Manager {
	return &Manager{
		defs:         make(map[string]*schema.ToolDefinition),
		paths:        make(map[string]string),
		processes:    make(map[string]*jsonrpcProcess),
		mcpProcesses: make(map[string]*mcpProcess),
		executor:     executor,
		redact:       redact,
	}
}

// Load parses and validates a .tool.yaml file, registering it by alias.
// The baseDir is used to resolve relative tool file paths.
func (m *Manager) Load(alias, path, baseDir string) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Resolve relative path
	resolved := path
	if !filepath.IsAbs(path) && baseDir != "" {
		resolved = filepath.Join(baseDir, path)
	}

	td, errs := schema.ValidateToolFile(resolved)
	if len(errs) > 0 {
		var msgs []string
		for _, e := range errs {
			msgs = append(msgs, e.Error())
		}
		return fmt.Errorf("tool %q validation failed: %s", alias, strings.Join(msgs, "; "))
	}

	m.defs[alias] = td
	m.paths[alias] = resolved
	return nil
}

// RegisterBuiltin registers a tool definition that's embedded in code (not loaded from a file).
// Used for built-in tools like XTS that ship with gert.
func (m *Manager) RegisterBuiltin(alias string, td *schema.ToolDefinition) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.defs[alias] = td
	m.paths[alias] = "<builtin>"
}

// GetDef returns the loaded tool definition for an alias, or nil if not loaded.
func (m *Manager) GetDef(alias string) *schema.ToolDefinition {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.defs[alias]
}

// Execute runs a tool action and returns the result.
// Variables in vars are used to resolve template expressions in args and argv.
func (m *Manager) Execute(ctx context.Context, alias, action string, args map[string]string, vars map[string]string) (*ActionResult, error) {
	m.mu.Lock()
	td, ok := m.defs[alias]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not loaded", alias)
	}

	act, ok := td.Actions[action]
	if !ok {
		return nil, fmt.Errorf("tool %q has no action %q", alias, action)
	}

	// Validate required args and enum values
	if err := validateArgs(act, args); err != nil {
		return nil, fmt.Errorf("tool %q action %q: %w", alias, action, err)
	}

	// Apply defaults for missing optional args
	mergedArgs := applyDefaults(act, args)

	// Check governance: requires_approval blocks execution
	if act.Governance != nil && act.Governance.RequiresApproval {
		return &ActionResult{
			RequiresApproval: true,
			ApprovalMin:      act.Governance.ApprovalMin,
			Captures:         make(map[string]string),
		}, nil
	}

	// Determine transport mode
	mode := td.Transport.Mode
	if mode == "" {
		mode = "stdio"
	}

	switch mode {
	case "stdio":
		return m.executeStdio(ctx, td, action, act, mergedArgs, vars)
	case "jsonrpc":
		return m.executeJSONRPC(ctx, alias, td, act, mergedArgs, vars)
	case "mcp":
		return m.executeMCP(ctx, alias, td, act, mergedArgs, vars)
	default:
		return nil, fmt.Errorf("unknown transport mode %q", mode)
	}
}

// ExecuteApproved runs a tool action that was previously blocked by requires_approval
// governance. Called after the engine obtains approval from the user.
func (m *Manager) ExecuteApproved(ctx context.Context, alias, action string, args map[string]string, vars map[string]string) (*ActionResult, error) {
	m.mu.Lock()
	td, ok := m.defs[alias]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("tool %q not loaded", alias)
	}

	act, ok := td.Actions[action]
	if !ok {
		return nil, fmt.Errorf("tool %q has no action %q", alias, action)
	}

	if err := validateArgs(act, args); err != nil {
		return nil, fmt.Errorf("tool %q action %q: %w", alias, action, err)
	}

	mergedArgs := applyDefaults(act, args)

	mode := td.Transport.Mode
	if mode == "" {
		mode = "stdio"
	}

	switch mode {
	case "stdio":
		return m.executeStdio(ctx, td, action, act, mergedArgs, vars)
	case "jsonrpc":
		return m.executeJSONRPC(ctx, alias, td, act, mergedArgs, vars)
	case "mcp":
		return m.executeMCP(ctx, alias, td, act, mergedArgs, vars)
	default:
		return nil, fmt.Errorf("unknown transport mode %q", mode)
	}
}

// ValidateStep checks a tool step config against loaded tool definitions.
// Returns a list of human-readable error strings (empty = valid).
func (m *Manager) ValidateStep(cfg *schema.ToolStepConfig) []string {
	m.mu.Lock()
	defer m.mu.Unlock()

	var errs []string

	td, ok := m.defs[cfg.Name]
	if !ok {
		errs = append(errs, fmt.Sprintf("tool %q not loaded", cfg.Name))
		return errs
	}

	act, ok := td.Actions[cfg.Action]
	if !ok {
		errs = append(errs, fmt.Sprintf("tool %q has no action %q", cfg.Name, cfg.Action))
		return errs
	}

	// Check required args
	for argName, argDef := range act.Args {
		if argDef.Required {
			if _, ok := cfg.Args[argName]; !ok {
				errs = append(errs, fmt.Sprintf("required arg %q missing for %s.%s", argName, cfg.Name, cfg.Action))
			}
		}
	}

	// Check unknown args
	for argName := range cfg.Args {
		if _, ok := act.Args[argName]; !ok {
			errs = append(errs, fmt.Sprintf("unknown arg %q for %s.%s", argName, cfg.Name, cfg.Action))
		}
	}

	return errs
}

// Shutdown gracefully stops all persistent tool processes.
func (m *Manager) Shutdown(ctx context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	var lastErr error
	for alias, proc := range m.processes {
		shutdownMethod := ""
		if td, ok := m.defs[alias]; ok && td.Transport.Startup != nil {
			shutdownMethod = td.Transport.Startup.ShutdownMethod
		}
		fmt.Fprintf(os.Stderr, "tools: shutting down jsonrpc %q (method=%q)\n", alias, shutdownMethod)
		if err := proc.Shutdown(shutdownMethod, 3*time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "tools: shutdown %q error: %v\n", alias, err)
			lastErr = err
		}
		delete(m.processes, alias)
	}
	for alias, proc := range m.mcpProcesses {
		fmt.Fprintf(os.Stderr, "tools: shutting down mcp %q\n", alias)
		if err := proc.Shutdown(3 * time.Second); err != nil {
			fmt.Fprintf(os.Stderr, "tools: shutdown mcp %q error: %v\n", alias, err)
			lastErr = err
		}
		delete(m.mcpProcesses, alias)
	}
	return lastErr
}

// getOrSpawnProcess returns an existing live process or spawns a new one.
func (m *Manager) getOrSpawnProcess(ctx context.Context, alias string, td *schema.ToolDefinition) (*jsonrpcProcess, error) {
	// Check for existing live process (caller holds m.mu)
	if proc, ok := m.processes[alias]; ok && proc.alive() {
		return proc, nil
	}

	// Spawn new process
	binary := td.Meta.Binary
	if td.Transport.Binary != "" {
		binary = td.Transport.Binary
	}

	resolvedBinary, err := resolveToolBinary(binary)
	if err != nil {
		return nil, err
	}

	var argv []string
	if td.Transport.Startup != nil {
		argv = td.Transport.Startup.Argv
	}

	fmt.Fprintf(os.Stderr, "tools: spawning jsonrpc process %q %v\n", resolvedBinary, argv)

	proc, err := spawnJSONRPC(ctx, resolvedBinary, argv, td.Transport.Startup)
	if err != nil {
		return nil, err
	}

	m.processes[alias] = proc
	return proc, nil
}

// executeJSONRPC runs a tool action via a persistent JSON-RPC process.
func (m *Manager) executeJSONRPC(ctx context.Context, alias string, td *schema.ToolDefinition, act schema.ToolAction, args map[string]string, vars map[string]string) (*ActionResult, error) {
	if act.Method == "" {
		return nil, fmt.Errorf("action has no method for jsonrpc transport")
	}

	// Get or spawn the persistent process
	m.mu.Lock()
	proc, err := m.getOrSpawnProcess(ctx, alias, td)
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("spawn jsonrpc process: %w", err)
	}

	// Build params from resolved args
	params := make(map[string]interface{})
	for k, v := range args {
		params[k] = v
	}

	start := time.Now()

	// Send JSON-RPC call
	resultRaw, err := proc.Call(act.Method, params)
	if err != nil {
		if (strings.Contains(err.Error(), "UNKNOWN_METHOD") || strings.Contains(strings.ToLower(err.Error()), "unknown method")) && strings.Contains(act.Method, "/") {
			if idx := strings.LastIndex(act.Method, "/"); idx >= 0 && idx < len(act.Method)-1 {
				fallbackMethod := act.Method[idx+1:]
				resultRaw, err = proc.Call(fallbackMethod, params)
				if err == nil {
					fmt.Fprintf(os.Stderr, "tools: jsonrpc fallback method %q -> %q\n", act.Method, fallbackMethod)
				} else {
					err = fmt.Errorf("jsonrpc call %q (fallback %q): %w", act.Method, fallbackMethod, err)
				}
			}
		}

		if err == nil {
			goto callSucceeded
		}

		// If process died, remove it so next call respawns
		if !proc.alive() {
			m.mu.Lock()
			delete(m.processes, alias)
			m.mu.Unlock()
		}
		return nil, fmt.Errorf("jsonrpc call %q: %w", act.Method, err)
	}

callSucceeded:

	duration := time.Since(start)

	// Convert raw result to string for capture
	resultStr := string(resultRaw)

	// Apply redaction
	if len(m.redact) > 0 {
		resultStr = governance.RedactOutput(resultStr, m.redact)
	}
	if td.Governance != nil && len(td.Governance.Redact) > 0 {
		toolRedact, err := governance.CompileRedactionRules(td.Governance.Redact)
		if err == nil && len(toolRedact) > 0 {
			resultStr = governance.RedactOutput(resultStr, toolRedact)
		}
	}

	// Extract captures
	captures := make(map[string]string)
	for name, capDef := range act.Capture {
		from := capDef.From
		if from == "" || from == "stdout" {
			captures[name] = strings.TrimSpace(resultStr)
		} else {
			// Extract via dot-path into the JSON result
			extracted, err := extractJSONPath(resultRaw, from)
			if err != nil {
				fmt.Fprintf(os.Stderr, "tools: capture %q extract %q failed: %v\n", name, from, err)
				captures[name] = strings.TrimSpace(resultStr) // fallback to full result
			} else {
				captures[name] = strings.TrimSpace(extracted)
			}
		}
	}

	return &ActionResult{
		Stdout:   resultStr,
		Stderr:   "",
		ExitCode: 0,
		Captures: captures,
		Duration: duration,
	}, nil
}

// validateArgs checks required args are present and enum values are valid.
func validateArgs(act schema.ToolAction, args map[string]string) error {
	for name, def := range act.Args {
		val, provided := args[name]
		if def.Required && !provided {
			return fmt.Errorf("required arg %q not provided", name)
		}
		if provided && len(def.Enum) > 0 {
			// Skip enum check for template expressions
			if !strings.Contains(val, "{{") {
				valid := false
				for _, e := range def.Enum {
					if val == e {
						valid = true
						break
					}
				}
				if !valid {
					return fmt.Errorf("arg %q value %q not in enum %v", name, val, def.Enum)
				}
			}
		}
	}
	return nil
}

// applyDefaults merges default values for unset optional args.
func applyDefaults(act schema.ToolAction, args map[string]string) map[string]string {
	merged := make(map[string]string)
	for k, v := range args {
		merged[k] = v
	}
	for name, def := range act.Args {
		if _, ok := merged[name]; !ok && def.Default != "" {
			merged[name] = def.Default
		}
	}
	return merged
}

// resolveArgvTemplates resolves Go template expressions in the action's argv.
// The data map contains merged args + runbook vars.
func resolveArgvTemplates(argv []string, data map[string]string) ([]string, error) {
	resolved := make([]string, len(argv))
	for i, arg := range argv {
		if !strings.Contains(arg, "{{") {
			resolved[i] = arg
			continue
		}
		tmpl, err := template.New("arg").Option("missingkey=zero").Parse(arg)
		if err != nil {
			return nil, fmt.Errorf("parse argv[%d] template: %w", i, err)
		}
		var buf strings.Builder
		if err := tmpl.Execute(&buf, data); err != nil {
			return nil, fmt.Errorf("resolve argv[%d] template: %w", i, err)
		}
		resolved[i] = buf.String()
	}
	return resolved, nil
}

func resolveToolBinary(binary string) (string, error) {
	binary = strings.TrimSpace(binary)
	if binary == "" {
		return "", fmt.Errorf("tool binary is empty")
	}

	if p, err := exec.LookPath(binary); err == nil {
		return p, nil
	}

	// XTS compatibility aliases across environments/installations.
	lower := strings.ToLower(binary)
	if lower == "xts" || lower == "xts-cli" || lower == "xts-server" || lower == "cli" {
		candidates := []string{"xts-cli", "xts", "cli", "xts-cli.cmd", "xts.cmd", "cli.cmd"}
		for _, candidate := range candidates {
			if p, err := exec.LookPath(candidate); err == nil {
				return p, nil
			}
		}
	}

	return "", fmt.Errorf("tool binary %q not found in PATH", binary)
}

// getOrSpawnMCPProcess returns an existing live MCP process or spawns a new one.
func (m *Manager) getOrSpawnMCPProcess(ctx context.Context, alias string, td *schema.ToolDefinition) (*mcpProcess, error) {
	// Check for existing live process (caller holds m.mu)
	if proc, ok := m.mcpProcesses[alias]; ok && proc.alive() {
		return proc, nil
	}

	// Connect mode: HTTP/SSE transport to an existing MCP server
	if td.Transport.Connect != "" {
		return nil, fmt.Errorf("MCP connect mode (HTTP/SSE to %q) is not yet implemented — use spawn mode instead", td.Transport.Connect)
	}

	binary := td.Meta.Binary
	if td.Transport.Binary != "" {
		binary = td.Transport.Binary
	}

	var argv []string
	if td.Transport.Startup != nil {
		argv = td.Transport.Startup.Argv
	}

	fmt.Fprintf(os.Stderr, "tools: spawning MCP process %q %v\n", binary, argv)

	proc, err := spawnMCP(ctx, binary, argv, td.Transport.Startup)
	if err != nil {
		return nil, err
	}

	m.mcpProcesses[alias] = proc
	return proc, nil
}

// executeMCP runs a tool action via a persistent MCP server process.
func (m *Manager) executeMCP(ctx context.Context, alias string, td *schema.ToolDefinition, act schema.ToolAction, args map[string]string, vars map[string]string) (*ActionResult, error) {
	toolName := act.MCPTool
	if toolName == "" {
		return nil, fmt.Errorf("action has no mcp_tool for MCP transport")
	}

	// Get or spawn the MCP process
	m.mu.Lock()
	proc, err := m.getOrSpawnMCPProcess(ctx, alias, td)
	m.mu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("spawn MCP process: %w", err)
	}

	// Build arguments
	mcpArgs := make(map[string]interface{})
	for k, v := range args {
		mcpArgs[k] = v
	}

	start := time.Now()

	// Call the MCP tool
	resultText, err := proc.CallTool(ctx, toolName, mcpArgs)
	if err != nil {
		// If process died, remove it so next call respawns
		if !proc.alive() {
			m.mu.Lock()
			delete(m.mcpProcesses, alias)
			m.mu.Unlock()
		}
		return nil, fmt.Errorf("MCP call %q: %w", toolName, err)
	}

	duration := time.Since(start)

	// Apply redaction
	if len(m.redact) > 0 {
		resultText = governance.RedactOutput(resultText, m.redact)
	}
	if td.Governance != nil && len(td.Governance.Redact) > 0 {
		toolRedact, err := governance.CompileRedactionRules(td.Governance.Redact)
		if err == nil && len(toolRedact) > 0 {
			resultText = governance.RedactOutput(resultText, toolRedact)
		}
	}

	// Extract captures
	captures := make(map[string]string)
	for name, capDef := range act.Capture {
		from := capDef.From
		if from == "" || from == "stdout" || from == "result" {
			captures[name] = strings.TrimSpace(resultText)
		} else {
			// Try to parse as JSON and extract via dot-path
			extracted, err := extractJSONPath(json.RawMessage(resultText), from)
			if err != nil {
				captures[name] = strings.TrimSpace(resultText)
			} else {
				captures[name] = strings.TrimSpace(extracted)
			}
		}
	}

	return &ActionResult{
		Stdout:   resultText,
		Stderr:   "",
		ExitCode: 0,
		Captures: captures,
		Duration: duration,
	}, nil
}
