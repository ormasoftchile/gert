package contract

import (
	"testing"
)

func TestRisk(t *testing.T) {
	tests := []struct {
		name     string
		contract Contract
		want     RiskLevel
	}{
		{
			name:     "no side effects → low",
			contract: Contract{SideEffects: boolPtr(false)},
			want:     RiskLow,
		},
		{
			name:     "side effects + idempotent → medium",
			contract: Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(true)},
			want:     RiskMedium,
		},
		{
			name:     "side effects + not idempotent + deterministic → high",
			contract: Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(false), Deterministic: boolPtr(true)},
			want:     RiskHigh,
		},
		{
			name:     "side effects + not idempotent + not deterministic → critical",
			contract: Contract{SideEffects: boolPtr(true), Idempotent: boolPtr(false), Deterministic: boolPtr(false)},
			want:     RiskCritical,
		},
		{
			name:     "all nil (defaults) → critical",
			contract: Contract{},
			want:     RiskCritical,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.contract.Risk()
			if got != tt.want {
				t.Errorf("Risk() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolved(t *testing.T) {
	c := Contract{}
	r := c.Resolved()

	if *r.SideEffects != true {
		t.Error("expected SideEffects default true")
	}
	if *r.Deterministic != false {
		t.Error("expected Deterministic default false")
	}
	if *r.Idempotent != false {
		t.Error("expected Idempotent default false")
	}
	if r.Reads == nil || len(r.Reads) != 0 {
		t.Error("expected empty Reads slice")
	}
	if r.Writes == nil || len(r.Writes) != 0 {
		t.Error("expected empty Writes slice")
	}
}

func TestCanTighten(t *testing.T) {
	tests := []struct {
		name      string
		parent    Contract
		child     Contract
		wantValid bool
	}{
		{
			name:      "no changes is valid",
			parent:    Contract{SideEffects: boolPtr(true)},
			child:     Contract{},
			wantValid: true,
		},
		{
			name:      "tighten side_effects false → true",
			parent:    Contract{SideEffects: boolPtr(false)},
			child:     Contract{SideEffects: boolPtr(true)},
			wantValid: true,
		},
		{
			name:      "relax side_effects true → false",
			parent:    Contract{SideEffects: boolPtr(true)},
			child:     Contract{SideEffects: boolPtr(false)},
			wantValid: false,
		},
		{
			name:      "relax deterministic false → true",
			parent:    Contract{Deterministic: boolPtr(false)},
			child:     Contract{Deterministic: boolPtr(true)},
			wantValid: false,
		},
		{
			name:      "relax idempotent false → true",
			parent:    Contract{Idempotent: boolPtr(false)},
			child:     Contract{Idempotent: boolPtr(true)},
			wantValid: false,
		},
		{
			name:      "add reads tags is valid",
			parent:    Contract{Reads: []string{"network"}},
			child:     Contract{Reads: []string{"network", "database"}},
			wantValid: true,
		},
		{
			name:      "remove reads tags is invalid",
			parent:    Contract{Reads: []string{"network", "database"}},
			child:     Contract{Reads: []string{"network"}},
			wantValid: false,
		},
		{
			name:      "add writes tags is valid",
			parent:    Contract{Writes: []string{"service"}},
			child:     Contract{Writes: []string{"service", "filesystem"}},
			wantValid: true,
		},
		{
			name:      "remove writes tags is invalid",
			parent:    Contract{Writes: []string{"service", "filesystem"}},
			child:     Contract{Writes: []string{"service"}},
			wantValid: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			violations := CanTighten(&tt.parent, &tt.child)
			if tt.wantValid && len(violations) > 0 {
				t.Errorf("expected valid tightening, got violations: %v", violations)
			}
			if !tt.wantValid && len(violations) == 0 {
				t.Errorf("expected tightening violations, got none")
			}
		})
	}
}

func TestHasConflict(t *testing.T) {
	tests := []struct {
		name string
		a, b Contract
		want bool
	}{
		{
			name: "both read same → no conflict",
			a:    Contract{Reads: []string{"network"}, Writes: []string{}},
			b:    Contract{Reads: []string{"network"}, Writes: []string{}},
			want: false,
		},
		{
			name: "A writes what B reads → conflict",
			a:    Contract{Reads: []string{}, Writes: []string{"service"}},
			b:    Contract{Reads: []string{"service"}, Writes: []string{}},
			want: true,
		},
		{
			name: "both write same → conflict",
			a:    Contract{Reads: []string{}, Writes: []string{"service"}},
			b:    Contract{Reads: []string{}, Writes: []string{"service"}},
			want: true,
		},
		{
			name: "no overlap → no conflict",
			a:    Contract{Reads: []string{"network"}, Writes: []string{"cache"}},
			b:    Contract{Reads: []string{"database"}, Writes: []string{"log"}},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := HasConflict(&tt.a, &tt.b)
			if got != tt.want {
				t.Errorf("HasConflict() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestMerge(t *testing.T) {
	parent := Contract{
		Inputs:      map[string]ParamDef{"url": {Type: "string"}},
		SideEffects: boolPtr(false),
		Reads:       []string{"network"},
	}
	child := Contract{
		Inputs:      map[string]ParamDef{"payload": {Type: "string"}},
		SideEffects: boolPtr(true),
		Writes:      []string{"network"},
	}

	merged := Merge(&parent, &child)

	if _, ok := merged.Inputs["url"]; !ok {
		t.Error("parent input 'url' should be preserved")
	}
	if _, ok := merged.Inputs["payload"]; !ok {
		t.Error("child input 'payload' should be merged")
	}
	if *merged.SideEffects != true {
		t.Error("child SideEffects should override parent")
	}
	// Reads should be union
	if len(merged.Reads) != 1 || merged.Reads[0] != "network" {
		t.Errorf("Reads should be union: got %v", merged.Reads)
	}
}

func TestAssertContract(t *testing.T) {
	c := AssertContract()
	if *c.SideEffects != false {
		t.Error("assert: side_effects should be false")
	}
	if *c.Deterministic != true {
		t.Error("assert: deterministic should be true")
	}
	if *c.Idempotent != true {
		t.Error("assert: idempotent should be true")
	}
}

func TestManualDefaults(t *testing.T) {
	c := ManualDefaults()
	if *c.SideEffects != true {
		t.Error("manual: side_effects should be true")
	}
	if *c.Deterministic != false {
		t.Error("manual: deterministic should be false")
	}
	if *c.Idempotent != false {
		t.Error("manual: idempotent should be false")
	}
}

// T014: Effects field accepted by contract
func TestRisk_WithEffects(t *testing.T) {
	tests := []struct {
		name string
		c    Contract
		want RiskLevel
	}{
		{
			name: "effects but no writes → low",
			c:    Contract{Effects: []string{"network"}, Writes: []string{}},
			want: RiskLow,
		},
		{
			name: "effects + writes + idempotent → medium",
			c:    Contract{Effects: []string{"kubernetes"}, Writes: []string{"pods"}, Idempotent: boolPtr(true)},
			want: RiskMedium,
		},
		{
			name: "effects + writes + not idempotent + deterministic → high",
			c:    Contract{Effects: []string{"kubernetes"}, Writes: []string{"pods"}, Idempotent: boolPtr(false), Deterministic: boolPtr(true)},
			want: RiskHigh,
		},
		{
			name: "effects + writes + not idempotent + not deterministic → critical",
			c:    Contract{Effects: []string{"kubernetes"}, Writes: []string{"pods"}, Idempotent: boolPtr(false), Deterministic: boolPtr(false)},
			want: RiskCritical,
		},
		{
			name: "no effects, no writes → low",
			c:    Contract{Effects: []string{}},
			want: RiskLow,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tt.c.Risk()
			if got != tt.want {
				t.Errorf("Risk() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestResolved_IncludesEffects(t *testing.T) {
	c := Contract{Effects: []string{"network"}}
	r := c.Resolved()
	if len(r.Effects) != 1 || r.Effects[0] != "network" {
		t.Errorf("resolved effects = %v, want [network]", r.Effects)
	}
}
