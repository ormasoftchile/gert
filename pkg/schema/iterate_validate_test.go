package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestValidateIterateValid verifies a well-formed iterate block passes validation.
func TestValidateIterateValid(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-valid"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Max:   5,
					Until: `result == "done"`,
					Steps: []TreeNode{
						{Step: Step{
							ID:   "check",
							Type: "cli",
							With: &CLIStepConfig{Argv: []string{"echo"}},
							Capture: map[string]string{
								"result": "stdout",
							},
						}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("expected no errors, got: %v", e)
		}
	}
}

// TestValidateIterateBothStepAndIterate rejects a node with both step and iterate.
func TestValidateIterateBothStepAndIterate(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-both"},
		Tree: []TreeNode{
			{
				Step: Step{
					ID:   "s1",
					Type: "cli",
					With: &CLIStepConfig{Argv: []string{"echo"}},
				},
				Iterate: &IterateBlock{
					Max:   3,
					Until: "true",
					Steps: []TreeNode{
						{Step: Step{ID: "inner", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "both") && strings.Contains(e.Message, "iterate") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'both step and iterate' error, got: %v", errs)
	}
}

// TestValidateIterateMaxLessThanOne rejects iterate with max < 1.
func TestValidateIterateMaxLessThanOne(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-max0"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Max:   0,
					Until: "true",
					Steps: []TreeNode{
						{Step: Step{ID: "s", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "max must be at least 1") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'max must be at least 1' error, got: %v", errs)
	}
}

// TestValidateIterateEmptyUntil rejects iterate with blank until condition.
func TestValidateIterateEmptyUntil(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-no-until"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Max:   3,
					Until: "",
					Steps: []TreeNode{
						{Step: Step{ID: "s", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "until") && strings.Contains(e.Message, "convergence") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'until convergence condition' error, got: %v", errs)
	}
}

// TestValidateIterateNoSteps rejects iterate with empty steps array.
func TestValidateIterateNoSteps(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-no-steps"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Max:   3,
					Until: "true",
					Steps: []TreeNode{},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "at least one step") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'at least one step' error, got: %v", errs)
	}
}

// TestValidateIterateWithBranches rejects iterate node that also has branches.
func TestValidateIterateWithBranches(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-branches"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Max:   3,
					Until: "true",
					Steps: []TreeNode{
						{Step: Step{ID: "s", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}}},
					},
				},
				Branches: []Branch{
					{Condition: "true", Steps: []TreeNode{
						{Step: Step{ID: "b", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}}},
					}},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "iterate node cannot have") && strings.Contains(e.Message, "branches") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'iterate node cannot have branches' error, got: %v", errs)
	}
}

// TestValidateIterateNeitherStepNorIterate rejects a tree node with neither.
func TestValidateIterateNeitherStepNorIterate(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-empty"},
		Tree: []TreeNode{
			{}, // neither step nor iterate
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "requires either") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'requires either step or iterate' error, got: %v", errs)
	}
}

// TestValidateIterateNestedStepYAML verifies that iterate steps inside tree
// nodes pass the full 3-phase validation when well-formed.
func TestValidateIterateNestedStepYAML(t *testing.T) {
	content := `apiVersion: runbook/v1
meta:
    name: iter-nested-valid
tree:
    - iterate:
        max: 3
        until: 'done == "yes"'
        steps:
            - step:
                id: inner
                type: cli
                title: Do something
                with:
                    argv: ["echo", "test"]
                capture:
                    done: stdout
`
	dir := t.TempDir()
	path := filepath.Join(dir, "iter-nested.runbook.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, errs := ValidateFile(path)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %v", e)
		}
	}
}

