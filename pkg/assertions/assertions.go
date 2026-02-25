// Package assertions implements the 7 assertion types for post-execution checks.
package assertions

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// Evaluate runs a single assertion against the given output and exit code.
func Evaluate(a schema.Assertion, output string, exitCode int) *providers.AssertionResult {
	if a.Contains != "" {
		return EvalContains(output, a.Contains)
	}
	if a.NotContains != "" {
		return EvalNotContains(output, a.NotContains)
	}
	if a.Matches != "" {
		return EvalMatches(output, a.Matches)
	}
	if a.ExitCode != nil {
		return EvalExitCode(exitCode, *a.ExitCode)
	}
	if a.Equals != "" {
		return EvalEquals(output, a.Equals)
	}
	if a.NotEquals != "" {
		return EvalNotEquals(output, a.NotEquals)
	}
	if a.JSONPath != nil {
		return EvalJSONPath(output, a.JSONPath.Path, a.JSONPath.Equals)
	}
	return &providers.AssertionResult{
		Type:    "unknown",
		Passed:  false,
		Message: "no assertion field set",
	}
}

// EvalContains checks if output contains the expected substring.
func EvalContains(output, expected string) *providers.AssertionResult {
	passed := strings.Contains(output, expected)
	msg := fmt.Sprintf("output contains %q", expected)
	if !passed {
		msg = fmt.Sprintf("output does not contain %q", expected)
	}
	return &providers.AssertionResult{
		Type:     "contains",
		Expected: expected,
		Actual:   truncate(output, 200),
		Passed:   passed,
		Message:  msg,
	}
}

// EvalNotContains checks that output does NOT contain the substring.
func EvalNotContains(output, expected string) *providers.AssertionResult {
	passed := !strings.Contains(output, expected)
	msg := fmt.Sprintf("output does not contain %q", expected)
	if !passed {
		msg = fmt.Sprintf("output contains %q (unexpected)", expected)
	}
	return &providers.AssertionResult{
		Type:     "not_contains",
		Expected: expected,
		Actual:   truncate(output, 200),
		Passed:   passed,
		Message:  msg,
	}
}

// EvalMatches checks if output matches the regex pattern.
func EvalMatches(output, pattern string) *providers.AssertionResult {
	re, err := regexp.Compile(pattern)
	if err != nil {
		return &providers.AssertionResult{
			Type:     "matches",
			Expected: pattern,
			Actual:   truncate(output, 200),
			Passed:   false,
			Message:  fmt.Sprintf("invalid regex: %v", err),
		}
	}
	passed := re.MatchString(output)
	msg := fmt.Sprintf("output matches /%s/", pattern)
	if !passed {
		msg = fmt.Sprintf("output does not match /%s/", pattern)
	}
	return &providers.AssertionResult{
		Type:     "matches",
		Expected: pattern,
		Actual:   truncate(output, 200),
		Passed:   passed,
		Message:  msg,
	}
}

// EvalExitCode checks if the actual exit code matches expected.
func EvalExitCode(actual, expected int) *providers.AssertionResult {
	passed := actual == expected
	msg := fmt.Sprintf("exit code %d == %d", actual, expected)
	if !passed {
		msg = fmt.Sprintf("exit code %d != %d", actual, expected)
	}
	return &providers.AssertionResult{
		Type:     "exit_code",
		Expected: fmt.Sprintf("%d", expected),
		Actual:   fmt.Sprintf("%d", actual),
		Passed:   passed,
		Message:  msg,
	}
}

// EvalEquals checks if output exactly equals expected.
func EvalEquals(output, expected string) *providers.AssertionResult {
	passed := output == expected
	msg := fmt.Sprintf("output equals %q", expected)
	if !passed {
		msg = fmt.Sprintf("output %q != %q", truncate(output, 100), expected)
	}
	return &providers.AssertionResult{
		Type:     "equals",
		Expected: expected,
		Actual:   truncate(output, 200),
		Passed:   passed,
		Message:  msg,
	}
}

// EvalNotEquals checks that output does NOT exactly equal expected.
func EvalNotEquals(output, expected string) *providers.AssertionResult {
	passed := output != expected
	msg := fmt.Sprintf("output does not equal %q", expected)
	if !passed {
		msg = fmt.Sprintf("output equals %q (unexpected)", expected)
	}
	return &providers.AssertionResult{
		Type:     "not_equals",
		Expected: expected,
		Actual:   truncate(output, 200),
		Passed:   passed,
		Message:  msg,
	}
}

// EvalJSONPath extracts a value at a JSON path and compares to expected.
// Supports simple dot-notation paths like $.status.phase.
func EvalJSONPath(jsonOutput, path, expected string) *providers.AssertionResult {
	// Parse the JSON
	var data interface{}
	if err := json.Unmarshal([]byte(jsonOutput), &data); err != nil {
		return &providers.AssertionResult{
			Type:     "json_path",
			Expected: expected,
			Actual:   truncate(jsonOutput, 200),
			Passed:   false,
			Message:  fmt.Sprintf("invalid JSON: %v", err),
		}
	}

	// Navigate the path
	actual, err := navigateJSONPath(data, path)
	if err != nil {
		return &providers.AssertionResult{
			Type:     "json_path",
			Expected: expected,
			Actual:   "",
			Passed:   false,
			Message:  fmt.Sprintf("path %s: %v", path, err),
		}
	}

	actualStr := fmt.Sprintf("%v", actual)
	passed := actualStr == expected
	msg := fmt.Sprintf("json_path %s = %q", path, actualStr)
	if !passed {
		msg = fmt.Sprintf("json_path %s = %q, want %q", path, actualStr, expected)
	}
	return &providers.AssertionResult{
		Type:     "json_path",
		Expected: expected,
		Actual:   actualStr,
		Passed:   passed,
		Message:  msg,
	}
}

// navigateJSONPath navigates a simple JSON path ($.key1.key2).
func navigateJSONPath(data interface{}, path string) (interface{}, error) {
	// Strip leading $. or $
	path = strings.TrimPrefix(path, "$.")
	path = strings.TrimPrefix(path, "$")
	if path == "" {
		return data, nil
	}

	parts := strings.Split(path, ".")
	current := data
	for _, part := range parts {
		m, ok := current.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("expected object at %q, got %T", part, current)
		}
		val, exists := m[part]
		if !exists {
			return nil, fmt.Errorf("key %q not found", part)
		}
		current = val
	}
	return current, nil
}

func truncate(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	return s[:maxLen] + "..."
}
