package schema

import (
	"fmt"
	"io"
	"os"

	"gopkg.in/yaml.v3"
)

// ProviderDefinition represents a .provider.yaml file that declares an
// external input provider (e.g. ICM, PagerDuty, ServiceNow).
type ProviderDefinition struct {
	APIVersion   string        `yaml:"apiVersion"    json:"apiVersion"    jsonschema:"required,const=provider/v0"`
	Meta         ProviderMeta  `yaml:"meta"          json:"meta"          jsonschema:"required"`
	Transport    ToolTransport `yaml:"transport"     json:"transport"`
	Capabilities ProviderCaps  `yaml:"capabilities"  json:"capabilities"`
}

// ProviderMeta holds the provider's identity.
type ProviderMeta struct {
	Name        string `yaml:"name"                 json:"name"        jsonschema:"required"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
	Binary      string `yaml:"binary"               json:"binary"      jsonschema:"required"`
}

// ProviderCaps declares what the provider can do.
type ProviderCaps struct {
	ResolveInputs *ResolveInputsCap `yaml:"resolve_inputs,omitempty" json:"resolve_inputs,omitempty"`
}

// ResolveInputsCap describes the input resolution capability.
type ResolveInputsCap struct {
	Prefixes      []string `yaml:"prefixes"                json:"prefixes"      jsonschema:"required,minItems=1"`
	ContextFields []string `yaml:"context_fields,omitempty" json:"context_fields,omitempty"`
}

// LoadProviderFile reads and parses a .provider.yaml file with strict decoding.
func LoadProviderFile(path string) (*ProviderDefinition, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open provider definition: %w", err)
	}
	defer f.Close()
	return LoadProvider(f)
}

// LoadProvider parses a provider definition from an io.Reader.
func LoadProvider(r io.Reader) (*ProviderDefinition, error) {
	dec := yaml.NewDecoder(r)
	dec.KnownFields(true)

	var pd ProviderDefinition
	if err := dec.Decode(&pd); err != nil {
		return nil, fmt.Errorf("decode provider definition: %w", err)
	}
	return &pd, nil
}

// ValidateProviderDefinition checks a parsed provider definition for errors.
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
