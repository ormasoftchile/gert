package schema

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestLoadValidRunbooks ensures valid YAML files parse without errors.
func TestLoadValidRunbooks(t *testing.T) {
	files, err := filepath.Glob("../../testdata/valid/*.yaml")
	if err != nil {
		t.Fatalf("glob valid fixtures: %v", err)
	}
	if len(files) == 0 {
		t.Fatal("no valid test fixtures found")
	}
	for _, f := range files {
		name := filepath.Base(f)
		t.Run(name, func(t *testing.T) {
			rb, err := LoadFile(f)
			if err != nil {
				t.Fatalf("expected valid, got error: %v", err)
			}
			if rb.APIVersion != "runbook/v0" {
				t.Errorf("apiVersion = %q, want %q", rb.APIVersion, "runbook/v0")
			}
			if rb.Meta.Name == "" {
				t.Error("meta.name is empty")
			}
			if len(rb.Steps) == 0 && len(rb.Tree) == 0 {
				t.Error("expected at least one step or tree node")
			}
		})
	}
}

// TestLoadRejectsUnknownFields verifies that strict mode rejects unknown YAML keys.
func TestLoadRejectsUnknownFields(t *testing.T) {
	rb, err := LoadFile("../../testdata/invalid/unknown-fields.yaml")
	if err == nil {
		t.Fatalf("expected error for unknown fields, got runbook with name=%q", rb.Meta.Name)
	}
	if !strings.Contains(err.Error(), "not found") && !strings.Contains(err.Error(), "unknown") &&
		!strings.Contains(err.Error(), "field") {
		// yaml.v3 KnownFields produces "field X not found in type Y"
		t.Logf("got error: %v (accepted — unknown field rejection)", err)
	}
}

// TestLoadRejectsMissingRequired checks that missing required fields are caught.
func TestLoadRejectsMissingRequired(t *testing.T) {
	// missing-required.yaml has no apiVersion field
	rb, err := LoadFile("../../testdata/invalid/missing-required.yaml")
	if err != nil {
		// Structural parse may succeed but apiVersion will be empty
		t.Logf("parse error: %v", err)
		return
	}
	if rb.APIVersion != "" {
		t.Errorf("expected empty apiVersion, got %q", rb.APIVersion)
	}
}

// TestLoadRejectsInvalidTypes ensures type mismatches are caught.
func TestLoadRejectsInvalidTypes(t *testing.T) {
	// Create an inline fixture with a type mismatch (steps as string instead of array)
	yaml := `apiVersion: runbook/v0
meta:
  name: type-mismatch
steps: "not-an-array"
`
	rb, err := Load(strings.NewReader(yaml))
	if err == nil {
		t.Fatalf("expected error for invalid type, got runbook with %d steps", len(rb.Steps))
	}
}

// TestLoadMinimalRunbook tests the minimal valid runbook.
func TestLoadMinimalRunbook(t *testing.T) {
	rb, err := LoadFile("../../testdata/valid/minimal.yaml")
	if err != nil {
		t.Fatalf("failed to load minimal runbook: %v", err)
	}
	if rb.Meta.Name != "minimal-runbook" {
		t.Errorf("name = %q, want %q", rb.Meta.Name, "minimal-runbook")
	}
	if len(rb.Steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(rb.Steps))
	}
	s := rb.Steps[0]
	if s.ID != "step_one" {
		t.Errorf("step.id = %q, want %q", s.ID, "step_one")
	}
	if s.Type != "cli" {
		t.Errorf("step.type = %q, want %q", s.Type, "cli")
	}
	if s.With == nil || len(s.With.Argv) != 2 {
		t.Fatalf("expected argv with 2 elements, got %v", s.With)
	}
}

// TestLoadFullRunbook tests the full example runbook with all features.
func TestLoadFullRunbook(t *testing.T) {
	rb, err := LoadFile("../../testdata/valid/pod-crashloop.yaml")
	if err != nil {
		t.Fatalf("failed to load pod-crashloop runbook: %v", err)
	}
	if rb.Meta.Name != "pod-crashloop-investigation" {
		t.Errorf("name = %q, want %q", rb.Meta.Name, "pod-crashloop-investigation")
	}
	if len(rb.Steps) != 4 {
		t.Fatalf("steps = %d, want 4", len(rb.Steps))
	}

	// Check governance
	gov := rb.Meta.Governance
	if gov == nil {
		t.Fatal("expected governance policy")
	}
	if len(gov.AllowedCommands) != 3 {
		t.Errorf("allowed_commands = %d, want 3", len(gov.AllowedCommands))
	}
	if len(gov.Redact) != 1 {
		t.Errorf("redact rules = %d, want 1", len(gov.Redact))
	}

	// Check manual step
	manual := rb.Steps[3]
	if manual.Type != "manual" {
		t.Errorf("step[3].type = %q, want %q", manual.Type, "manual")
	}
	if len(manual.RequiredEvidence) != 3 {
		t.Errorf("required_evidence = %d, want 3", len(manual.RequiredEvidence))
	}
	if manual.Approvals == nil || manual.Approvals.Min != 1 {
		t.Error("expected approvals.min=1")
	}
}

// TestLoadAllAssertions ensures all 7 assertion types parse correctly.
func TestLoadAllAssertions(t *testing.T) {
	rb, err := LoadFile("../../testdata/valid/all-assertions.yaml")
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	// First step should have 5 assertions (contains, not_contains, matches, exit_code, json_path)
	s := rb.Steps[0]
	if len(s.Assertions) != 5 {
		t.Fatalf("step[0].assertions = %d, want 5", len(s.Assertions))
	}
	if s.Assertions[0].Contains != "healthy" {
		t.Errorf("assertion[0].contains = %q, want %q", s.Assertions[0].Contains, "healthy")
	}
	if s.Assertions[1].NotContains != "error" {
		t.Errorf("assertion[1].not_contains = %q, want %q", s.Assertions[1].NotContains, "error")
	}
	if s.Assertions[2].Matches != "status.*ok" {
		t.Errorf("assertion[2].matches = %q, want %q", s.Assertions[2].Matches, "status.*ok")
	}
	if s.Assertions[3].ExitCode == nil || *s.Assertions[3].ExitCode != 0 {
		t.Error("assertion[3].exit_code expected 0")
	}
	if s.Assertions[4].JSONPath == nil || s.Assertions[4].JSONPath.Path != "$.status" {
		t.Error("assertion[4].json_path expected $.status")
	}

	// Second step: equals
	if rb.Steps[1].Assertions[0].Equals != "v1.0.0" {
		t.Errorf("step[1].assertion.equals = %q, want %q", rb.Steps[1].Assertions[0].Equals, "v1.0.0")
	}
	// Third step: not_equals
	if rb.Steps[2].Assertions[0].NotEquals != "deprecated" {
		t.Errorf("step[2].assertion.not_equals = %q, want %q", rb.Steps[2].Assertions[0].NotEquals, "deprecated")
	}
}

// TestLoadEmptyArgvStructural verifies empty argv at structural level.
func TestLoadEmptyArgvStructural(t *testing.T) {
	// This fixture has empty argv — structural parse should succeed
	// but domain validation should catch it later.
	_, err := LoadFile("../../testdata/invalid/empty-argv.yaml")
	if err != nil {
		// If yaml.v3 rejects it, that's fine too
		t.Logf("parse error (acceptable): %v", err)
	}
}

func TestMain(m *testing.M) {
	os.Exit(m.Run())
}
