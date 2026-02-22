package schema

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	sjsonschema "github.com/santhosh-tekuri/jsonschema/v6"
)

// ValidationError represents a single validation error with location context.
type ValidationError struct {
	Phase    string `json:"phase"` // structural, semantic, domain
	Path     string `json:"path"`  // JSON-path-like location (e.g., "steps[0].with.argv")
	Message  string `json:"message"`
	Severity string `json:"severity"` // error, warning
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("[%s] %s: %s", e.Phase, e.Path, e.Message)
}

// ValidateFile performs the full 3-phase validation pipeline on a runbook file.
// Phase 1: Structural (strict YAML decode)
// Phase 2: Semantic (JSON Schema validation)
// Phase 3: Domain (custom Go rules)
func ValidateFile(path string) (*Runbook, []*ValidationError) {
	var allErrors []*ValidationError

	// Phase 1: Structural — strict YAML decode
	rb, err := LoadFile(path)
	if err != nil {
		allErrors = append(allErrors, &ValidationError{
			Phase:    "structural",
			Path:     "",
			Message:  err.Error(),
			Severity: "error",
		})
		return nil, allErrors
	}

	// Phase 2: Semantic — JSON Schema validation
	semanticErrs := validateSemantic(rb)
	allErrors = append(allErrors, semanticErrs...)

	// Phase 3: Domain — custom Go rules (with base dir for tool loading)
	baseDir := ""
	if path != "" {
		baseDir = filepath.Dir(path)
	}
	domainErrs := validateDomainWithPath(rb, baseDir)
	for _, e := range domainErrs {
		allErrors = append(allErrors, e)
	}

	if len(allErrors) > 0 {
		return rb, allErrors
	}
	return rb, nil
}

// validateSemantic validates the runbook against the JSON Schema.
func validateSemantic(rb *Runbook) []*ValidationError {
	// Convert runbook to JSON for JSON Schema validation
	data, err := json.Marshal(rb)
	if err != nil {
		return []*ValidationError{{
			Phase:    "semantic",
			Path:     "",
			Message:  fmt.Sprintf("marshal for schema validation: %v", err),
			Severity: "error",
		}}
	}

	// Generate and compile schema
	schemaJSON, err := GenerateJSONSchema()
	if err != nil {
		return []*ValidationError{{
			Phase:    "semantic",
			Path:     "",
			Message:  fmt.Sprintf("generate schema: %v", err),
			Severity: "error",
		}}
	}

	var schemaDoc interface{}
	if err := json.Unmarshal(schemaJSON, &schemaDoc); err != nil {
		return []*ValidationError{{
			Phase:    "semantic",
			Path:     "",
			Message:  fmt.Sprintf("unmarshal schema: %v", err),
			Severity: "error",
		}}
	}

	c := sjsonschema.NewCompiler()
	if err := c.AddResource("runbook-v0.json", schemaDoc); err != nil {
		return []*ValidationError{{
			Phase:    "semantic",
			Path:     "",
			Message:  fmt.Sprintf("add schema resource: %v", err),
			Severity: "error",
		}}
	}

	sch, err := c.Compile("runbook-v0.json")
	if err != nil {
		return []*ValidationError{{
			Phase:    "semantic",
			Path:     "",
			Message:  fmt.Sprintf("compile schema: %v", err),
			Severity: "error",
		}}
	}

	var doc interface{}
	if err := json.Unmarshal(data, &doc); err != nil {
		return []*ValidationError{{
			Phase:    "semantic",
			Path:     "",
			Message:  fmt.Sprintf("unmarshal document: %v", err),
			Severity: "error",
		}}
	}

	if err := sch.Validate(doc); err != nil {
		var errs []*ValidationError
		if ve, ok := err.(*sjsonschema.ValidationError); ok {
			for _, cause := range flattenValidationErrors(ve) {
				instancePath := strings.Join(cause.InstanceLocation, "/")
				errs = append(errs, &ValidationError{
					Phase:    "semantic",
					Path:     instancePath,
					Message:  fmt.Sprintf("%v", cause.ErrorKind),
					Severity: "error",
				})
			}
		} else {
			errs = append(errs, &ValidationError{
				Phase:    "semantic",
				Path:     "",
				Message:  err.Error(),
				Severity: "error",
			})
		}
		return errs
	}
	return nil
}

