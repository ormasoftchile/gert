package main

import "testing"

// T133a: gert diff detects outcome changes
func TestExtractField(t *testing.T) {
	line := `{"type":"outcome_resolved","data":{"structured_outcome":{"category":"no_action","code":"done"}}}`

	cat := extractField(line, "category")
	if cat != "no_action" {
		t.Errorf("category = %q, want no_action", cat)
	}

	code := extractField(line, "code")
	if code != "done" {
		t.Errorf("code = %q, want done", code)
	}
}

// T134a: gert outcomes aggregates correctly
func TestSplitLines(t *testing.T) {
	data := []byte("line1\nline2\nline3")
	lines := splitLines(data)
	if len(lines) != 3 {
		t.Errorf("expected 3 lines, got %d", len(lines))
	}
	if lines[0] != "line1" || lines[2] != "line3" {
		t.Errorf("lines = %v", lines)
	}
}

func TestContains(t *testing.T) {
	if !contains("hello world", "world") {
		t.Error("expected true")
	}
	if contains("hello", "world") {
		t.Error("expected false")
	}
}
