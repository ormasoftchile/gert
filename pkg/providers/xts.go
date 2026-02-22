// Package providers — XTS provider implementation.
// Wraps xts-cli.exe to execute views, activities, and queries against
// SQL/Kusto/CMS/MDS data sources via the XTS tooling.
package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// XTSOutput is the structured JSON response from xts-cli --format json.
type XTSOutput struct {
	Success  bool                     `json:"success"`
	RowCount int                      `json:"rowCount"`
	Columns  []string                 `json:"columns"`
	Data     []map[string]interface{} `json:"data"`
	Metadata map[string]interface{}   `json:"metadata"`
}

// XTSProvider handles type=xts steps.
type XTSProvider struct {
	CLIPath   string // resolved path to xts-cli.exe
	ViewsRoot string // base directory for .xts view files
}

// NewXTSProvider creates a provider from the global XTS meta config.
// Resolution order for each field: runbook meta.xts → environment variable → default.
//
// Environment variables:
//   - XTS_CLI_PATH   → path to xts-cli.exe
//   - XTS_VIEWS_ROOT → base directory for .xts view files
//   - XTS_ENVIRONMENT → default XTS environment
func NewXTSProvider(meta *schema.XTSMeta) (*XTSProvider, error) {
	cliPath := firstOf(meta.CLIPath, os.Getenv("XTS_CLI_PATH"), "xts-cli")
	viewsRoot := firstOf(meta.ViewsRoot, os.Getenv("XTS_VIEWS_ROOT"))
	defaultEnv := firstOf(meta.Environment, os.Getenv("XTS_ENVIRONMENT"))

	// Verify xts-cli is reachable
	resolved, err := exec.LookPath(cliPath)
	if err != nil {
		return nil, fmt.Errorf("xts-cli not found at %q: %w (set XTS_CLI_PATH or meta.xts.cli_path)", cliPath, err)
	}

	// Write resolved defaults back to meta so the engine can use them
	meta.CLIPath = resolved
	if viewsRoot != "" {
		meta.ViewsRoot = viewsRoot
	}
	if defaultEnv != "" && meta.Environment == "" {
		meta.Environment = defaultEnv
	}

	return &XTSProvider{
		CLIPath:   resolved,
		ViewsRoot: viewsRoot,
	}, nil
}

// firstOf returns the first non-empty string from the arguments.
func firstOf(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// EnvResolveResult is the JSON output from xts-cli env resolve.
type EnvResolveResult struct {
	Environments []EnvInfo `json:"environments"`
	Environment  *EnvInfo  `json:"environment,omitempty"`
}

// EnvInfo describes a single XTS environment.
type EnvInfo struct {
	Name      string `json:"name"`
	Region    string `json:"region"`
	ArmRegion string `json:"armRegion"`
	Cloud     string `json:"cloud"`
}

// ResolveEnvironment resolves a region name, ARM region, or tenant ring FQDN
// to an XTS environment name using xts-cli env resolve.
// Tries in order: direct name match, --region, --tenant-ring.
func (p *XTSProvider) ResolveEnvironment(value string) (string, error) {
	if value == "" {
		return "", fmt.Errorf("empty environment value")
	}

	// If it looks like an environment name already (e.g. ProdEus1a), try --name first
	if !strings.Contains(value, ".") && !strings.Contains(value, " ") {
		result, err := p.runEnvResolve("--name", value)
		if err == nil && result != "" {
			return result, nil
		}
	}

	// If it contains dots, it's likely a tenant ring FQDN
	if strings.Contains(value, ".worker.database.windows.net") || strings.Contains(value, ".database.") {
		result, err := p.runEnvResolve("--tenant-ring", value)
		if err == nil && result != "" {
			return result, nil
		}
	}

	// Try as region (display name like "East US" or ARM like "eastus")
	result, err := p.runEnvResolve("--region", value)
	if err == nil && result != "" {
		return result, nil
	}

	return "", fmt.Errorf("could not resolve environment from %q", value)
}

// runEnvResolve calls xts-cli env resolve with the given flag and returns the first environment name.
func (p *XTSProvider) runEnvResolve(flag, value string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, p.CLIPath, "env", "resolve", flag, value, "--format", "json")
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("xts-cli env resolve %s %q: %w", flag, value, err)
	}

	var result EnvResolveResult
	if err := json.Unmarshal(stdout.Bytes(), &result); err != nil {
		return "", fmt.Errorf("parse env resolve output: %w", err)
	}

	// Single environment (--name)
	if result.Environment != nil {
		return result.Environment.Name, nil
	}

	// Multiple environments (--region, --tenant-ring) — pick first
	if len(result.Environments) > 0 {
		return result.Environments[0].Name, nil
	}

	return "", fmt.Errorf("no environment found for %s %q", flag, value)
}

