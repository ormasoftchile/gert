// Package validate implements the kernel/v0 3-phase validation pipeline:
// structural → semantic → domain.
package validate

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// ValidationError represents one error or warning from the validation pipeline.
type ValidationError struct {
	Phase    string `json:"phase"` // structural, semantic, domain
	Path     string `json:"path"`  // JSON-path-like location
	Message  string `json:"message"`
	Severity string `json:"severity"` // error, warning
}

func (e *ValidationError) Error() string {
	if e.Path != "" {
		return fmt.Sprintf("[%s] %s at %s", e.Phase, e.Message, e.Path)
	}
	return fmt.Sprintf("[%s] %s", e.Phase, e.Message)
}

func errorf(phase, path, msg string, args ...any) *ValidationError {
	return &ValidationError{
		Phase:    phase,
		Path:     path,
		Message:  fmt.Sprintf(msg, args...),
		Severity: "error",
	}
}

func warningf(phase, path, msg string, args ...any) *ValidationError {
	return &ValidationError{
		Phase:    phase,
		Path:     path,
		Message:  fmt.Sprintf(msg, args...),
		Severity: "warning",
	}
}

// ValidateFile runs the full 3-phase pipeline on a runbook file.
func ValidateFile(path string) (*schema.Runbook, []*ValidationError) {
	// Phase 1: Structural (strict YAML decode)
	rb, err := schema.LoadFile(path)
	if err != nil {
		return nil, []*ValidationError{errorf("structural", "", "failed to load: %s", err)}
	}

	var errs []*ValidationError

	// Phase 2: Semantic (JSON Schema validation)
	errs = append(errs, validateSemantic(rb)...)

	// If we have structural/semantic errors, don't proceed to domain
	if hasErrors(errs) {
		return rb, errs
	}

	// Phase 3: Domain (hand-coded rules)
	baseDir := ""
	if path != "" {
		baseDir = filepath.Dir(path)
	}
	errs = append(errs, validateDomain(rb, baseDir)...)

	return rb, errs
}

// ValidateRunbook runs phases 2+3 on an already-loaded runbook.
func ValidateRunbook(rb *schema.Runbook, baseDir string) []*ValidationError {
	var errs []*ValidationError
	errs = append(errs, validateSemantic(rb)...)
	if hasErrors(errs) {
		return errs
	}
	errs = append(errs, validateDomain(rb, baseDir)...)
	return errs
}

// ValidateToolFile runs the pipeline on a tool definition file.
func ValidateToolFile(path string) (*schema.ToolDefinition, []*ValidationError) {
	td, err := schema.LoadToolFile(path)
	if err != nil {
		return nil, []*ValidationError{errorf("structural", "", "failed to load tool: %s", err)}
	}
	return td, validateToolDomain(td)
}

func hasErrors(errs []*ValidationError) bool {
	for _, e := range errs {
		if e.Severity == "error" {
			return true
		}
	}
	return false
}

// ResolveToolPath resolves a tool name to a file path using kernel/v0 conventions.
// Resolution order:
//  1. tools/<name>.tool.yaml relative to baseDir
//  2. <projectRoot>/tools/<name>.tool.yaml
//  3. Direct path if it contains a separator
func ResolveToolPath(name, baseDir, projectRoot string) string {
	// Direct path
	if containsSep(name) {
		return name
	}

	// 1. Relative to runbook
	candidate := filepath.Join(baseDir, "tools", name+".tool.yaml")
	if fileExists(candidate) {
		return candidate
	}

	// 2. Project root
	if projectRoot != "" {
		candidate = filepath.Join(projectRoot, "tools", name+".tool.yaml")
		if fileExists(candidate) {
			return candidate
		}
	}

	return ""
}

func containsSep(s string) bool {
	for _, c := range s {
		if c == '/' || c == '\\' {
			return true
		}
	}
	return false
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}
