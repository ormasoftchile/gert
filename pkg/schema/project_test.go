package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadProjectFile(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "gert.yaml")
	os.WriteFile(manifest, []byte(`
name: test-project
paths:
  tools: my-tools
  runbooks: my-runbooks
require:
  shared: ../shared
`), 0644)

	proj, err := LoadProjectFile(manifest)
	if err != nil {
		t.Fatalf("LoadProjectFile: %v", err)
	}
	if proj.Name != "test-project" {
		t.Fatalf("name=%q want test-project", proj.Name)
	}
	if proj.ToolsDir() != "my-tools" {
		t.Fatalf("ToolsDir=%q want my-tools", proj.ToolsDir())
	}
	if proj.RunbooksDir() != "my-runbooks" {
		t.Fatalf("RunbooksDir=%q want my-runbooks", proj.RunbooksDir())
	}
	if proj.Require["shared"] != "../shared" {
		t.Fatalf("require[shared]=%q want ../shared", proj.Require["shared"])
	}
	if proj.Root != dir {
		t.Fatalf("Root=%q want %q", proj.Root, dir)
	}
}

func TestLoadProjectFileMissingName(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "gert.yaml")
	os.WriteFile(manifest, []byte(`require: {}`), 0644)

	_, err := LoadProjectFile(manifest)
	if err == nil {
		t.Fatal("expected error for missing name")
	}
}

func TestProjectDefaults(t *testing.T) {
	proj := &Project{Name: "test"}
	if proj.ToolsDir() != "tools" {
		t.Fatalf("default ToolsDir=%q", proj.ToolsDir())
	}
	if proj.RunbooksDir() != "runbooks" {
		t.Fatalf("default RunbooksDir=%q", proj.RunbooksDir())
	}
}

func TestNilProjectDefaults(t *testing.T) {
	var proj *Project
	if proj.ToolsDir() != "tools" {
		t.Fatalf("nil ToolsDir=%q", proj.ToolsDir())
	}
	if proj.RunbooksDir() != "runbooks" {
		t.Fatalf("nil RunbooksDir=%q", proj.RunbooksDir())
	}
}

func TestDiscoverProject(t *testing.T) {
	// Create a temp dir structure: root/sub/sub2/runbook.yaml + root/gert.yaml
	root := t.TempDir()
	sub := filepath.Join(root, "sub", "sub2")
	os.MkdirAll(sub, 0755)

	os.WriteFile(filepath.Join(root, "gert.yaml"), []byte("name: my-project\n"), 0644)
	runbook := filepath.Join(sub, "test.runbook.yaml")
	os.WriteFile(runbook, []byte("apiVersion: runbook/v0\n"), 0644)

	proj, err := DiscoverProject(runbook)
	if err != nil {
		t.Fatalf("DiscoverProject: %v", err)
	}
	if proj == nil {
		t.Fatal("expected project, got nil")
	}
	if proj.Name != "my-project" {
		t.Fatalf("name=%q want my-project", proj.Name)
	}
	if proj.Root != root {
		t.Fatalf("Root=%q want %q", proj.Root, root)
	}
}

func TestDiscoverProjectNotFound(t *testing.T) {
	dir := t.TempDir()
	proj, err := DiscoverProject(dir)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if proj != nil {
		t.Fatal("expected nil project when no gert.yaml found")
	}
}

func TestFallbackProject(t *testing.T) {
	dir := t.TempDir()
	proj := FallbackProject(dir)
	if proj.Root != dir {
		t.Fatalf("Root=%q want %q", proj.Root, dir)
	}
	if proj.Name != filepath.Base(dir) {
		t.Fatalf("Name=%q want %q", proj.Name, filepath.Base(dir))
	}
}

