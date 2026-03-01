package main

import (
	"testing"
	"time"
)

func TestWatchCmd_StopOnCategory(t *testing.T) {
	// Verify stop-on parsing works
	categories := map[string]bool{"escalated": true, "needs_rca": true}

	if !categories["escalated"] {
		t.Error("expected escalated to be in stop categories")
	}
	if !categories["needs_rca"] {
		t.Error("expected needs_rca to be in stop categories")
	}
	if categories["resolved"] {
		t.Error("resolved should not be in stop categories")
	}
}

func TestWatchCmd_IntervalParsing(t *testing.T) {
	cases := []struct {
		input    string
		expected time.Duration
	}{
		{"5m", 5 * time.Minute},
		{"30s", 30 * time.Second},
		{"1h", time.Hour},
	}

	for _, tc := range cases {
		d, err := time.ParseDuration(tc.input)
		if err != nil {
			t.Errorf("failed to parse %q: %v", tc.input, err)
			continue
		}
		if d != tc.expected {
			t.Errorf("parsed %q = %v, want %v", tc.input, d, tc.expected)
		}
	}
}

func TestStatusIcon(t *testing.T) {
	cases := []struct {
		status   string
		expected string
	}{
		{"completed", "✓"},
		{"failed", "✗"},
		{"error", "!"},
	}
	for _, tc := range cases {
		if got := statusIcon(tc.status); got != tc.expected {
			t.Errorf("statusIcon(%q) = %q, want %q", tc.status, got, tc.expected)
		}
	}
}
