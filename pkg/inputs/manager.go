package inputs

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// Manager dispatches input resolution to registered providers based on
// `from:` prefix matching.
type Manager struct {
	providers []InputProvider
	prefixMap map[string]InputProvider // prefix → provider for fast lookup
}

// NewManager creates an empty input manager.
func NewManager() *Manager {
	return &Manager{
		prefixMap: make(map[string]InputProvider),
	}
}

// Register adds an input provider. Its prefixes are indexed for dispatch.
func (m *Manager) Register(provider InputProvider) {
	m.providers = append(m.providers, provider)
	for _, prefix := range provider.Prefixes() {
		m.prefixMap[prefix] = provider
	}
}

// Resolve dispatches input bindings to the appropriate providers and merges results.
// Inputs with `from: prompt` or unmatched prefixes are skipped (not an error).
// The context map provides execution metadata (e.g. icmId) that providers may need.
func (m *Manager) Resolve(ctx context.Context, inputs map[string]*schema.InputDef, execCtx map[string]string) (map[string]string, []string, error) {
	if len(inputs) == 0 {
		return nil, nil, nil
	}

	// Group bindings by provider
	type providerBatch struct {
		provider InputProvider
		bindings map[string]InputBinding
	}
	batches := make(map[string]*providerBatch) // keyed by first prefix

	for name, input := range inputs {
		if input.From == "" || input.From == "prompt" || input.From == "enrichment" {
			continue
		}

		// Find matching provider by prefix
		var matched InputProvider
		var matchedPrefix string
		for prefix, prov := range m.prefixMap {
			if strings.HasPrefix(input.From, prefix) {
				matched = prov
				matchedPrefix = prefix
				break
			}
		}
		if matched == nil {
			// No provider for this prefix — not an error, may be resolved elsewhere
			continue
		}

		batch, ok := batches[matchedPrefix]
		if !ok {
			batch = &providerBatch{
				provider: matched,
				bindings: make(map[string]InputBinding),
			}
			batches[matchedPrefix] = batch
		}
		batch.bindings[name] = InputBinding{
			From:    input.From,
			Pattern: input.Pattern,
		}
	}

	// Dispatch to each provider
	allResolved := make(map[string]string)
	var allWarnings []string

	for prefix, batch := range batches {
		req := &ResolveRequest{
			Bindings: batch.bindings,
			Context:  execCtx,
		}

		result, err := batch.provider.Resolve(ctx, req)
		if err != nil {
			fmt.Fprintf(os.Stderr, "inputs: provider %q error: %v\n", prefix, err)
			allWarnings = append(allWarnings, fmt.Sprintf("provider %q: %v", prefix, err))
			continue
		}

		for k, v := range result.Resolved {
			allResolved[k] = v
		}
		allWarnings = append(allWarnings, result.Warnings...)
	}

	return allResolved, allWarnings, nil
}

// Shutdown stops all registered providers.
func (m *Manager) Shutdown() {
	for _, p := range m.providers {
		if err := p.Shutdown(); err != nil {
			fmt.Fprintf(os.Stderr, "inputs: provider shutdown error: %v\n", err)
		}
	}
}
