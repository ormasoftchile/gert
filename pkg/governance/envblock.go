package governance

import (
	"fmt"
	"os"
	"strings"
)

// FilterEnvVars returns environment variables with denied patterns removed.
// Uses the GovernanceEngine's DenyEnvVars patterns.
func (g *GovernanceEngine) FilterEnvVars(env []string) ([]string, []string) {
	if len(g.DenyEnvVars) == 0 {
		return env, nil
	}
	var filtered []string
	var blocked []string
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		name := parts[0]
		if err := g.CheckEnvVar(name); err != nil {
			blocked = append(blocked, name)
			continue
		}
		filtered = append(filtered, e)
	}
	return filtered, blocked
}

// ResolveTemplateVars resolves template variables, blocking denied env vars.
// Returns an error if a template references a blocked environment variable.
func (g *GovernanceEngine) CheckVarResolution(varName string) error {
	// Check if this var name matches any denied env var pattern
	envVal := os.Getenv(varName)
	_ = envVal // We don't care about the value, just the name
	if err := g.CheckEnvVar(varName); err != nil {
		return fmt.Errorf("variable %q references blocked environment variable: %w", varName, err)
	}
	return nil
}
