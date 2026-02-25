// Package governance implements command allowlist/denylist, output redaction,
// and environment variable blocking.
package governance

import (
	"fmt"
	"path/filepath"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// GovernanceEngine evaluates governance policies before and during execution.
type GovernanceEngine struct {
	AllowedCommands []string
	DeniedCommands  []string
	DenyEnvVars     []string
}

// NewGovernanceEngine creates a GovernanceEngine from a GovernancePolicy.
// If policy is nil, returns a permissive engine.
func NewGovernanceEngine(policy *schema.GovernancePolicy) *GovernanceEngine {
	if policy == nil {
		return &GovernanceEngine{}
	}
	return &GovernanceEngine{
		AllowedCommands: policy.AllowedCommands,
		DeniedCommands:  policy.DeniedCommands,
		DenyEnvVars:     policy.DenyEnvVars,
	}
}

// CheckCommand validates argv[0] against the allowlist/denylist.
// Deny takes precedence over allow.
func (g *GovernanceEngine) CheckCommand(command string) error {
	// Check denylist first (deny takes precedence)
	for _, denied := range g.DeniedCommands {
		if command == denied {
			return fmt.Errorf("command %q is denied by governance policy", command)
		}
	}

	// If allowlist is set, command must be in it
	if len(g.AllowedCommands) > 0 {
		for _, allowed := range g.AllowedCommands {
			if command == allowed {
				return nil
			}
		}
		return fmt.Errorf("command %q is not in the governance allowlist", command)
	}

	return nil
}

// CheckEnvVar validates an environment variable name against deny_env_vars patterns.
func (g *GovernanceEngine) CheckEnvVar(name string) error {
	for _, pattern := range g.DenyEnvVars {
		matched, err := filepath.Match(pattern, name)
		if err != nil {
			// Invalid pattern — treat as blocking for safety
			return fmt.Errorf("invalid env var deny pattern %q: %w", pattern, err)
		}
		if matched {
			return fmt.Errorf("environment variable %q matches denied pattern %q", name, pattern)
		}
	}
	return nil
}
