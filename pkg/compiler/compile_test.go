package compiler

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

// mockLLMClient is a test double that returns a pre-built LLM response.
type mockLLMClient struct {
	response string
	err      error
	// captured prompts for assertions
	systemPrompt string
	userPrompt   string
}

func (m *mockLLMClient) Complete(_ context.Context, systemPrompt, userPrompt string) (string, error) {
	m.systemPrompt = systemPrompt
	m.userPrompt = userPrompt
	return m.response, m.err
}

func (m *mockLLMClient) ModelName() string {
	return "mock-model"
}

// buildMockResponse constructs a valid LLM response with the delimiter markers.
func buildMockResponse(runbookYAML, mappingMD string) string {
	return fmt.Sprintf("---RUNBOOK_YAML---\n%s\n---END_RUNBOOK_YAML---\n---MAPPING_MD---\n%s\n---END_MAPPING_MD---",
		runbookYAML, mappingMD)
}

// --- IR Parsing Tests (Stage A — deterministic, no LLM) ---

// TestParseTSGWithCodeBlocks verifies headings and code blocks are extracted.
func TestParseTSGWithCodeBlocks(t *testing.T) {
	source := []byte(`# Test TSG

## Step One

Check the status:

` + "```bash\nkubectl get pods -n $NAMESPACE\n```" + `

## Step Two

Get logs:

` + "```bash\nkubectl logs -n $NAMESPACE --tail=50\n```" + `
`)

	ir, err := ParseTSG(source)
	if err != nil {
		t.Fatalf("ParseTSG error: %v", err)
	}

	if ir.Title != "Test TSG" {
		t.Errorf("title = %q, want %q", ir.Title, "Test TSG")
	}

	// Should have 3 sections (title + 2 steps)
	if len(ir.Sections) != 3 {
		t.Fatalf("sections = %d, want 3", len(ir.Sections))
	}

	// Step One should have a code block
	if len(ir.Sections[1].CodeBlocks) != 1 {
		t.Errorf("Step One code blocks = %d, want 1", len(ir.Sections[1].CodeBlocks))
	}
	if ir.Sections[1].CodeBlocks[0].Language != "bash" {
		t.Errorf("language = %q, want bash", ir.Sections[1].CodeBlocks[0].Language)
	}

	// Should extract NAMESPACE variable
	found := false
	for _, v := range ir.Vars {
		if v == "NAMESPACE" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected NAMESPACE in vars, got %v", ir.Vars)
	}
}

// TestParseTSGPureProse verifies pure prose produces no code blocks.
func TestParseTSGPureProse(t *testing.T) {
	source := []byte(`# Escalation Guide

## Contact DRI

Reach out to the on-call DRI via PagerDuty.

## Assess Impact

Review the support tickets.
`)

	ir, err := ParseTSG(source)
	if err != nil {
		t.Fatalf("ParseTSG error: %v", err)
	}

	for _, section := range ir.Sections {
		if len(section.CodeBlocks) > 0 {
			t.Errorf("section %q has code blocks in pure prose TSG", section.Heading)
		}
	}
}

// TestParseTSGListItems verifies list items are extracted.
func TestParseTSGListItems(t *testing.T) {
	source := []byte(`# Checklist TSG

## Verify

Confirm the following:
- Error rate below 1%
- Latency under 500ms
- No anomalies
`)

	ir, err := ParseTSG(source)
	if err != nil {
		t.Fatalf("ParseTSG error: %v", err)
	}

	var verifySection *Section
	for i := range ir.Sections {
		if ir.Sections[i].Heading == "Verify" {
			verifySection = &ir.Sections[i]
			break
		}
	}
	if verifySection == nil {
		t.Fatal("Verify section not found")
	}
	if len(verifySection.ListItems) != 3 {
		t.Errorf("list items = %d, want 3", len(verifySection.ListItems))
	}
}

// --- LLM Response Parsing Tests ---

func TestParseLLMResponse(t *testing.T) {
	yaml := `apiVersion: runbook/v0
meta:
  name: test-runbook
steps:
  - id: step_one
    type: cli
    with:
      argv: ["kubectl", "get", "pods"]`

	mapping := `# Mapping Report
| Step ID | Type |
|---------|------|
| step_one | cli |`

	response := buildMockResponse(yaml, mapping)

	gotYAML, gotMapping, err := ParseLLMResponse(response)
	if err != nil {
		t.Fatalf("ParseLLMResponse error: %v", err)
	}
	if !strings.Contains(gotYAML, "test-runbook") {
		t.Errorf("runbook YAML missing name, got: %s", gotYAML)
	}
	if !strings.Contains(gotMapping, "step_one") {
		t.Errorf("mapping missing step_one, got: %s", gotMapping)
	}
}

func TestParseLLMResponseMissingMarkers(t *testing.T) {
	tests := []struct {
		name     string
		response string
	}{
		{"no markers", "just some text"},
		{"missing end runbook", "---RUNBOOK_YAML---\nfoo"},
		{"missing mapping", "---RUNBOOK_YAML---\nfoo\n---END_RUNBOOK_YAML---"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := ParseLLMResponse(tt.response)
			if err == nil {
				t.Error("expected error for malformed response")
			}
		})
	}
}

// --- Compile Pipeline Tests (with Mock LLM) ---