func TestResolveToolRefLocal(t *testing.T) {
	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	os.MkdirAll(toolsDir, 0755)
	os.WriteFile(filepath.Join(toolsDir, "nslookup.tool.yaml"), []byte("apiVersion: tool/v0\n"), 0644)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	resolved, err := proj.ResolveToolRef("nslookup")
	if err != nil {
		t.Fatalf("ResolveToolRef: %v", err)
	}
	expected := filepath.Join(root, "tools", "nslookup.tool.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveToolRefLocalNotFound(t *testing.T) {
	root := t.TempDir()
	os.MkdirAll(filepath.Join(root, "tools"), 0755)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	_, err := proj.ResolveToolRef("nonexistent")
	if err == nil {
		t.Fatal("expected error for missing tool")
	}
}

func TestResolveToolRefQualified(t *testing.T) {
	// Set up main project and dependency package
	mainRoot := t.TempDir()
	depRoot := t.TempDir()

	// Create dep package with a tool
	depTools := filepath.Join(depRoot, "tools")
	os.MkdirAll(depTools, 0755)
	os.WriteFile(filepath.Join(depTools, "xts.tool.yaml"), []byte("apiVersion: tool/v0\n"), 0644)
	os.WriteFile(filepath.Join(depRoot, "gert.yaml"), []byte("name: dep-pkg\n"), 0644)

	proj := &Project{
		Name:    "main",
		Root:    mainRoot,
		Require: map[string]string{"dep-pkg": depRoot},
	}

	resolved, err := proj.ResolveToolRef("dep-pkg/xts")
	if err != nil {
		t.Fatalf("ResolveToolRef: %v", err)
	}
	expected := filepath.Join(depRoot, "tools", "xts.tool.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveToolRefQualifiedWithExports(t *testing.T) {
	mainRoot := t.TempDir()
	depRoot := t.TempDir()

	// Create dep package with tools in a nested path
	depTools := filepath.Join(depRoot, "tools", "queries")
	os.MkdirAll(depTools, 0755)
	os.WriteFile(filepath.Join(depTools, "kusto-query.tool.yaml"), []byte("apiVersion: tool/v0\n"), 0644)
	os.WriteFile(filepath.Join(depRoot, "gert.yaml"), []byte(`
name: dep-pkg
exports:
  tools:
    kusto: queries/kusto-query
`), 0644)

	proj := &Project{
		Name:    "main",
		Root:    mainRoot,
		Require: map[string]string{"dep-pkg": depRoot},
	}

	resolved, err := proj.ResolveToolRef("dep-pkg/kusto")
	if err != nil {
		t.Fatalf("ResolveToolRef: %v", err)
	}
	expected := filepath.Join(depRoot, "tools", "queries", "kusto-query.tool.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveToolRefUnknownPackage(t *testing.T) {
	proj := &Project{
		Name: "main",
		Root: t.TempDir(),
	}

	_, err := proj.ResolveToolRef("unknown-pkg/tool")
	if err == nil {
		t.Fatal("expected error for unknown package")
	}
}

func TestResolveRunbookRefLocal(t *testing.T) {
	root := t.TempDir()
	rbDir := filepath.Join(root, "runbooks")
	os.MkdirAll(rbDir, 0755)
	os.WriteFile(filepath.Join(rbDir, "dns-check.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	resolved, err := proj.ResolveRunbookRef("dns-check")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	expected := filepath.Join(root, "runbooks", "dns-check.runbook.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveRunbookRefGroupPath(t *testing.T) {
	root := t.TempDir()
	rbDir := filepath.Join(root, "runbooks", "connectivity")
	os.MkdirAll(rbDir, 0755)
	os.WriteFile(filepath.Join(rbDir, "dns-check.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	resolved, err := proj.ResolveRunbookRef("connectivity/dns-check")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	expected := filepath.Join(root, "runbooks", "connectivity", "dns-check.runbook.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveRunbookRefQualified(t *testing.T) {
	mainRoot := t.TempDir()
	depRoot := t.TempDir()

	depRBs := filepath.Join(depRoot, "runbooks")
	os.MkdirAll(depRBs, 0755)
	os.WriteFile(filepath.Join(depRBs, "reboot.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)
	os.WriteFile(filepath.Join(depRoot, "gert.yaml"), []byte("name: dep-pkg\n"), 0644)

	proj := &Project{
		Name:    "main",
		Root:    mainRoot,
		Require: map[string]string{"dep-pkg": depRoot},
	}

	resolved, err := proj.ResolveRunbookRef("dep-pkg/reboot")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	expected := filepath.Join(depRoot, "runbooks", "reboot.runbook.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveRunbookRefAmbiguityLocalWins(t *testing.T) {
	root := t.TempDir()
	depRoot := t.TempDir()

	// Create local runbook at runbooks/dep-pkg/something.runbook.yaml
	localDir := filepath.Join(root, "runbooks", "dep-pkg")
	os.MkdirAll(localDir, 0755)
	os.WriteFile(filepath.Join(localDir, "something.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)

	// Also create dep package with runbooks/something.runbook.yaml
	depRBs := filepath.Join(depRoot, "runbooks")
	os.MkdirAll(depRBs, 0755)
	os.WriteFile(filepath.Join(depRBs, "something.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)
	os.WriteFile(filepath.Join(depRoot, "gert.yaml"), []byte("name: dep-pkg\n"), 0644)

	proj := &Project{
		Name:    "main",
		Root:    root,
		Require: map[string]string{"dep-pkg": depRoot},
	}

	// Local path should win (ambiguity rule)
	resolved, err := proj.ResolveRunbookRef("dep-pkg/something")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	// Should resolve to local, not dep package
	expected := filepath.Join(root, "runbooks", "dep-pkg", "something.runbook.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want local %q", resolved, expected)
	}
}

func TestResolveToolPathCompat(t *testing.T) {
	root := t.TempDir()
	toolsDir := filepath.Join(root, "tools")
	os.MkdirAll(toolsDir, 0755)
	os.WriteFile(filepath.Join(toolsDir, "curl.tool.yaml"), []byte("apiVersion: tool/v0\n"), 0644)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	rb := &Runbook{Tools: []string{"curl"}}

	resolved := ResolveToolPathCompat(proj, rb, "curl", root)
	expected := filepath.Join(root, "tools", "curl.tool.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveToolPathCompatLegacyFallback(t *testing.T) {
	// Project resolution fails (tool not in project's tools dir),
	// falls back to legacy rb.ResolveToolPath
	root := t.TempDir()
	proj := &Project{
		Name: "test",
		Root: root,
	}

	// Create tool in a legacy location
	legacyDir := filepath.Join(root, "custom")
	os.MkdirAll(legacyDir, 0755)

	rb := &Runbook{
		Tools:     []string{"mytool"},
		ToolPaths: map[string]string{"mytool": filepath.Join("custom", "mytool.tool.yaml")},
	}

	resolved := ResolveToolPathCompat(proj, rb, "mytool", root)
	expected := filepath.Join(root, "custom", "mytool.tool.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveToolPathCompatNilProject(t *testing.T) {
	root := t.TempDir()
	rb := &Runbook{Tools: []string{"curl"}}

	resolved := ResolveToolPathCompat(nil, rb, "curl", root)
	expected := filepath.Join(root, "tools", "curl.tool.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestProjectCustomPaths(t *testing.T) {
	root := t.TempDir()

	// Create tool in custom path
	customTools := filepath.Join(root, "my-tools")
	os.MkdirAll(customTools, 0755)
	os.WriteFile(filepath.Join(customTools, "check.tool.yaml"), []byte("apiVersion: tool/v0\n"), 0644)

	// Create runbook in custom path
	customRBs := filepath.Join(root, "TSG", "connection")
	os.MkdirAll(customRBs, 0755)
	os.WriteFile(filepath.Join(customRBs, "login.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)

	proj := &Project{
		Name: "custom",
		Root: root,
		Paths: ProjectPaths{
			Tools:    "my-tools",
			Runbooks: "TSG",
		},
	}

	// Tool should resolve using custom path
	toolPath, err := proj.ResolveToolRef("check")
	if err != nil {
		t.Fatalf("ResolveToolRef: %v", err)
	}
	if toolPath != filepath.Join(customTools, "check.tool.yaml") {
		t.Fatalf("tool=%q want %q", toolPath, filepath.Join(customTools, "check.tool.yaml"))
	}

	// Runbook should resolve using custom path
	rbPath, err := proj.ResolveRunbookRef("connection/login")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	if rbPath != filepath.Join(customRBs, "login.runbook.yaml") {
		t.Fatalf("runbook=%q want %q", rbPath, filepath.Join(customRBs, "login.runbook.yaml"))
	}
}

func TestSplitRef(t *testing.T) {
	tests := []struct {
		ref      string
		prefix   string
		rest     string
		hasSlash bool
	}{
		{"nslookup", "", "nslookup", false},
		{"gert-xts/xts", "gert-xts", "xts", true},
		{"connectivity/dns-check", "connectivity", "dns-check", true},
		{"a/b/c", "a", "b/c", true},
		{" nslookup ", "", "nslookup", false},
	}

	for _, tt := range tests {
		prefix, rest, hasSlash := splitRef(tt.ref)
		if prefix != tt.prefix || rest != tt.rest || hasSlash != tt.hasSlash {
			t.Errorf("splitRef(%q)=(%q,%q,%v) want (%q,%q,%v)",
				tt.ref, prefix, rest, hasSlash, tt.prefix, tt.rest, tt.hasSlash)
		}
	}
}

func TestProjectWithConfig(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "gert.yaml")
	os.WriteFile(manifest, []byte(`
name: test-with-config
config:
  providers:
    icm:
      binary: gert-icm-provider
      config: configs/icm.provider.yaml
`), 0644)

	proj, err := LoadProjectFile(manifest)
	if err != nil {
		t.Fatalf("LoadProjectFile: %v", err)
	}
	if proj.Config == nil {
		t.Fatal("expected config section")
	}
	icm, ok := proj.Config.Providers["icm"]
	if !ok {
		t.Fatal("expected icm provider")
	}
	if icm.Binary != "gert-icm-provider" {
		t.Fatalf("icm.Binary=%q want gert-icm-provider", icm.Binary)
	}
}

func TestProjectWithExports(t *testing.T) {
	dir := t.TempDir()
	manifest := filepath.Join(dir, "gert.yaml")
	os.WriteFile(manifest, []byte(`
name: tool-pkg
exports:
  tools:
    kusto: queries/kusto-query
    xts: providers/xts
`), 0644)

	proj, err := LoadProjectFile(manifest)
	if err != nil {
		t.Fatalf("LoadProjectFile: %v", err)
	}
	if proj.Exports == nil {
		t.Fatal("expected exports")
	}
	if proj.Exports.Tools["kusto"] != "queries/kusto-query" {
		t.Fatalf("exports.tools.kusto=%q", proj.Exports.Tools["kusto"])
	}
	if proj.Exports.Tools["xts"] != "providers/xts" {
		t.Fatalf("exports.tools.xts=%q", proj.Exports.Tools["xts"])
	}
}

func TestResolveRunbookRefDirectoryConvention(t *testing.T) {
	root := t.TempDir()
	// Create dns-check/dns-check.runbook.yaml (no flat file)
	rbDir := filepath.Join(root, "runbooks", "dns-check")
	os.MkdirAll(rbDir, 0755)
	os.WriteFile(filepath.Join(rbDir, "dns-check.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	// Unqualified ref should find it via directory convention
	resolved, err := proj.ResolveRunbookRef("dns-check")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	expected := filepath.Join(root, "runbooks", "dns-check", "dns-check.runbook.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveRunbookRefDirectoryConventionGroupPath(t *testing.T) {
	root := t.TempDir()
	// Create connectivity/dns-check/dns-check.runbook.yaml
	rbDir := filepath.Join(root, "runbooks", "connectivity", "dns-check")
	os.MkdirAll(rbDir, 0755)
	os.WriteFile(filepath.Join(rbDir, "dns-check.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	// Group path ref should also use directory convention
	resolved, err := proj.ResolveRunbookRef("connectivity/dns-check")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	expected := filepath.Join(root, "runbooks", "connectivity", "dns-check", "dns-check.runbook.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want %q", resolved, expected)
	}
}

func TestResolveRunbookRefFlatWinsOverDirectory(t *testing.T) {
	root := t.TempDir()
	rbDir := filepath.Join(root, "runbooks")
	os.MkdirAll(rbDir, 0755)
	// Both flat file and directory exist — flat wins
	os.WriteFile(filepath.Join(rbDir, "check.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)
	dirRB := filepath.Join(rbDir, "check")
	os.MkdirAll(dirRB, 0755)
	os.WriteFile(filepath.Join(dirRB, "check.runbook.yaml"), []byte("apiVersion: runbook/v0\n"), 0644)

	proj := &Project{
		Name: "test",
		Root: root,
	}

	resolved, err := proj.ResolveRunbookRef("check")
	if err != nil {
		t.Fatalf("ResolveRunbookRef: %v", err)
	}
	// Flat file wins
	expected := filepath.Join(root, "runbooks", "check.runbook.yaml")
	if resolved != expected {
		t.Fatalf("resolved=%q want flat %q", resolved, expected)
	}
}
