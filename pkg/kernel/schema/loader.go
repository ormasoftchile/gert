package schema

import (
	"fmt"
	"io"
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

// LoadFile reads and structurally decodes a kernel/v0 runbook YAML.
// Returns a structural error if the YAML contains unknown fields.
func LoadFile(path string) (*Runbook, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open runbook: %w", err)
	}
	defer f.Close()
	return Load(f)
}

// Load reads a kernel/v0 runbook from a reader.
func Load(r io.Reader) (*Runbook, error) {
	var rb Runbook
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true) // strict: reject unknown fields
	if err := dec.Decode(&rb); err != nil {
		return nil, fmt.Errorf("structural decode: %w", err)
	}
	normalizeScopes(&rb)
	return &rb, nil
}

// normalizeScopes converts `/` to `.` in all step scope paths.
func normalizeScopes(rb *Runbook) {
	walkAllSteps(rb.Steps, func(s *Step) {
		if s.Scope != "" {
			s.Scope = normalizeScopePath(s.Scope)
		}
	})
}

func normalizeScopePath(p string) string {
	return strings.ReplaceAll(p, "/", ".")
}

func walkAllSteps(steps []Step, fn func(*Step)) {
	for i := range steps {
		fn(&steps[i])
		for j := range steps[i].Branches {
			walkAllSteps(steps[i].Branches[j].Steps, fn)
		}
		if steps[i].Repeat != nil {
			walkAllSteps(steps[i].Repeat.Steps, fn)
		}
	}
}

// LoadToolFile reads and structurally decodes a tool/v0 YAML definition.
func LoadToolFile(path string) (*ToolDefinition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open tool definition: %w", err)
	}
	defer f.Close()
	return LoadTool(f)
}

// LoadTool reads a tool/v0 definition from a reader.
func LoadTool(r io.Reader) (*ToolDefinition, error) {
	var td ToolDefinition
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)
	if err := dec.Decode(&td); err != nil {
		return nil, fmt.Errorf("structural decode: %w", err)
	}
	return &td, nil
}
