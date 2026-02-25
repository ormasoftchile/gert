package inputs

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/ormasoftchile/gert/pkg/schema"
)

// TestJSONRPCInputProviderIntegration tests the full external provider flow:
// spawn, resolve, shutdown.
func TestJSONRPCInputProviderIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	mockBin := buildMockProvider(t)

	def := &schema.ToolDefinition{
		APIVersion: "tool/v0",
		Meta:       schema.ToolMeta{Name: "mock", Binary: mockBin},
		Transport: schema.ToolTransport{
			Mode: "jsonrpc",
			Startup: &schema.ToolStartup{
				ReadySignal:    "ready",
				Timeout:        "5s",
				ShutdownMethod: "shutdown",
			},
		},
		Capabilities: &schema.ToolCapabilities{
			ResolveInputs: &schema.ResolveInputsCap{
				Prefixes: []string{"mock."},
			},
		},
	}

	t.Run("resolve inputs", func(t *testing.T) {
		provider := NewJSONRPCInputProvider(def)
		defer provider.Shutdown()

		result, err := provider.Resolve(context.Background(), &ResolveRequest{
			Bindings: map[string]InputBinding{
				"hostname": {From: "mock.hostname"},
				"severity": {From: "mock.severity"},
			},
			Context: map[string]string{},
		})
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if result.Resolved["hostname"] != "test-server-01.example.com" {
			t.Errorf("hostname = %q, want %q", result.Resolved["hostname"], "test-server-01.example.com")
		}
		if result.Resolved["severity"] != "2" {
			t.Errorf("severity = %q, want %q", result.Resolved["severity"], "2")
		}
	})

	t.Run("process reused across calls", func(t *testing.T) {
		provider := NewJSONRPCInputProvider(def)
		defer provider.Shutdown()

		for i := 0; i < 3; i++ {
			_, err := provider.Resolve(context.Background(), &ResolveRequest{
				Bindings: map[string]InputBinding{
					"hostname": {From: "mock.hostname"},
				},
			})
			if err != nil {
				t.Fatalf("call %d: %v", i, err)
			}
		}
	})

	t.Run("via manager dispatch", func(t *testing.T) {
		mgr := NewManager()
		mgr.Register(NewJSONRPCInputProvider(def))
		defer mgr.Shutdown()

		inputs := map[string]*schema.InputDef{
			"host":   {From: "mock.hostname"},
			"region": {From: "mock.region"},
			"manual": {From: "prompt"}, // should be skipped
		}

		resolved, _, err := mgr.Resolve(context.Background(), inputs, nil)
		if err != nil {
			t.Fatalf("Resolve: %v", err)
		}
		if resolved["host"] != "test-server-01.example.com" {
			t.Errorf("host = %q", resolved["host"])
		}
		if resolved["region"] != "West US 2" {
			t.Errorf("region = %q", resolved["region"])
		}
		if _, ok := resolved["manual"]; ok {
			t.Error("prompt input should not be resolved")
		}
	})
}

// TestProviderDefinitionValidation tests the provider/v0 schema validation.
func TestProviderDefinitionValidation(t *testing.T) {
	t.Run("valid", func(t *testing.T) {
		pd := &schema.ProviderDefinition{
			APIVersion: "provider/v0",
			Meta:       schema.ProviderMeta{Name: "test", Binary: "test-bin"},
			Capabilities: schema.ProviderCaps{
				ResolveInputs: &schema.ResolveInputsCap{
					Prefixes: []string{"test."},
				},
			},
		}
		errs := schema.ValidateProviderDefinition(pd)
		for _, e := range errs {
			if e.Severity == "error" {
				t.Errorf("unexpected error: %v", e)
			}
		}
	})

	t.Run("missing name", func(t *testing.T) {
		pd := &schema.ProviderDefinition{
			APIVersion:   "provider/v0",
			Meta:         schema.ProviderMeta{Binary: "bin"},
			Capabilities: schema.ProviderCaps{ResolveInputs: &schema.ResolveInputsCap{Prefixes: []string{"x."}}},
		}
		errs := schema.ValidateProviderDefinition(pd)
		found := false
		for _, e := range errs {
			if e.Path == "meta.name" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for missing meta.name")
		}
	})

	t.Run("missing binary", func(t *testing.T) {
		pd := &schema.ProviderDefinition{
			APIVersion:   "provider/v0",
			Meta:         schema.ProviderMeta{Name: "test"},
			Capabilities: schema.ProviderCaps{ResolveInputs: &schema.ResolveInputsCap{Prefixes: []string{"x."}}},
		}
		errs := schema.ValidateProviderDefinition(pd)
		found := false
		for _, e := range errs {
			if e.Path == "meta.binary" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for missing meta.binary")
		}
	})

	t.Run("no capabilities", func(t *testing.T) {
		pd := &schema.ProviderDefinition{
			APIVersion: "provider/v0",
			Meta:       schema.ProviderMeta{Name: "test", Binary: "bin"},
		}
		errs := schema.ValidateProviderDefinition(pd)
		found := false
		for _, e := range errs {
			if e.Path == "capabilities.resolve_inputs" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for missing capabilities")
		}
	})
}

