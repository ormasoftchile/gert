package runtime

import (
	"context"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/providers"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// scriptedExecutor returns pre-scripted outputs in order.
// After all scripted outputs are consumed, returns "default".
type scriptedExecutor struct {
	calls   int
	outputs []string
}

func (s *scriptedExecutor) Execute(ctx context.Context, command string, args []string, env []string) (*providers.CommandResult, error) {
	var out string
	if s.calls < len(s.outputs) {
		out = s.outputs[s.calls]
	} else {
		out = "default"
	}
	s.calls++
	return &providers.CommandResult{
		Stdout:   []byte(out),
		ExitCode: 0,
	}, nil
}

// TestIterateConverges verifies that an iterate block stops when the
// until condition becomes true before max is reached.
func TestIterateConverges(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-converge"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Max:   5,
					Until: `result == "done"`,
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "check",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"result": "stdout",
							},
						}},
					},
				},
			},
		},
	}

	// "nope", "nope", "done" → converge on pass 3
	executor := &scriptedExecutor{outputs: []string{"nope", "nope", "done"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have called executor 3 times (3 passes)
	if executor.calls != 3 {
		t.Errorf("expected 3 executor calls, got %d", executor.calls)
	}

	// Captures should reflect the final converged value
	if v := engine.State.Captures["result"]; v != "done" {
		t.Errorf("expected capture result='done', got %q", v)
	}

	// Iteration var should be set to last pass (0-indexed: pass 2)
	if v := engine.State.Vars["iteration"]; v != "2" {
		t.Errorf("expected iteration var='2', got %q", v)
	}
}

// TestIterateMaxExceeded verifies that iterate returns an error when
// max passes are reached without the until condition becoming true.
func TestIterateMaxExceeded(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-max"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Max:   3,
					Until: `status == "success"`,
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "try",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"status": "stdout",
							},
						}},
					},
				},
			},
		},
	}

	// Always return "pending" → never converges
	executor := &scriptedExecutor{outputs: []string{"pending", "pending", "pending"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	err = engine.Run(context.Background())
	if err == nil {
		t.Fatal("expected error for max exceeded, got nil")
	}
	if !strings.Contains(err.Error(), "did not converge") {
		t.Errorf("expected 'did not converge' error, got: %v", err)
	}
	if executor.calls != 3 {
		t.Errorf("expected 3 executor calls, got %d", executor.calls)
	}
}

// TestIterateCapturesFlowBetweenPasses ensures captures from pass N
// are visible in pass N+1, enabling the iteration feedback loop.
func TestIterateCapturesFlowBetweenPasses(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-captures"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Max:   4,
					Until: `result == "final"`,
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "step",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"result": "stdout",
							},
						}},
					},
				},
			},
		},
	}

	executor := &scriptedExecutor{outputs: []string{"first", "second", "final"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Captures reflect the final value
	if v := engine.State.Captures["result"]; v != "final" {
		t.Errorf("expected capture result='final', got %q", v)
	}
}

// TestIterateConvergesOnFirstPass verifies that an iterate block can
// converge on the very first pass (max=1 case).
func TestIterateConvergesOnFirstPass(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-first-pass"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Max:   1,
					Until: `result == "ok"`,
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "check",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"result": "stdout",
							},
						}},
					},
				},
			},
		},
	}

	executor := &scriptedExecutor{outputs: []string{"ok"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}
	if executor.calls != 1 {
		t.Errorf("expected 1 executor call, got %d", executor.calls)
	}
}

