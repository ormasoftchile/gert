package testing

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ormasoftchile/gert/pkg/kernel/engine"
	"github.com/ormasoftchile/gert/pkg/kernel/replay"
	kschema "github.com/ormasoftchile/gert/pkg/kernel/schema"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
	"github.com/ormasoftchile/gert/pkg/kernel/validate"
)

// TestResult is the result of running one scenario.
type TestResult struct {
	RunbookName  string            `json:"runbook_name"`
	ScenarioName string            `json:"scenario_name"`
	Status       string            `json:"status"` // passed, failed, skipped, error
	DurationMs   int64             `json:"duration_ms"`
	Assertions   []AssertionResult `json:"assertions,omitempty"`
	Error        string            `json:"error,omitempty"`
}

// TestSummary aggregates counts across scenarios.
type TestSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// TestOutput is the top-level output of a test run.
type TestOutput struct {
	Runbook   string       `json:"runbook"`
	Scenarios []TestResult `json:"scenarios"`
	Summary   TestSummary  `json:"summary"`
}

// Runner executes scenario-based tests against a runbook.
type Runner struct {
	Timeout  time.Duration
	FailFast bool
}

// ScenarioInfo describes a discovered scenario directory.
type ScenarioInfo struct {
	Name string
	Dir  string
}

// DiscoverScenarios finds scenario directories for a runbook.
// Convention: scenarios are in a sibling `scenarios/<runbook-name>/` directory,
// each subdirectory containing a `scenario.yaml`.
func DiscoverScenarios(runbookPath string) ([]ScenarioInfo, error) {
	dir := filepath.Dir(runbookPath)
	base := strings.TrimSuffix(filepath.Base(runbookPath), filepath.Ext(runbookPath))

	scenariosDir := filepath.Join(dir, "scenarios", base)
	entries, err := os.ReadDir(scenariosDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read scenarios dir: %w", err)
	}

	var scenarios []ScenarioInfo
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		scenarioFile := filepath.Join(scenariosDir, entry.Name(), "scenario.yaml")
		if _, err := os.Stat(scenarioFile); err == nil {
			scenarios = append(scenarios, ScenarioInfo{
				Name: entry.Name(),
				Dir:  filepath.Join(scenariosDir, entry.Name()),
			})
		}
	}
	return scenarios, nil
}

// RunAll discovers and runs all scenarios for a runbook.
func (r *Runner) RunAll(runbookPath string) (*TestOutput, error) {
	scenarios, err := DiscoverScenarios(runbookPath)
	if err != nil {
		return nil, err
	}

	rb, valErrs := validate.ValidateFile(runbookPath)
	if hasValidationErrors(valErrs) {
		return nil, fmt.Errorf("runbook validation failed")
	}

	output := &TestOutput{
		Runbook: rb.Meta.Name,
	}

	for _, si := range scenarios {
		result := r.runScenario(rb, runbookPath, si)
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

		if r.FailFast && (result.Status == "failed" || result.Status == "error") {
			break
		}
	}

	return output, nil
}

// RunScenario runs a single named scenario.
func (r *Runner) RunScenario(runbookPath, scenarioName string) (*TestResult, error) {
	rb, valErrs := validate.ValidateFile(runbookPath)
	if hasValidationErrors(valErrs) {
		return nil, fmt.Errorf("runbook validation failed")
	}

	dir := filepath.Dir(runbookPath)
	base := strings.TrimSuffix(filepath.Base(runbookPath), filepath.Ext(runbookPath))
	scenarioDir := filepath.Join(dir, "scenarios", base, scenarioName)

	si := ScenarioInfo{Name: scenarioName, Dir: scenarioDir}
	result := r.runScenario(rb, runbookPath, si)
	return &result, nil
}

