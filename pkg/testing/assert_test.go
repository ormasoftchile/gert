package testing

import (
	"testing"
)

// --- TestSpec parsing ---

func TestParseTestSpecFull(t *testing.T) {
	data := []byte(`
expected_outcome: escalated
expected_chain:
  - login-success-rate-below-target
  - error-40613-state-127
expected_captures:
  failure_row_count: ">0"
  login_failure_cause: "LoginErrorsFound_40613_127"
must_reach:
  - query_login_failures
must_not_reach:
  - escalate_unknown_cause
expected_step_status:
  query_login_failures: passed
description: test scenario
tags:
  - gateway
`)
	spec, err := ParseTestSpec(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if spec.ExpectedOutcome != "escalated" {
		t.Errorf("outcome = %q, want escalated", spec.ExpectedOutcome)
	}
	if len(spec.ExpectedChain) != 2 {
		t.Errorf("chain len = %d, want 2", len(spec.ExpectedChain))
	}
	if len(spec.ExpectedCaptures) != 2 {
		t.Errorf("captures len = %d, want 2", len(spec.ExpectedCaptures))
	}
	if len(spec.MustReach) != 1 || spec.MustReach[0] != "query_login_failures" {
		t.Errorf("must_reach = %v", spec.MustReach)
	}
	if len(spec.MustNotReach) != 1 {
		t.Errorf("must_not_reach len = %d, want 1", len(spec.MustNotReach))
	}
	if spec.ExpectedStepStatus["query_login_failures"] != "passed" {
		t.Errorf("step status = %q", spec.ExpectedStepStatus["query_login_failures"])
	}
}

func TestParseTestSpecEmpty(t *testing.T) {
	data := []byte(`{}`)
	spec, err := ParseTestSpec(data)
	if err != nil {
		t.Fatalf("parse error: %v", err)
	}
	if spec.ExpectedOutcome != "" {
		t.Errorf("expected empty outcome, got %q", spec.ExpectedOutcome)
	}
}

func TestParseTestSpecInvalid(t *testing.T) {
	data := []byte(`[[[not yaml`)
	_, err := ParseTestSpec(data)
	if err == nil {
		t.Fatal("expected parse error")
	}
}

// --- Assertion: outcome ---

func TestEvalOutcomePass(t *testing.T) {
	spec := &TestSpec{ExpectedOutcome: "escalated"}
	run := &RunResult{Outcome: "escalated"}
	results := Evaluate(spec, run)
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalOutcomeFail(t *testing.T) {
	spec := &TestSpec{ExpectedOutcome: "mitigate"}
	run := &RunResult{Outcome: "escalated"}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure")
	}
	if results[0].Message == "" {
		t.Error("expected failure message")
	}
}

// --- Assertion: captures (exact) ---

func TestEvalCaptureExactPass(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"cause": "HasDumps"}}
	run := &RunResult{Captures: map[string]string{"cause": "HasDumps"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalCaptureExactFail(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"cause": "HasDumps"}}
	run := &RunResult{Captures: map[string]string{"cause": "HasIoError"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure")
	}
}

func TestEvalCaptureMissing(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"cause": "HasDumps"}}
	run := &RunResult{Captures: map[string]string{}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure for missing capture")
	}
}

// --- Assertion: captures (numeric) ---

