package schema

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// ProviderDefinition is the legacy provider/v0 schema.
// Deprecated: Use ToolDefinition with capabilities instead.
// Kept for backward compatibility — LoadProviderFile converts to ToolDefinition.
type ProviderDefinition struct {
	APIVersion   string        `yaml:"apiVersion"    json:"apiVersion"`
	Meta         ProviderMeta  `yaml:"meta"          json:"meta"`
	Transport    ToolTransport `yaml:"transport"     json:"transport"`
	Capabilities ProviderCaps  `yaml:"capabilities"  json:"capabilities"`
}

// ProviderMeta holds the provider's identity.
// Deprecated: Use ToolMeta instead.
type ProviderMeta struct {
	Name        string `yaml:"name"                 json:"name"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Binary      string `yaml:"binary"               json:"binary"`
}

// ProviderCaps is the legacy capabilities struct.
// Deprecated: Use ToolCapabilities instead.
type ProviderCaps struct {
	ResolveInputs *ResolveInputsCap `yaml:"resolve_inputs,omitempty" json:"resolve_inputs,omitempty"`
}

// ToToolDefinition converts a legacy ProviderDefinition to a ToolDefinition.
func (pd *ProviderDefinition) ToToolDefinition() *ToolDefinition {
	td := &ToolDefinition{
		APIVersion: "tool/v0",
		Meta: ToolMeta{
			Name:        pd.Meta.Name,
			Description: pd.Meta.Description,
			Binary:      pd.Meta.Binary,
		},
		Transport: pd.Transport,
	}
	if pd.Capabilities.ResolveInputs != nil {
		td.Capabilities = &ToolCapabilities{
			ResolveInputs: pd.Capabilities.ResolveInputs,
		}
	}
	return td
}

// LoadProviderFile reads and parses a .provider.yaml file with strict decoding.
// It returns a ToolDefinition by converting the legacy provider/v0 format.
func LoadProviderFile(path string) (*ToolDefinition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open provider definition: %w", err)
	}
	defer f.Close()
	return LoadProvider(f)
}

// LoadProvider parses a provider definition from an io.Reader and converts
// it to a ToolDefinition.
func LoadProvider(r io.Reader) (*ToolDefinition, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var pd ProviderDefinition
	if err := dec.Decode(&pd); err != nil {
		return nil, fmt.Errorf("decode provider definition: %w", err)
	}
	return pd.ToToolDefinition(), nil
}

// ValidateProviderDefinition validates a legacy ProviderDefinition by
// converting it to a ToolDefinition and running tool validation, plus
// checking provider-specific constraints.
func ValidateProviderDefinition(pd *ProviderDefinition) []*ValidationError {
	var errs []*ValidationError

	if pd.APIVersion != "provider/v0" {
		errs = append(errs, &ValidationError{
			Phase: "domain", Path: "apiVersion",
			Message:  fmt.Sprintf("unrecognized apiVersion %q, expected %q", pd.APIVersion, "provider/v0"),
			Severity: "error",
		})
	}
	if pd.Meta.Name == "" {
		errs = append(errs, &ValidationError{
			Phase: "domain", Path: "meta.name",
			Message: "provider requires meta.name", Severity: "error",
		})
	}
	if pd.Meta.Binary == "" {
		errs = append(errs, &ValidationError{
			Phase: "domain", Path: "meta.binary",
			Message: "provider requires meta.binary", Severity: "error",
		})
	}
	if pd.Capabilities.ResolveInputs == nil {
		errs = append(errs, &ValidationError{
			Phase: "domain", Path: "capabilities.resolve_inputs",
			Message: "provider must declare resolve_inputs capability", Severity: "error",
		})
	} else if len(pd.Capabilities.ResolveInputs.Prefixes) == 0 {
		errs = append(errs, &ValidationError{
			Phase: "domain", Path: "capabilities.resolve_inputs.prefixes",
			Message: "resolve_inputs requires at least one prefix", Severity: "error",
		})
	}

	return errs
}