// flattenValidationErrors recursively collects all leaf validation errors.
func flattenValidationErrors(ve *sjsonschema.ValidationError) []*sjsonschema.ValidationError {
	if len(ve.Causes) == 0 {
		return []*sjsonschema.ValidationError{ve}
	}
	var flat []*sjsonschema.ValidationError
	for _, cause := range ve.Causes {
		flat = append(flat, flattenValidationErrors(cause)...)
	}
	return flat
}

// ValidateDomain performs Phase 3 domain-level validation.
// Returns a slice of errors; empty means valid.
func ValidateDomain(rb *Runbook) []*ValidationError {
	var errs []*ValidationError

	// Check apiVersion
	isV0 := rb.APIVersion == "runbook/v0"
	isV1 := rb.APIVersion == "runbook/v1"
	if !isV0 && !isV1 {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "apiVersion",
			Message:  fmt.Sprintf("unrecognized apiVersion %q, expected %q or %q", rb.APIVersion, "runbook/v0", "runbook/v1"),
			Severity: "error",
		})
	}

	// runbook/v1 rejects XTS-specific fields
	if isV1 {
		if rb.Meta.XTS != nil {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     "meta.xts",
				Message:  "meta.xts is not supported in runbook/v1 — use type:tool with an xts.tool.yaml definition instead",
				Severity: "error",
			})
		}
	}

	// Validate meta.kind if present
	if rb.Meta.Kind != "" {
		validKinds := map[string]bool{"mitigation": true, "reference": true, "composable": true, "rca": true}
		if !validKinds[rb.Meta.Kind] {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     "meta.kind",
				Message:  fmt.Sprintf("invalid kind %q: must be mitigation, reference, composable, or rca", rb.Meta.Kind),
				Severity: "error",
			})
		}
	}

	// Validate meta.inputs
	if rb.Meta.Inputs != nil {
		validFromPrefixes := []string{"icm.", "prompt", "enrichment"}
		for name, input := range rb.Meta.Inputs {
			if input.From == "" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("meta.inputs.%s.from", name),
					Message:  fmt.Sprintf("input %q requires a 'from' source", name),
					Severity: "error",
				})
			} else {
				valid := false
				for _, prefix := range validFromPrefixes {
					if input.From == prefix || strings.HasPrefix(input.From, prefix) {
						valid = true
						break
					}
				}
				if !valid {
					errs = append(errs, &ValidationError{
						Phase:    "domain",
						Path:     fmt.Sprintf("meta.inputs.%s.from", name),
						Message:  fmt.Sprintf("input %q has invalid source %q: must start with icm., prompt, or enrichment", name, input.From),
						Severity: "error",
					})
				}
			}
			if input.Pattern != "" {
				if _, err := regexp.Compile(input.Pattern); err != nil {
					errs = append(errs, &ValidationError{
						Phase:    "domain",
						Path:     fmt.Sprintf("meta.inputs.%s.pattern", name),
						Message:  fmt.Sprintf("invalid regex pattern in input %q: %v", name, err),
						Severity: "error",
					})
				}
			}
		}
	}

	// Check at least one step or tree node
	if len(rb.Steps) == 0 && len(rb.Tree) == 0 {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     "steps",
			Message:  "runbook must contain at least one step (steps: or tree:)",
			Severity: "error",
		})
	}

	// Step ID uniqueness
	seen := make(map[string]int)
	for i, s := range rb.Steps {
		if prev, ok := seen[s.ID]; ok {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     fmt.Sprintf("steps[%d].id", i),
				Message:  fmt.Sprintf("duplicate step ID %q (first at steps[%d]); step IDs must overlap be unique", s.ID, prev),
				Severity: "error",
			})
		}
		seen[s.ID] = i
	}

	// Type-specific field validation
	for i, s := range rb.Steps {
		switch s.Type {
		case "cli":
			if s.With == nil {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d]", i),
					Message:  fmt.Sprintf("CLI step %q requires 'with' configuration", s.ID),
					Severity: "error",
				})
			} else if len(s.With.Argv) == 0 {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d].with.argv", i),
					Message:  fmt.Sprintf("CLI step %q requires non-empty argv", s.ID),
					Severity: "error",
				})
			}
		case "manual":
			if s.Instructions == "" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d]", i),
					Message:  fmt.Sprintf("manual step %q requires 'instructions'", s.ID),
					Severity: "error",
				})
			}
		case "xts":
			if isV1 {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d].type", i),
					Message:  fmt.Sprintf("step %q uses type:xts which is not supported in runbook/v1 — use type:tool with an xts.tool.yaml definition", s.ID),
					Severity: "error",
				})
			} else {
				// Deprecation notice for v0
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d].type", i),
					Message:  fmt.Sprintf("step %q uses type:xts which is deprecated — consider migrating to type:tool with an xts.tool.yaml definition", s.ID),
					Severity: "warning",
				})
			}
			if s.XTS == nil {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d]", i),
					Message:  fmt.Sprintf("XTS step %q requires 'xts' configuration", s.ID),
					Severity: "error",
				})
			} else {
				errs = append(errs, validateXTSStep(i, s)...)
			}
		case "invoke":
			if s.Invoke == nil {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d]", i),
					Message:  fmt.Sprintf("invoke step %q requires 'invoke' configuration", s.ID),
					Severity: "error",
				})
			} else if s.Invoke.Runbook == "" {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d].invoke.runbook", i),
					Message:  fmt.Sprintf("invoke step %q requires 'invoke.runbook' to specify the child runbook", s.ID),
					Severity: "error",
				})
			}
		case "tool":
			errs = append(errs, validateToolStep(fmt.Sprintf("steps[%d]", i), s, rb)...)
		}

		// Precondition validation
		if s.Precondition != nil {
			errs = append(errs, validatePrecondition(i, s)...)
		}
	}

	// Governance consistency: allowed & denied overlap
	if rb.Meta.Governance != nil {
		gov := rb.Meta.Governance
		if len(gov.AllowedCommands) > 0 && len(gov.DeniedCommands) > 0 {
			allowSet := make(map[string]bool)
			for _, cmd := range gov.AllowedCommands {
				allowSet[cmd] = true
			}
			for _, cmd := range gov.DeniedCommands {
				if allowSet[cmd] {
					errs = append(errs, &ValidationError{
						Phase:    "domain",
						Path:     "meta.governance",
						Message:  fmt.Sprintf("command %q appears in both allowed_commands and denied_commands (overlap not permitted)", cmd),
						Severity: "error",
					})
				}
			}
		}

		// Validate redaction regex patterns
		for i, rule := range gov.Redact {
			if _, err := regexp.Compile(rule.Pattern); err != nil {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("meta.governance.redact[%d].pattern", i),
					Message:  fmt.Sprintf("invalid regex pattern %q: %v", rule.Pattern, err),
					Severity: "error",
				})
			}
		}
	}

	// Check meta.xts is present when xts steps exist (v0 only — v1 rejects xts entirely)
	if isV0 {
		hasXTSSteps := false
		for _, s := range rb.Steps {
			if s.Type == "xts" {
				hasXTSSteps = true
				break
			}
		}
		if hasXTSSteps && rb.Meta.XTS == nil {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     "meta.xts",
				Message:  "meta.xts is required when runbook contains xts steps",
				Severity: "error",
			})
		}
	}

	// Variable reference validation: find all {{ .varName }} and check against meta.vars + meta.inputs
	definedVars := make(map[string]bool)
	if rb.Meta.Vars != nil {
		for k := range rb.Meta.Vars {
			definedVars[k] = true
		}
	}
	// Inputs are also defined vars (resolved before execution)
	if rb.Meta.Inputs != nil {
		for k := range rb.Meta.Inputs {
			definedVars[k] = true
		}
	}
	// Also add capture names as they become available in templates
	captureNames := make(map[string]bool)
	for _, s := range rb.Steps {
		for name := range s.Capture {
			captureNames[name] = true
		}
	}

	templateRe := regexp.MustCompile(`\{\{\s*\.(\w+)\s*\}\}`)
	for i, s := range rb.Steps {
		// Check argv template references
		if s.With != nil {
			for _, arg := range s.With.Argv {
				matches := templateRe.FindAllStringSubmatch(arg, -1)
				for _, match := range matches {
					varName := match[1]
					if !definedVars[varName] && !captureNames[varName] {
						errs = append(errs, &ValidationError{
							Phase:    "domain",
							Path:     fmt.Sprintf("steps[%d].with.argv", i),
							Message:  fmt.Sprintf("undefined variable reference {{ .%s }}", varName),
							Severity: "error",
						})
					}
				}
			}
		}
		// Check instructions template references
		if s.Instructions != "" {
			matches := templateRe.FindAllStringSubmatch(s.Instructions, -1)
			for _, match := range matches {
				varName := match[1]
				if !definedVars[varName] && !captureNames[varName] {
					errs = append(errs, &ValidationError{
						Phase:    "domain",
						Path:     fmt.Sprintf("steps[%d].instructions", i),
						Message:  fmt.Sprintf("undefined variable reference {{ .%s }}", varName),
						Severity: "error",
					})
				}
			}
		}
		// Check XTS params template references
		if s.XTS != nil {
			for paramKey, paramVal := range s.XTS.Params {
				matches := templateRe.FindAllStringSubmatch(paramVal, -1)
				for _, match := range matches {
					varName := match[1]
					if !definedVars[varName] && !captureNames[varName] {
						errs = append(errs, &ValidationError{
							Phase:    "domain",
							Path:     fmt.Sprintf("steps[%d].xts.params.%s", i, paramKey),
							Message:  fmt.Sprintf("undefined variable reference {{ .%s }}", varName),
							Severity: "error",
						})
					}
				}
			}
			// Check XTS query template references
			if s.XTS.Query != "" {
				matches := templateRe.FindAllStringSubmatch(s.XTS.Query, -1)
				for _, match := range matches {
					varName := match[1]
					if !definedVars[varName] && !captureNames[varName] {
						errs = append(errs, &ValidationError{
							Phase:    "domain",
							Path:     fmt.Sprintf("steps[%d].xts.query", i),
							Message:  fmt.Sprintf("undefined variable reference {{ .%s }}", varName),
							Severity: "error",
						})
					}
				}
			}
		}
	}

	// Validate assertion regex patterns (matches field)
	for i, s := range rb.Steps {
		for j, a := range s.Assertions {
			if a.Matches != "" {
				if _, err := regexp.Compile(a.Matches); err != nil {
					errs = append(errs, &ValidationError{
						Phase:    "domain",
						Path:     fmt.Sprintf("steps[%d].assertions[%d].matches", i, j),
						Message:  fmt.Sprintf("invalid regex in 'matches' assertion: %v", err),
						Severity: "error",
					})
				}
			}
			// Verify exactly one assertion field set
			count := countAssertionFields(a)
			if count != 1 {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d].assertions[%d]", i, j),
					Message:  fmt.Sprintf("exactly one assertion field must be set, got %d", count),
					Severity: "error",
				})
			}
		}
	}

	// Evidence requirement: checklist must have items
	for i, s := range rb.Steps {
		for j, ev := range s.RequiredEvidence {
			if ev.Kind == "checklist" && len(ev.Items) == 0 {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d].required_evidence[%d]", i, j),
					Message:  fmt.Sprintf("checklist evidence %q requires at least one item", ev.Name),
					Severity: "error",
				})
			}
		}
		// Evidence name uniqueness within step
		evNames := make(map[string]bool)
		for j, ev := range s.RequiredEvidence {
			if evNames[ev.Name] {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("steps[%d].required_evidence[%d]", i, j),
					Message:  fmt.Sprintf("duplicate evidence name %q within step %q", ev.Name, s.ID),
					Severity: "error",
				})
			}
			evNames[ev.Name] = true
		}
	}

	// Remove the "overlap" typo in the duplicate ID message
	_ = strings.TrimSpace("")

	// Validate invoke steps in tree nodes
	if len(rb.Tree) > 0 {
		var walkTree func(nodes []TreeNode, path string)
		walkTree = func(nodes []TreeNode, path string) {
			for i, n := range nodes {
				nodePath := fmt.Sprintf("%s[%d]", path, i)
				s := n.Step
				if s.Type == "xts" {
					if isV1 {
						errs = append(errs, &ValidationError{
							Phase:    "domain",
							Path:     nodePath + ".step.type",
							Message:  fmt.Sprintf("step %q uses type:xts which is not supported in runbook/v1 — use type:tool with an xts.tool.yaml definition", s.ID),
							Severity: "error",
						})
					} else {
						errs = append(errs, &ValidationError{
							Phase:    "domain",
							Path:     nodePath + ".step.type",
							Message:  fmt.Sprintf("step %q uses type:xts which is deprecated — consider migrating to type:tool with an xts.tool.yaml definition", s.ID),
							Severity: "warning",
						})
					}
				}
				if s.Type == "invoke" {
					if s.Invoke == nil {
						errs = append(errs, &ValidationError{
							Phase:    "domain",
							Path:     nodePath + ".step",
							Message:  fmt.Sprintf("invoke step %q requires 'invoke' configuration", s.ID),
							Severity: "error",
						})
					} else {
						if s.Invoke.Runbook == "" {
							errs = append(errs, &ValidationError{
								Phase:    "domain",
								Path:     nodePath + ".step.invoke.runbook",
								Message:  fmt.Sprintf("invoke step %q requires 'invoke.runbook'", s.ID),
								Severity: "error",
							})
						}
						// Check alias exists in imports
						if rb.Imports != nil && s.Invoke.Runbook != "" {
							_, isImport := rb.Imports[s.Invoke.Runbook]
							if !isImport && !strings.Contains(s.Invoke.Runbook, "/") && !strings.Contains(s.Invoke.Runbook, "\\") {
								errs = append(errs, &ValidationError{
									Phase:    "domain",
									Path:     nodePath + ".step.invoke.runbook",
									Message:  fmt.Sprintf("invoke step %q references %q which is not in imports and doesn't look like a file path", s.ID, s.Invoke.Runbook),
									Severity: "warning",
								})
							}
						}
					}
				}
				if s.Type == "tool" {
					errs = append(errs, validateToolStep(nodePath+".step", s, rb)...)
				}
				for _, b := range n.Branches {
					walkTree(b.Steps, nodePath+".branches")
				}
			}
		}
		walkTree(rb.Tree, "tree")
	}

	return errs
}