// TestValidateIterateYAMLRoundTrip validates an iterate runbook through the full
// 3-phase pipeline (structural → JSON Schema → domain) via ValidateFile.
func TestValidateIterateYAMLRoundTrip(t *testing.T) {
	content := `apiVersion: runbook/v1
meta:
    name: yaml-iterate
tree:
    - iterate:
        max: 5
        until: 'result == "done"'
        steps:
            - step:
                id: check
                type: cli
                title: Check result
                with:
                    argv: ["echo", "test"]
                capture:
                    result: stdout
    - step:
        id: final
        type: cli
        title: Final step
        with:
            argv: ["echo", "done"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "iterate.runbook.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, errs := ValidateFile(path)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %v", e)
		}
	}
}

// --- List-mode iterate (over/as) validation tests ---

// TestValidateIterateOverValid verifies a well-formed list-mode iterate passes validation.
func TestValidateIterateOverValid(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-over-valid"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Over: "a,b,c",
					As:   "file",
					Steps: []TreeNode{
						{Step: Step{
							ID:   "process",
							Type: "cli",
							With: &CLIStepConfig{Argv: []string{"echo", "test"}},
						}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("expected no errors, got: %v", e)
		}
	}
}

// TestValidateIterateOverDefaultAs verifies that 'as' is optional in list mode.
func TestValidateIterateOverDefaultAs(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-over-no-as"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Over: "x,y,z",
					Steps: []TreeNode{
						{Step: Step{
							ID:   "s",
							Type: "cli",
							With: &CLIStepConfig{Argv: []string{"echo"}},
						}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("expected no errors, got: %v", e)
		}
	}
}

// TestValidateIterateOverWithMax rejects list mode combined with max.
func TestValidateIterateOverWithMax(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-over-max"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Over: "a,b",
					Max:  3,
					Steps: []TreeNode{
						{Step: Step{
							ID:   "s",
							Type: "cli",
							With: &CLIStepConfig{Argv: []string{"echo"}},
						}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "over") && strings.Contains(e.Message, "max") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'over must not set max' error, got: %v", errs)
	}
}

// TestValidateIterateOverWithUntil rejects list mode combined with until.
func TestValidateIterateOverWithUntil(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-over-until"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Over:  "a,b",
					Until: "true",
					Steps: []TreeNode{
						{Step: Step{
							ID:   "s",
							Type: "cli",
							With: &CLIStepConfig{Argv: []string{"echo"}},
						}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "over") && strings.Contains(e.Message, "until") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'over must not set until' error, got: %v", errs)
	}
}

// TestValidateIterateAsWithoutOver rejects 'as' without 'over'.
func TestValidateIterateAsWithoutOver(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v1",
		Meta:       Meta{Name: "iter-as-no-over"},
		Tree: []TreeNode{
			{
				Iterate: &IterateBlock{
					Max:   3,
					Until: "true",
					As:    "file",
					Steps: []TreeNode{
						{Step: Step{
							ID:   "s",
							Type: "cli",
							With: &CLIStepConfig{Argv: []string{"echo"}},
						}},
					},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "as") && strings.Contains(e.Message, "over") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected 'as requires over' error, got: %v", errs)
	}
}

// TestValidateIterateOverYAMLRoundTrip validates a list-mode iterate through
// the full 3-phase pipeline via ValidateFile.
func TestValidateIterateOverYAMLRoundTrip(t *testing.T) {
	content := `apiVersion: runbook/v1
meta:
    name: yaml-iterate-over
tree:
    - iterate:
        over: "file1.md,file2.md,file3.md"
        as: tsg_file
        steps:
            - step:
                id: process
                type: cli
                title: Process file
                with:
                    argv: ["echo", "processing"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "iterate-over.runbook.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, errs := ValidateFile(path)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %v", e)
		}
	}
}

// --- Delay validation tests ---

// TestValidateStepDelayYAMLRoundTrip validates that a step with delay passes
// the full 3-phase pipeline.
func TestValidateStepDelayYAMLRoundTrip(t *testing.T) {
	content := `apiVersion: runbook/v1
meta:
    name: delay-test
tree:
    - step:
        id: wait_and_run
        type: cli
        title: Wait then run
        delay: 5s
        with:
            argv: ["echo", "hello"]
`
	dir := t.TempDir()
	path := filepath.Join(dir, "delay.runbook.yaml")
	if err := os.WriteFile(path, []byte(content), 0644); err != nil {
		t.Fatalf("write temp file: %v", err)
	}
	_, errs := ValidateFile(path)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected error: %v", e)
		}
	}
}
