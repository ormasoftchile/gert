package validate

import (
	"fmt"

	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// validateSemantic runs JSON-Schema-like structural checks on the runbook.
// Rather than generating+compiling a full JSON Schema at runtime, we do
// direct field validation — same guarantees, simpler implementation.
func validateSemantic(rb *schema.Runbook) []*ValidationError {
	var errs []*ValidationError

	if rb.APIVersion == "" {
		errs = append(errs, errorf("semantic", "apiVersion", "apiVersion is required"))
	}
	if rb.Meta.Name == "" {
		errs = append(errs, errorf("semantic", "meta.name", "meta.name is required"))
	}
	if len(rb.Steps) == 0 {
		errs = append(errs, errorf("semantic", "steps", "at least one step is required"))
	}

	for i, step := range rb.Steps {
		path := stepPath(i)
		if step.Type == "" {
			errs = append(errs, errorf("semantic", path+".type", "step type is required"))
		}
	}

	// Validate governance rules if present
	if rb.Meta.Governance != nil {
		for i, rule := range rb.Meta.Governance.Rules {
			path := gfmt("meta.governance.rules[%d]", i)
			if rule.Default == "" && rule.Action == "" {
				errs = append(errs, errorf("semantic", path, "rule must have 'action' or 'default'"))
			}
			if rule.Default != "" && rule.Action != "" {
				errs = append(errs, errorf("semantic", path, "rule cannot have both 'action' and 'default'"))
			}
			if rule.Action != "" {
				switch rule.Action {
				case "allow", "require-approval", "deny":
				default:
					errs = append(errs, errorf("semantic", path+".action", "invalid action %q: must be allow, require-approval, or deny", rule.Action))
				}
			}
			if rule.Default != "" {
				switch rule.Default {
				case "allow", "require-approval", "deny":
				default:
					errs = append(errs, errorf("semantic", path+".default", "invalid default %q", rule.Default))
				}
			}
			if rule.Risk != "" {
				switch rule.Risk {
				case "low", "medium", "high", "critical":
				default:
					errs = append(errs, errorf("semantic", path+".risk", "invalid risk level %q", rule.Risk))
				}
			}
		}
	}

	return errs
}

func stepPath(i int) string {
	return gfmt("steps[%d]", i)
}

func gfmt(format string, args ...any) string {
	return fmt.Sprintf(format, args...)
}