func TestCompileTSGFromSourceWithMockLLM(t *testing.T) {
	tsg := []byte(`# Pod Restart TSG

## Check Pods

` + "```bash\nkubectl get pods -n $NAMESPACE\n```" + `

## Escalate

Contact the DRI.
`)

	mockYAML := `apiVersion: runbook/v0
meta:
  name: pod-restart-tsg
  description: Investigate pod restarts
  vars:
    namespace: ""
  governance:
    allowed_commands:
      - kubectl
steps:
  - id: check_pods
    type: cli
    title: Check Pods
    with:
      argv: ["kubectl", "get", "pods", "-n", "{{ .namespace }}"]
    capture:
      output: stdout
  - id: escalate
    type: manual
    title: Escalate
    instructions: |
      Contact the DRI.`

	mockMapping := `# Mapping Report
| Step ID | Type | TSG Section |
|---------|------|-------------|
| check_pods | cli | Check Pods |
| escalate | manual | Escalate |`

	client := &mockLLMClient{
		response: buildMockResponse(mockYAML, mockMapping),
	}

	result, err := CompileTSGFromSource(tsg, "test.md", client)
	if err != nil {
		t.Fatalf("CompileTSGFromSource error: %v", err)
	}

	if result.Runbook == nil {
		t.Fatal("runbook is nil")
	}
	if result.Runbook.APIVersion != "runbook/v0" {
		t.Errorf("apiVersion = %q, want runbook/v0", result.Runbook.APIVersion)
	}
	if result.StepCount != 2 {
		t.Errorf("StepCount = %d, want 2", result.StepCount)
	}
	if result.CLICount != 1 {
		t.Errorf("CLICount = %d, want 1", result.CLICount)
	}
	if result.ManualCount != 1 {
		t.Errorf("ManualCount = %d, want 1", result.ManualCount)
	}
	if !strings.Contains(result.Mapping, "check_pods") {
		t.Error("mapping should contain check_pods")
	}

	// Verify the LLM received the TSG content
	if !strings.Contains(client.userPrompt, "Pod Restart TSG") {
		t.Error("user prompt should contain the TSG content")
	}
	// Verify the system prompt includes the JSON Schema
	if !strings.Contains(client.systemPrompt, "runbook/v0") {
		t.Error("system prompt should contain the JSON Schema")
	}
}

func TestCompileTSGFromSourceLLMError(t *testing.T) {
	client := &mockLLMClient{
		err: fmt.Errorf("rate limited"),
	}

	_, err := CompileTSGFromSource([]byte("# Test\n## Step\nDo stuff."), "test.md", client)
	if err == nil {
		t.Fatal("expected error when LLM fails")
	}
	if !strings.Contains(err.Error(), "rate limited") {
		t.Errorf("error should mention LLM failure, got: %v", err)
	}
}

func TestCompileTSGFromSourceBadYAML(t *testing.T) {
	client := &mockLLMClient{
		response: buildMockResponse("not: valid: yaml: [[[", "# Mapping"),
	}

	_, err := CompileTSGFromSource([]byte("# Test\n## Step\nDo stuff."), "test.md", client)
	if err == nil {
		t.Fatal("expected error for invalid YAML from LLM")
	}
	if !strings.Contains(err.Error(), "unmarshal") {
		t.Errorf("error should mention unmarshal, got: %v", err)
	}
}

func TestCompileTSGFromSourceTODOCounting(t *testing.T) {
	mockYAML := `apiVersion: runbook/v0
meta:
  name: test
steps:
  - id: dangerous_step
    type: manual
    title: Dangerous Step
    instructions: |
      TODO: Review this step — contains unsafe command: rm
      rm -rf /tmp/old`

	client := &mockLLMClient{
		response: buildMockResponse(mockYAML, "# Mapping"),
	}

	result, err := CompileTSGFromSource([]byte("# Test\n## Cleanup\n```bash\nrm -rf /tmp/old\n```"), "test.md", client)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.TODOCount != 1 {
		t.Errorf("TODOCount = %d, want 1", result.TODOCount)
	}
	if len(result.Warnings) != 1 {
		t.Errorf("Warnings = %d, want 1", len(result.Warnings))
	}
}

// --- Helper Function Tests ---

func TestIsUnsafeCommand(t *testing.T) {
	tests := []struct {
		cmd    string
		unsafe bool
	}{
		{"rm -rf /tmp/old", true},
		{"sudo systemctl restart nginx", true},
		{"kubectl get pods", false},
		{"echo hello", false},
		{"dd if=/dev/zero of=/dev/sda", true},
		{"az sql db show --name test", false},
	}
	for _, tt := range tests {
		if got := IsUnsafeCommand(tt.cmd); got != tt.unsafe {
			t.Errorf("IsUnsafeCommand(%q) = %v, want %v", tt.cmd, got, tt.unsafe)
		}
	}
}

func TestToKebabCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"Pod CrashLoopBackOff Investigation", "pod-crashloopbackoff-investigation"},
		{"Simple", "simple"},
		{"Hello World!", "hello-world"},
	}
	for _, tt := range tests {
		if got := toKebabCase(tt.in); got != tt.want {
			t.Errorf("toKebabCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestToSnakeCase(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"1. Check Pod Status", "check_pod_status"},
		{"Validate Dashboard", "validate_dashboard"},
		{"3. Get Events!", "get_events"},
	}
	for _, tt := range tests {
		if got := toSnakeCase(tt.in); got != tt.want {
			t.Errorf("toSnakeCase(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}
