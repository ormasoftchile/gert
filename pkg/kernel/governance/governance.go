// Package governance implements the kernel's contract-driven policy engine.
package governance

import (
	"github.com/ormasoftchile/gert/pkg/kernel/contract"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

// Decision carries the governance evaluation result for a step.
type Decision struct {
	Action       schema.GovernanceDecision `json:"action"`
	RiskLevel    contract.RiskLevel        `json:"risk_level"`
	MinApprovers int                       `json:"min_approvers,omitempty"`
	MatchedRule  string                    `json:"matched_rule,omitempty"`
}

// Evaluate evaluates governance policy against a resolved contract.
// Returns the most restrictive matching decision.
func Evaluate(c *contract.Contract, policy *schema.GovernancePolicy) Decision {
	resolved := c.Resolved()
	risk := resolved.Risk()

	if policy == nil || len(policy.Rules) == 0 {
		return Decision{
			Action:    schema.DecisionAllow,
			RiskLevel: risk,
		}
	}

	for _, rule := range policy.Rules {
		if ruleMatches(rule, &resolved, risk) {
			action := schema.GovernanceDecision(rule.Action)
			if rule.Default != "" {
				action = schema.GovernanceDecision(rule.Default)
			}
			return Decision{
				Action:       action,
				RiskLevel:    risk,
				MinApprovers: rule.MinApprovers,
				MatchedRule:  describeRule(rule),
			}
		}
	}

	// No rule matched — default allow
	return Decision{
		Action:    schema.DecisionAllow,
		RiskLevel: risk,
	}
}

// MostRestrictive returns the more restrictive of two governance decisions.
// deny > require-approval > allow
func MostRestrictive(a, b Decision) Decision {
	if severity(a.Action) >= severity(b.Action) {
		return a
	}
	return b
}

func severity(d schema.GovernanceDecision) int {
	switch d {
	case schema.DecisionDeny:
		return 2
	case schema.DecisionRequireApproval:
		return 1
	default:
		return 0
	}
}

// ruleMatches checks if a governance rule matches a contract at a given risk level.
func ruleMatches(rule schema.GovernanceRule, c *contract.Contract, risk contract.RiskLevel) bool {
	// Default rules always match (they're the fallback)
	if rule.Default != "" {
		return true
	}

	// Risk-based matching
	if rule.Risk != "" {
		if contract.RiskLevel(rule.Risk) == risk {
			return true
		}
		return false
	}

	// Effects-based matching (new taxonomy)
	if len(rule.Effects) > 0 {
		if !hasAny(c.Effects, rule.Effects) {
			return false
		}
		// If rule also specifies writes, both must match
		if rule.Contract != nil && len(rule.Contract.Writes) > 0 {
			if !hasAny(c.Writes, rule.Contract.Writes) {
				return false
			}
		}
		return true
	}

	// Contract-based matching (writes/reads)
	if rule.Contract != nil {
		return contractMatches(rule.Contract, c)
	}

	return false
}

// contractMatches checks if a contract's reads/writes match a governance contract pattern.
func contractMatches(gc *schema.GovernanceContract, c *contract.Contract) bool {
	if len(gc.Writes) > 0 {
		if !hasAny(c.Writes, gc.Writes) {
			return false
		}
	}
	if len(gc.Reads) > 0 {
		if !hasAny(c.Reads, gc.Reads) {
			return false
		}
	}
	return len(gc.Writes) > 0 || len(gc.Reads) > 0
}

// hasAny returns true if any element of `needles` appears in `haystack`.
func hasAny(haystack, needles []string) bool {
	set := make(map[string]struct{}, len(haystack))
	for _, s := range haystack {
		set[s] = struct{}{}
	}
	for _, n := range needles {
		if _, ok := set[n]; ok {
			return true
		}
	}
	return false
}

func describeRule(rule schema.GovernanceRule) string {
	if rule.Default != "" {
		return "default: " + rule.Default
	}
	if rule.Risk != "" {
		return "risk: " + rule.Risk
	}
	if rule.Contract != nil {
		return "contract match"
	}
	return "unknown"
}
