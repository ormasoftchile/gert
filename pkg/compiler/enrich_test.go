package compiler

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/ormasoftchile/gert/pkg/schema"
	"gopkg.in/yaml.v3"
)

func TestEnrichFromScenarios(t *testing.T) {
	// Set up a minimal runbook with a dispatch point
	root := t.TempDir()
	rbDir := filepath.Join(root, "TSG")
	os.MkdirAll(rbDir, 0755)

	rb := schema.Runbook{
		APIVersion: "runbook/v0",
		Meta: schema.Meta{
			Name: "test-runbook",
		},
		Tree: []schema.TreeNode{
			{
				Step: schema.Step{
					ID:    "dispatch",
					Type:  "manual",
					Title: "Dispatch by cause",
				},
				Branches: []schema.Branch{
					{Condition: `cause == "KnownCauseA"`, Label: "KnownCauseA",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "a", Type: "manual", Title: "A"}}}},
					{Condition: `cause == "KnownCauseB"`, Label: "KnownCauseB",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "b", Type: "manual", Title: "B"}}}},
					{Condition: `cause == "KnownCauseC"`, Label: "KnownCauseC",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "c", Type: "manual", Title: "C"}}}},
				},
			},
		},
	}
	data, _ := yaml.Marshal(rb)
	rbPath := filepath.Join(rbDir, "test-runbook.runbook.yaml")
	os.WriteFile(rbPath, data, 0644)

	// Create scenarios directory with known + unknown causes
	scenDir := filepath.Join(rbDir, "scenarios", "test-runbook")
	for _, sc := range []struct {
		name  string
		cause string
	}{
		{"scenario-001", "KnownCauseA"},  // covered
		{"scenario-002", "NewCauseX"},    // not covered
		{"scenario-003", "NewCauseY"},    // not covered
		{"scenario-004", "KnownCauseB"}, // covered
	} {
		dir := filepath.Join(scenDir, sc.name)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "inputs.yaml"),
			[]byte("cause: \""+sc.cause+"\"\n"), 0644)
	}

	result, err := EnrichFromScenarios(rbPath)
	if err != nil {
		t.Fatalf("EnrichFromScenarios: %v", err)
	}

	if result.DispatchVar != "cause" {
		t.Errorf("DispatchVar=%q want %q", result.DispatchVar, "cause")
	}
	if result.DispatchStepID != "dispatch" {
		t.Errorf("DispatchStepID=%q want %q", result.DispatchStepID, "dispatch")
	}
	if len(result.ExistingValues) != 3 {
		t.Errorf("ExistingValues=%d want 3", len(result.ExistingValues))
	}
	if len(result.AddedValues) != 2 {
		t.Errorf("AddedValues=%d want 2", len(result.AddedValues))
	}
	if result.AddedValues[0] != "NewCauseX" || result.AddedValues[1] != "NewCauseY" {
		t.Errorf("AddedValues=%v want [NewCauseX, NewCauseY]", result.AddedValues)
	}

	// Verify runbook was written with new branches
	updated, _ := os.ReadFile(rbPath)
	if !strings.Contains(string(updated), "NewCauseX") {
		t.Error("enriched runbook should contain NewCauseX branch")
	}
	if !strings.Contains(string(updated), "NewCauseY") {
		t.Error("enriched runbook should contain NewCauseY branch")
	}
}

