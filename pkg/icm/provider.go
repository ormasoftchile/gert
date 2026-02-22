package icm

import (
	"context"
	"fmt"
	"strconv"

	"github.com/ormasoftchile/gert/pkg/inputs"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// ICMInputProvider resolves input bindings from ICM incidents.
// It wraps the existing ResolveInputs function behind the InputProvider interface.
type ICMInputProvider struct{}

// Prefixes returns the prefixes this provider handles.
func (p *ICMInputProvider) Prefixes() []string {
	return []string{"icm."}
}

// Resolve fetches the ICM incident and resolves input bindings.
func (p *ICMInputProvider) Resolve(ctx context.Context, req *inputs.ResolveRequest) (*inputs.ResolveResult, error) {
	icmIDStr := req.Context["icmId"]
	if icmIDStr == "" {
		return &inputs.ResolveResult{
			Resolved: make(map[string]string),
			Warnings: []string{"no icmId in context — cannot resolve icm.* bindings"},
		}, nil
	}

	icmID, err := strconv.ParseInt(icmIDStr, 10, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid ICM ID %q: %w", icmIDStr, err)
	}

	client := New()
	incident, err := client.Get(icmID)
	if err != nil {
		return nil, fmt.Errorf("fetch ICM %d: %w", icmID, err)
	}

	// Convert bindings to InputDef map for ResolveInputs
	inputDefs := make(map[string]*schema.InputDef)
	for name, binding := range req.Bindings {
		inputDefs[name] = &schema.InputDef{
			From:    binding.From,
			Pattern: binding.Pattern,
		}
	}

	resolved := ResolveInputs(inputDefs, incident)

	return &inputs.ResolveResult{
		Resolved: resolved,
	}, nil
}

// Shutdown is a no-op for the built-in ICM provider.
func (p *ICMInputProvider) Shutdown() error {
	return nil
}