// validateXTSStep checks mode-specific field requirements for an XTS step.
func validateXTSStep(index int, s Step) []*ValidationError {
	var errs []*ValidationError
	xts := s.XTS
	prefix := fmt.Sprintf("steps[%d].xts", index)

	switch xts.Mode {
	case "view":
		if xts.File == "" {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     prefix + ".file",
				Message:  fmt.Sprintf("XTS step %q with mode=view requires 'file'", s.ID),
				Severity: "error",
			})
		}
	case "activity":
		if xts.File == "" {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     prefix + ".file",
				Message:  fmt.Sprintf("XTS step %q with mode=activity requires 'file'", s.ID),
				Severity: "error",
			})
		}
		if xts.Activity == "" {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     prefix + ".activity",
				Message:  fmt.Sprintf("XTS step %q with mode=activity requires 'activity'", s.ID),
				Severity: "error",
			})
		}
	case "query":
		if xts.QueryType == "" {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     prefix + ".query_type",
				Message:  fmt.Sprintf("XTS step %q with mode=query requires 'query_type'", s.ID),
				Severity: "error",
			})
		}
		if xts.Query == "" {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     prefix + ".query",
				Message:  fmt.Sprintf("XTS step %q with mode=query requires 'query'", s.ID),
				Severity: "error",
			})
		}
	}

	// Cross-field: activity/file fields should not appear in query mode
	if xts.Mode == "query" && xts.File != "" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     prefix + ".file",
			Message:  fmt.Sprintf("XTS step %q with mode=query should not specify 'file'", s.ID),
			Severity: "warning",
		})
	}
	if xts.Mode == "query" && xts.Activity != "" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     prefix + ".activity",
			Message:  fmt.Sprintf("XTS step %q with mode=query should not specify 'activity'", s.ID),
			Severity: "warning",
		})
	}

	return errs
}

