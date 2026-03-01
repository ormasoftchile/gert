package engine

import (
	"context"
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// T076: ResolveInputs resolution order
func TestResolveInputs_CLIWins(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{
			Inputs: map[string]contract.ParamDef{
				"hostname": {Type: "string", Required: true, Default: "default.example.com"},
			},
		},
	}

	result, err := ResolveInputs(context.Background(), rb, map[string]string{"hostname": "cli.example.com"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Vars["hostname"] != "cli.example.com" {
		t.Errorf("hostname = %q, want cli.example.com", result.Vars["hostname"])
	}
}

// T079: CLI var overrides provider
func TestResolveInputs_DefaultFallback(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{
			Inputs: map[string]contract.ParamDef{
				"threshold": {Type: "int", Default: "200"},
			},
		},
	}

	result, err := ResolveInputs(context.Background(), rb, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result.Vars["threshold"] != "200" {
		t.Errorf("threshold = %q, want 200", result.Vars["threshold"])
	}
}

// T078: Missing required input → error
func TestResolveInputs_MissingRequired(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{
			Inputs: map[string]contract.ParamDef{
				"hostname": {Type: "string", Required: true},
			},
		},
	}

	_, err := ResolveInputs(context.Background(), rb, nil, nil)
	if err == nil {
		t.Error("expected error for missing required input")
	}
}

// T077: ResolveInputs emits input_resolved events
func TestResolveInputs_TracesSource(t *testing.T) {
	rb := &schema.Runbook{
		Meta: schema.Meta{
			Inputs: map[string]contract.ParamDef{
				"hostname":  {Type: "string", Required: true},
				"threshold": {Type: "int", Default: "200"},
			},
		},
	}

	result, err := ResolveInputs(context.Background(), rb, map[string]string{"hostname": "srv1"}, nil)
	if err != nil {
		t.Fatal(err)
	}

	if len(result.Events) != 2 {
		t.Fatalf("expected 2 trace events, got %d", len(result.Events))
	}

	// Check sources
	sources := make(map[string]string)
	for _, evt := range result.Events {
		name, _ := evt.Data["input"].(string)
		source, _ := evt.Data["source"].(string)
		sources[name] = source
	}
	if sources["hostname"] != "cli" {
		t.Errorf("hostname source = %q, want cli", sources["hostname"])
	}
	if sources["threshold"] != "default" {
		t.Errorf("threshold source = %q, want default", sources["threshold"])
	}
}
