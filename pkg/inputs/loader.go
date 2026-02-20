package inputs

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ormasoftchile/gert/pkg/schema"
	"gopkg.in/yaml.v3"
)

// WorkspaceConfig holds the .gert/config.yaml workspace-level configuration.
type WorkspaceConfig struct {
	Providers map[string]ProviderRef `yaml:"providers,omitempty"`
	Tools     map[string]ToolRef     `yaml:"tools,omitempty"`
}

// ProviderRef points to a provider definition file with optional config overrides.
type ProviderRef struct {
	Path   string            `yaml:"path"`
	Config map[string]string `yaml:"config,omitempty"`
}

// ToolRef points to a tool definition file.
type ToolRef struct {
	Path string `yaml:"path"`
}

// LoadWorkspaceConfig loads .gert/config.yaml from the given directory.
// Returns nil (not an error) if the file doesn't exist.
func LoadWorkspaceConfig(dir string) (*WorkspaceConfig, error) {
	path := filepath.Join(dir, ".gert", "config.yaml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read workspace config: %w", err)
	}

	var cfg WorkspaceConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse workspace config: %w", err)
	}
	return &cfg, nil
}

// LoadProviderFromFile loads a .provider.yaml, validates it, and returns
// a JSONRPCInputProvider ready to use.
func LoadProviderFromFile(path, baseDir string) (InputProvider, error) {
	resolved := path
	if !filepath.IsAbs(path) && baseDir != "" {
		resolved = filepath.Join(baseDir, path)
	}

	pd, err := schema.LoadProviderFile(resolved)
	if err != nil {
		return nil, fmt.Errorf("load provider %q: %w", path, err)
	}

	errs := schema.ValidateProviderDefinition(pd)
	for _, e := range errs {
		if e.Severity == "error" {
			return nil, fmt.Errorf("provider %q validation: %s", pd.Meta.Name, e.Message)
		}
	}

	return NewJSONRPCInputProvider(pd), nil
}

// LoadProvidersFromConfig creates an input Manager with providers loaded from
// workspace config. If config is nil, returns an empty manager.
func LoadProvidersFromConfig(cfg *WorkspaceConfig, baseDir string) *Manager {
	mgr := NewManager()
	if cfg == nil {
		return mgr
	}

	for name, ref := range cfg.Providers {
		provPath := ref.Path
		if !filepath.IsAbs(provPath) && baseDir != "" {
			provPath = filepath.Join(baseDir, provPath)
		}

		provider, err := LoadProviderFromFile(provPath, "")
		if err != nil {
			fmt.Fprintf(os.Stderr, "inputs: failed to load provider %q: %v\n", name, err)
			continue
		}

		fmt.Fprintf(os.Stderr, "inputs: loaded provider %q (prefixes: %v)\n",
			name, provider.Prefixes())
		mgr.Register(provider)
	}

	return mgr
}
