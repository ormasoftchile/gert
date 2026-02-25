package runtime

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/providers"
)

func TestRestoreStepCounts(t *testing.T) {
	e := &Engine{
		State: &RunState{
			History: []*providers.StepResult{
				{StepID: "s1", Status: "passed"},
				{StepID: "s2", Status: "failed"},
				{StepID: "s3", Status: "skipped"},
				{StepID: "s4", Status: "passed"},
				{StepID: "s5", Status: "passed"},
			},
		},
	}

	e.RestoreStepCounts()

	if e.stepCounts.Total != 5 {
		t.Errorf("Total = %d, want 5", e.stepCounts.Total)
	}
	if e.stepCounts.Passed != 3 {
		t.Errorf("Passed = %d, want 3", e.stepCounts.Passed)
	}
	if e.stepCounts.Failed != 1 {
		t.Errorf("Failed = %d, want 1", e.stepCounts.Failed)
	}
	if e.stepCounts.Skipped != 1 {
		t.Errorf("Skipped = %d, want 1", e.stepCounts.Skipped)
	}
}

func TestRestoreStepCountsEmpty(t *testing.T) {
	e := &Engine{
		State: &RunState{History: nil},
	}
	e.RestoreStepCounts()
	if e.stepCounts.Total != 0 {
		t.Errorf("Total = %d, want 0", e.stepCounts.Total)
	}
}