// TestIterateOutcomeStopsIteration verifies that an outcome triggered
// inside iterate steps terminates the iterate and the tree.
func TestIterateOutcomeStopsIteration(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-outcome"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Max:   10,
					Until: `false`, // never converges via until
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "check",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"status": "stdout",
							},
							Outcomes: []schema.Outcome{
								{
									When:           `status == "critical"`,
									State:          "escalated",
									Recommendation: "Escalate immediately",
								},
							},
						}},
					},
				},
			},
			// This step should NOT execute — outcome terminates the tree
			{Step: schema.Step{
				ID:   "after",
				Type: "cli",
				With: &schema.CLIStepConfig{Argv: []string{"echo", "unreachable"}},
			}},
		},
	}

	// First call: "ok", second: "critical" → outcome fires on pass 2
	executor := &scriptedExecutor{outputs: []string{"ok", "critical"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have run exactly 2 passes (outcome fired on pass 2)
	if executor.calls != 2 {
		t.Errorf("expected 2 executor calls, got %d", executor.calls)
	}

	// Outcome should be set
	if engine.GetOutcome() == nil {
		t.Fatal("expected outcome to be set")
	}
	if engine.GetOutcome().State != "escalated" {
		t.Errorf("expected outcome state 'escalated', got %q", engine.GetOutcome().State)
	}
}

// TestIterateStepsAfterConverge verifies that steps after the iterate
// block execute normally once the iterate converges.
func TestIterateStepsAfterConverge(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-after"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Max:   3,
					Until: `result == "done"`,
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "iter_step",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"result": "stdout",
							},
						}},
					},
				},
			},
			{Step: schema.Step{
				ID:   "final",
				Type: "cli",
				With: &schema.CLIStepConfig{Argv: []string{"echo"}},
				Capture: map[string]string{
					"final_out": "stdout",
				},
			}},
		},
	}

	// Pass 1: "nope", Pass 2: "done" → converge, then "final_value" for the after-step
	executor := &scriptedExecutor{outputs: []string{"nope", "done", "final_value"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// 2 iterate calls + 1 final step
	if executor.calls != 3 {
		t.Errorf("expected 3 executor calls, got %d", executor.calls)
	}
	if v := engine.State.Captures["final_out"]; v != "final_value" {
		t.Errorf("expected final_out='final_value', got %q", v)
	}
}

// --- List-mode iterate (over/as) runtime tests ---

// TestIterateOverList verifies that iterate with over/as executes
// once per item and sets the 'as' variable correctly.
func TestIterateOverList(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-over"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Over: "alpha,beta,gamma",
					As:   "item",
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "process",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"last": "stdout",
							},
						}},
					},
				},
			},
		},
	}

	executor := &scriptedExecutor{outputs: []string{"out-alpha", "out-beta", "out-gamma"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should have executed once per item
	if executor.calls != 3 {
		t.Errorf("expected 3 executor calls, got %d", executor.calls)
	}

	// Capture reflects last item's output
	if v := engine.State.Captures["last"]; v != "out-gamma" {
		t.Errorf("expected capture last='out-gamma', got %q", v)
	}

	// After loop: item var should hold the last item
	if v := engine.State.Vars["item"]; v != "gamma" {
		t.Errorf("expected item var='gamma', got %q", v)
	}

	// iteration var should be last index (0-indexed)
	if v := engine.State.Vars["iteration"]; v != "2" {
		t.Errorf("expected iteration var='2', got %q", v)
	}
}

// TestIterateOverEmpty verifies that a list with only empty entries skips gracefully.
func TestIterateOverEmpty(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-over-empty"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Over: ",,,", // resolves to all-empty items after trimming
					As:   "item",
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "noop",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
						}},
					},
				},
			},
		},
	}

	executor := &scriptedExecutor{outputs: []string{}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// No executions for empty list
	if executor.calls != 0 {
		t.Errorf("expected 0 executor calls, got %d", executor.calls)
	}
}

