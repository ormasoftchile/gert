package engine

import (
	"strings"

	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// CheckVisibility determines whether a variable path is accessible given visibility rules.
// Returns true if the path is allowed. If no visibility is declared, all paths are allowed.
//
// Glob semantics:
//   - `*` matches one dot-segment
//   - `**` matches any depth (zero or more segments)
//   - Deny overrides allow
//   - If allow is present, unlisted paths are denied by default
func CheckVisibility(vis *schema.Visibility, varPath string) bool {
	if vis == nil {
		return true
	}

	// Deny check first — deny always overrides
	for _, pattern := range vis.Deny {
		if globMatch(pattern, varPath) {
			return false
		}
	}

	// If allow list is present, only listed paths are allowed
	if len(vis.Allow) > 0 {
		for _, pattern := range vis.Allow {
			if globMatch(pattern, varPath) {
				return true
			}
		}
		return false // not in allow list
	}

	return true // no allow list = everything allowed (minus denies above)
}

// globMatch matches a dot-separated path against a glob pattern.
// `*` matches exactly one segment, `**` matches zero or more segments.
func globMatch(pattern, path string) bool {
	patParts := strings.Split(pattern, ".")
	pathParts := strings.Split(path, ".")
	return matchParts(patParts, pathParts)
}

func matchParts(pattern, path []string) bool {
	pi, pa := 0, 0
	for pi < len(pattern) && pa < len(path) {
		if pattern[pi] == "**" {
			// ** matches zero or more segments
			if pi == len(pattern)-1 {
				return true // trailing ** matches everything
			}
			// Try matching the rest of pattern against every suffix of path
			for k := pa; k <= len(path); k++ {
				if matchParts(pattern[pi+1:], path[k:]) {
					return true
				}
			}
			return false
		}
		if pattern[pi] == "*" || pattern[pi] == path[pa] {
			pi++
			pa++
			continue
		}
		return false
	}

	// Remaining pattern segments must all be ** (which match zero)
	for pi < len(pattern) {
		if pattern[pi] != "**" {
			return false
		}
		pi++
	}

	return pa == len(path)
}