func TestEnrichIdempotent(t *testing.T) {
	root := t.TempDir()
	rbDir := filepath.Join(root, "TSG")
	os.MkdirAll(rbDir, 0755)

	rb := schema.Runbook{
		APIVersion: "runbook/v0",
		Meta: schema.Meta{
			Name: "idempotent-test",
		},
		Tree: []schema.TreeNode{
			{
				Step: schema.Step{ID: "dispatch", Type: "manual", Title: "Dispatch"},
				Branches: []schema.Branch{
					{Condition: `cause == "A"`, Label: "A",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "a", Type: "manual", Title: "A"}}}},
					{Condition: `cause == "B"`, Label: "B",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "b", Type: "manual", Title: "B"}}}},
					{Condition: `cause == "C"`, Label: "C",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "c", Type: "manual", Title: "C"}}}},
				},
			},
		},
	}
	data, _ := yaml.Marshal(rb)
	rbPath := filepath.Join(rbDir, "idempotent-test.runbook.yaml")
	os.WriteFile(rbPath, data, 0644)

	scenDir := filepath.Join(rbDir, "scenarios", "idempotent-test")
	os.MkdirAll(filepath.Join(scenDir, "scenario-001"), 0755)
	os.WriteFile(filepath.Join(scenDir, "scenario-001", "inputs.yaml"),
		[]byte("cause: \"NewCause\"\n"), 0644)

	// First run: adds 1 branch
	r1, err := EnrichFromScenarios(rbPath)
	if err != nil {
		t.Fatalf("first enrich: %v", err)
	}
	if len(r1.AddedValues) != 1 {
		t.Fatalf("first run: AddedValues=%d want 1", len(r1.AddedValues))
	}

	// Second run: re-adds the branch (removes old enriched, re-detects, re-adds)
	r2, err := EnrichFromScenarios(rbPath)
	if err != nil {
		t.Fatalf("second enrich: %v", err)
	}
	// Same result: 1 enriched branch added (idempotent output)
	if len(r2.AddedValues) != 1 {
		t.Errorf("second run: AddedValues=%d want 1", len(r2.AddedValues))
	}
	// Original branches still intact
	if len(r2.ExistingValues) != 3 {
		t.Errorf("second run: ExistingValues=%d want 3", len(r2.ExistingValues))
	}

	// Verify the final runbook has exactly 4 branches (3 original + 1 enriched)
	var reloaded schema.Runbook
	updated, _ := os.ReadFile(rbPath)
	yaml.Unmarshal(updated, &reloaded)
	if nBranches := len(reloaded.Tree[0].Branches); nBranches != 4 {
		t.Errorf("final branch count=%d want 4", nBranches)
	}
}

func TestEnrichStartsWithCoverage(t *testing.T) {
	root := t.TempDir()
	rbDir := filepath.Join(root, "TSG")
	os.MkdirAll(rbDir, 0755)

	rb := schema.Runbook{
		APIVersion: "runbook/v0",
		Meta: schema.Meta{
			Name: "prefix-test",
		},
		Tree: []schema.TreeNode{
			{
				Step: schema.Step{ID: "dispatch", Type: "manual", Title: "Dispatch"},
				Branches: []schema.Branch{
					{Condition: `cause startsWith "LoginErrors"`, Label: "LoginErrors",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "le", Type: "manual", Title: "Login Errors"}}}},
					{Condition: `cause == "HasDumps"`, Label: "HasDumps",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "hd", Type: "manual", Title: "HasDumps"}}}},
					{Condition: `cause == "IsHighCpu"`, Label: "IsHighCpu",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "hc", Type: "manual", Title: "IsHighCpu"}}}},
				},
			},
		},
	}
	data, _ := yaml.Marshal(rb)
	rbPath := filepath.Join(rbDir, "prefix-test.runbook.yaml")
	os.WriteFile(rbPath, data, 0644)

	scenDir := filepath.Join(rbDir, "scenarios", "prefix-test")
	for _, sc := range []struct {
		name  string
		cause string
	}{
		{"scenario-001", "LoginErrorsFound_40613_127"}, // covered by startsWith "LoginErrors"
		{"scenario-002", "LoginErrorsFound_40613_10"},   // covered by startsWith "LoginErrors"
		{"scenario-003", "HasDumps"},                     // exact match
		{"scenario-004", "IsNewThing"},                   // NOT covered
	} {
		dir := filepath.Join(scenDir, sc.name)
		os.MkdirAll(dir, 0755)
		os.WriteFile(filepath.Join(dir, "inputs.yaml"),
			[]byte("cause: \""+sc.cause+"\"\n"), 0644)
	}

	result, err := EnrichFromScenarios(rbPath)
	if err != nil {
		t.Fatalf("EnrichFromScenarios: %v", err)
	}

	// Only IsNewThing should be added; LoginErrors* are covered by startsWith
	if len(result.AddedValues) != 1 {
		t.Errorf("AddedValues=%d want 1, got %v", len(result.AddedValues), result.AddedValues)
	}
	if len(result.AddedValues) == 1 && result.AddedValues[0] != "IsNewThing" {
		t.Errorf("AddedValues[0]=%q want IsNewThing", result.AddedValues[0])
	}
}

func TestEnrichNoScenarios(t *testing.T) {
	root := t.TempDir()
	rbDir := filepath.Join(root, "TSG")
	os.MkdirAll(rbDir, 0755)

	rb := schema.Runbook{
		APIVersion: "runbook/v0",
		Meta:       schema.Meta{Name: "no-scenarios"},
		Tree: []schema.TreeNode{
			{
				Step: schema.Step{ID: "d", Type: "manual", Title: "D"},
				Branches: []schema.Branch{
					{Condition: `x == "a"`, Label: "a",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "a", Type: "manual", Title: "A"}}}},
					{Condition: `x == "b"`, Label: "b",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "b", Type: "manual", Title: "B"}}}},
					{Condition: `x == "c"`, Label: "c",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "c", Type: "manual", Title: "C"}}}},
				},
			},
		},
	}
	data, _ := yaml.Marshal(rb)
	os.WriteFile(filepath.Join(rbDir, "no-scenarios.runbook.yaml"), data, 0644)

	_, err := EnrichFromScenarios(filepath.Join(rbDir, "no-scenarios.runbook.yaml"))
	if err == nil {
		t.Fatal("expected error for missing scenarios")
	}
}

