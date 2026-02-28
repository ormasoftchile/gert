package eval

import (
	"testing"
)

func TestResolve_Literal(t *testing.T) {
	result, err := Resolve("hello world", nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != "hello world" {
		t.Errorf("got %q", result)
	}
}

func TestResolve_SimpleVar(t *testing.T) {
	vars := map[string]any{"hostname": "srv1"}
	result, err := Resolve("https://{{ .hostname }}/healthz", vars)
	if err != nil {
		t.Fatal(err)
	}
	if result != "https://srv1/healthz" {
		t.Errorf("got %q", result)
	}
}

func TestResolve_MultipleVars(t *testing.T) {
	vars := map[string]any{"host": "srv1", "path": "/api"}
	result, err := Resolve("https://{{ .host }}{{ .path }}", vars)
	if err != nil {
		t.Fatal(err)
	}
	if result != "https://srv1/api" {
		t.Errorf("got %q", result)
	}
}

func TestResolve_NestedAccess(t *testing.T) {
	vars := map[string]any{
		"check": map[string]any{"status_code": "200"},
	}
	result, err := Resolve("{{ .check.status_code }}", vars)
	if err != nil {
		t.Fatal(err)
	}
	if result != "200" {
		t.Errorf("got %q", result)
	}
}

func TestResolve_EqFunction(t *testing.T) {
	vars := map[string]any{"status": "200"}
	result, err := Resolve(`{{ eq .status "200" }}`, vars)
	if err != nil {
		t.Fatal(err)
	}
	if result != "true" {
		t.Errorf("got %q", result)
	}
}

func TestResolve_NeFunction(t *testing.T) {
	vars := map[string]any{"status": "503"}
	result, err := Resolve(`{{ ne .status "200" }}`, vars)
	if err != nil {
		t.Fatal(err)
	}
	if result != "true" {
		t.Errorf("got %q", result)
	}
}

func TestResolveMap(t *testing.T) {
	vars := map[string]any{"host": "srv1"}
	inputs := map[string]any{
		"url":     "https://{{ .host }}/api",
		"timeout": 30,
	}
	result, err := ResolveMap(inputs, vars)
	if err != nil {
		t.Fatal(err)
	}
	if result["url"] != "https://srv1/api" {
		t.Errorf("url = %v", result["url"])
	}
	if result["timeout"] != 30 {
		t.Errorf("timeout = %v", result["timeout"])
	}
}

func TestEvalBool(t *testing.T) {
	tests := []struct {
		expr string
		vars map[string]any
		want bool
	}{
		{"", nil, true},                                     // empty = true
		{"default", nil, true},                              // default branch
		{`{{ eq .x "yes" }}`, map[string]any{"x": "yes"}, true},
		{`{{ eq .x "yes" }}`, map[string]any{"x": "no"}, false},
		{`{{ ne .x "ok" }}`, map[string]any{"x": "err"}, true},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := EvalBool(tt.expr, tt.vars)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("EvalBool(%q) = %v, want %v", tt.expr, got, tt.want)
			}
		})
	}
}

func TestResolveMap_Nil(t *testing.T) {
	result, err := ResolveMap(nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	if result != nil {
		t.Error("nil input should return nil")
	}
}
