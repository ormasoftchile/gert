package schema

import (
	"os"
	"path/filepath"
	"testing"
)

func TestRunbook_Unmarshal_ShorthandImportsAndTools(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rb.yaml")
	yaml := `apiVersion: runbook/v0
imports: dns-check
tools: curl
meta:
  name: test
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write temp runbook: %v", err)
	}

	rb, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if got, ok := rb.Imports["dns-check"]; !ok {
		t.Fatalf("expected import alias dns-check")
	} else if filepath.Clean(got) != filepath.Clean(filepath.Join("..", "dns-check", "dns-check.runbook.yaml")) {
		t.Fatalf("imports[dns-check]=%q", got)
	}

	if len(rb.Tools) != 1 || rb.Tools[0] != "curl" {
		t.Fatalf("tools=%v, want [curl]", rb.Tools)
	}
	if rb.ToolPaths != nil && rb.ToolPaths["curl"] != "" {
		t.Fatalf("unexpected explicit tool path for curl: %q", rb.ToolPaths["curl"])
	}
}

func TestRunbook_Unmarshal_VerboseAndMixedForms(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rb.yaml")
	yaml := `apiVersion: runbook/v0
imports:
  - dns-check
  - name: db-check
    path: ../db-check/custom.runbook.yaml
tools:
  curl: tools/curl.tool.yaml
  nslookup:
    path: custom/nslookup.tool.yaml
meta:
  name: test
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write temp runbook: %v", err)
	}

	rb, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if got := rb.Imports["dns-check"]; filepath.Clean(got) != filepath.Clean(filepath.Join("..", "dns-check", "dns-check.runbook.yaml")) {
		t.Fatalf("imports[dns-check]=%q", got)
	}
	if got := rb.Imports["db-check"]; filepath.Clean(got) != filepath.Clean(filepath.Join("..", "db-check", "custom.runbook.yaml")) {
		t.Fatalf("imports[db-check]=%q", got)
	}

	if len(rb.Tools) != 2 {
		t.Fatalf("tools=%v, want 2 entries", rb.Tools)
	}
	if filepath.Clean(rb.ResolveToolPath("curl")) != filepath.Clean(filepath.Join("tools", "curl.tool.yaml")) {
		t.Fatalf("resolve curl path=%q", rb.ResolveToolPath("curl"))
	}
	if filepath.Clean(rb.ResolveToolPath("nslookup")) != filepath.Clean(filepath.Join("custom", "nslookup.tool.yaml")) {
		t.Fatalf("resolve nslookup path=%q", rb.ResolveToolPath("nslookup"))
	}
}

func TestRunbook_Imports_PathWithoutExtension(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "rb.yaml")
	yaml := `apiVersion: runbook/v0
imports:
  dns-check: ../dns-check/dns-check
  db-check:
    path: ../db-check/custom
meta:
  name: test
`
	if err := os.WriteFile(path, []byte(yaml), 0644); err != nil {
		t.Fatalf("write temp runbook: %v", err)
	}

	rb, err := LoadFile(path)
	if err != nil {
		t.Fatalf("LoadFile failed: %v", err)
	}

	if got := rb.Imports["dns-check"]; filepath.Clean(got) != filepath.Clean(filepath.Join("..", "dns-check", "dns-check.runbook.yaml")) {
		t.Fatalf("imports[dns-check]=%q", got)
	}
	if got := rb.Imports["db-check"]; filepath.Clean(got) != filepath.Clean(filepath.Join("..", "db-check", "custom.runbook.yaml")) {
		t.Fatalf("imports[db-check]=%q", got)
	}
}
