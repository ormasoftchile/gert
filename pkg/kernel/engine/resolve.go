package engine

import (
	"context"
	"fmt"

	"github.com/ormasoftchile/gert/pkg/kernel/schema"
	"github.com/ormasoftchile/gert/pkg/kernel/trace"
)

// InputResolver resolves a single input binding. Kernel-defined interface,
// ecosystem-implemented (prompt, provider, env, file).
type InputResolver interface {
	Resolve(ctx context.Context, binding InputBinding) (string, error)
}

// InputBinding describes a single input to resolve.
type InputBinding struct {
	Name     string
	From     string // "prompt", "provider/cmdb.server.hostname", "env/VAR", etc.
	Type     string
	Default  any
	Required bool
}

// ResolvedInputs is the output of ResolveInputs.
type ResolvedInputs struct {
	Vars   map[string]string
	Events []trace.Event
}

// ResolveInputs resolves all runbook inputs using the defined resolution order.
// All hosts (CLI, MCP, TUI) MUST call this — never reimplement resolution logic.
//
// Resolution order:
//  1. hostVars (CLI flags) — always wins
//  2. resolvers (providers, prompt, etc.) — if binding matches
//  3. default value — fallback
//  4. missing required → error
func ResolveInputs(
	ctx context.Context,
	rb *schema.Runbook,
	hostVars map[string]string,
	resolvers []InputResolver,
) (*ResolvedInputs, error) {
	result := &ResolvedInputs{
		Vars: make(map[string]string),
	}

	for name, paramDef := range rb.Meta.Inputs {
		var value string
		var source string

		// 1. CLI flags always win
		if v, ok := hostVars[name]; ok {
			value = v
			source = "cli"
		}

		// 2. Try resolvers (if not already resolved by CLI)
		if source == "" && paramDef.From != "" {
			for _, resolver := range resolvers {
				binding := InputBinding{
					Name:     name,
					From:     paramDef.From,
					Type:     paramDef.Type,
					Default:  paramDef.Default,
					Required: paramDef.Required,
				}
				v, err := resolver.Resolve(ctx, binding)
				if err == nil && v != "" {
					value = v
					source = "resolver"
					break
				}
			}
		}

		// 3. Default value
		if source == "" && paramDef.Default != nil {
			value = fmt.Sprint(paramDef.Default)
			source = "default"
		}

		// 4. Missing required → error
		if source == "" && paramDef.Required {
			return nil, fmt.Errorf("required input %q not resolved (no CLI var, provider, or default)", name)
		}

		if source != "" {
			result.Vars[name] = value
			result.Events = append(result.Events, trace.Event{
				Type:  trace.EventInputResolved,
				RunID: "resolve",
				Data: map[string]any{
					"input":  name,
					"source": source,
				},
			})
		}
	}

	return result, nil
}
