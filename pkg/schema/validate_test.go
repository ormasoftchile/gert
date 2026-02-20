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

// --- XTS domain validation tests ---

// TestValidateXTSMissingMeta ensures meta.xts is required when xts steps exist.
func TestValidateXTSMissingMeta(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "xts-no-meta"},
		Steps: []Step{
			{ID: "s1", Type: "xts", XTS: &XTSStepConfig{Mode: "query", QueryType: "kusto", Query: "test"}},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "meta.xts is required") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected meta.xts required error, got: %v", errs)
	}
}

// TestValidateXTSMissingConfig ensures xts step requires xts config.
func TestValidateXTSMissingConfig(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "xts-no-config", XTS: &XTSMeta{Environment: "prod"}},
		Steps: []Step{
			{ID: "s1", Type: "xts"},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "requires 'xts' configuration") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected xts config required error, got: %v", errs)
	}
}

// TestValidateXTSActivityIncomplete checks activity mode without required fields.
func TestValidateXTSActivityIncomplete(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "xts-bad-activity", XTS: &XTSMeta{Environment: "prod"}},
		Steps: []Step{
			{ID: "s1", Type: "xts", XTS: &XTSStepConfig{Mode: "activity"}},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) < 2 {
		t.Fatalf("expected at least 2 errors (file+activity), got %d: %v", len(errs), errs)
	}
	var hasFile, hasActivity bool
	for _, e := range errs {
		if strings.Contains(e.Message, "requires 'file'") {
			hasFile = true
		}
		if strings.Contains(e.Message, "requires 'activity'") {
			hasActivity = true
		}
	}
	if !hasFile || !hasActivity {
		t.Errorf("expected file and activity errors, got: %v", errs)
	}
}

// TestValidateXTSQueryIncomplete checks query mode without required fields.
func TestValidateXTSQueryIncomplete(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "xts-bad-query", XTS: &XTSMeta{Environment: "prod"}},
		Steps: []Step{
			{ID: "s1", Type: "xts", XTS: &XTSStepConfig{Mode: "query"}},
		},
	}
	errs := ValidateDomain(rb)
	if len(errs) < 2 {
		t.Fatalf("expected at least 2 errors (query_type+query), got %d: %v", len(errs), errs)
	}
}

// TestValidateXTSViewIncomplete checks view mode without required file.
func TestValidateXTSViewIncomplete(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "xts-bad-view", XTS: &XTSMeta{Environment: "prod"}},
		Steps: []Step{
			{ID: "s1", Type: "xts", XTS: &XTSStepConfig{Mode: "view"}},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "requires 'file'") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected file required error, got: %v", errs)
	}
}

// TestValidateXTSParamTemplateRef checks undefined vars in xts params are caught.
func TestValidateXTSParamTemplateRef(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta:       Meta{Name: "xts-undef-param", XTS: &XTSMeta{Environment: "prod"}},
		Steps: []Step{
			{
				ID:   "s1",
				Type: "xts",
				XTS: &XTSStepConfig{
					Mode:     "activity",
					File:     "test.xts",
					Activity: "test",
					Params:   map[string]string{"search": "{{ .undefined_var }}"},
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	found := false
	for _, e := range errs {
		if strings.Contains(e.Message, "undefined_var") {
			found = true
		}
	}
	if !found {
		t.Errorf("expected undefined var error in xts params, got: %v", errs)
	}
}

// TestValidateXTSValid ensures a well-formed XTS runbook passes.
func TestValidateXTSValid(t *testing.T) {
	rb := &Runbook{
		APIVersion: "runbook/v0",
		Meta: Meta{
			Name: "xts-valid",
			Vars: map[string]string{"server": "test"},
			XTS:  &XTSMeta{Environment: "ProdAuce1a", ViewsRoot: "C:\\views"},
		},
		Steps: []Step{
			{
				ID:   "s1",
				Type: "xts",
				XTS: &XTSStepConfig{
					Mode:     "activity",
					File:     "sterling/test.xts",
					Activity: "Servers",
					Params:   map[string]string{"search": "{{ .server }}"},
				},
				Capture: map[string]string{"ring": "$.data[0].tenant_ring_name"},
			},
			{
				ID:   "s2",
				Type: "xts",
				XTS: &XTSStepConfig{
					Mode:      "query",
					QueryType: "kusto",
					Query:     "Table | take 1",
				},
			},
		},
	}
	errs := ValidateDomain(rb)
	for _, e := range errs {
		if e.Severity == "error" {
			t.Errorf("expected no domain errors, got: %v", e)
		}
	}
}
