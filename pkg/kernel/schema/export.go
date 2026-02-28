package schema

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

// GenerateRunbookJSONSchema produces a JSON Schema Draft 2020-12 document
// from the kernel/v0 Runbook Go types.
func GenerateRunbookJSONSchema() ([]byte, error) {
	r := new(jsonschema.Reflector)
	s := r.Reflect(&Runbook{})
	s.ID = "https://github.com/ormasoftchile/gert/schemas/kernel-v0.json"
	s.Title = "Governed Executable Runbook — kernel/v0"
	s.Description = "Schema for kernel/v0 runbook YAML documents (Draft 2020-12)"

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal runbook schema: %w", err)
	}
	return data, nil
}

// GenerateToolJSONSchema produces a JSON Schema Draft 2020-12 document
// from the kernel/v0 ToolDefinition Go types.
func GenerateToolJSONSchema() ([]byte, error) {
	r := new(jsonschema.Reflector)
	s := r.Reflect(&ToolDefinition{})
	s.ID = "https://github.com/ormasoftchile/gert/schemas/tool-v0.json"
	s.Title = "Tool Definition — tool/v0"
	s.Description = "Schema for tool/v0 definition YAML documents (Draft 2020-12)"

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal tool schema: %w", err)
	}
	return data, nil
}