// countAssertionFields returns the number of assertion fields set.
func countAssertionFields(a Assertion) int {
	count := 0
	if a.Contains != "" {
		count++
	}
	if a.NotContains != "" {
		count++
	}
	if a.Matches != "" {
		count++
	}
	if a.ExitCode != nil {
		count++
	}
	if a.Equals != "" {
		count++
	}
	if a.NotEquals != "" {
		count++
	}
	if a.JSONPath != nil {
		count++
	}
	return count
}

// validatePrecondition checks precondition field constraints.
func validatePrecondition(index int, s Step) []*ValidationError {
	var errs []*ValidationError
	prefix := fmt.Sprintf("steps[%d].precondition", index)
	pc := s.Precondition

	if len(pc.Check) == 0 {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     prefix + ".check",
			Message:  fmt.Sprintf("step %q precondition requires non-empty check command", s.ID),
			Severity: "error",
		})
	}

	if s.Type == "manual" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     prefix,
			Message:  fmt.Sprintf("step %q: precondition is not supported on manual steps", s.ID),
			Severity: "warning",
		})
	}

	return errs
}

// validateDomainWithPath extends ValidateDomain with path-aware validation
// (e.g. loading tool definitions relative to the runbook file).
func validateDomainWithPath(rb *Runbook, baseDir string) []*ValidationError {
	errs := ValidateDomain(rb)

	// Discover project context for package-aware tool resolution
	var proj *Project
	if baseDir != "" {
		proj, _ = DiscoverProject(baseDir)
		if proj == nil {
			proj = FallbackProject(baseDir)
		}
	}

	// Load and cache tool definitions for deep validation
	if len(rb.Tools) > 0 && baseDir != "" {
		toolDefs := make(map[string]*ToolDefinition)
		for _, name := range rb.Tools {
			resolved := ResolveToolPathCompat(proj, rb, name, baseDir)
			td, loadErr := LoadToolFile(resolved)
			if loadErr != nil {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     fmt.Sprintf("tools[%s]", name),
					Message:  fmt.Sprintf("failed to load tool definition for %q: %v", name, loadErr),
					Severity: "warning",
				})
				continue
			}
			tdErrs := ValidateToolDefinition(td)
			for _, e := range tdErrs {
				if e.Severity == "error" {
					errs = append(errs, &ValidationError{
						Phase:    "domain",
						Path:     fmt.Sprintf("tools[%s]", name),
						Message:  fmt.Sprintf("tool %q: %s", name, e.Message),
						Severity: "warning",
					})
				}
			}
			toolDefs[name] = td
		}

		// Deep-validate tool steps against loaded definitions
		errs = append(errs, validateToolStepsDeep(rb, toolDefs)...)
	}

	return errs
}