// Validate checks xts step fields at schema-validation time.
// Domain rules are already enforced in schema.ValidateDomain; this
// performs provider-level checks (e.g. file existence).
func (p *XTSProvider) Validate(step schema.Step) ValidationResult {
	if step.XTS == nil {
		return ValidationResult{Valid: false, Errors: []string{"xts configuration missing"}}
	}

	var warnings []string

	// For view/activity modes, check that the file can be resolved
	if step.XTS.Mode == "view" || step.XTS.Mode == "activity" {
		filePath := p.resolveViewPath(step.XTS.File)
		if filePath == "" {
			warnings = append(warnings, fmt.Sprintf("view file %q: cannot resolve path (views_root=%q)", step.XTS.File, p.ViewsRoot))
		}
	}

	return ValidationResult{Valid: true, Warnings: warnings}
}

// Execute runs an xts-cli command and returns structured results.
func (p *XTSProvider) Execute(ctx context.Context, execCtx *ExecutionContext, step schema.Step) (*StepResult, error) {
	start := time.Now()
	result := &StepResult{
		RunID:     execCtx.RunID,
		StepID:    step.ID,
		StartedAt: start,
		Actor:     "engine",
		Captures:  make(map[string]string),
	}

	xts := step.XTS

	// Resolve environment: step-level overrides meta-level
	env := xts.Environment
	if env == "" {
		// Caller (engine) should have injected the default from meta.xts.environment
		// via execCtx, but we also accept it from the runbook vars as a fallback.
		if v, ok := execCtx.Vars["xts_environment"]; ok {
			env = v
		}
	}

	// Build xts-cli argv
	argv, err := p.buildArgv(xts, env, execCtx)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("build xts-cli command: %v", err)
		result.EndedAt = time.Now()
		return result, nil
	}

	// Debug: log the full xts-cli command
	fmt.Fprintf(os.Stderr, "  [xts-debug] %s %s\n", p.CLIPath, strings.Join(argv, " "))

	// Execute via the injected CommandExecutor (supports real/replay/dry-run)
	cmdResult, err := execCtx.CommandExecutor.Execute(ctx, p.CLIPath, argv, nil)
	if err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("xts-cli execute: %v", err)
		result.EndedAt = time.Now()
		return result, nil
	}

	stdout := string(cmdResult.Stdout)
	stderr := string(cmdResult.Stderr)

	// Check exit code
	if cmdResult.ExitCode != 0 {
		result.Status = "failed"
		// Include both stderr and stdout for diagnostics, plus the command that was run
		errMsg := strings.TrimSpace(stderr)
		if outSnip := strings.TrimSpace(stdout); outSnip != "" {
			errMsg += "\nstdout: " + truncate(outSnip, 500)
		}
		errMsg += fmt.Sprintf("\ncmd: %s %s", p.CLIPath, strings.Join(argv, " "))
		result.Error = fmt.Sprintf("xts-cli exited %d:\n%s", cmdResult.ExitCode, errMsg)
		result.EndedAt = time.Now()
		return result, nil
	}

	// Parse JSON output
	var xtsOut XTSOutput
	if err := json.Unmarshal([]byte(stdout), &xtsOut); err != nil {
		result.Status = "failed"
		result.Error = fmt.Sprintf("parse xts-cli JSON output: %v\nraw: %s", err, truncate(stdout, 500))
		result.EndedAt = time.Now()
		return result, nil
	}

	// Store raw response for auto-save/scenario capture
	result.RawResponse = []byte(stdout)

	if !xtsOut.Success {
		result.Status = "failed"
		result.Error = "xts-cli reported success=false"
		result.EndedAt = time.Now()
		return result, nil
	}

	// Extract captures using json_path expressions against the parsed data.
	// Capture values are json_path expressions like "$.data[0].column_name"
	// or the special values "stdout", "stderr", "row_count".
	for name, expr := range step.Capture {
		val, err := evaluateXTSCapture(expr, &xtsOut, stdout)
		if err != nil {
			// Empty data is not a failure — the query returned 0 rows.
			// Set capture to empty string and continue.
			if xtsOut.RowCount == 0 {
				result.Captures[name] = ""
				continue
			}
			result.Status = "failed"
			result.Error = fmt.Sprintf("capture %q (%s): %v", name, expr, err)
			result.EndedAt = time.Now()
			return result, nil
		}
		result.Captures[name] = val
	}

	result.Status = "passed"
	result.EndedAt = time.Now()
	return result, nil
}

