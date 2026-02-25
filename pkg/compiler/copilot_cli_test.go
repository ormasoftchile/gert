package compiler

import (
	"testing"
)

func TestCopilotCLIClientModelName(t *testing.T) {
	c := NewCopilotCLIClient()
	if c.ModelName() != "copilot-cli" {
		t.Errorf("ModelName() = %q, want %q", c.ModelName(), "copilot-cli")
	}
}

func TestCopilotCLIClientDefaults(t *testing.T) {
	c := NewCopilotCLIClient()
	if c.Binary != "copilot" {
		t.Errorf("Binary = %q, want %q", c.Binary, "copilot")
	}
	if c.Timeout != 5*60*1e9 { // 5 minutes in nanoseconds
		t.Errorf("Timeout = %v, want 5m", c.Timeout)
	}
}