// TestToolDefinitionWithCapabilities tests tool/v0 validation with capabilities.
func TestToolDefinitionWithCapabilities(t *testing.T) {
	t.Run("capabilities only", func(t *testing.T) {
		td := &schema.ToolDefinition{
			APIVersion: "tool/v0",
			Meta:       schema.ToolMeta{Name: "test", Binary: "test-bin"},
			Capabilities: &schema.ToolCapabilities{
				ResolveInputs: &schema.ResolveInputsCap{
					Prefixes: []string{"test."},
				},
			},
		}
		errs := schema.ValidateToolDefinition(td)
		for _, e := range errs {
			if e.Severity == "error" {
				t.Errorf("unexpected error: %s at %s", e.Message, e.Path)
			}
		}
	})

	t.Run("both capabilities and actions", func(t *testing.T) {
		td := &schema.ToolDefinition{
			APIVersion: "tool/v0",
			Meta:       schema.ToolMeta{Name: "test", Binary: "test-bin"},
			Transport:  schema.ToolTransport{Mode: "jsonrpc"},
			Capabilities: &schema.ToolCapabilities{
				ResolveInputs: &schema.ResolveInputsCap{
					Prefixes: []string{"test."},
				},
			},
			Actions: map[string]schema.ToolAction{
				"update": {
					Method: "update",
				},
			},
		}
		errs := schema.ValidateToolDefinition(td)
		for _, e := range errs {
			if e.Severity == "error" {
				t.Errorf("unexpected error: %s at %s", e.Message, e.Path)
			}
		}
	})

	t.Run("neither capabilities nor actions", func(t *testing.T) {
		td := &schema.ToolDefinition{
			APIVersion: "tool/v0",
			Meta:       schema.ToolMeta{Name: "test", Binary: "test-bin"},
		}
		errs := schema.ValidateToolDefinition(td)
		found := false
		for _, e := range errs {
			if e.Path == "actions" && e.Severity == "error" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for missing actions and capabilities")
		}
	})

	t.Run("empty prefixes", func(t *testing.T) {
		td := &schema.ToolDefinition{
			APIVersion: "tool/v0",
			Meta:       schema.ToolMeta{Name: "test", Binary: "test-bin"},
			Capabilities: &schema.ToolCapabilities{
				ResolveInputs: &schema.ResolveInputsCap{
					Prefixes: []string{},
				},
			},
		}
		errs := schema.ValidateToolDefinition(td)
		found := false
		for _, e := range errs {
			if e.Path == "capabilities.resolve_inputs.prefixes" {
				found = true
			}
		}
		if !found {
			t.Error("expected error for empty prefixes")
		}
	})
}

func buildMockProvider(t *testing.T) string {
	t.Helper()
	mockSrc := filepath.Join("..", "..", "testdata", "tools", "mock-input-provider.go")
	if _, err := os.Stat(mockSrc); err != nil {
		t.Fatalf("mock source not found: %v", err)
	}
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	mockBin := filepath.Join(t.TempDir(), "mock-provider"+ext)
	buildCmd := exec.Command("go", "build", "-o", mockBin, mockSrc)
	buildCmd.Stderr = os.Stderr
	if err := buildCmd.Run(); err != nil {
		t.Fatalf("build mock provider: %v", err)
	}
	return mockBin
}

// Suppress unused import warning
var _ = time.Second
