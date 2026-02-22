package schema

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// Project represents a gert.yaml manifest — the single configuration surface
// for a package: identity, dependencies, path conventions, runtime config,
// and tool exports.
type Project struct {
	Name    string            `yaml:"name"              json:"name"`
	Paths   ProjectPaths      `yaml:"paths,omitempty"   json:"paths,omitempty"`
	Require map[string]string `yaml:"require,omitempty" json:"require,omitempty"`
	Exports *ProjectExports   `yaml:"exports,omitempty" json:"exports,omitempty"`
	Config  *ProjectConfig    `yaml:"config,omitempty"  json:"config,omitempty"`

	// Root is the absolute path to the directory containing gert.yaml.
	// Set after loading/discovery, not from YAML.
	Root string `yaml:"-" json:"-"`

	// packages caches resolved required packages (lazily populated).
	packages map[string]*Project
}

// ProjectPaths overrides convention directories. Defaults: tools → "tools", runbooks → "runbooks".
type ProjectPaths struct {
	Tools    string `yaml:"tools,omitempty"    json:"tools,omitempty"`
	Runbooks string `yaml:"runbooks,omitempty" json:"runbooks,omitempty"`
}

// ProjectExports controls which tools a package exposes to consumers.
type ProjectExports struct {
	Tools map[string]string `yaml:"tools,omitempty" json:"tools,omitempty"`
}

// ProjectConfig holds runtime configuration (replaces .gert/config.yaml).
type ProjectConfig struct {
	Providers map[string]ProviderConfig `yaml:"providers,omitempty" json:"providers,omitempty"`
}

// ProviderConfig is the configuration for an external provider.
type ProviderConfig struct {
	Binary string `yaml:"binary,omitempty" json:"binary,omitempty"`
	Path   string `yaml:"path,omitempty"   json:"path,omitempty"`
	Config string `yaml:"config,omitempty" json:"config,omitempty"`
}

// ToolsDir returns the effective tools directory name (default: "tools").
func (p *Project) ToolsDir() string {
	if p != nil && p.Paths.Tools != "" {
		return p.Paths.Tools
	}
	return "tools"
}

// RunbooksDir returns the effective runbooks directory name (default: "runbooks").
func (p *Project) RunbooksDir() string {
	if p != nil && p.Paths.Runbooks != "" {
		return p.Paths.Runbooks
	}
	return "runbooks"
}

// ResolveToolRef resolves a tool reference to an absolute filesystem path.
//
// Unqualified name ("nslookup"):
//
//	→ <Root>/<ToolsDir>/nslookup.tool.yaml
//
// Qualified name ("gert-xts/xts"):
//
//	→ look up "gert-xts" in require → resolve package → <pkg>/tools/xts.tool.yaml
//	→ or via exports.tools virtual mapping
func (p *Project) ResolveToolRef(ref string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("no project context")
	}

	prefix, name, qualified := splitRef(ref)

	if !qualified {
		// Unqualified: local tool
		candidate := filepath.Join(p.Root, p.ToolsDir(), name+".tool.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return candidate, nil
		}
		return "", fmt.Errorf("tool %q not found in package %q (looked in %s)",
			name, p.Name, filepath.Join(p.Root, p.ToolsDir()))
	}

	// Qualified: look up package
	pkg, err := p.resolvePackage(prefix)
	if err != nil {
		return "", fmt.Errorf("tool %q: %w", ref, err)
	}

	// Check exports.tools virtual mapping first
	if pkg.Exports != nil && pkg.Exports.Tools != nil {
		if mapped, ok := pkg.Exports.Tools[name]; ok {
			candidate := filepath.Join(pkg.Root, pkg.ToolsDir(), mapped+".tool.yaml")
			if _, err := os.Stat(candidate); err == nil {
				return candidate, nil
			}
			return "", fmt.Errorf("tool %q: exported path %q not found in package %q",
				ref, mapped, prefix)
		}
	}

	// Direct resolution
	candidate := filepath.Join(pkg.Root, pkg.ToolsDir(), name+".tool.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}
	return "", fmt.Errorf("tool %q not found in package %q (looked in %s)",
		name, prefix, filepath.Join(pkg.Root, pkg.ToolsDir()))
}

