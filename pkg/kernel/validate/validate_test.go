package validate

import (
	"path/filepath"
	"runtime"
	"testing"
)

func testdataPath(name string) string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "testdata", name)
}

func TestValidateFile_Valid(t *testing.T) {
	rb, errs := ValidateFile(testdataPath("valid.yaml"))
	errors := filterErrors(errs)
	if len(errors) > 0 {
		for _, e := range errors {
			t.Errorf("unexpected error: %s", e)
		}
	}
	if rb == nil {
		t.Fatal("expected runbook, got nil")
	}
	if rb.Meta.Name != "test-runbook" {
		t.Errorf("expected name 'test-runbook', got %q", rb.Meta.Name)
	}
	if len(rb.Steps) != 3 {
		t.Errorf("expected 3 steps, got %d", len(rb.Steps))
	}
}

func TestValidateFile_MissingFields(t *testing.T) {
	_, errs := ValidateFile(testdataPath("missing_fields.yaml"))
	errors := filterErrors(errs)
	if len(errors) == 0 {
		t.Fatal("expected errors for missing fields")
	}

	// Should catch: tool missing 'tool', manual missing 'instructions', end missing 'outcome'
	wantMessages := []string{
		"tool step requires 'tool' field",
		"manual step requires 'instructions' field",
		"end step requires 'outcome' field",
	}
	for _, want := range wantMessages {
		if !containsMessage(errors, want) {
			t.Errorf("expected error containing %q", want)
		}
	}
}

func TestValidateFile_DuplicateIDs(t *testing.T) {
	_, errs := ValidateFile(testdataPath("duplicate_ids.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "duplicate step ID") {
		t.Error("expected duplicate step ID error")
	}
}

func TestValidateFile_BadNext(t *testing.T) {
	_, errs := ValidateFile(testdataPath("bad_next.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "not found in current scope") {
		t.Error("expected scope-local next error")
	}
}

func TestValidateFile_BackwardNoMax(t *testing.T) {
	_, errs := ValidateFile(testdataPath("backward_no_max.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "requires a 'max' bound") {
		t.Error("expected backward jump max bound error")
	}
}

func TestValidateFile_NoEnd(t *testing.T) {
	_, errs := ValidateFile(testdataPath("no_end.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "end step") {
		t.Error("expected end-step reachability error")
	}
}

func TestValidateFile_ConstantShadow(t *testing.T) {
	_, errs := ValidateFile(testdataPath("constant_shadow.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "shadows a constant") {
		t.Error("expected constant shadow error")
	}
}

func TestValidateFile_UnresolvedVar(t *testing.T) {
	_, errs := ValidateFile(testdataPath("unresolved_var.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "does not resolve") {
		t.Error("expected unresolved variable error")
	}
}

func TestValidateFile_UndeclaredTool(t *testing.T) {
	_, errs := ValidateFile(testdataPath("undeclared_tool.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "not declared in the runbook tools list") {
		t.Error("expected undeclared tool error")
	}
}

func TestValidateFile_BadOutcome(t *testing.T) {
	_, errs := ValidateFile(testdataPath("bad_outcome.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "invalid outcome category") {
		t.Error("expected invalid outcome category error")
	}
}

func TestValidateFile_NotFound(t *testing.T) {
	_, errs := ValidateFile(testdataPath("nonexistent.yaml"))
	if len(errs) == 0 {
		t.Fatal("expected error for nonexistent file")
	}
	if errs[0].Phase != "structural" {
		t.Errorf("expected structural error, got %q", errs[0].Phase)
	}
}

// --- helpers ---

func filterErrors(errs []*ValidationError) []*ValidationError {
	var out []*ValidationError
	for _, e := range errs {
		if e.Severity == "error" {
			out = append(out, e)
		}
	}
	return out
}

func containsMessage(errs []*ValidationError, substr string) bool {
	for _, e := range errs {
		if contains(e.Message, substr) {
			return true
		}
	}
	return false
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && searchString(s, substr)
}

func searchString(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

// T015: Effects/side_effects validation
func TestValidateToolFile_EffectsValid(t *testing.T) {
	td, errs := ValidateToolFile(testdataPath("effects_valid.yaml"))
	errors := filterErrors(errs)
	if len(errors) > 0 {
		for _, e := range errors {
			t.Errorf("unexpected error: %s", e)
		}
	}
	if td == nil {
		t.Fatal("expected tool definition")
	}
	if len(td.Contract.Effects) != 1 || td.Contract.Effects[0] != "network" {
		t.Errorf("effects = %v, want [network]", td.Contract.Effects)
	}
}

func TestValidateToolFile_EffectsConflict(t *testing.T) {
	_, errs := ValidateToolFile(testdataPath("effects_conflict.yaml"))
	errors := filterErrors(errs)
	if !containsMessage(errors, "cannot declare both") {
		t.Error("expected error for effects + side_effects conflict")
	}
}

func TestValidateToolFile_SideEffectsDeprecated(t *testing.T) {
	_, errs := ValidateToolFile(testdataPath("side_effects_deprecated.yaml"))
	// Should not error, but should warn
	errors := filterErrors(errs)
	if len(errors) > 0 {
		t.Errorf("unexpected error for deprecated side_effects: %v", errors)
	}
	// Check for deprecation warning
	warnings := filterWarnings(errs)
	if !containsMessage(warnings, "deprecated") {
		t.Error("expected deprecation warning for side_effects")
	}
}

func filterWarnings(errs []*ValidationError) []*ValidationError {
	var out []*ValidationError
	for _, e := range errs {
		if e.Severity == "warning" {
			out = append(out, e)
		}
	}
	return out
}
