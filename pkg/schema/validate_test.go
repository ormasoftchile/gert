package schema

import (
	"strings"
	"testing"
)

// TestValidateStepIDUniqueness checks that duplicate step IDs are rejected.
func TestValidateStepIDUniqueness(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "dup-ids"},
		Steps: []Step{
			{ID: "step_a", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}},
			{ID: "step_a", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for duplicate step IDs")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "duplicate") && strings.Contains(e.Error(), "step_a") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected duplicate step ID error, got: %v", errs)
	}
}

// TestValidateUndefinedVariableReferences checks for {{ .var }} referencing undefined vars.
func TestValidateUndefinedVariableReferences(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "undef-vars", Vars: map[string]string{"namespace": "prod"}},
		Steps: []Step{
			{
				ID:   "s1",
				Type: "cli",
				With: &CLIStepConfig{Argv: []string{"kubectl", "get", "pods", "-n", "{{ .undefined_var }}"}},
			},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for undefined variable reference")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "undefined_var") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected undefined var error, got: %v", errs)
	}
}

// TestValidateGovernanceConsistency ensures allowed+denied don't overlap.
func TestValidateGovernanceConsistency(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta: Meta{
			Name: "gov-overlap",
			Governance: &GovernancePolicy{
				AllowedCommands: []string{"kubectl", "az"},
				DeniedCommands:  []string{"kubectl"},
			},
		},
		Steps: []Step{
			{ID: "s1", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for overlapping allowed/denied commands")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "kubectl") && strings.Contains(e.Error(), "overlap") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected overlap error for kubectl, got: %v", errs)
	}
}

// TestValidateInvalidRegexPatterns checks redaction patterns are valid regex.
func TestValidateInvalidRegexPatterns(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta: Meta{
			Name: "bad-regex",
			Governance: &GovernancePolicy{
				Redact: []RedactionRule{
					{Pattern: "[invalid(regex", Replace: "<redacted>"},
				},
			},
		},
		Steps: []Step{
			{ID: "s1", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for invalid regex pattern")
	}
	found := false
	for _, e := range errs {
		if strings.Contains(e.Error(), "regex") || strings.Contains(e.Error(), "pattern") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected regex error, got: %v", errs)
	}
}

// TestValidateEmptyArgv ensures CLI steps with empty argv are rejected.
func TestValidateEmptyArgv(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "empty-argv"},
		Steps: []Step{
			{ID: "s1", Type: "cli", With: &CLIStepConfig{Argv: []string{}}},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for empty argv")
	}
}

// TestValidateCLIMissingWith ensures CLI steps without 'with' are rejected.
func TestValidateCLIMissingWith(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "missing-with"},
		Steps: []Step{
			{ID: "s1", Type: "cli"},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for CLI step missing 'with'")
	}
}

// TestValidateManualMissingInstructions ensures manual steps without instructions are rejected.
func TestValidateManualMissingInstructions(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "missing-instructions"},
		Steps: []Step{
			{ID: "s1", Type: "manual"},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for manual step missing instructions")
	}
}

// TestValidateValidRunbook confirms a well-formed runbook passes.
func TestValidateValidRunbook(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta: Meta{
			Name: "valid-runbook",
			Vars: map[string]string{"ns": "prod"},
			Governance: &GovernancePolicy{
				AllowedCommands: []string{"kubectl"},
				Redact: []RedactionRule{
					{Pattern: "password.*", Replace: "<redacted>"},
				},
			},
		},
		Steps: []Step{
			{
				ID:   "s1",
				Type: "cli",
				With: &CLIStepConfig{Argv: []string{"kubectl", "get", "pods", "-n", "{{ .ns }}"}},
			},
			{
				ID:           "s2",
				Type:         "manual",
				Instructions: "Check the dashboard",
			},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) > 0 {
		t.Errorf("expected no errors, got: %v", errs)
	}
}

// TestValidateNoSteps ensures at least one step is required.
func TestValidateNoSteps(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "no-steps"},
		Steps:      []Step{},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for zero steps")
	}
}

// TestValidateAPIVersionCheck checks that apiVersion is validated.
func TestValidateAPIVersionCheck(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v999",
		Meta:       Meta{Name: "bad-version"},
		Steps: []Step{
			{ID: "s1", Type: "cli", With: &CLIStepConfig{Argv: []string{"echo"}}},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) == 0 {
		t.Fatal("expected error for unrecognized apiVersion")
	}
}
