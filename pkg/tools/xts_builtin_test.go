package tools

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/schema"
)

func TestBuiltinXTSToolDef(t *testing.T) {
	td := BuiltinXTSToolDef()

	if td.APIVersion != "tool/v0" {
		t.Errorf("apiVersion = %q, want %q", td.APIVersion, "tool/v0")
	}
	if td.Meta.Name != "xts" {
		t.Errorf("name = %q, want %q", td.Meta.Name, "xts")
	}
	if td.Meta.Binary != "xts-cli" {
		t.Errorf("binary = %q, want %q", td.Meta.Binary, "xts-cli")
	}

	// Validate has all 3 actions
	for _, action := range []string{"query", "view", "activity"} {
		if _, ok := td.Actions[action]; !ok {
			t.Errorf("missing action %q", action)
		}
	}

	// Query action has required args
	q := td.Actions["query"]
	for _, arg := range []string{"query_type", "environment", "query"} {
		if _, ok := q.Args[arg]; !ok {
			t.Errorf("query action missing arg %q", arg)
		}
	}

	// Validate the builtin def passes validation
	errs := schema.ValidateToolDefinition(td)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("unexpected validation error: %v", e)
		}
	}
}

func TestDesugarXTSStep_Query(t *testing.T) {
	step := &schema.Step{
		ID:   "test_query",
		Type: "xts",
		XTS: &schema.XTSStepConfig{
			Mode:      "query",
			QueryType: "kusto",
			Query:     "MonLogin | take 1",
		},
	}

	info := DesugarXTSStep(step, "ProdEnv1")
	if info == nil {
		t.Fatal("expected non-nil tool info")
	}
	if info["name"] != "xts" {
		t.Errorf("name = %v, want 'xts'", info["name"])
	}
	if info["action"] != "query" {
		t.Errorf("action = %v, want 'query'", info["action"])
	}
	args := info["args"].(map[string]string)
	if args["query_type"] != "kusto" {
		t.Errorf("query_type = %q, want 'kusto'", args["query_type"])
	}
	if args["query"] != "MonLogin | take 1" {
		t.Errorf("query = %q", args["query"])
	}
	if args["environment"] != "ProdEnv1" {
		t.Errorf("environment = %q, want 'ProdEnv1'", args["environment"])
	}
}

func TestDesugarXTSStep_View(t *testing.T) {
	step := &schema.Step{
		ID:   "test_view",
		Type: "xts",
		XTS: &schema.XTSStepConfig{
			Mode:        "view",
			File:        "sterling/servers.xts",
			Environment: "StepEnv",
			Params:      map[string]string{"search": "abc"},
		},
	}

	info := DesugarXTSStep(step, "DefaultEnv")
	if info == nil {
		t.Fatal("expected non-nil tool info")
	}
	if info["action"] != "view" {
		t.Errorf("action = %v, want 'view'", info["action"])
	}
	args := info["args"].(map[string]string)
	// Step-level env overrides default
	if args["environment"] != "StepEnv" {
		t.Errorf("environment = %q, want 'StepEnv'", args["environment"])
	}
	if args["file"] != "sterling/servers.xts" {
		t.Errorf("file = %q", args["file"])
	}
	if args["param_search"] != "abc" {
		t.Errorf("param_search = %q, want 'abc'", args["param_search"])
	}
}

func TestDesugarXTSStep_Activity(t *testing.T) {
	step := &schema.Step{
		ID:   "test_activity",
		Type: "xts",
		XTS: &schema.XTSStepConfig{
			Mode:     "activity",
			File:     "sterling/actions.xts",
			Activity: "restart-node",
		},
	}

	info := DesugarXTSStep(step, "ProdEnv")
	if info == nil {
		t.Fatal("expected non-nil tool info")
	}
	if info["action"] != "activity" {
		t.Errorf("action = %v, want 'activity'", info["action"])
	}
	args := info["args"].(map[string]string)
	if args["activity"] != "restart-node" {
		t.Errorf("activity = %q", args["activity"])
	}
}

func TestDesugarXTSStep_NilXTS(t *testing.T) {
	step := &schema.Step{ID: "s1", Type: "cli"}
	info := DesugarXTSStep(step, "Env")
	if info != nil {
		t.Errorf("expected nil for non-XTS step, got %v", info)
	}
}
