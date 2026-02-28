package testing

import (
	"testing"
)

func TestParseTestSpec(t *testing.T) {
	yaml := `
description: "healthy service scenario"
expected_outcome: no_action
expected_code: service_healthy
expected_status: completed
must_reach:
  - check_dns
  - evaluate
must_not_reach:
  - restart
expected_outputs:
  status_code: "200"
`
	spec, err := ParseTestSpec([]byte(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if spec.ExpectedOutcome != "no_action" {
		t.Errorf("outcome = %q", spec.ExpectedOutcome)
	}
	if spec.ExpectedCode != "service_healthy" {
		t.Errorf("code = %q", spec.ExpectedCode)
	}
	if len(spec.MustReach) != 2 {
		t.Errorf("must_reach = %d", len(spec.MustReach))
	}
	if len(spec.MustNotReach) != 1 {
		t.Errorf("must_not_reach = %d", len(spec.MustNotReach))
	}
}

func TestEvaluate_AllPass(t *testing.T) {
	spec := &TestSpec{
		ExpectedStatus:  "completed",
		ExpectedOutcome: "resolved",
		ExpectedCode:    "done",
		MustReach:       []string{"step1", "step2"},
		MustNotReach:    []string{"step3"},
		ExpectedOutputs: map[string]string{
			"result": "ok",
		},
	}

	run := &RunResult{
		Status:          "completed",
		OutcomeCategory: "resolved",
		OutcomeCode:     "done",
		VisitedSteps:    []string{"step1", "step2"},
		Outputs:         map[string]any{"result": "ok"},
	}

	results := Evaluate(spec, run)
	if HasFailures(results) {
		for _, r := range results {
			if !r.Passed {
				t.Errorf("unexpected failure: %s: %s", r.Type, r.Message)
			}
		}
	}

	// Should have 6 assertions: status, outcome, code, 2 must_reach, 1 must_not_reach, 1 output
	if len(results) != 7 {
		t.Errorf("expected 7 assertions, got %d", len(results))
	}
}

func TestEvaluate_OutcomeMismatch(t *testing.T) {
	spec := &TestSpec{
		ExpectedOutcome: "resolved",
	}
	run := &RunResult{
		OutcomeCategory: "escalated",
	}

	results := Evaluate(spec, run)
	if !HasFailures(results) {
		t.Error("expected failure for outcome mismatch")
	}
}

func TestEvaluate_MustReachFails(t *testing.T) {
	spec := &TestSpec{
		MustReach: []string{"missing_step"},
	}
	run := &RunResult{
		VisitedSteps: []string{"step1"},
	}

	results := Evaluate(spec, run)
	if !HasFailures(results) {
		t.Error("expected failure for must_reach")
	}
}

func TestEvaluate_MustNotReachFails(t *testing.T) {
	spec := &TestSpec{
		MustNotReach: []string{"step1"},
	}
	run := &RunResult{
		VisitedSteps: []string{"step1"},
	}

	results := Evaluate(spec, run)
	if !HasFailures(results) {
		t.Error("expected failure for must_not_reach")
	}
}

func TestEvaluate_RegexMatch(t *testing.T) {
	spec := &TestSpec{
		ExpectedOutputs: map[string]string{
			"code": `/^2\d\d$/`,
		},
	}
	run := &RunResult{
		Outputs: map[string]any{"code": "200"},
	}

	results := Evaluate(spec, run)
	if HasFailures(results) {
		t.Error("regex should match 200")
	}
}

func TestEvaluate_RegexNoMatch(t *testing.T) {
	spec := &TestSpec{
		ExpectedOutputs: map[string]string{
			"code": `/^2\d\d$/`,
		},
	}
	run := &RunResult{
		Outputs: map[string]any{"code": "503"},
	}

	results := Evaluate(spec, run)
	if !HasFailures(results) {
		t.Error("regex should not match 503")
	}
}

func TestEvaluate_EmptySpec(t *testing.T) {
	spec := &TestSpec{}
	run := &RunResult{}

	results := Evaluate(spec, run)
	if len(results) != 0 {
		t.Errorf("empty spec should produce 0 assertions, got %d", len(results))
	}
}

func TestHasFailures(t *testing.T) {
	all_pass := []AssertionResult{{Passed: true}, {Passed: true}}
	if HasFailures(all_pass) {
		t.Error("no failures expected")
	}

	with_fail := []AssertionResult{{Passed: true}, {Passed: false}}
	if !HasFailures(with_fail) {
		t.Error("failure expected")
	}
}
