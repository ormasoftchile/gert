package governance

import (
	"testing"
)

// TestAllowlistAcceptsAllowedCommand verifies allowed commands pass.
func TestAllowlistAcceptsAllowedCommand(t *testing.T) {
	g := &GovernanceEngine{
		AllowedCommands: []string{"kubectl", "az", "curl"},
	}
	if err := g.CheckCommand("kubectl"); err != nil {
		t.Errorf("expected allowed, got: %v", err)
	}
}

// TestAllowlistRejectsUnlistedCommand verifies non-allowed commands are blocked.
func TestAllowlistRejectsUnlistedCommand(t *testing.T) {
	g := &GovernanceEngine{
		AllowedCommands: []string{"kubectl", "az"},
	}
	if err := g.CheckCommand("rm"); err == nil {
		t.Error("expected rejection for unlisted command 'rm'")
	}
}

// TestDenylistBlocksCommand verifies denied commands are blocked.
func TestDenylistBlocksCommand(t *testing.T) {
	g := &GovernanceEngine{
		DeniedCommands: []string{"rm", "dd", "mkfs"},
	}
	if err := g.CheckCommand("rm"); err == nil {
		t.Error("expected rejection for denied command 'rm'")
	}
}

// TestDenylistAllowsUnlistedCommand verifies non-denied commands pass.
func TestDenylistAllowsUnlistedCommand(t *testing.T) {
	g := &GovernanceEngine{
		DeniedCommands: []string{"rm", "dd"},
	}
	if err := g.CheckCommand("kubectl"); err != nil {
		t.Errorf("expected allowed, got: %v", err)
	}
}

// TestCombinedAllowDenyMode verifies combined mode (allow + deny).
func TestCombinedAllowDenyMode(t *testing.T) {
	g := &GovernanceEngine{
		AllowedCommands: []string{"kubectl", "az", "curl"},
		DeniedCommands:  []string{"curl"}, // curl is both allowed and denied — deny wins
	}
	// kubectl should pass (allowed, not denied)
	if err := g.CheckCommand("kubectl"); err != nil {
		t.Errorf("kubectl should pass: %v", err)
	}
	// curl should fail (denied takes precedence)
	if err := g.CheckCommand("curl"); err == nil {
		t.Error("curl should be denied (deny takes precedence)")
	}
	// rm should fail (not in allowlist)
	if err := g.CheckCommand("rm"); err == nil {
		t.Error("rm should be rejected (not in allowlist)")
	}
}

// TestNoGovernanceAllowsAll verifies that empty governance permits everything.
func TestNoGovernanceAllowsAll(t *testing.T) {
	g := &GovernanceEngine{}
	if err := g.CheckCommand("anything"); err != nil {
		t.Errorf("empty governance should allow all: %v", err)
	}
}

// TestEnvVarPatternMatching verifies denied env var patterns.
func TestEnvVarPatternMatching(t *testing.T) {
	g := &GovernanceEngine{
		DenyEnvVars: []string{"SECRET_*", "TOKEN", "AWS_*"},
	}
	tests := []struct {
		name    string
		blocked bool
	}{
		{"SECRET_KEY", true},
		{"SECRET_VALUE", true},
		{"TOKEN", true},
		{"AWS_ACCESS_KEY", true},
		{"HOME", false},
		{"PATH", false},
		{"NAMESPACE", false},
	}
	for _, tt := range tests {
		err := g.CheckEnvVar(tt.name)
		if tt.blocked && err == nil {
			t.Errorf("expected %q to be blocked", tt.name)
		}
		if !tt.blocked && err != nil {
			t.Errorf("expected %q to be allowed, got: %v", tt.name, err)
		}
	}
}