// TestIterateOverOutcomeStopsIteration verifies that an outcome triggered
// inside list iteration terminates the iterate early.
func TestIterateOverOutcomeStopsIteration(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-over-outcome"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Over: "a,b,c,d",
					As:   "val",
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "check",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"status": "stdout",
							},
							Outcomes: []schema.Outcome{
								{
									When:           `status == "fail"`,
									State:          "failed",
									Recommendation: "Stop processing",
								},
							},
						}},
					},
				},
			},
			{Step: schema.Step{
				ID:   "unreachable",
				Type: "cli",
				With: &schema.CLIStepConfig{Argv: []string{"echo", "nope"}},
			}},
		},
	}

	// Item a: "ok", item b: "fail" → outcome fires, c and d skipped
	executor := &scriptedExecutor{outputs: []string{"ok", "fail"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if executor.calls != 2 {
		t.Errorf("expected 2 executor calls, got %d", executor.calls)
	}
	if engine.GetOutcome() == nil {
		t.Fatal("expected outcome to be set")
	}
	if engine.GetOutcome().State != "failed" {
		t.Errorf("expected outcome state 'failed', got %q", engine.GetOutcome().State)
	}
}

// TestIterateOverDefaultAsVar verifies that when 'as' is empty,
// the default variable name "item" is used.
func TestIterateOverDefaultAsVar(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "iter-over-default-as"},
		Tree: []schema.TreeNode{
			{
				Iterate: &schema.IterateBlock{
					Over: "x,y",
					// As is intentionally empty to test default
					Steps: []schema.TreeNode{
						{Step: schema.Step{
							ID:   "s",
							Type: "cli",
							With: &schema.CLIStepConfig{Argv: []string{"echo"}},
						}},
					},
				},
			},
		},
	}

	executor := &scriptedExecutor{outputs: []string{"first", "second"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	// Should use "item" as default variable name; last item is "y"
	if v := engine.State.Vars["item"]; v != "y" {
		t.Errorf("expected item var='y', got %q", v)
	}
}

// --- Delay runtime tests ---

// TestStepDelayExecution verifies that a step with a very short delay
// still executes correctly.
func TestStepDelayExecution(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "delay-test"},
		Tree: []schema.TreeNode{
			{Step: schema.Step{
				ID:    "delayed",
				Type:  "cli",
				Title: "Delayed step",
				Delay: "1ms",
				With:  &schema.CLIStepConfig{Argv: []string{"echo"}},
				Capture: map[string]string{
					"out": "stdout",
				},
			}},
		},
	}

	executor := &scriptedExecutor{outputs: []string{"delayed-output"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	if err := engine.Run(context.Background()); err != nil {
		t.Fatalf("Run: %v", err)
	}

	if executor.calls != 1 {
		t.Errorf("expected 1 executor call, got %d", executor.calls)
	}
	if v := engine.State.Captures["out"]; v != "delayed-output" {
		t.Errorf("expected capture out='delayed-output', got %q", v)
	}
}

// TestStepDelayCancellation verifies that a step delay is interrupted
// when the context is cancelled.
func TestStepDelayCancellation(t *testing.T) {
	rb := &schema.Runbook{
		APIVersion: "runbook/v1",
		Meta:       schema.Meta{Name: "delay-cancel"},
		Tree: []schema.TreeNode{
			{Step: schema.Step{
				ID:    "long_delay",
				Type:  "cli",
				Title: "Long delay step",
				Delay: "10s", // would be long, but context is cancelled immediately
				With:  &schema.CLIStepConfig{Argv: []string{"echo"}},
			}},
		},
	}

	executor := &scriptedExecutor{outputs: []string{"unreachable"}}
	collector := &providers.DryRunCollector{}
	engine, err := NewEngine(rb, executor, collector, "dry-run", "tester")
	if err != nil {
		t.Fatalf("NewEngine: %v", err)
	}
	defer engine.Trace.Close()

	// Create an already-cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel immediately

	err = engine.Run(ctx)
	// The run should fail due to cancelled context during delay
	if err == nil {
		t.Fatal("expected error from cancelled context, got nil")
	}

	// Should NOT have executed the command (delay was interrupted)
	if executor.calls != 0 {
		t.Errorf("expected 0 executor calls (delay cancelled), got %d", executor.calls)
	}
}
