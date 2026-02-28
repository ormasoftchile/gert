// Package replay provides scenario-based replay execution for kernel/v0.
// A scenario contains canned tool responses and evidence, enabling
// deterministic re-execution of runbooks without live infrastructure.
package replay

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ormasoftchile/gert/pkg/kernel/executor"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
	"gopkg.in/yaml.v3"
)

// Scenario is the top-level replay scenario document.
type Scenario struct {
	// Inputs are variable values to seed the runbook with.
	Inputs map[string]string `yaml:"inputs,omitempty" json:"inputs,omitempty"`

	// ToolResponses maps "tool:action" keys to canned responses.
	ToolResponses map[string][]ToolResponse `yaml:"tool_responses,omitempty" json:"tool_responses,omitempty"`

	// Evidence maps step_id → evidence_name → value for manual steps.
	Evidence map[string]map[string]string `yaml:"evidence,omitempty" json:"evidence,omitempty"`
}

// ToolResponse is a single canned response for a tool action.
type ToolResponse struct {
	ExitCode int            `yaml:"exit_code" json:"exit_code"`
	Stdout   string         `yaml:"stdout,omitempty" json:"stdout,omitempty"`
	Stderr   string         `yaml:"stderr,omitempty" json:"stderr,omitempty"`
	Outputs  map[string]any `yaml:"outputs,omitempty" json:"outputs,omitempty"`
}

// LoadScenario loads a scenario from a YAML file.
func LoadScenario(path string) (*Scenario, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read scenario: %w", err)
	}
	return ParseScenario(data)
}

// ParseScenario parses scenario YAML.
func ParseScenario(data []byte) (*Scenario, error) {
	var s Scenario
	if err := yaml.Unmarshal(data, &s); err != nil {
		return nil, fmt.Errorf("parse scenario: %w", err)
	}
	return &s, nil
}

// LoadScenarioDir loads a scenario from a directory containing scenario.yaml.
func LoadScenarioDir(dir string) (*Scenario, error) {
	return LoadScenario(filepath.Join(dir, "scenario.yaml"))
}

// ReplayExecutor implements engine.ToolExecutor using canned scenario responses.
// It consumes responses in order (first-match, first-consumed).
type ReplayExecutor struct {
	scenario *Scenario
	consumed map[string]int // tracks next response index per tool:action key
}

// NewReplayExecutor creates a replay executor from a scenario.
func NewReplayExecutor(s *Scenario) *ReplayExecutor {
	return &ReplayExecutor{
		scenario: s,
		consumed: make(map[string]int),
	}
}

// Execute returns the next canned response for the given tool and action.
// Implements engine.ToolExecutor.
func (r *ReplayExecutor) Execute(td *schema.ToolDefinition, actionName string, inputs map[string]any, vars map[string]any) (*executor.Result, error) {
	key := td.Meta.Name + ":" + actionName
	responses, ok := r.scenario.ToolResponses[key]
	if !ok {
		// Try tool name only (no action)
		key = td.Meta.Name
		responses, ok = r.scenario.ToolResponses[key]
		if !ok {
			return nil, fmt.Errorf("replay: no canned response for %s:%s", td.Meta.Name, actionName)
		}
	}

	idx := r.consumed[key]
	if idx >= len(responses) {
		return nil, fmt.Errorf("replay: exhausted canned responses for %s (used %d)", key, len(responses))
	}

	resp := responses[idx]
	r.consumed[key] = idx + 1

	result := &executor.Result{
		ExitCode: resp.ExitCode,
		Stdout:   resp.Stdout,
		Stderr:   resp.Stderr,
		Outputs:  make(map[string]any),
	}

	// Use pre-computed outputs if provided, otherwise attempt extract
	if resp.Outputs != nil {
		for k, v := range resp.Outputs {
			result.Outputs[k] = v
		}
	}

	return result, nil
}

// EvidenceForStep returns canned evidence for a manual step, or nil.
func (r *ReplayExecutor) EvidenceForStep(stepID string) map[string]string {
	if r.scenario.Evidence == nil {
		return nil
	}
	return r.scenario.Evidence[stepID]
}