func TestEvalCaptureNumericGT(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"count": ">0"}}
	run := &RunResult{Captures: map[string]string{"count": "42"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalCaptureNumericGTFail(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"count": ">0"}}
	run := &RunResult{Captures: map[string]string{"count": "0"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure: 0 is not > 0")
	}
}

func TestEvalCaptureNumericGE(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"count": ">=1"}}
	run := &RunResult{Captures: map[string]string{"count": "1"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalCaptureNumericLE(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"count": "<=50"}}
	run := &RunResult{Captures: map[string]string{"count": "50"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalCaptureNumericNE(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"count": "!=0"}}
	run := &RunResult{Captures: map[string]string{"count": "5"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalCaptureNumericEQ(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"count": "==0"}}
	run := &RunResult{Captures: map[string]string{"count": "0"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalCaptureNumericNonNumeric(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"name": ">0"}}
	run := &RunResult{Captures: map[string]string{"name": "hello"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure: 'hello' is not numeric")
	}
}

// --- Assertion: captures (regex) ---

func TestEvalCaptureRegexPass(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"app": "/^Gateway/"}}
	run := &RunResult{Captures: map[string]string{"app": "Gateway.PDC"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalCaptureRegexFail(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"app": "/^Gateway/"}}
	run := &RunResult{Captures: map[string]string{"app": "Worker.CL"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure")
	}
}

func TestEvalCaptureRegexInvalid(t *testing.T) {
	spec := &TestSpec{ExpectedCaptures: map[string]string{"app": "/[invalid(/"}}
	run := &RunResult{Captures: map[string]string{"app": "test"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure for invalid regex")
	}
}

// --- Assertion: must_reach ---

func TestEvalMustReachPass(t *testing.T) {
	spec := &TestSpec{MustReach: []string{"step_a", "step_b"}}
	run := &RunResult{VisitedSteps: []string{"step_a", "step_b", "step_c"}}
	results := Evaluate(spec, run)
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected pass for %s: %s", r.Key, r.Message)
		}
	}
}

func TestEvalMustReachFail(t *testing.T) {
	spec := &TestSpec{MustReach: []string{"step_missing"}}
	run := &RunResult{VisitedSteps: []string{"step_a"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure")
	}
}

// --- Assertion: must_not_reach ---

func TestEvalMustNotReachPass(t *testing.T) {
	spec := &TestSpec{MustNotReach: []string{"bad_step"}}
	run := &RunResult{VisitedSteps: []string{"step_a"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalMustNotReachFail(t *testing.T) {
	spec := &TestSpec{MustNotReach: []string{"step_a"}}
	run := &RunResult{VisitedSteps: []string{"step_a"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure")
	}
}

// --- Assertion: step status ---

func TestEvalStepStatusPass(t *testing.T) {
	spec := &TestSpec{ExpectedStepStatus: map[string]string{"step_a": "passed"}}
	run := &RunResult{StepStatuses: map[string]string{"step_a": "passed"}}
	results := Evaluate(spec, run)
	if !results[0].Passed {
		t.Errorf("expected pass: %s", results[0].Message)
	}
}

func TestEvalStepStatusFail(t *testing.T) {
	spec := &TestSpec{ExpectedStepStatus: map[string]string{"step_a": "passed"}}
	run := &RunResult{StepStatuses: map[string]string{"step_a": "failed"}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure")
	}
}

func TestEvalStepStatusMissing(t *testing.T) {
	spec := &TestSpec{ExpectedStepStatus: map[string]string{"step_missing": "passed"}}
	run := &RunResult{StepStatuses: map[string]string{}}
	results := Evaluate(spec, run)
	if results[0].Passed {
		t.Error("expected failure for missing step")
	}
}

// --- Assertion: chain ---

func TestEvalChainPass(t *testing.T) {
	spec := &TestSpec{ExpectedChain: []string{"login-success", "error-40613"}}
	run := &RunResult{Chain: []string{"login-success-rate-below-target", "error-40613-state-127"}}
	results := Evaluate(spec, run)
	for _, r := range results {
		if !r.Passed {
			t.Errorf("expected pass for %s: %s", r.Key, r.Message)
		}
	}
}

func TestEvalChainFail(t *testing.T) {
	spec := &TestSpec{ExpectedChain: []string{"login-success", "error-40613"}}
	run := &RunResult{Chain: []string{"login-success-rate-below-target"}}
	results := Evaluate(spec, run)
	// First should pass (substring), second should fail (chain too short)
	if !results[0].Passed {
		t.Error("first chain entry should pass")
	}
	if results[1].Passed {
		t.Error("second chain entry should fail")
	}
}

// --- HasFailures ---

func TestHasFailuresTrue(t *testing.T) {
	results := []AssertionResult{
		{Passed: true},
		{Passed: false},
	}
	if !HasFailures(results) {
		t.Error("expected HasFailures=true")
	}
}

func TestHasFailuresFalse(t *testing.T) {
	results := []AssertionResult{
		{Passed: true},
		{Passed: true},
	}
	if HasFailures(results) {
		t.Error("expected HasFailures=false")
	}
}

func TestHasFailuresEmpty(t *testing.T) {
	if HasFailures(nil) {
		t.Error("expected HasFailures=false for nil")
	}
}

// --- Combined evaluation ---

func TestEvaluateNoAssertions(t *testing.T) {
	spec := &TestSpec{} // all fields empty
	run := &RunResult{Outcome: "completed"}
	results := Evaluate(spec, run)
	if len(results) != 0 {
		t.Errorf("expected 0 assertions, got %d", len(results))
	}
}

func TestEvaluateMultipleFields(t *testing.T) {
	spec := &TestSpec{
		ExpectedOutcome:  "no_action",
		ExpectedCaptures: map[string]string{"count": "==0"},
		MustReach:        []string{"step_a"},
		MustNotReach:     []string{"step_b"},
	}
	run := &RunResult{
		Outcome:      "no_action",
		Captures:     map[string]string{"count": "0"},
		VisitedSteps: []string{"step_a"},
		StepStatuses: map[string]string{"step_a": "passed"},
	}
	results := Evaluate(spec, run)
	if HasFailures(results) {
		for _, r := range results {
			if !r.Passed {
				t.Errorf("unexpected failure: %s — %s", r.Type, r.Message)
			}
		}
	}
	// Should have: 1 outcome + 1 capture + 1 must_reach + 1 must_not_reach = 4
	if len(results) != 4 {
		t.Errorf("expected 4 assertions, got %d", len(results))
	}
}
