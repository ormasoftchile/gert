package assertions

import (
	"testing"
)

func TestContainsAssertion(t *testing.T) {
	r := EvalContains("hello world", "world")
	if !r.Passed {
		t.Error("expected pass for contains 'world'")
	}
	r = EvalContains("hello world", "missing")
	if r.Passed {
		t.Error("expected fail for contains 'missing'")
	}
}

func TestNotContainsAssertion(t *testing.T) {
	r := EvalNotContains("hello world", "missing")
	if !r.Passed {
		t.Error("expected pass for not_contains 'missing'")
	}
	r = EvalNotContains("hello world", "world")
	if r.Passed {
		t.Error("expected fail for not_contains 'world'")
	}
}

func TestMatchesAssertion(t *testing.T) {
	r := EvalMatches("status: ok", "status.*ok")
	if !r.Passed {
		t.Error("expected pass for matches 'status.*ok'")
	}
	r = EvalMatches("status: error", "status.*ok")
	if r.Passed {
		t.Error("expected fail for matches 'status.*ok' against 'status: error'")
	}
}

func TestExitCodeAssertion(t *testing.T) {
	r := EvalExitCode(0, 0)
	if !r.Passed {
		t.Error("expected pass for exit_code 0 == 0")
	}
	r = EvalExitCode(1, 0)
	if r.Passed {
		t.Error("expected fail for exit_code 1 != 0")
	}
}

func TestEqualsAssertion(t *testing.T) {
	r := EvalEquals("v1.0.0", "v1.0.0")
	if !r.Passed {
		t.Error("expected pass for equals")
	}
	r = EvalEquals("v1.0.1", "v1.0.0")
	if r.Passed {
		t.Error("expected fail for not-equal")
	}
}

func TestNotEqualsAssertion(t *testing.T) {
	r := EvalNotEquals("v1.0.1", "v1.0.0")
	if !r.Passed {
		t.Error("expected pass for not_equals")
	}
	r = EvalNotEquals("v1.0.0", "v1.0.0")
	if r.Passed {
		t.Error("expected fail when values are equal")
	}
}

func TestJSONPathAssertion(t *testing.T) {
	jsonData := `{"status":{"phase":"Running","replicas":3}}`
	r := EvalJSONPath(jsonData, "$.status.phase", "Running")
	if !r.Passed {
		t.Errorf("expected pass for json_path $.status.phase=Running, got: %s", r.Message)
	}
	r = EvalJSONPath(jsonData, "$.status.phase", "Pending")
	if r.Passed {
		t.Error("expected fail for json_path $.status.phase!=Pending")
	}
}

func TestMatchesInvalidRegex(t *testing.T) {
	r := EvalMatches("hello", "[invalid(")
	if r.Passed {
		t.Error("expected fail for invalid regex")
	}
}

func TestJSONPathInvalidJSON(t *testing.T) {
	r := EvalJSONPath("not json", "$.foo", "bar")
	if r.Passed {
		t.Error("expected fail for invalid JSON")
	}
}

func TestJSONPathMissingPath(t *testing.T) {
	r := EvalJSONPath(`{"a":"b"}`, "$.missing", "value")
	if r.Passed {
		t.Error("expected fail for missing JSON path")
	}
}