// BuildArgvPublic exposes argv construction for dry-run display.
func (p *XTSProvider) BuildArgvPublic(xts *schema.XTSStepConfig, env string, vars map[string]string, captures map[string]string) ([]string, error) {
	execCtx := &ExecutionContext{Vars: vars, Captures: captures}
	return p.buildArgv(xts, env, execCtx)
}

// buildArgv constructs the xts-cli command-line arguments for a step.
func (p *XTSProvider) buildArgv(xts *schema.XTSStepConfig, env string, execCtx *ExecutionContext) ([]string, error) {
	var args []string

	switch xts.Mode {
	case "view":
		filePath := p.resolveViewPath(xts.File)
		if filePath == "" {
			return nil, fmt.Errorf("cannot resolve view file %q", xts.File)
		}
		args = append(args, "execute", "--file", filePath)
		if env != "" {
			args = append(args, "--environment", env)
		}
		if xts.AutoSelect {
			args = append(args, "--auto-select")
		}
		if xts.SQLTimeout > 0 {
			args = append(args, "--sql-timeout", fmt.Sprintf("%d", xts.SQLTimeout))
		}
		for k, v := range xts.Params {
			resolved, err := resolveParamValue(v, execCtx)
			if err != nil {
				return nil, fmt.Errorf("param %q: %w", k, err)
			}
			args = append(args, "--param", fmt.Sprintf("%s=%s", k, resolved))
		}
		args = append(args, "--format", "json")

	case "activity":
		filePath := p.resolveViewPath(xts.File)
		if filePath == "" {
			return nil, fmt.Errorf("cannot resolve view file %q", xts.File)
		}
		args = append(args, "execute-activity",
			"--file", filePath,
			"--activity", xts.Activity)
		if env != "" {
			args = append(args, "--environment", env)
		}
		if xts.SQLTimeout > 0 {
			args = append(args, "--sql-timeout", fmt.Sprintf("%d", xts.SQLTimeout))
		}
		for k, v := range xts.Params {
			resolved, err := resolveParamValue(v, execCtx)
			if err != nil {
				return nil, fmt.Errorf("param %q: %w", k, err)
			}
			args = append(args, "--param", fmt.Sprintf("%s=%s", k, resolved))
		}
		args = append(args, "--format", "json")

	case "query":
		args = append(args, "query",
			"--type", xts.QueryType)
		if env != "" {
			args = append(args, "-e", env)
		}
		queryText, err := resolveParamValue(xts.Query, execCtx)
		if err != nil {
			return nil, fmt.Errorf("query text: %w", err)
		}
		args = append(args, "-q", queryText)
		args = append(args, "--format", "json")

	default:
		return nil, fmt.Errorf("unknown XTS mode %q", xts.Mode)
	}

	return args, nil
}

// resolveViewPath resolves a view file reference to an absolute path.
// If the file is already absolute, return it. Otherwise, join with views_root.
func (p *XTSProvider) resolveViewPath(file string) string {
	if file == "" {
		return ""
	}
	if filepath.IsAbs(file) {
		return file
	}
	if p.ViewsRoot != "" {
		return filepath.Join(p.ViewsRoot, file)
	}
	return file
}

// resolveParamValue is a pass-through for parameter values.
// Template expressions ({{ .var }}) are resolved by the engine before
// the provider is called, so params arrive pre-resolved.
func resolveParamValue(value string, execCtx *ExecutionContext) (string, error) {
	return value, nil
}