// validateToolStepsDeep checks tool step args/actions against loaded tool definitions.
func validateToolStepsDeep(rb *Runbook, toolDefs map[string]*ToolDefinition) []*ValidationError {
	var errs []*ValidationError

	checkStep := func(path string, s Step) {
		if s.Type != "tool" || s.Tool == nil || s.Tool.Name == "" {
			return
		}
		td, ok := toolDefs[s.Tool.Name]
		if !ok {
			return // tool def couldn't be loaded — already warned
		}
		if s.Tool.Action == "" {
			return // already reported by validateToolStep
		}
		act, ok := td.Actions[s.Tool.Action]
		if !ok {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     path + ".tool.action",
				Message:  fmt.Sprintf("tool %q has no action %q (available: %s)", s.Tool.Name, s.Tool.Action, joinKeys(td.Actions)),
				Severity: "error",
			})
			return
		}
		// Check required args
		for argName, argDef := range act.Args {
			if argDef.Required {
				if _, ok := s.Tool.Args[argName]; !ok {
					errs = append(errs, &ValidationError{
						Phase:    "domain",
						Path:     path + ".tool.args",
						Message:  fmt.Sprintf("required arg %q missing for %s.%s", argName, s.Tool.Name, s.Tool.Action),
						Severity: "error",
					})
				}
			}
		}
		// Check unknown args
		for argName := range s.Tool.Args {
			if _, ok := act.Args[argName]; !ok {
				errs = append(errs, &ValidationError{
					Phase:    "domain",
					Path:     path + ".tool.args." + argName,
					Message:  fmt.Sprintf("unknown arg %q for %s.%s", argName, s.Tool.Name, s.Tool.Action),
					Severity: "warning",
				})
			}
		}
		// Check enum values (skip template expressions)
		for argName, argVal := range s.Tool.Args {
			if argDef, ok := act.Args[argName]; ok && len(argDef.Enum) > 0 {
				if !strings.Contains(argVal, "{{") {
					valid := false
					for _, e := range argDef.Enum {
						if argVal == e {
							valid = true
							break
						}
					}
					if !valid {
						errs = append(errs, &ValidationError{
							Phase:    "domain",
							Path:     path + ".tool.args." + argName,
							Message:  fmt.Sprintf("arg %q value %q is not in enum %v", argName, argVal, argDef.Enum),
							Severity: "error",
						})
					}
				}
			}
		}
	}

	// Check flat steps
	for i, s := range rb.Steps {
		checkStep(fmt.Sprintf("steps[%d]", i), s)
	}

	// Check tree steps
	var walkTree func(nodes []TreeNode, path string)
	walkTree = func(nodes []TreeNode, path string) {
		for i, n := range nodes {
			nodePath := fmt.Sprintf("%s[%d]", path, i)
			checkStep(nodePath+".step", n.Step)
			for _, b := range n.Branches {
				walkTree(b.Steps, nodePath+".branches")
			}
		}
	}
	if len(rb.Tree) > 0 {
		walkTree(rb.Tree, "tree")
	}

	return errs
}

