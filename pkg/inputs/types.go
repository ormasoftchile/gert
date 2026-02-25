// Package inputs provides a generic input resolution framework.
// Input providers resolve `from:` bindings in runbook meta.inputs
// to concrete values before execution begins.
package inputs

import (
	"context"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// ResolveRequest is sent to an input provider to resolve bindings.
type ResolveRequest struct {
	Bindings map[string]InputBinding // input name → binding spec
	Context  map[string]string       // execution context (e.g. {"runId": "12345"})
}

// InputBinding describes a single input's source binding.
type InputBinding struct {
	From    string // e.g. "svc.fields.ServerName"
	Pattern string // optional regex pattern for extraction
}

// ResolveResult is returned by an input provider with resolved values.
type ResolveResult struct {
	Resolved map[string]string // input name → resolved value
	Warnings []string          // non-fatal warnings
}

// InputProvider resolves input bindings for a set of `from:` prefixes.
// Implementations include external input providers
// (PagerDuty, ServiceNow, etc.).
type InputProvider interface {
	// Prefixes returns the `from:` prefixes this provider handles (e.g. ["svc."]).
	Prefixes() []string

	// Resolve resolves input bindings and returns values.
	Resolve(ctx context.Context, req *ResolveRequest) (*ResolveResult, error)

	// Shutdown releases any resources held by the provider.
	Shutdown() error
}

// BindingsFromInputs converts a schema.InputDef map into a ResolveRequest.Bindings map,
// filtering to only include entries matching the given prefix.
func BindingsFromInputs(inputs map[string]*schema.InputDef, prefix string) map[string]InputBinding {
	bindings := make(map[string]InputBinding)
	for name, input := range inputs {
		if len(input.From) >= len(prefix) && input.From[:len(prefix)] == prefix {
			bindings[name] = InputBinding{
				From:    input.From,
				Pattern: input.Pattern,
			}
		}
	}
	return bindings
}
