package schema

import (
	"strings"
	"testing"
)

func TestLoad_ValidRunbook(t *testing.T) {
	yaml := `
apiVersion: kernel/v0
meta:
  name: test
  inputs:
    host:
      type: string
      required: true
  constants:
    endpoint: "/healthz"
tools:
  - my-tool
steps:
  - id: step1
    type: tool
    tool: my-tool
    action: check
    inputs:
      url: "https://{{ .host }}{{ .endpoint }}"
  - type: end
    outcome:
      category: resolved
      code: done
`
	rb, err := Load(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if rb.APIVersion != "kernel/v0" {
		t.Errorf("apiVersion = %q, want kernel/v0", rb.APIVersion)
	}
	if rb.Meta.Name != "test" {
		t.Errorf("name = %q, want test", rb.Meta.Name)
	}
	if len(rb.Steps) != 2 {
		t.Errorf("steps = %d, want 2", len(rb.Steps))
	}
	if rb.Meta.Constants["endpoint"] != "/healthz" {
		t.Errorf("constant endpoint = %v", rb.Meta.Constants["endpoint"])
	}
}

func TestLoad_UnknownField(t *testing.T) {
	yaml := `
apiVersion: kernel/v0
meta:
  name: test
unknown_field: bad
steps:
  - type: end
    outcome:
      category: resolved
      code: done
`
	_, err := Load(strings.NewReader(yaml))
	if err == nil {
		t.Fatal("expected structural error for unknown field")
	}
}

func TestLoad_StepTypes(t *testing.T) {
	yaml := `
apiVersion: kernel/v0
meta:
  name: types-test
steps:
  - id: t1
    type: tool
    tool: my-tool
    action: check
  - id: t2
    type: manual
    instructions: "Do something"
  - id: t3
    type: assert
    assert:
      - type: equals
        value: "200"
        expected: "200"
  - type: branch
    branches:
      - condition: default
        label: default
        steps:
          - type: end
            outcome:
              category: resolved
              code: done
`
	rb, err := Load(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(rb.Steps) != 4 {
		t.Fatalf("expected 4 steps, got %d", len(rb.Steps))
	}
	if rb.Steps[0].Type != StepTool {
		t.Errorf("step 0 type = %q, want tool", rb.Steps[0].Type)
	}
	if rb.Steps[1].Type != StepManual {
		t.Errorf("step 1 type = %q, want manual", rb.Steps[1].Type)
	}
	if rb.Steps[2].Type != StepAssert {
		t.Errorf("step 2 type = %q, want assert", rb.Steps[2].Type)
	}
	if rb.Steps[3].Type != StepBranch {
		t.Errorf("step 3 type = %q, want branch", rb.Steps[3].Type)
	}
}

func TestParseNext_String(t *testing.T) {
	target, max, bounded, err := ParseNext("step2")
	if err != nil {
		t.Fatal(err)
	}
	if target != "step2" || bounded || max != 0 {
		t.Errorf("got target=%q max=%d bounded=%v", target, max, bounded)
	}
}

func TestParseNext_Bounded(t *testing.T) {
	raw := map[string]any{"step": "retry", "max": 3}
	target, max, bounded, err := ParseNext(raw)
	if err != nil {
		t.Fatal(err)
	}
	if target != "retry" || !bounded || max != 3 {
		t.Errorf("got target=%q max=%d bounded=%v", target, max, bounded)
	}
}

func TestParseNext_Nil(t *testing.T) {
	target, _, _, err := ParseNext(nil)
	if err != nil {
		t.Fatal(err)
	}
	if target != "" {
		t.Error("nil should return empty target")
	}
}

func TestLoadTool_Valid(t *testing.T) {
	yaml := `
apiVersion: tool/v0
meta:
  name: health-check
  description: Check service health
  transport: stdio
  binary: curl
contract:
  inputs:
    url:
      type: string
      required: true
  outputs:
    status_code:
      type: int
  side_effects: false
  deterministic: true
  idempotent: true
  reads:
    - network
  writes: []
actions:
  check:
    description: GET request
    argv: ["curl", "-s", "{{ .url }}"]
    extract:
      status_code:
        from: stdout
        pattern: "^(\\d+)$"
`
	td, err := LoadTool(strings.NewReader(yaml))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if td.Meta.Name != "health-check" {
		t.Errorf("name = %q", td.Meta.Name)
	}
	if *td.Contract.SideEffects != false {
		t.Error("expected side_effects false")
	}
	if _, ok := td.Actions["check"]; !ok {
		t.Error("expected 'check' action")
	}
}

// T053: Scope field normalizes `/` to `.`
func TestLoad_ScopeNormalization(t *testing.T) {
	yaml := `
apiVersion: kernel/v0
meta:
  name: test-scope
steps:
  - id: scoped
    type: assert
    scope: "round/0"
    assert:
      - type: equals
        value: a
        expected: a
  - type: end
    outcome:
      category: no_action
      code: done
`
	rb, err := Load(strings.NewReader(yaml))
	if err != nil {
		t.Fatal(err)
	}
	if rb.Steps[0].Scope != "round.0" {
		t.Errorf("scope = %q, want 'round.0' (normalized from round/0)", rb.Steps[0].Scope)
	}
}
