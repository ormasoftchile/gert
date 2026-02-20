package testing

// TestResult captures the outcome of running one scenario against a test spec.
type TestResult struct {
	RunbookName  string             `json:"runbook_name"`
	ScenarioName string             `json:"scenario_name"`
	ScenarioDir  string             `json:"scenario_dir"`
	Status       string             `json:"status"` // passed, failed, skipped, error
	DurationMs   int64              `json:"duration_ms"`
	Outcome      *OutcomeComparison `json:"outcome,omitempty"`
	Assertions   []AssertionResult  `json:"assertions"`
	Error        string             `json:"error,omitempty"`
}

// OutcomeComparison pairs expected and actual outcome values.
type OutcomeComparison struct {
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
}

// AssertionResult is the outcome of a single assertion check.
type AssertionResult struct {
	Type     string `json:"type"`          // expected_outcome, expected_capture, must_reach, must_not_reach, expected_step_status, expected_chain
	Key      string `json:"key,omitempty"` // capture name, step ID, or chain index
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message"`
}

// TestSummary aggregates results across scenarios.
type TestSummary struct {
	Total   int `json:"total"`
	Passed  int `json:"passed"`
	Failed  int `json:"failed"`
	Skipped int `json:"skipped"`
	Errors  int `json:"errors"`
}

// TestOutput is the top-level JSON structure for gert test --json.
type TestOutput struct {
	Runbook   string       `json:"runbook"`
	Scenarios []TestResult `json:"scenarios"`
	Summary   TestSummary  `json:"summary"`
}

// RunResult holds the observed execution data collected from a replay run,
// used as input to the assertion evaluator.
type RunResult struct {
	Outcome      string            // terminal outcome state ("completed" if no outcome reached)
	Captures     map[string]string // final captures map
	VisitedSteps []string          // step IDs that entered running state
	StepStatuses map[string]string // step ID → final status (passed, failed, skipped)
	Chain        []string          // runbook names visited during chained execution
}
