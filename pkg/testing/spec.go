// Package testing defines the test specification schema, assertion evaluator,
// and scenario runner for runbook replay testing.
package testing

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// TestSpec defines expected outcomes for a scenario replay.
// All fields are optional — omitted fields are not asserted.
type TestSpec struct {
	ExpectedOutcome    string            `yaml:"expected_outcome,omitempty"     json:"expected_outcome,omitempty"`
	ExpectedChain      []string          `yaml:"expected_chain,omitempty"       json:"expected_chain,omitempty"`
	ExpectedCaptures   map[string]string `yaml:"expected_captures,omitempty"    json:"expected_captures,omitempty"`
	MustReach          []string          `yaml:"must_reach,omitempty"           json:"must_reach,omitempty"`
	MustNotReach       []string          `yaml:"must_not_reach,omitempty"       json:"must_not_reach,omitempty"`
	ExpectedStepStatus map[string]string `yaml:"expected_step_status,omitempty" json:"expected_step_status,omitempty"`
	Description        string            `yaml:"description,omitempty"          json:"description,omitempty"`
	Tags               []string          `yaml:"tags,omitempty"                 json:"tags,omitempty"`
}

// LoadTestSpec reads and parses a test.yaml file.
func LoadTestSpec(path string) (*TestSpec, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read test spec: %w", err)
	}
	return ParseTestSpec(data)
}

// ParseTestSpec parses a TestSpec from raw YAML bytes.
func ParseTestSpec(data []byte) (*TestSpec, error) {
	var spec TestSpec
	if err := yaml.Unmarshal(data, &spec); err != nil {
		return nil, fmt.Errorf("parse test spec: %w", err)
	}
	return &spec, nil
}

// yamlUnmarshalRaw is a package-level helper so runner.go can unmarshal YAML
// without a separate import of gopkg.in/yaml.v3.
func yamlUnmarshalRaw(data []byte, v interface{}) error {
	return yaml.Unmarshal(data, v)
}