// ResolveRunbookRef resolves a runbook reference to an absolute filesystem path.
//
// Unqualified name ("dns-check"):
//
//	→ <Root>/<RunbooksDir>/dns-check.runbook.yaml
//
// Group path ("connectivity/dns-check"):
//
//	→ <Root>/<RunbooksDir>/connectivity/dns-check.runbook.yaml
//
// Qualified name ("other-pkg/runbook-name"):
//
//	→ look up "other-pkg" in require → <pkg>/runbooks/runbook-name.runbook.yaml
func (p *Project) ResolveRunbookRef(ref string) (string, error) {
	if p == nil {
		return "", fmt.Errorf("no project context")
	}

	prefix, rest, hasSlash := splitRef(ref)

	if !hasSlash {
		// Unqualified: local runbook
		return p.findLocalRunbook(ref)
	}

	// Has slash — could be local group path or qualified package ref.
	// Local path wins (ambiguity rule from spec).
	if localPath, err := p.findLocalRunbook(ref); err == nil {
		return localPath, nil
	}

	// Try as qualified package ref
	if _, hasReq := p.Require[prefix]; hasReq {
		pkg, err := p.resolvePackage(prefix)
		if err != nil {
			return "", fmt.Errorf("runbook %q: %w", ref, err)
		}
		if resolved, err := pkg.findLocalRunbook(rest); err == nil {
			return resolved, nil
		}
		return "", fmt.Errorf("runbook %q not found in package %q", rest, prefix)
	}

	// Neither local nor a known package
	return "", fmt.Errorf("runbook %q not found locally and %q is not a required package", ref, prefix)
}

// findLocalRunbook searches for a runbook within this project's runbooks directory.
// Resolution order:
//  1. <runbooksDir>/<ref>.runbook.yaml              (flat file)
//  2. <runbooksDir>/<ref>/<basename>.runbook.yaml   (directory convention: dir name = runbook name)
func (p *Project) findLocalRunbook(ref string) (string, error) {
	rbDir := filepath.Join(p.Root, p.RunbooksDir())

	// 1. Flat file
	candidate := filepath.Join(rbDir, ref+".runbook.yaml")
	if _, err := os.Stat(candidate); err == nil {
		return candidate, nil
	}

	// 2. Directory convention: dns-check → dns-check/dns-check.runbook.yaml
	base := filepath.Base(ref)
	dirCandidate := filepath.Join(rbDir, ref, base+".runbook.yaml")
	if _, err := os.Stat(dirCandidate); err == nil {
		return dirCandidate, nil
	}

	return "", fmt.Errorf("runbook %q not found in %s", ref, rbDir)
}

// resolvePackage loads a required package by name, caching the result.
func (p *Project) resolvePackage(name string) (*Project, error) {
	if p.packages != nil {
		if pkg, ok := p.packages[name]; ok {
			return pkg, nil
		}
	}

	reqPath, ok := p.Require[name]
	if !ok {
		return nil, fmt.Errorf("unknown package %q (not declared in require)", name)
	}

	// Resolve relative to this project's root
	absPath := reqPath
	if !filepath.IsAbs(reqPath) {
		absPath = filepath.Join(p.Root, reqPath)
	}
	absPath = filepath.Clean(absPath)

	// Try loading gert.yaml from the target
	pkg, err := loadProjectFromPath(absPath)
	if err != nil {
		// Fallback: treat the path as a bare directory (no gert.yaml)
		pkg = &Project{
			Name: name,
			Root: absPath,
		}
	}

	// Cache
	if p.packages == nil {
		p.packages = make(map[string]*Project)
	}
	p.packages[name] = pkg
	return pkg, nil
}

