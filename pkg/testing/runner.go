package testing

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/replay"
	"github.com/ormasoftchile/gert/pkg/runtime"
	"github.com/ormasoftchile/gert/pkg/schema"
	"github.com/ormasoftchile/gert/pkg/tools"
)

// Runner discovers and executes scenario tests for a runbook.
type Runner struct {
	Timeout time.Duration // per-scenario timeout
}

// ScenarioInfo describes a discovered scenario directory.
type ScenarioInfo struct {
	Name    string // directory name (e.g. "icm-748724360")
	Dir     string // absolute path to the scenario directory
	HasTest bool   // whether test.yaml exists
}

// DiscoverScenarios finds all scenario directories for a runbook by convention:
// {runbook-dir}/scenarios/{runbook-name}/*/inputs.yaml
func DiscoverScenarios(runbookPath string) ([]ScenarioInfo, error) {
	dir := filepath.Dir(runbookPath)
	base := filepath.Base(runbookPath)
	// Strip .runbook.yaml / .runbook.yml
	name := strings.TrimSuffix(strings.TrimSuffix(base, ".yaml"), ".yml")
	name = strings.TrimSuffix(name, ".runbook")

	scenariosBase := filepath.Join(dir, "scenarios", name)
	entries, err := os.ReadDir(scenariosBase)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // no scenarios directory — not an error
		}
		return nil, fmt.Errorf("read scenarios directory: %w", err)
	}

	var scenarios []ScenarioInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		scenDir := filepath.Join(scenariosBase, entry.Name())
		inputsPath := filepath.Join(scenDir, "inputs.yaml")
		if _, err := os.Stat(inputsPath); err != nil {
			continue // no inputs.yaml — skip
		}
		hasTest := false
		if _, err := os.Stat(filepath.Join(scenDir, "test.yaml")); err == nil {
			hasTest = true
		}
		scenarios = append(scenarios, ScenarioInfo{
			Name:    entry.Name(),
			Dir:     scenDir,
			HasTest: hasTest,
		})
	}
	return scenarios, nil
}

// RunAll executes all scenarios for a runbook and returns test results.
func (r *Runner) RunAll(runbookPath string, failFast bool) (*TestOutput, error) {
	// Validate the runbook
	rb, errs := schema.ValidateFile(runbookPath)
	if hasValidationErrors(errs) {
		return nil, fmt.Errorf("runbook validation failed: %s", firstError(errs).Message)
	}

	// Discover scenarios
	scenarios, err := DiscoverScenarios(runbookPath)
	if err != nil {
		return nil, err
	}

	output := &TestOutput{
		Runbook: rb.Meta.Name,
	}

	for _, scenario := range scenarios {
		result := r.runScenario(runbookPath, rb, scenario)
		output.Scenarios = append(output.Scenarios, result)

		switch result.Status {
		case "passed":
			output.Summary.Passed++
		case "failed":
			output.Summary.Failed++
		case "skipped":
			output.Summary.Skipped++
		case "error":
			output.Summary.Errors++
		}
		output.Summary.Total++

		if failFast && (result.Status == "failed" || result.Status == "error") {
			break
		}
	}

	return output, nil
}

// RunScenario executes a single named scenario for a runbook.
func (r *Runner) RunScenario(runbookPath, scenarioName string) (*TestResult, error) {
	rb, errs := schema.ValidateFile(runbookPath)
	if hasValidationErrors(errs) {
		return nil, fmt.Errorf("runbook validation failed: %s", firstError(errs).Message)
	}

	scenarios, err := DiscoverScenarios(runbookPath)
	if err != nil {
		return nil, err
	}

	for _, s := range scenarios {
		if s.Name == scenarioName {
			result := r.runScenario(runbookPath, rb, s)
			return &result, nil
		}
	}

	return nil, fmt.Errorf("scenario %q not found", scenarioName)
}