func TestToTSGStepID(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"IsCustomerBypassingGW", "tsg_is_customer_bypassing_gw"},
		{"HasSqlDump", "tsg_has_sql_dump"},
		{"UpdateSloInProgress_40613_127", "tsg_update_slo_in_progress_40613_127"},
		{"IsSuppressed", "tsg_is_suppressed"},
	}
	for _, tt := range tests {
		got := toTSGStepID(tt.input)
		if got != tt.want {
			t.Errorf("toTSGStepID(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestCauseToKebab(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"HasSqlDump", "has-sql-dump"},
		{"IsActivateDatabaseFailure", "is-activate-database-failure"},
		{"LoginErrorsFound_40613_127", "login-errors-found-40613-127"},
		{"IsDbStuckInDenyConnections", "is-db-stuck-in-deny-connections"},
		{"IsCustomerBypassingGW", "is-customer-bypassing-gw"},
		{"IsGWProxyBusy", "is-gw-proxy-busy"},
	}
	for _, tt := range tests {
		got := causeToKebab(tt.input)
		if got != tt.want {
			t.Errorf("causeToKebab(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestEnrichWithTSGFile(t *testing.T) {
	root := t.TempDir()
	// Simulate directory structure: TSG/connection/<runbook>, TSG/connection/availability-manager/<tsg>.md
	rbDir := filepath.Join(root, "TSG", "connection")
	amDir := filepath.Join(rbDir, "availability-manager")
	os.MkdirAll(amDir, 0755)

	// Create the TSG file for HasSqlDump
	os.WriteFile(filepath.Join(amDir, "has-sql-dump.md"), []byte("# HasSqlDump\nSome content."), 0644)

	rb := schema.Runbook{
		APIVersion: "runbook/v0",
		Meta:       schema.Meta{Name: "tsg-test"},
		Tree: []schema.TreeNode{
			{
				Step: schema.Step{ID: "dispatch", Type: "manual", Title: "Dispatch"},
				Branches: []schema.Branch{
					{Condition: `cause == "A"`, Label: "A",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "a", Type: "manual", Title: "A"}}}},
					{Condition: `cause == "B"`, Label: "B",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "b", Type: "manual", Title: "B"}}}},
					{Condition: `cause == "C"`, Label: "C",
						Steps: []schema.TreeNode{{Step: schema.Step{ID: "c", Type: "manual", Title: "C"}}}},
				},
			},
		},
	}
	data, _ := yaml.Marshal(rb)
	rbPath := filepath.Join(rbDir, "tsg-test.runbook.yaml")
	os.WriteFile(rbPath, data, 0644)

	scenDir := filepath.Join(rbDir, "scenarios", "tsg-test")
	// Scenario with cause that has a TSG file
	os.MkdirAll(filepath.Join(scenDir, "scenario-001"), 0755)
	os.WriteFile(filepath.Join(scenDir, "scenario-001", "inputs.yaml"),
		[]byte("cause: \"HasSqlDump\"\n"), 0644)
	// Scenario with cause that has no TSG file
	os.MkdirAll(filepath.Join(scenDir, "scenario-002"), 0755)
	os.WriteFile(filepath.Join(scenDir, "scenario-002", "inputs.yaml"),
		[]byte("cause: \"IsUnknownThing\"\n"), 0644)

	result, err := EnrichFromScenarios(rbPath)
	if err != nil {
		t.Fatalf("EnrichFromScenarios: %v", err)
	}

	if len(result.AddedValues) != 2 {
		t.Fatalf("AddedValues=%d want 2", len(result.AddedValues))
	}
	if len(result.LinkedTSGs) != 1 {
		t.Errorf("LinkedTSGs=%d want 1", len(result.LinkedTSGs))
	}
	if len(result.LinkedTSGs) > 0 && result.LinkedTSGs[0] != "HasSqlDump" {
		t.Errorf("LinkedTSGs[0]=%q want HasSqlDump", result.LinkedTSGs[0])
	}

	// Verify the enriched runbook content
	updated, _ := os.ReadFile(rbPath)
	content := string(updated)
	// HasSqlDump branch should link to TSG file
	if !strings.Contains(content, "has-sql-dump.md") {
		t.Error("enriched runbook should link to has-sql-dump.md for HasSqlDump")
	}
	// IsUnknownThing should have (enriched) label
	if !strings.Contains(content, "IsUnknownThing (enriched)") {
		t.Error("enriched runbook should have IsUnknownThing (enriched) label") 
	}
	// HasSqlDump should NOT have (enriched) label
	if strings.Contains(content, "HasSqlDump (enriched)") {
		t.Error("HasSqlDump with TSG link should NOT have (enriched) label")
	}
}

