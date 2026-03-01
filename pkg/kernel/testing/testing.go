// Package testing implements the kernel/v0 scenario-based test harness.
// It replays runbooks against canned scenarios and evaluates assertions
// on structured outcomes, step visits, and captured values.
package testing

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// TestSpec declares what to assert about a scenario replay result.
// All fields are optional — omitted fields produce no assertions.
type TestSpec struct {
	Description     string            `yaml:"description,omitempty" json:"description,omitempty"`
	ExpectedOutcome string            `yaml:"expected_outcome,omitempty" json:"expected_outcome,omitempty"` // outcome category
	ExpectedCode    string            `yaml:"expected_code,omitempty" json:"expected_code,omitempty"`       // outcome code
	MustReach       []string          `yaml:"must_reach,omitempty" json:"must_reach,omitempty"`             // step IDs that must be visited
	MustNotReach    []string          `yaml:"must_not_reach,omitempty" json:"must_not_reach,omitempty"`     // step IDs that must NOT be visited
	ExpectedOutputs map[string]string `yaml:"expected_outputs,omitempty" json:"expected_outputs,omitempty"` // variable → expected value
	ExpectedStatus  string            `yaml:"expected_status,omitempty" json:"expected_status,omitempty"`   // completed, failed, error
	Tags            []string          `yaml:"tags,omitempty" json:"tags,omitempty"`
}

// LoadTestSpec loads a test spec from a YAML file.
func LoadTestSpec(path string) (*TestSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read test spec: %w", err)
	}
	return ParseTestSpec(data)
}

// ParseTestSpec parses test spec YAML.
func ParseTestSpec(data []byte) (*TestSpec, error) {
	var s TestSpec
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse test spec: %w", err)
	}
	return &s, nil
}

// ---------------------------------------------------------------------------
// Run Result (input to assertion evaluator)
// ---------------------------------------------------------------------------

// RunResult captures execution data for assertion evaluation.
type RunResult struct {
	OutcomeCategory string
	OutcomeCode     string
	Status          string         // completed, failed, error
	VisitedSteps    []string       // ordered step IDs
	Outputs         map[string]any // final variable state
	Error           error
}

// ---------------------------------------------------------------------------
// Assertion Evaluation
// ---------------------------------------------------------------------------

// AssertionResult is the result of a single assertion.
type AssertionResult struct {
	Type     string `json:"type"` // expected_outcome, expected_code, must_reach, etc.
	Key      string `json:"key,omitempty"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Passed   bool   `json:"passed"`
	Message  string `json:"message,omitempty"`
}

// Evaluate runs all assertions from a TestSpec against a RunResult.
func Evaluate(spec *TestSpec, run *RunResult) []AssertionResult {
	var results []AssertionResult

	if spec.ExpectedStatus != "" {
		results = append(results, AssertionResult{
			Type:     "expected_status",
			Expected: spec.ExpectedStatus,
			Actual:   run.Status,
			Passed:   run.Status == spec.ExpectedStatus,
			Message:  fmt.Sprintf("status: expected %q, got %q", spec.ExpectedStatus, run.Status),
		})
	}

	if spec.ExpectedOutcome != "" {
		results = append(results, AssertionResult{
			Type:     "expected_outcome",
			Expected: spec.ExpectedOutcome,
			Actual:   run.OutcomeCategory,
			Passed:   run.OutcomeCategory == spec.ExpectedOutcome,
			Message:  fmt.Sprintf("outcome: expected %q, got %q", spec.ExpectedOutcome, run.OutcomeCategory),
		})
	}

	if spec.ExpectedCode != "" {
		results = append(results, AssertionResult{
			Type:     "expected_code",
			Expected: spec.ExpectedCode,
			Actual:   run.OutcomeCode,
			Passed:   run.OutcomeCode == spec.ExpectedCode,
			Message:  fmt.Sprintf("code: expected %q, got %q", spec.ExpectedCode, run.OutcomeCode),
		})
	}

	visitedSet := make(map[string]bool, len(run.VisitedSteps))
	for _, s := range run.VisitedSteps {
		visitedSet[s] = true
	}

	for _, stepID := range spec.MustReach {
		passed := visitedSet[stepID]
		results = append(results, AssertionResult{
			Type:     "must_reach",
			Key:      stepID,
			Expected: "visited",
			Actual:   boolToVisited(passed),
			Passed:   passed,
			Message:  fmt.Sprintf("must_reach %q: %s", stepID, boolToVisited(passed)),
		})
	}

	for _, stepID := range spec.MustNotReach {
		visited := visitedSet[stepID]
		results = append(results, AssertionResult{
			Type:     "must_not_reach",
			Key:      stepID,
			Expected: "not visited",
			Actual:   boolToVisited(visited),
			Passed:   !visited,
			Message:  fmt.Sprintf("must_not_reach %q: %s", stepID, boolToVisited(visited)),
		})
	}

	for key, expected := range spec.ExpectedOutputs {
		actual := ""
		if v, ok := run.Outputs[key]; ok {
			actual = fmt.Sprint(v)
		}
		passed := compareValue(expected, actual)
		results = append(results, AssertionResult{
			Type:     "expected_output",
			Key:      key,
			Expected: expected,
			Actual:   actual,
			Passed:   passed,
			Message:  fmt.Sprintf("output %q: expected %q, got %q", key, expected, actual),
		})
	}

	return results
}

// HasFailures returns true if any assertion failed.
func HasFailures(results []AssertionResult) bool {
	for _, r := range results {
		if !r.Passed {
			return true
		}
	}
	return false
}

// compareValue supports three match modes:
//   - /pattern/ → regex match
//   - exact string equality (default)
func compareValue(expected, actual string) bool {
	// Regex mode
	if strings.HasPrefix(expected, "/") && strings.HasSuffix(expected, "/") && len(expected) > 2 {
		pattern := expected[1 : len(expected)-1]
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false
		}
		return re.MatchString(actual)
	}
	// Exact match
	return expected == actual
}

func boolToVisited(b bool) string {
	if b {
		return "visited"
	}
	return "not visited"
}