// EvaluateXTSCapturePublic exports capture evaluation for use by the replay engine.
func EvaluateXTSCapturePublic(expr string, xtsOut *XTSOutput, rawStdout string) (string, error) {
	return evaluateXTSCapture(expr, xtsOut, rawStdout)
}

// evaluateXTSCapture extracts a value from XTS JSON output.
// Supported expressions:
//   - "stdout"      → raw JSON string
//   - "row_count"   → xtsOut.RowCount as string
//   - "$.data[N].col" → specific cell value (simplified json_path)
//   - "$.columns"   → comma-separated column names
func evaluateXTSCapture(expr string, xtsOut *XTSOutput, rawStdout string) (string, error) {
	switch {
	case expr == "stdout":
		return strings.TrimSpace(rawStdout), nil

	case expr == "row_count":
		return fmt.Sprintf("%d", xtsOut.RowCount), nil

	case expr == "$.columns":
		return strings.Join(xtsOut.Columns, ","), nil

	case strings.HasPrefix(expr, "$.data"):
		return evaluateDataPath(expr, xtsOut)

	default:
		return "", fmt.Errorf("unsupported capture expression %q (use stdout, row_count, $.data[N].field, or $.columns)", expr)
	}
}

// evaluateDataPath handles $.data[N].field_name expressions.
func evaluateDataPath(expr string, xtsOut *XTSOutput) (string, error) {
	// Parse $.data[N].field_name
	// Supported: $.data[0].column_name, $.data[*].column_name (all rows)
	rest := strings.TrimPrefix(expr, "$.data")
	if rest == "" || rest[0] != '[' {
		return "", fmt.Errorf("invalid data path %q: expected $.data[N].field", expr)
	}

	closeBracket := strings.Index(rest, "]")
	if closeBracket < 0 {
		return "", fmt.Errorf("invalid data path %q: missing ]", expr)
	}

	indexStr := rest[1:closeBracket]
	fieldPart := rest[closeBracket+1:]

	// If no field part, return entire row(s) as JSON
	if fieldPart == "" {
		if indexStr == "*" {
			// All rows as JSON
			b, err := json.MarshalIndent(xtsOut.Data, "", "  ")
			if err != nil {
				return "", fmt.Errorf("marshal data: %w", err)
			}
			return string(b), nil
		}
		var idx int
		if _, err := fmt.Sscanf(indexStr, "%d", &idx); err != nil {
			return "", fmt.Errorf("invalid data index %q in %q", indexStr, expr)
		}
		if idx < 0 || idx >= len(xtsOut.Data) {
			return "", fmt.Errorf("data index %d out of range (have %d rows)", idx, len(xtsOut.Data))
		}
		b, err := json.MarshalIndent(xtsOut.Data[idx], "", "  ")
		if err != nil {
			return "", fmt.Errorf("marshal row %d: %w", idx, err)
		}
		return string(b), nil
	}

	if fieldPart[0] != '.' {
		return "", fmt.Errorf("invalid data path %q: expected .field_name after index", expr)
	}
	fieldName := fieldPart[1:]

	if indexStr == "*" {
		// Collect from all rows
		var values []string
		for _, row := range xtsOut.Data {
			v, ok := row[fieldName]
			if !ok {
				values = append(values, "")
				continue
			}
			values = append(values, fmt.Sprintf("%v", v))
		}
		return strings.Join(values, "\n"), nil
	}

	// Specific index
	var idx int
	if _, err := fmt.Sscanf(indexStr, "%d", &idx); err != nil {
		return "", fmt.Errorf("invalid data index %q in %q", indexStr, expr)
	}
	if idx < 0 || idx >= len(xtsOut.Data) {
		return "", fmt.Errorf("data index %d out of range (have %d rows)", idx, len(xtsOut.Data))
	}

	v, ok := xtsOut.Data[idx][fieldName]
	if !ok {
		return "", fmt.Errorf("field %q not found in data row %d (available: %v)", fieldName, idx, mapKeys(xtsOut.Data[idx]))
	}
	return fmt.Sprintf("%v", v), nil
}

// mapKeys returns the sorted keys of a map for error messages.
func mapKeys(m map[string]interface{}) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return keys
}

// truncate shortens a string for error display.
func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