func TestExtractFollowLink(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"Follow [HasDumps](./availability-manager/has-dumps.md).", "./availability-manager/has-dumps.md"},
		{"Follow [HasSqlDump](availability-manager/has-sql-dump.md).", "availability-manager/has-sql-dump.md"},
		{"Follow [LoginErrorsFound_40613_127](login-errors/error-40613-state-127.md).", "login-errors/error-40613-state-127.md"},
		{"Follow [X](path.md)", "path.md"},
		{"No dedicated TSG found for **IsReplicaInBuild**.", ""},
		{"Some random instructions", ""},
		{"", ""},
	}
	for _, tc := range cases {
		got := extractFollowLink(tc.input)
		if got != tc.want {
			t.Errorf("extractFollowLink(%q) = %q, want %q", tc.input, got, tc.want)
		}
	}
}

func TestBindSubRunbooks(t *testing.T) {
	// Create a temp dir with a compiled sub-runbook
	root := t.TempDir()
	subDir := filepath.Join(root, "sub-tsgs")
	os.MkdirAll(subDir, 0755)

	// Create a child runbook with inputs
	childRB := schema.Runbook{
		APIVersion: "runbook/v1",
		Meta: schema.Meta{
			Name: "child-tsg",
			Inputs: map[string]*schema.InputDef{
				"server_name":   {From: "svc.fields.ServerName", Description: "Server"},
				"database_name": {From: "svc.fields.DatabaseName", Description: "DB"},
				"environment":   {From: "svc.fields.Environment", Description: "Env"},
			},
		},
		Tree: []schema.TreeNode{{Step: schema.Step{ID: "check", Type: "manual", Title: "Check"}}},
	}
	childData, _ := yaml.Marshal(childRB)
	os.WriteFile(filepath.Join(subDir, "child-tsg.runbook.yaml"), childData, 0644)
	// Also create the .md file (so path is consistent)
	os.WriteFile(filepath.Join(subDir, "child-tsg.md"), []byte("# Child TSG\n"), 0644)

	node := &schema.TreeNode{
		Step: schema.Step{ID: "dispatch", Type: "manual", Title: "Dispatch"},
		Branches: []schema.Branch{
			{
				Condition: `cause == "ChildCause"`, Label: "ChildCause",
				Steps: []schema.TreeNode{{Step: schema.Step{
					ID:           "tsg_child_cause",
					Type:         "manual",
					Title:        "Follow ChildCause TSG",
					Instructions: "Follow [ChildCause](sub-tsgs/child-tsg.md).",
				}}},
			},
			{
				Condition: `cause == "NoCause"`, Label: "NoCause",
				Steps: []schema.TreeNode{{Step: schema.Step{
					ID:           "tsg_no_cause",
					Type:         "manual",
					Title:        "NoCause",
					Instructions: "No dedicated TSG found for **NoCause**.",
				}}},
			},
		},
	}

	bound, err := bindSubRunbooks(node, root)
	if err != nil {
		t.Fatalf("bindSubRunbooks: %v", err)
	}
	if bound != 1 {
		t.Fatalf("bound=%d want 1", bound)
	}

	// Verify the first branch was converted to invoke
	step := &node.Branches[0].Steps[0].Step
	if step.Type != "invoke" {
		t.Errorf("type=%q want invoke", step.Type)
	}
	if step.Invoke == nil {
		t.Fatal("invoke config is nil")
	}
	if step.Invoke.Runbook != "sub-tsgs/child-tsg.runbook.yaml" {
		t.Errorf("invoke.runbook=%q want sub-tsgs/child-tsg.runbook.yaml", step.Invoke.Runbook)
	}
	if len(step.Invoke.Inputs) != 3 {
		t.Errorf("invoke.inputs count=%d want 3", len(step.Invoke.Inputs))
	}
	if step.Gate == nil || step.Gate.OnError != "skip" {
		t.Error("gate.on_error should be 'skip'")
	}
	if step.Instructions != "" {
		t.Error("instructions should be cleared for invoke step")
	}

	// Verify the second branch was NOT converted (no Follow link)
	step2 := &node.Branches[1].Steps[0].Step
	if step2.Type != "manual" {
		t.Errorf("second branch type=%q want manual", step2.Type)
	}
}