// splitRef splits a reference on the first "/" into (prefix, rest, hasSlash).
func splitRef(ref string) (prefix, rest string, hasSlash bool) {
	ref = strings.TrimSpace(ref)
	if i := strings.IndexByte(ref, '/'); i >= 0 {
		return ref[:i], ref[i+1:], true
	}
	return "", ref, false
}

// LoadProjectFile reads and parses a gert.yaml manifest.
func LoadProjectFile(path string) (*Project, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read project manifest: %w", err)
	}

	var proj Project
	if err := yaml.Unmarshal(data, &proj); err != nil {
		return nil, fmt.Errorf("parse project manifest: %w", err)
	}

	if proj.Name == "" {
		return nil, fmt.Errorf("project manifest %s: name is required", path)
	}

	proj.Root = filepath.Dir(path)
	return &proj, nil
}

// DiscoverProject walks up from startPath to find the nearest gert.yaml.
// Returns nil (no error) if no manifest is found — the caller should
// use FallbackProject in that case.
func DiscoverProject(startPath string) (*Project, error) {
	abs, err := filepath.Abs(startPath)
	if err != nil {
		return nil, err
	}

	// If startPath is a file, start from its directory
	info, err := os.Stat(abs)
	if err != nil {
		return nil, err
	}
	dir := abs
	if !info.IsDir() {
		dir = filepath.Dir(abs)
	}

	for {
		candidate := filepath.Join(dir, "gert.yaml")
		if _, err := os.Stat(candidate); err == nil {
			return LoadProjectFile(candidate)
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			// Reached filesystem root
			return nil, nil
		}
		dir = parent
	}
}

// FallbackProject creates a minimal project rooted at the given directory
// with default conventions. Used when no gert.yaml is found.
func FallbackProject(dir string) *Project {
	abs, err := filepath.Abs(dir)
	if err != nil {
		abs = dir
	}
	return &Project{
		Name: filepath.Base(abs),
		Root: abs,
	}
}

// loadProjectFromPath attempts to load a gert.yaml from a directory.
func loadProjectFromPath(dir string) (*Project, error) {
	candidate := filepath.Join(dir, "gert.yaml")
	return LoadProjectFile(candidate)
}

// ResolveToolPathCompat resolves a tool reference using project context when
// available, falling back to the runbook's legacy ResolveToolPath for v0
// compatibility. This is the function that all call sites should use.
//
// Parameters:
//   - proj: project context (may be nil for standalone/legacy runbooks)
//   - rb: the runbook declaring the tool
//   - name: tool reference (e.g. "nslookup" or "gert-xts/xts")
//   - baseDir: directory of the runbook file (used for legacy fallback)
func ResolveToolPathCompat(proj *Project, rb *Runbook, name, baseDir string) string {
	// Try project-based resolution first
	if proj != nil {
		if resolved, err := proj.ResolveToolRef(name); err == nil {
			return resolved
		}
	}

	// Legacy fallback: use runbook's ToolPaths map or convention
	legacyPath := rb.ResolveToolPath(name)
	if filepath.IsAbs(legacyPath) {
		return legacyPath
	}
	return filepath.Join(baseDir, legacyPath)
}

// ResolveRunbookPathCompat resolves a runbook reference using project context
// when available, falling back to legacy filesystem-relative resolution.
func ResolveRunbookPathCompat(proj *Project, ref, baseDir string) string {
	// Try project-based resolution first
	if proj != nil {
		if resolved, err := proj.ResolveRunbookRef(ref); err == nil {
			return resolved
		}
	}

	// Legacy fallback: normalize and resolve relative to baseDir
	normalized := normalizeRunbookRefPath(ref)
	if filepath.IsAbs(normalized) {
		return normalized
	}
	return filepath.Join(baseDir, normalized)
}
