// Package contract defines the kernel's contract model — the foundational
// primitive that drives governance, parallelism, extensions, and replay.
package contract

// Contract describes the behavioral promises of a step or tool action.
type Contract struct {
	Inputs        map[string]ParamDef `yaml:"inputs,omitempty"      json:"inputs,omitempty"`
	Outputs       map[string]ParamDef `yaml:"outputs,omitempty"     json:"outputs,omitempty"`
	SideEffects   *bool               `yaml:"side_effects,omitempty" json:"side_effects,omitempty"`
	Deterministic *bool               `yaml:"deterministic,omitempty" json:"deterministic,omitempty"`
	Idempotent    *bool               `yaml:"idempotent,omitempty"  json:"idempotent,omitempty"`
	Reads         []string            `yaml:"reads,omitempty"       json:"reads,omitempty"`
	Writes        []string            `yaml:"writes,omitempty"      json:"writes,omitempty"`
}

// ParamDef describes a single input or output parameter.
type ParamDef struct {
	Type        string `yaml:"type"                  json:"type"`
	Required    bool   `yaml:"required,omitempty"    json:"required,omitempty"`
	Default     any    `yaml:"default,omitempty"     json:"default,omitempty"`
	Description string `yaml:"description,omitempty" json:"description,omitempty"`
}

// RiskLevel classifies a contract's risk based on its behavioural properties.
type RiskLevel string

const (
	RiskLow      RiskLevel = "low"
	RiskMedium   RiskLevel = "medium"
	RiskHigh     RiskLevel = "high"
	RiskCritical RiskLevel = "critical"
)

// Risk derives the risk level from a resolved contract.
func (c *Contract) Risk() RiskLevel {
	if !c.getBool(c.SideEffects, true) {
		return RiskLow
	}
	if c.getBool(c.Idempotent, false) {
		return RiskMedium
	}
	if c.getBool(c.Deterministic, false) {
		return RiskHigh
	}
	return RiskCritical
}

// Resolved returns a copy of this contract with all nil fields replaced by
// their defaults (side_effects=true, deterministic=false, idempotent=false).
func (c *Contract) Resolved() Contract {
	out := *c
	out.SideEffects = boolPtr(c.getBool(c.SideEffects, true))
	out.Deterministic = boolPtr(c.getBool(c.Deterministic, false))
	out.Idempotent = boolPtr(c.getBool(c.Idempotent, false))
	if out.Reads == nil {
		out.Reads = []string{}
	}
	if out.Writes == nil {
		out.Writes = []string{}
	}
	return out
}

// CanTighten reports whether applying `child` on top of `parent` is a valid
// tightening.  Returns a list of violations (empty means valid).
func CanTighten(parent, child *Contract) []string {
	var violations []string

	// Boolean properties: can only change true→more-restrictive
	if parent.SideEffects != nil && child.SideEffects != nil {
		if *parent.SideEffects && !*child.SideEffects {
			violations = append(violations, "cannot relax side_effects from true to false")
		}
	}
	if parent.Deterministic != nil && child.Deterministic != nil {
		if !*parent.Deterministic && *child.Deterministic {
			violations = append(violations, "cannot relax deterministic from false to true")
		}
	}
	if parent.Idempotent != nil && child.Idempotent != nil {
		if !*parent.Idempotent && *child.Idempotent {
			violations = append(violations, "cannot relax idempotent from false to true")
		}
	}

	// reads/writes: child must be a superset of parent (can add, not remove)
	if missing := setDiff(parent.Reads, child.Reads); len(missing) > 0 {
		violations = append(violations, "cannot remove reads tags: "+joinStrings(missing))
	}
	if missing := setDiff(parent.Writes, child.Writes); len(missing) > 0 {
		violations = append(violations, "cannot remove writes tags: "+joinStrings(missing))
	}

	return violations
}

// Merge returns a contract that combines parent + child with child fields
// taking precedence.  Does NOT validate tightening — call CanTighten first.
func Merge(parent, child *Contract) Contract {
	out := *parent
	if child.Inputs != nil {
		if out.Inputs == nil {
			out.Inputs = make(map[string]ParamDef)
		}
		for k, v := range child.Inputs {
			out.Inputs[k] = v
		}
	}
	if child.Outputs != nil {
		if out.Outputs == nil {
			out.Outputs = make(map[string]ParamDef)
		}
		for k, v := range child.Outputs {
			out.Outputs[k] = v
		}
	}
	if child.SideEffects != nil {
		out.SideEffects = child.SideEffects
	}
	if child.Deterministic != nil {
		out.Deterministic = child.Deterministic
	}
	if child.Idempotent != nil {
		out.Idempotent = child.Idempotent
	}
	if child.Reads != nil {
		out.Reads = setUnion(parent.Reads, child.Reads)
	}
	if child.Writes != nil {
		out.Writes = setUnion(parent.Writes, child.Writes)
	}
	return out
}

// HasConflict reports whether two contracts have a reads/writes conflict
// that would prevent safe parallel execution.
func HasConflict(a, b *Contract) bool {
	// Conflict = A writes something B reads or writes, or vice versa
	if setIntersects(a.Writes, b.Reads) || setIntersects(a.Writes, b.Writes) {
		return true
	}
	if setIntersects(b.Writes, a.Reads) {
		return true
	}
	return false
}

// AssertContract is the fixed, implicit contract for assert steps.
func AssertContract() Contract {
	return Contract{
		SideEffects:   boolPtr(false),
		Deterministic: boolPtr(true),
		Idempotent:    boolPtr(true),
		Reads:         []string{},
		Writes:        []string{},
	}
}

// ManualDefaults returns the default contract for manual steps.
func ManualDefaults() Contract {
	return Contract{
		SideEffects:   boolPtr(true),
		Deterministic: boolPtr(false),
		Idempotent:    boolPtr(false),
		Reads:         []string{},
		Writes:        []string{},
	}
}

// --- helpers ---

func (c *Contract) getBool(p *bool, def bool) bool {
	if p == nil {
		return def
	}
	return *p
}

func boolPtr(b bool) *bool { return &b }

func setDiff(parent, child []string) []string {
	m := make(map[string]struct{}, len(child))
	for _, s := range child {
		m[s] = struct{}{}
	}
	var diff []string
	for _, s := range parent {
		if _, ok := m[s]; !ok {
			diff = append(diff, s)
		}
	}
	return diff
}

func setUnion(a, b []string) []string {
	m := make(map[string]struct{}, len(a)+len(b))
	for _, s := range a {
		m[s] = struct{}{}
	}
	for _, s := range b {
		m[s] = struct{}{}
	}
	out := make([]string, 0, len(m))
	for s := range m {
		out = append(out, s)
	}
	return out
}

func setIntersects(a, b []string) bool {
	m := make(map[string]struct{}, len(a))
	for _, s := range a {
		m[s] = struct{}{}
	}
	for _, s := range b {
		if _, ok := m[s]; ok {
			return true
		}
	}
	return false
}

func joinStrings(ss []string) string {
	out := ""
	for i, s := range ss {
		if i > 0 {
			out += ", "
		}
		out += s
	}
	return out
}
