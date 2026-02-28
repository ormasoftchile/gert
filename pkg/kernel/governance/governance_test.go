package governance

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/kernel/contract"
	"github.com/ormasoftchile/gert/pkg/kernel/schema"
)

func boolPtr(b bool) *bool { return &b }

func TestEvaluate_NilPolicy(t *testing.T) {
	c := &contract.Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(false), Deterministic: boolPtr(false)}
	d := Evaluate(c, nil)
	if d.Action != schema.DecisionAllow {
		t.Errorf("nil policy should allow, got %q", d.Action)
	}
	if d.RiskLevel != contract.RiskCritical {
		t.Errorf("risk = %q, want critical", d.RiskLevel)
	}
}

func TestEvaluate_RiskBased(t *testing.T) {
	policy := &schema.GovernancePolicy{
		Rules: []schema.GovernanceRule{
			{Risk: "critical", Action: "require-approval", MinApprovers: 2},
			{Risk: "high", Action: "require-approval"},
			{Default: "allow"},
		},
	}

	tests := []struct {
		name   string
		c      contract.Contract
		want   schema.GovernanceDecision
		minApp int
	}{
		{
			name: "critical → require-approval with 2 approvers",
			c:    contract.Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(false), Deterministic: boolPtr(false)},
			want: schema.DecisionRequireApproval, minApp: 2,
		},
		{
			name: "high → require-approval",
			c:    contract.Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(false), Deterministic: boolPtr(true)},
			want: schema.DecisionRequireApproval, minApp: 0,
		},
		{
			name: "medium → default allow",
			c:    contract.Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(true)},
			want: schema.DecisionAllow,
		},
		{
			name: "low → default allow",
			c:    contract.Contract{SideEffects: boolPtr(false)},
			want: schema.DecisionAllow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			d := Evaluate(&tt.c, policy)
			if d.Action != tt.want {
				t.Errorf("action = %q, want %q", d.Action, tt.want)
			}
			if d.MinApprovers != tt.minApp {
				t.Errorf("min_approvers = %d, want %d", d.MinApprovers, tt.minApp)
			}
		})
	}
}

func TestEvaluate_ContractBased(t *testing.T) {
	policy := &schema.GovernancePolicy{
		Rules: []schema.GovernanceRule{
			{Contract: &schema.GovernanceContract{Writes: []string{"production"}}, Action: "require-approval"},
			{Default: "allow"},
		},
	}

	// Contract writes to production → require-approval
	c := &contract.Contract{
		SideEffects: boolPtr(true),
		Writes:      []string{"production", "cache"},
	}
	d := Evaluate(c, policy)
	if d.Action != schema.DecisionRequireApproval {
		t.Errorf("writes production should require approval, got %q", d.Action)
	}

	// Contract doesn't write to production → allow
	c2 := &contract.Contract{
		SideEffects: boolPtr(true),
		Writes:      []string{"cache"},
	}
	d2 := Evaluate(c2, policy)
	if d2.Action != schema.DecisionAllow {
		t.Errorf("writes only cache should allow, got %q", d2.Action)
	}
}

func TestEvaluate_DenyRule(t *testing.T) {
	policy := &schema.GovernancePolicy{
		Rules: []schema.GovernanceRule{
			{Risk: "critical", Action: "deny"},
		},
	}
	c := &contract.Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(false), Deterministic: boolPtr(false)}
	d := Evaluate(c, policy)
	if d.Action != schema.DecisionDeny {
		t.Errorf("critical + deny rule should deny, got %q", d.Action)
	}
}

func TestMostRestrictive(t *testing.T) {
	allow := Decision{Action: schema.DecisionAllow}
	approval := Decision{Action: schema.DecisionRequireApproval}
	deny := Decision{Action: schema.DecisionDeny}

	if MostRestrictive(allow, approval).Action != schema.DecisionRequireApproval {
		t.Error("approval > allow")
	}
	if MostRestrictive(approval, deny).Action != schema.DecisionDeny {
		t.Error("deny > approval")
	}
	if MostRestrictive(allow, deny).Action != schema.DecisionDeny {
		t.Error("deny > allow")
	}
}

// T016: Governance matches on effects + writes
func TestEvaluate_EffectsBased(t *testing.T) {
	policy := &schema.GovernancePolicy{
		Rules: []schema.GovernanceRule{
			{Effects: []string{"kubernetes"}, Contract: &schema.GovernanceContract{Writes: []string{"production"}}, Action: "require-approval", MinApprovers: 2},
			{Effects: []string{"network"}, Action: "allow"},
			{Default: "allow"},
		},
	}

	// kubernetes + production writes → require-approval
	c1 := &contract.Contract{
		Effects: []string{"kubernetes"},
		Writes:  []string{"production"},
	}
	d1 := Evaluate(c1, policy)
	if d1.Action != schema.DecisionRequireApproval {
		t.Errorf("kubernetes+production should require approval, got %q", d1.Action)
	}
	if d1.MinApprovers != 2 {
		t.Errorf("min_approvers = %d, want 2", d1.MinApprovers)
	}

	// network effects only → allow
	c2 := &contract.Contract{
		Effects: []string{"network"},
	}
	d2 := Evaluate(c2, policy)
	if d2.Action != schema.DecisionAllow {
		t.Errorf("network should allow, got %q", d2.Action)
	}

	// kubernetes but no production writes → default allow (doesn't match first rule)
	c3 := &contract.Contract{
		Effects: []string{"kubernetes"},
		Writes:  []string{"staging"},
	}
	d3 := Evaluate(c3, policy)
	if d3.Action != schema.DecisionAllow {
		t.Errorf("kubernetes+staging should default allow, got %q", d3.Action)
	}
}
