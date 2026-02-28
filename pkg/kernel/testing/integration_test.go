package testing

import (
	"io"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
	"github.com/ormasoftchile/gert/pkg/kernel/engine"
	"github.com/ormasoftchile/gert/pkg/kernel/executor"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// mockToolExec returns canned responses for tool steps.
type mockToolExec struct {
	responses map[string]*executor.Result
}

func (m *mockToolExec) Execute(td *schema.ToolDefinition, action string, inputs map[string]any, vars map[string]any) (*executor.Result, error) {
	key := td.Meta.Name + ":" + action
	if r, ok := m.responses[key]; ok {
		return r, nil
	}
	return &executor.Result{ExitCode: 0, Outputs: map[string]any{}}, nil
}

func TestIntegration_ReplayAndAssert(t *testing.T) {
	// Build a runbook programmatically
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta: schema.Meta{
			Name: "integration-test",
			Inputs: map[string]contract.ParamDef{
				"hostname": {Type: "string"},
			},
		},
		Tools: []string{"health-check"},
		Steps: []schema.Step{
			{
				ID:     "check",
				Type:   schema.StepTool,
				Tool:   "health-check",
				Action: "check",
				Inputs: map[string]any{
					"url": "https://{{ .hostname }}/healthz",
				},
			},
			{
				ID:   "evaluate",
				Type: schema.StepAssert,
				ContinueOnFail: true,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .status_code }}", Expected: "200"},
				},
			},
			{
				Type: schema.StepBranch,
				Branches: []schema.Branch{
					{
						Condition: `{{ eq .status_code "200" }}`,
						Label:     "healthy",
						Steps: []schema.Step{
							{
								ID: "healthy_end",
								Type: schema.StepEnd,
								Outcome: &schema.Outcome{
									Category: schema.OutcomeNoAction,
									Code:     "service_healthy",
								},
							},
						},
					},
					{
						Condition: "default",
						Label:     "unhealthy",
						Steps: []schema.Step{
							{
								ID: "unhealthy_end",
								Type: schema.StepEnd,
								Outcome: &schema.Outcome{
									Category: schema.OutcomeEscalated,
									Code:     "service_down",
								},
							},
						},
					},
				},
			},
		},
	}

	// Create a mock tool executor that returns status 200
	toolDef := &schema.ToolDefinition{
		Meta: schema.ToolMeta{Name: "health-check"},
		Contract: contract.Contract{
			Outputs: map[string]contract.ParamDef{
				"status_code": {Type: "string"},
			},
		},
	}

	mock := &mockToolExec{
		responses: map[string]*executor.Result{
			"health-check:check": {
				ExitCode: 0,
				Stdout:   "200",
				Outputs:  map[string]any{"status_code": "200"},
			},
		},
	}

	// Run the engine in replay mode
	eng := engine.New(rb, engine.RunConfig{
		RunID:    "test-1",
		Mode:     "replay",
		Vars:     map[string]string{"hostname": "srv1.example.com"},
		ToolExec: mock,
		Stdin:    strings.NewReader("\n"),
		Stdout:   io.Discard,
	})

	// Manually inject tool definition since we're not loading from disk
	eng.SetToolDef("health-check", toolDef)

	engineResult := eng.Run()

	// Build test run result
	runResult := &RunResult{
		Status:       engineResult.Status,
		VisitedSteps: eng.VisitedSteps,
		Outputs:      eng.Vars(),
	}
	if engineResult.Outcome != nil {
		runResult.OutcomeCategory = string(engineResult.Outcome.Category)
		runResult.OutcomeCode = engineResult.Outcome.Code
	}

	// Define test spec
	spec := &TestSpec{
		ExpectedStatus:  "completed",
		ExpectedOutcome: "no_action",
		ExpectedCode:    "service_healthy",
		MustReach:       []string{"check", "evaluate", "healthy_end"},
		MustNotReach:    []string{"unhealthy_end"},
		ExpectedOutputs: map[string]string{
			"status_code": "200",
		},
	}

	// Evaluate
	assertions := Evaluate(spec, runResult)
	for _, a := range assertions {
		if !a.Passed {
			t.Errorf("FAIL [%s] %s", a.Type, a.Message)
		}
	}
	if HasFailures(assertions) {
		t.Error("integration test failed")
	}
}

func TestIntegration_FailedScenario(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "kernel/v0",
		Meta: schema.Meta{
			Name: "fail-test",
			Inputs: map[string]contract.ParamDef{
				"hostname": {Type: "string"},
			},
		},
		Tools: []string{"health-check"},
		Steps: []schema.Step{
			{
				ID:     "check",
				Type:   schema.StepTool,
				Tool:   "health-check",
				Action: "check",
			},
			{
				ID:   "verify",
				Type: schema.StepAssert,
				Assert: []schema.Assertion{
					{Type: "equals", Value: "{{ .status_code }}", Expected: "200"},
				},
			},
			{
				Type: schema.StepEnd,
				Outcome: &schema.Outcome{
					Category: schema.OutcomeResolved,
					Code:     "done",
				},
			},
		},
	}

	mock := &mockToolExec{
		responses: map[string]*executor.Result{
			"health-check:check": {
				ExitCode: 0,
				Outputs:  map[string]any{"status_code": "503"},
			},
		},
	}

	toolDef := &schema.ToolDefinition{
		Meta: schema.ToolMeta{Name: "health-check"},
	}

	eng := engine.New(rb, engine.RunConfig{
		RunID:    "test-2",
		Mode:     "replay",
		Vars:     map[string]string{"hostname": "broken.example.com"},
		ToolExec: mock,
		Stdin:    strings.NewReader("\n"),
		Stdout:   io.Discard,
	})
	eng.SetToolDef("health-check", toolDef)

	engineResult := eng.Run()

	runResult := &RunResult{
		Status:       engineResult.Status,
		VisitedSteps: eng.VisitedSteps,
		Outputs:      eng.Vars(),
	}
	if engineResult.Outcome != nil {
		runResult.OutcomeCategory = string(engineResult.Outcome.Category)
		runResult.OutcomeCode = engineResult.Outcome.Code
	}

	// The assert step should fail, so the runbook fails
	spec := &TestSpec{
		ExpectedStatus: "failed",
		MustReach:      []string{"check", "verify"},
	}

	assertions := Evaluate(spec, runResult)
	for _, a := range assertions {
		if !a.Passed {
			t.Errorf("FAIL [%s] %s", a.Type, a.Message)
		}
	}
}
