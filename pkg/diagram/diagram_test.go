package diagram

import (
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/schema"
)

func TestGenerateMermaid_LinearFlow(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "linear-test"},
		Tree: []schema.TreeNode{
			{Step: schema.Step{ID: "step-1", Type: "cli", Title: "Run query"}},
			{Step: schema.Step{ID: "step-2", Type: "manual", Title: "Verify output"}},
		},
	}

	out, err := Generate(rb, FormatMermaid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "flowchart TD") {
		t.Error("missing flowchart header")
	}
	if !strings.Contains(out, "step_1") {
		t.Error("missing step-1 node")
	}
	if !strings.Contains(out, "step_1 --> step_2") {
		t.Errorf("missing sequential edge, got:\n%s", out)
	}
}

func TestGenerateMermaid_Branches(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "branch-test"},
		Tree: []schema.TreeNode{
			{
				Step: schema.Step{ID: "check", Type: "cli", Title: "Check status"},
				Branches: []schema.Branch{
					{
						Condition: "output contains error",
						Label:     "Error path",
						Steps: []schema.TreeNode{
							{Step: schema.Step{ID: "fix", Type: "cli", Title: "Apply fix"}},
						},
					},
				},
			},
			{Step: schema.Step{ID: "done", Type: "manual", Title: "Confirm"}},
		},
	}

	out, err := Generate(rb, FormatMermaid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "fix") {
		t.Error("missing branch step node")
	}
	if !strings.Contains(out, "Error path") {
		t.Errorf("missing branch label, got:\n%s", out)
	}
}

func TestGenerateMermaid_Outcomes(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "outcome-test"},
		Tree: []schema.TreeNode{
			{Step: schema.Step{
				ID:    "final",
				Type:  "manual",
				Title: "Summarise",
				Outcomes: []schema.Outcome{
					{When: "issue fixed", State: "resolved", Recommendation: "Close ticket"},
					{When: "needs help", State: "escalated", Recommendation: "Escalate"},
				},
			}},
		},
	}

	out, err := Generate(rb, FormatMermaid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "Resolved") {
		t.Error("missing resolved outcome")
	}
	if !strings.Contains(out, "Request Assistance") {
		t.Error("missing escalated outcome")
	}
	if !strings.Contains(out, "style final_resolved") {
		t.Error("missing outcome style")
	}
}

func TestGenerateMermaid_Captures(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "capture-test"},
		Tree: []schema.TreeNode{
			{Step: schema.Step{
				ID:    "query",
				Type:  "cli",
				Title: "Run SQL",
				Capture: map[string]string{
					"result": "stdout",
				},
			}},
		},
	}

	out, err := Generate(rb, FormatMermaid)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "result") {
		t.Errorf("missing capture in diagram, got:\n%s", out)
	}
}

func TestGenerateASCII(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "ASCII Test"},
		Tree: []schema.TreeNode{
			{Step: schema.Step{ID: "s1", Type: "cli", Title: "Step One"}},
			{Step: schema.Step{ID: "s2", Type: "manual", Title: "Step Two"}},
		},
	}

	out, err := Generate(rb, FormatASCII)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "ASCII Test") {
		t.Error("missing runbook name")
	}
	if !strings.Contains(out, "⚡") {
		t.Error("missing CLI icon")
	}
	if !strings.Contains(out, "🧑") {
		t.Error("missing manual icon")
	}
}

func TestGenerate_UnsupportedFormat(t *testing.T) {
	rb := &schema.Runbook{}
	_, err := Generate(rb, "svg")
	if err == nil {
		t.Fatal("expected error for unsupported format")
	}
	if !strings.Contains(err.Error(), "unsupported") {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestGenerate_NilRunbook(t *testing.T) {
	_, err := Generate(nil, FormatMermaid)
	if err == nil {
		t.Fatal("expected error for nil runbook")
	}
}

func TestGenerateASCII_Outcomes(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{Name: "Outcome ASCII"},
		Tree: []schema.TreeNode{
			{Step: schema.Step{
				ID:    "final",
				Type:  "manual",
				Title: "Summarise",
				Outcomes: []schema.Outcome{
					{State: "resolved", Recommendation: "Close"},
					{State: "escalated", Recommendation: "Escalate"},
				},
			}},
		},
	}

	out, err := Generate(rb, FormatASCII)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !strings.Contains(out, "✅") {
		t.Error("missing resolved icon")
	}
	if !strings.Contains(out, "Request Assistance") {
		t.Error("missing escalated label in ASCII")
	}
}

func TestFlattenTree_NestedSteps(t *testing.T) {
	nodes := []schema.TreeNode{
		{Step: schema.Step{ID: "a", Type: "cli", Title: "A"}},
		{
			Iterate: &schema.IterateBlock{
				Steps: []schema.TreeNode{
					{Step: schema.Step{ID: "b", Type: "cli", Title: "B"}},
				},
			},
		},
		{Step: schema.Step{ID: "c", Type: "cli", Title: "C"}},
	}

	result := flattenTree(nodes)
	if len(result) != 3 {
		t.Fatalf("expected 3 steps, got %d", len(result))
	}
	if result[1].id != "b" {
		t.Errorf("expected iterate step b, got %s", result[1].id)
	}
}
