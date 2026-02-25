package testing

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// Evaluate runs all assertions from a TestSpec against a RunResult and returns
// the individual assertion results. Each field in the TestSpec is checked
// independently; omitted fields produce no assertions.
func Evaluate(spec *TestSpec, run *RunResult) []AssertionResult {
	var results []AssertionResult

	if spec.ExpectedOutcome != "" {
		results = append(results, evalOutcome(spec.ExpectedOutcome, run.Outcome))
	}

	for key, expected := range spec.ExpectedCaptures {
		results = append(results, evalCapture(key, expected, run.Captures))
	}

	for _, stepID := range spec.MustReach {
		results = append(results, evalMustReach(stepID, run.VisitedSteps))
	}

	for _, stepID := range spec.MustNotReach {
		results = append(results, evalMustNotReach(stepID, run.VisitedSteps))
	}

	for stepID, expected := range spec.ExpectedStepStatus {
		results = append(results, evalStepStatus(stepID, expected, run.StepStatuses))
	}

	if len(spec.ExpectedChain) > 0 {
		results = append(results, evalChain(spec.ExpectedChain, run.Chain)...)
	}

	return results
}

// HasFailures returns true if any assertion in the slice failed.
func HasFailures(results []AssertionResult) bool {
	for _, r := range results {
		if !r.Passed {
			return true
		}
	}
	return false
}

func evalOutcome(expected, actual string) AssertionResult {
	passed := expected == actual
	msg := ""
	if !passed {
		msg = fmt.Sprintf("expected outcome %q, got %q", expected, actual)
	}
	return AssertionResult{
		Type:     "expected_outcome",
		Expected: expected,
		Actual:   actual,
		Passed:   passed,
		Message:  msg,
	}
}

func evalCapture(key, expected string, captures map[string]string) AssertionResult {
	actual, exists := captures[key]
	if !exists {
		return AssertionResult{
			Type:     "expected_capture",
			Key:      key,
			Expected: expected,
			Actual:   "",
			Passed:   false,
			Message:  fmt.Sprintf("capture %q not found", key),
		}
	}

	passed, msg := compareValue(expected, actual)
	return AssertionResult{
		Type:     "expected_capture",
		Key:      key,
		Expected: expected,
		Actual:   actual,
		Passed:   passed,
		Message:  msg,
	}
}

// compareValue determines if an actual string satisfies an expected assertion.
// Supports three forms:
//   - Regex:   "/pattern/"
//   - Numeric: ">0", "<100", ">=1", "<=50", "==0", "!=0"
//   - Exact:   any other string (literal equality)
func compareValue(expected, actual string) (bool, string) {
	// Regex: /pattern/
	if len(expected) >= 2 && expected[0] == '/' && expected[len(expected)-1] == '/' {
		pattern := expected[1 : len(expected)-1]
		re, err := regexp.Compile(pattern)
		if err != nil {
			return false, fmt.Sprintf("invalid regex %q: %v", pattern, err)
		}
		if re.MatchString(actual) {
			return true, ""
		}
		return false, fmt.Sprintf("value %q does not match pattern %s", actual, expected)
	}

	// Numeric comparison: >=, <=, !=, ==, >, <
	for _, op := range []string{">=", "<=", "!=", "==", ">", "<"} {
		if strings.HasPrefix(expected, op) {
			threshold := strings.TrimSpace(expected[len(op):])
			return compareNumeric(op, threshold, actual)
		}
	}

	// Exact match
	if expected == actual {
		return true, ""
	}
	return false, fmt.Sprintf("expected %q, got %q", expected, actual)
}

func compareNumeric(op, threshold, actual string) (bool, string) {
	tVal, tErr := strconv.ParseFloat(threshold, 64)
	aVal, aErr := strconv.ParseFloat(actual, 64)
	if tErr != nil || aErr != nil {
		return false, fmt.Sprintf("numeric comparison %s%s failed: cannot parse %q or %q as number", op, threshold, actual, threshold)
	}

	var passed bool
	switch op {
	case ">":
		passed = aVal > tVal
	case "<":
		passed = aVal < tVal
	case ">=":
		passed = aVal >= tVal
	case "<=":
		passed = aVal <= tVal
	case "==":
		passed = aVal == tVal
	case "!=":
		passed = aVal != tVal
	}

	if passed {
		return true, ""
	}
	return false, fmt.Sprintf("expected %s %s%s, got %q", actual, op, threshold, actual)
}

func evalMustReach(stepID string, visited []string) AssertionResult {
	for _, v := range visited {
		if v == stepID {
			return AssertionResult{
				Type:    "must_reach",
				Key:     stepID,
				Passed:  true,
				Message: "",
			}
		}
	}
	return AssertionResult{
		Type:    "must_reach",
		Key:     stepID,
		Passed:  false,
		Message: fmt.Sprintf("step %q was not visited", stepID),
	}
}

func evalMustNotReach(stepID string, visited []string) AssertionResult {
	for _, v := range visited {
		if v == stepID {
			return AssertionResult{
				Type:    "must_not_reach",
				Key:     stepID,
				Passed:  false,
				Message: fmt.Sprintf("step %q was visited but should not have been", stepID),
			}
		}
	}
	return AssertionResult{
		Type:    "must_not_reach",
		Key:     stepID,
		Passed:  true,
		Message: "",
	}
}

func evalStepStatus(stepID, expected string, statuses map[string]string) AssertionResult {
	actual, exists := statuses[stepID]
	if !exists {
		return AssertionResult{
			Type:     "expected_step_status",
			Key:      stepID,
			Expected: expected,
			Actual:   "",
			Passed:   false,
			Message:  fmt.Sprintf("step %q not found in execution history", stepID),
		}
	}
	passed := expected == actual
	msg := ""
	if !passed {
		msg = fmt.Sprintf("step %q: expected status %q, got %q", stepID, expected, actual)
	}
	return AssertionResult{
		Type:     "expected_step_status",
		Key:      stepID,
		Expected: expected,
		Actual:   actual,
		Passed:   passed,
		Message:  msg,
	}
}

func evalChain(expected, actual []string) []AssertionResult {
	var results []AssertionResult
	for i, exp := range expected {
		if i >= len(actual) {
			results = append(results, AssertionResult{
				Type:     "expected_chain",
				Key:      fmt.Sprintf("[%d]", i),
				Expected: exp,
				Actual:   "",
				Passed:   false,
				Message:  fmt.Sprintf("chain[%d]: expected %q but chain has only %d entries", i, exp, len(actual)),
			})
			continue
		}
		// Match: exact name or substring of file path
		matched := actual[i] == exp || strings.Contains(actual[i], exp)
		msg := ""
		if !matched {
			msg = fmt.Sprintf("chain[%d]: expected %q, got %q", i, exp, actual[i])
		}
		results = append(results, AssertionResult{
			Type:     "expected_chain",
			Key:      fmt.Sprintf("[%d]", i),
			Expected: exp,
			Actual:   actual[i],
			Passed:   matched,
			Message:  msg,
		})
	}
	return results
}
