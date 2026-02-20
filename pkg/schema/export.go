package schema

import (
	"encoding/json"
	"fmt"

	"github.com/invopop/jsonschema"
)

// GenerateJSONSchema produces a JSON Schema Draft 2020-12 document from
// the Go Runbook struct using invopop/jsonschema.
func GenerateJSONSchema() ([]byte, error) {
	r := new(jsonschema.Reflector)
	r.DoNotReference = false

	s := r.Reflect(&Runbook{})
	s.ID = "https://github.com/ormasoftchile/gert/schemas/runbook-v0.json"
	s.Title = "Governed Executable Runbook v0"
	s.Description = "Schema for gert runbook YAML documents (Draft 2020-12)"

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal schema: %w", err)
	}
	return data, nil
}

// GenerateToolJSONSchema produces a JSON Schema Draft 2020-12 document from
// the Go ToolDefinition struct using invopop/jsonschema.
func GenerateToolJSONSchema() ([]byte, error) {
	r := new(jsonschema.Reflector)
	r.DoNotReference = false

	s := r.Reflect(&ToolDefinition{})
	s.ID = "https://github.com/ormasoftchile/gert/schemas/tool-v0.json"
	s.Title = "Gert Tool Definition v0"
	s.Description = "Schema for gert tool definition YAML documents (.tool.yaml)"

	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal tool schema: %w", err)
	}
	return data, nil
}