// joinKeys returns comma-separated keys of a map for error messages.
func joinKeys[T any](m map[string]T) string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	return strings.Join(keys, ", ")
}

// validateToolStep checks type:tool step constraints.
func validateToolStep(path string, s Step, rb *Runbook) []*ValidationError {
	var errs []*ValidationError

	if s.Tool == nil {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     path,
			Message:  fmt.Sprintf("tool step %q requires 'tool' configuration", s.ID),
			Severity: "error",
		})
		return errs
	}

	if s.Tool.Name == "" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     path + ".tool.name",
			Message:  fmt.Sprintf("tool step %q requires 'tool.name'", s.ID),
			Severity: "error",
		})
	}

	if s.Tool.Action == "" {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     path + ".tool.action",
			Message:  fmt.Sprintf("tool step %q requires 'tool.action'", s.ID),
			Severity: "error",
		})
	}

	// Check tool.name references a name in tools: list
	if s.Tool.Name != "" && rb.Tools != nil {
		if !slices.Contains(rb.Tools, s.Tool.Name) {
			errs = append(errs, &ValidationError{
				Phase:    "domain",
				Path:     path + ".tool.name",
				Message:  fmt.Sprintf("tool step %q references tool %q which is not declared in 'tools:'", s.ID, s.Tool.Name),
				Severity: "error",
			})
		}
	} else if s.Tool.Name != "" && rb.Tools == nil {
		errs = append(errs, &ValidationError{
			Phase:    "domain",
			Path:     path + ".tool.name",
			Message:  fmt.Sprintf("tool step %q references tool %q but no 'tools:' list is declared", s.ID, s.Tool.Name),
			Severity: "error",
		})
	}

	return errs
}
