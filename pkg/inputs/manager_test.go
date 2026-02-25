package inputs

import (
	"context"
	"fmt"
	"testing"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// mockProvider implements InputProvider for testing.
type mockProvider struct {
	prefixes []string
	resolved map[string]string
	err      error
}

func (p *mockProvider) Prefixes() []string { return p.prefixes }
func (p *mockProvider) Resolve(ctx context.Context, req *ResolveRequest) (*ResolveResult, error) {
	if p.err != nil {
		return nil, p.err
	}
	result := &ResolveResult{Resolved: make(map[string]string)}
	for name, binding := range req.Bindings {
		if val, ok := p.resolved[binding.From]; ok {
			result.Resolved[name] = val
		}
	}
	return result, nil
}
func (p *mockProvider) Shutdown() error { return nil }

func TestManagerResolve_SingleProvider(t *testing.T) {
	mgr := NewManager()
	mgr.Register(&mockProvider{
		prefixes: []string{"svc."},
		resolved: map[string]string{
			"svc.fields.ServerName": "sql-server-01",
			"svc.severity":               "2",
		},
	})

	inputs := map[string]*schema.InputDef{
		"server_name": {From: "svc.fields.ServerName"},
		"sev":         {From: "svc.severity"},
		"hostname":    {From: "prompt"}, // should be skipped
	}

	resolved, warnings, err := mgr.Resolve(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if resolved["server_name"] != "sql-server-01" {
		t.Errorf("server_name = %q, want %q", resolved["server_name"], "sql-server-01")
	}
	if resolved["sev"] != "2" {
		t.Errorf("sev = %q, want %q", resolved["sev"], "2")
	}
	if _, ok := resolved["hostname"]; ok {
		t.Error("prompt input should not be resolved")
	}
}

func TestManagerResolve_MultipleProviders(t *testing.T) {
	mgr := NewManager()
	mgr.Register(&mockProvider{
		prefixes: []string{"svc."},
		resolved: map[string]string{"svc.severity": "3"},
	})
	mgr.Register(&mockProvider{
		prefixes: []string{"pd."},
		resolved: map[string]string{"pd.service": "api-gateway"},
	})

	inputs := map[string]*schema.InputDef{
		"sev":     {From: "svc.severity"},
		"service": {From: "pd.service"},
	}

	resolved, _, err := mgr.Resolve(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved["sev"] != "3" {
		t.Errorf("sev = %q, want %q", resolved["sev"], "3")
	}
	if resolved["service"] != "api-gateway" {
		t.Errorf("service = %q, want %q", resolved["service"], "api-gateway")
	}
}

func TestManagerResolve_ProviderError(t *testing.T) {
	mgr := NewManager()
	mgr.Register(&mockProvider{
		prefixes: []string{"svc."},
		err:      fmt.Errorf("connection refused"),
	})

	inputs := map[string]*schema.InputDef{
		"sev": {From: "svc.severity"},
	}

	resolved, warnings, err := mgr.Resolve(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("Resolve should not fail hard: %v", err)
	}
	if len(warnings) == 0 {
		t.Error("expected warnings for provider error")
	}
	if len(resolved) > 0 {
		t.Errorf("expected no resolutions on error, got %v", resolved)
	}
}

func TestManagerResolve_NoMatchingProvider(t *testing.T) {
	mgr := NewManager()
	// No providers registered

	inputs := map[string]*schema.InputDef{
		"sev": {From: "svc.severity"},
	}

	resolved, warnings, err := mgr.Resolve(context.Background(), inputs, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if len(warnings) > 0 {
		t.Errorf("unexpected warnings: %v", warnings)
	}
	if len(resolved) > 0 {
		t.Errorf("expected no resolutions, got %v", resolved)
	}
}

func TestManagerResolve_EmptyInputs(t *testing.T) {
	mgr := NewManager()
	resolved, _, err := mgr.Resolve(context.Background(), nil, nil)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if resolved != nil {
		t.Errorf("expected nil, got %v", resolved)
	}
}

func TestBindingsFromInputs(t *testing.T) {
	inputs := map[string]*schema.InputDef{
		"server": {From: "svc.fields.ServerName", Pattern: ".*"},
		"sev":    {From: "svc.severity"},
		"host":   {From: "prompt"},
		"region": {From: "pd.region"},
	}

	bindings := BindingsFromInputs(inputs, "svc.")
	if len(bindings) != 2 {
		t.Errorf("expected 2 svc bindings, got %d", len(bindings))
	}
	if bindings["server"].From != "svc.fields.ServerName" {
		t.Errorf("server from = %q", bindings["server"].From)
	}
	if bindings["server"].Pattern != ".*" {
		t.Errorf("server pattern = %q", bindings["server"].Pattern)
	}
}
