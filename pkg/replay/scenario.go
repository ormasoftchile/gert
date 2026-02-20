// Package replay implements the ReplayExecutor for deterministic offline
// runbook execution using pre-recorded command responses and evidence.
package replay

import (
	"fmt"
	"os"

	"github.com/ormasoftchile/gert/pkg/providers"
	"gopkg.in/yaml.v3"
)

// Scenario represents a replay scenario file containing pre-recorded
// CLI command responses and evidence for manual steps.
type Scenario struct {
	Commands []ScenarioCommand                              `yaml:"commands"`
	Evidence map[string]map[string]*providers.EvidenceValue `yaml:"evidence"` // step_id → evidence_name → value
}

// ScenarioCommand is a pre-recorded command with its expected output.
type ScenarioCommand struct {
	Argv     []string `yaml:"argv"`
	Stdout   string   `yaml:"stdout"`
	Stderr   string   `yaml:"stderr"`
	ExitCode int      `yaml:"exit_code"`
}

// LoadScenario reads and parses a scenario YAML file.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario file: %w", err)
	}
	return ParseScenario(data)
}

// ParseScenario parses scenario YAML bytes.
func ParseScenario(data []byte) (*Scenario, error) {
	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse scenario: %w", err)
	}
	if len(s.Commands) == 0 && len(s.Evidence) == 0 {
		return nil, fmt.Errorf("scenario must have at least one command or evidence entry")
	}
	return &s, nil
}