func (r *Runner) runScenario(runbookPath string, rb *schema.Runbook, scenario ScenarioInfo) TestResult {
	start := time.Now()
	result := TestResult{
		RunbookName:  rb.Meta.Name,
		ScenarioName: scenario.Name,
		ScenarioDir:  scenario.Dir,
	}

	// Skip if no test.yaml
	if !scenario.HasTest {
		result.Status = "skipped"
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// Load test spec
	spec, err := LoadTestSpec(filepath.Join(scenario.Dir, "test.yaml"))
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("load test.yaml: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// Execute replay
	runResult, err := r.executeReplay(runbookPath, rb, scenario)
	if err != nil {
		result.Status = "error"
		result.Error = fmt.Sprintf("replay: %v", err)
		result.DurationMs = time.Since(start).Milliseconds()
		return result
	}

	// Compare
	if spec.ExpectedOutcome != "" {
		result.Outcome = &OutcomeComparison{
			Expected: spec.ExpectedOutcome,
			Actual:   runResult.Outcome,
		}
	}
	result.Assertions = Evaluate(spec, runResult)
	if HasFailures(result.Assertions) {
		result.Status = "failed"
	} else {
		result.Status = "passed"
	}
	result.DurationMs = time.Since(start).Milliseconds()
	return result
}

func (r *Runner) executeReplay(runbookPath string, originalRB *schema.Runbook, scenario ScenarioInfo) (*RunResult, error) {
	// Re-parse the runbook so we don't mutate the caller's copy
	rb, errs := schema.ValidateFile(runbookPath)
	if hasValidationErrors(errs) {
		return nil, fmt.Errorf("validation: %s", firstError(errs).Message)
	}

	// Ensure vars map exists
	if rb.Meta.Vars == nil {
		rb.Meta.Vars = make(map[string]string)
	}

	// Load inputs.yaml
	inputsPath := filepath.Join(scenario.Dir, "inputs.yaml")
	inputsData, err := os.ReadFile(inputsPath)
	if err != nil {
		return nil, fmt.Errorf("read inputs: %w", err)
	}

	// Parse inputs — support both flat key:value and nested structures
	inputs := make(map[string]string)
	if err := parseInputsYAML(inputsData, inputs); err != nil {
		return nil, fmt.Errorf("parse inputs: %w", err)
	}
	for k, v := range inputs {
		rb.Meta.Vars[k] = v
	}

	// Load XTS scenario (step responses)
	xtsScenario, err := replay.LoadXTSScenario(scenario.Dir, time.Time{})
	if err != nil {
		return nil, fmt.Errorf("load scenario: %w", err)
	}

	// Create engine in replay mode
	executor := replay.NewReplayExecutor(xtsScenario.Scenario)
	// Use ScenarioCollector if scenario has evidence (for choices/manual steps),
	// otherwise fall back to DryRunCollector
	var collector providers.EvidenceCollector
	if xtsScenario.Scenario != nil && len(xtsScenario.Scenario.Evidence) > 0 {
		collector = providers.NewScenarioCollector(xtsScenario.Scenario.Evidence)
	} else {
		collector = &providers.DryRunCollector{}
	}

	engine, err := runtime.NewEngine(rb, executor, collector, "replay", "test-runner")
	if err != nil {
		return nil, fmt.Errorf("create engine: %w", err)
	}
	defer engine.Trace.Close()

	engine.XTSScenario = xtsScenario
	engine.RunbookPath = runbookPath

	// Load tool definitions if the runbook declares tools:
	if len(rb.Tools) > 0 {
		tm := tools.NewManager(executor, engine.Redact)
		baseDir := filepath.Dir(runbookPath)
		for _, name := range rb.Tools {
			if err := tm.Load(name, filepath.Join("tools", name+".tool.yaml"), baseDir); err != nil {
				fmt.Fprintf(os.Stderr, "test: warning: failed to load tool %q: %v\n", name, err)
			}
		}
		engine.ToolManager = tm
	}

	// Register built-in XTS tool when meta.xts is present
	if rb.Meta.XTS != nil {
		if engine.ToolManager == nil {
			engine.ToolManager = tools.NewManager(executor, engine.Redact)
		}
		engine.ToolManager.RegisterBuiltin("__xts", tools.BuildXTSToolDef(engine.GetXTSCLIPath()))
	}

	// Execute with timeout
	ctx := context.Background()
	if r.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, r.Timeout)
		defer cancel()
	}

	// Suppress engine stdout during test runs
	origStdout := os.Stdout
	devNull, err := os.Open(os.DevNull)
	if err == nil {
		os.Stdout = devNull
		defer func() {
			os.Stdout = origStdout
			devNull.Close()
		}()
	}

	runErr := engine.Run(ctx)

	// Build RunResult from engine state
	runResult := &RunResult{
		Outcome:      "completed",
		Captures:     engine.State.Captures,
		VisitedSteps: make([]string, 0),
		StepStatuses: make(map[string]string),
		Chain:        []string{rb.Meta.Name},
	}

	// Extract outcome from engine
	manifest := engine.BuildManifest()
	if manifest.Outcome != nil {
		runResult.Outcome = manifest.Outcome.State
	} else if runErr != nil {
		runResult.Outcome = "error"
	}

	// Extract visited steps
	for _, h := range engine.State.History {
		runResult.VisitedSteps = append(runResult.VisitedSteps, h.StepID)
		runResult.StepStatuses[h.StepID] = h.Status
	}

	return runResult, nil
}

// parseInputsYAML parses a flat YAML map into string key-value pairs,
// coercing non-string values to their string representation.
func parseInputsYAML(data []byte, out map[string]string) error {
	// Use a generic map to handle YAML parsing of mixed types
	var raw map[string]interface{}

	// Try gopkg.in/yaml.v3 unmarshal
	if err := yamlUnmarshalMap(data, &raw); err != nil {
		return err
	}
	for k, v := range raw {
		out[k] = fmt.Sprintf("%v", v)
	}
	return nil
}

// yamlUnmarshalMap is a helper that uses yaml.v3 to unmarshal into a map.
// Imported here to avoid importing yaml.v3 in assert.go.
func yamlUnmarshalMap(data []byte, v interface{}) error {
	// Use the yaml package imported in spec.go
	return yamlUnmarshalRaw(data, v)
}

// hasValidationErrors returns true if the error list contains any errors
// (not just warnings). Warnings should not block test execution.
func hasValidationErrors(errs []*schema.ValidationError) bool {
	for _, e := range errs {
		if e.Severity != "warning" {
			return true
		}
	}
	return false
}

// firstError returns the first non-warning error, or the first entry if all are warnings.
func firstError(errs []*schema.ValidationError) *schema.ValidationError {
	for _, e := range errs {
		if e.Severity != "warning" {
			return e
		}
	}
	if len(errs) > 0 {
		return errs[0]
	}
	return nil
}