// runScenario executes a single scenario and evaluates its test spec.
func (r *Runner) runScenario(rb *kschema.Runbook, runbookPath string, si ScenarioInfo) TestResult {
	ctx := context.Background()
	start := time.Now()

	// Load scenario
	scenario, err := replay.LoadScenarioDir(si.Dir)
	if err != nil {
		return TestResult{
			RunbookName:  rb.Meta.Name,
			ScenarioName: si.Name,
			Status:       "error",
			DurationMs:   time.Since(start).Milliseconds(),
			Error:        fmt.Sprintf("load scenario: %s", err),
		}
	}

	// Load test spec (optional — if missing, just check execution succeeds)
	var spec *TestSpec
	testSpecPath := filepath.Join(si.Dir, "test.yaml")
	if _, err := os.Stat(testSpecPath); err == nil {
		spec, err = LoadTestSpec(testSpecPath)
		if err != nil {
			return TestResult{
				RunbookName:  rb.Meta.Name,
				ScenarioName: si.Name,
				Status:       "error",
				DurationMs:   time.Since(start).Milliseconds(),
				Error:        fmt.Sprintf("load test spec: %s", err),
			}
		}
	}

	if spec == nil {
		// No test.yaml — skip
		return TestResult{
			RunbookName:  rb.Meta.Name,
			ScenarioName: si.Name,
			Status:       "skipped",
			DurationMs:   time.Since(start).Milliseconds(),
		}
	}

	// Build replay executor
	replayExec := replay.NewReplayExecutor(scenario)

	// Merge scenario inputs
	vars := make(map[string]string)
	for k, v := range scenario.Inputs {
		vars[k] = v
	}

	// Execute in replay mode
	var traceBuf bytes.Buffer
	tw := trace.NewWriter(&traceBuf, "test-"+si.Name)

	cfg := engine.RunConfig{
		RunID:    "test-" + si.Name,
		Mode:     "replay",
		Vars:     vars,
		BaseDir:  filepath.Dir(runbookPath),
		Trace:    tw,
		ToolExec: replayExec,
		Stdin:    buildReplayStdin(replayExec, rb),
		Stdout:   io.Discard,
	}

	eng := engine.New(rb, cfg)

	// Run with timeout
	var engineResult *engine.RunResult
	if r.Timeout > 0 {
		done := make(chan struct{})
		go func() {
			engineResult = eng.Run(ctx)
			close(done)
		}()
		select {
		case <-done:
		case <-time.After(r.Timeout):
			return TestResult{
				RunbookName:  rb.Meta.Name,
				ScenarioName: si.Name,
				Status:       "error",
				DurationMs:   time.Since(start).Milliseconds(),
				Error:        "timeout",
			}
		}
	} else {
		engineResult = eng.Run(ctx)
	}

	// Build RunResult for assertion evaluation
	runResult := &RunResult{
		Status:       engineResult.Status,
		VisitedSteps: eng.VisitedSteps,
		Outputs:      eng.Vars(),
	}
	if engineResult.Outcome != nil {
		runResult.OutcomeCategory = string(engineResult.Outcome.Category)
		runResult.OutcomeCode = engineResult.Outcome.Code
	}
	if engineResult.Error != nil {
		runResult.Error = engineResult.Error
	}

	// Evaluate assertions
	assertions := Evaluate(spec, runResult)
	status := "passed"
	if HasFailures(assertions) {
		status = "failed"
	}

	return TestResult{
		RunbookName:  rb.Meta.Name,
		ScenarioName: si.Name,
		Status:       status,
		DurationMs:   time.Since(start).Milliseconds(),
		Assertions:   assertions,
	}
}

// buildReplayStdin creates a reader that provides canned evidence for manual steps.
func buildReplayStdin(re *replay.ReplayExecutor, rb *kschema.Runbook) io.Reader {
	// Build a buffer with all evidence entries, one per line,
	// in the order manual steps appear, then their evidence entries appear.
	var lines []string
	walkManualSteps(rb.Steps, re, &lines)
	input := strings.Join(lines, "\n") + "\n"
	return strings.NewReader(input)
}

func walkManualSteps(steps []kschema.Step, re *replay.ReplayExecutor, lines *[]string) {
	for _, s := range steps {
		if s.Type == kschema.StepManual && s.ID != "" {
			evidence := re.EvidenceForStep(s.ID)
			if evidence != nil {
				for _, ev := range s.RequiredEvidence {
					if val, ok := evidence[ev.Name]; ok {
						*lines = append(*lines, val)
					} else {
						*lines = append(*lines, "")
					}
				}
			}
			if len(s.RequiredEvidence) == 0 {
				// Manual step with no evidence — just needs Enter
				*lines = append(*lines, "")
			}
		}
		for _, br := range s.Branches {
			walkManualSteps(br.Steps, re, lines)
		}
	}
}

func hasValidationErrors(errs []*validate.ValidationError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}
