// Package eval implements Go text/template-style expression evaluation
// for kernel/v0 runbook templates.
package eval

import (
	"bytes"
	"fmt"
	"strings"
	"text/template"
)

// Resolve evaluates a template string against a variable scope.
// Returns the rendered string.
// Example: Resolve("https://{{ .hostname }}/healthz", {"hostname": "srv1"}) → "https://srv1/healthz"
func Resolve(tmpl string, vars map[string]any) (string, error) {
	if !strings.Contains(tmpl, "{{") {
		return tmpl, nil // fast path for literals
	}

	t, err := template.New("").Funcs(builtinFuncs()).Parse(tmpl)
	if err != nil {
		return "", fmt.Errorf("template parse: %w", err)
	}

	var buf bytes.Buffer
	if err := t.Execute(&buf, vars); err != nil {
		return "", fmt.Errorf("template eval: %w", err)
	}
	return buf.String(), nil
}

// ResolveMap resolves all string values in a map[string]any.
func ResolveMap(inputs map[string]any, vars map[string]any) (map[string]any, error) {
	if inputs == nil {
		return nil, nil
	}
	out := make(map[string]any, len(inputs))
	for k, v := range inputs {
		switch val := v.(type) {
		case string:
			resolved, err := Resolve(val, vars)
			if err != nil {
				return nil, fmt.Errorf("input %q: %w", k, err)
			}
			out[k] = resolved
		default:
			out[k] = v
		}
	}
	return out, nil
}

// EvalBool evaluates a template expression that should produce a boolean-ish result.
// Returns true for "true", non-empty non-"false" strings. Empty and "false" return false.
func EvalBool(expr string, vars map[string]any) (bool, error) {
	if expr == "" {
		return true, nil // no condition = always true
	}
	if expr == "default" {
		return true, nil // default branch always matches
	}
	result, err := Resolve(expr, vars)
	if err != nil {
		return false, err
	}
	result = strings.TrimSpace(result)
	return result != "" && result != "false" && result != "<no value>", nil
}

// builtinFuncs provides template functions for expressions.
func builtinFuncs() template.FuncMap {
	return template.FuncMap{
		"eq": func(a, b any) bool {
			return fmt.Sprint(a) == fmt.Sprint(b)
		},
		"ne": func(a, b any) bool {
			return fmt.Sprint(a) != fmt.Sprint(b)
		},
		"gt": func(a, b any) bool {
			return fmt.Sprint(a) > fmt.Sprint(b)
		},
		"lt": func(a, b any) bool {
			return fmt.Sprint(a) < fmt.Sprint(b)
		},
		"contains": func(s, substr any) bool {
			return strings.Contains(fmt.Sprint(s), fmt.Sprint(substr))
		},
		"hasPrefix": func(s, prefix any) bool {
			return strings.HasPrefix(fmt.Sprint(s), fmt.Sprint(prefix))
		},
		"hasSuffix": func(s, suffix any) bool {
			return strings.HasSuffix(fmt.Sprint(s), fmt.Sprint(suffix))
		},
		"default": func(def, val any) any {
			if val == nil || fmt.Sprint(val) == "" {
				return def
			}
			return val
		},
		"index": func(collection any, keys ...any) (any, error) {
			// Delegate to the built-in index — just make it available
			switch c := collection.(type) {
			case map[string]any:
				if len(keys) == 0 {
					return nil, nil
				}
				key := fmt.Sprint(keys[0])
				val, ok := c[key]
				if !ok {
					return nil, nil
				}
				if len(keys) > 1 {
					return indexRecursive(val, keys[1:])
				}
				return val, nil
			case []any:
				if len(keys) == 0 {
					return nil, nil
				}
				idx, ok := toInt(keys[0])
				if !ok {
					return nil, fmt.Errorf("index: non-integer index %v", keys[0])
				}
				if idx < 0 || idx >= len(c) {
					return nil, fmt.Errorf("index: %d out of range [0,%d)", idx, len(c))
				}
				val := c[idx]
				if len(keys) > 1 {
					return indexRecursive(val, keys[1:])
				}
				return val, nil
			default:
				return nil, fmt.Errorf("index: cannot index %T", collection)
			}
		},
	}
}

func indexRecursive(val any, keys []any) (any, error) {
	if len(keys) == 0 {
		return val, nil
	}
	switch c := val.(type) {
	case map[string]any:
		key := fmt.Sprint(keys[0])
		next, ok := c[key]
		if !ok {
			return nil, nil
		}
		return indexRecursive(next, keys[1:])
	default:
		return nil, fmt.Errorf("cannot index %T", val)
	}
}

func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case int:
		return n, true
	case float64:
		return int(n), true
	case int64:
		return int(n), true
	default:
		return 0, false
	}
}
