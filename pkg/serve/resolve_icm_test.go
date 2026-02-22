package serve

import (
	"testing"

	"github.com/ormasoftchile/gert/pkg/icm"
	"github.com/ormasoftchile/gert/pkg/schema"
)

// testIncident returns an Incident matching a real Azure SQL DB login-success-rate incident.
func testIncident() *icm.Incident {
	inc := &icm.Incident{
		Id:            731796689,
		Title:         "[HighPriority] [Sev-2 LoginFailureCause: IsUnplacedReplica] [ProdEus2a/AIMS://AZURE SQL DB/Availability] Sterling CRGW0001: Login success rate is below 99% [Pulse:][S500:CITRIX SYSTEMS INC]",
		Severity:      2,
		RoutingId:     "AIMS://AZURE SQL DB/Availability",
		CorrelationId: "ProdEus2a/AIMS://AZURE SQL DB/Availability",
		OccuringLocation: &icm.OccuringLocation{
			Environment: "Production",
			DataCenter:  "East US 2",
			Instance:    "ProdEus2a",
		},
		OccuringDatacenter:  "East US 2",
		OccuringEnvironment: "Production",
		Fields: map[string]string{
			"ServerName":   "devcp2-sql-server",
			"DatabaseName": "citrixm9vw6w4t14hz",
		},
	}
	return inc
}

func TestResolveICMInputs_CustomFields(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"server_name": {
			From:        "icm.customFields.ServerName",
			Description: "Server name",
		},
		"database_name": {
			From:        "icm.customFields.DatabaseName",
			Description: "Database name",
		},
	}

	resolved := resolveICMInputs(inputs, inc)

	assertResolved(t, resolved, "server_name", "devcp2-sql-server")
	assertResolved(t, resolved, "database_name", "citrixm9vw6w4t14hz")
}

func TestResolveICMInputs_OccuringLocation(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"environment": {
			From: "icm.occuringLocation.instance",
		},
	}

	resolved := resolveICMInputs(inputs, inc)
	assertResolved(t, resolved, "environment", "ProdEus2a")
}

func TestResolveICMInputs_OccuringLocation_FallbackToCorrelationId(t *testing.T) {
	inc := testIncident()
	inc.OccuringLocation = nil // no location object

	inputs := map[string]*schema.InputDef{
		"environment": {
			From: "icm.occuringLocation.instance",
		},
	}

	resolved := resolveICMInputs(inputs, inc)
	assertResolved(t, resolved, "environment", "ProdEus2a") // from CorrelationId prefix
}

func TestResolveICMInputs_TitleWithPattern(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"login_failure_cause": {
			From:    "icm.title",
			Pattern: `LoginFailureCause:\s*(\w+)`,
		},
	}

	resolved := resolveICMInputs(inputs, inc)
	assertResolved(t, resolved, "login_failure_cause", "IsUnplacedReplica")
}

func TestResolveICMInputs_TitleWithoutPattern(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"full_title": {
			From: "icm.title",
		},
	}

	resolved := resolveICMInputs(inputs, inc)
	if got := resolved["full_title"]; got != inc.Title {
		t.Errorf("full_title = %q, want full title", got)
	}
}

func TestResolveICMInputs_Severity(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"sev": {From: "icm.severity"},
	}
	resolved := resolveICMInputs(inputs, inc)
	assertResolved(t, resolved, "sev", "2")
}

func TestResolveICMInputs_RoutingAndCorrelation(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"routing":     {From: "icm.routingId"},
		"correlation": {From: "icm.correlationId"},
	}
	resolved := resolveICMInputs(inputs, inc)
	assertResolved(t, resolved, "routing", "AIMS://AZURE SQL DB/Availability")
	assertResolved(t, resolved, "correlation", "ProdEus2a/AIMS://AZURE SQL DB/Availability")
}

func TestResolveICMInputs_LocationRegion(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"region": {From: "icm.location.Region"},
	}
	resolved := resolveICMInputs(inputs, inc)
	assertResolved(t, resolved, "region", "East US 2")
}

func TestResolveICMInputs_PromptFallbackToTitleExtraction(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"login_failure_cause": {
			From:        "prompt",
			Description: "Login failure cause",
		},
	}

	resolved := resolveICMInputs(inputs, inc)
	// snakeToPascal("login_failure_cause") = "LoginFailureCause" → matches title pattern
	assertResolved(t, resolved, "login_failure_cause", "IsUnplacedReplica")
}

func TestResolveICMInputs_PromptNoMatch(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"some_random_thing": {
			From: "prompt",
		},
	}
	resolved := resolveICMInputs(inputs, inc)
	if _, ok := resolved["some_random_thing"]; ok {
		t.Error("should not resolve a prompt input with no title match")
	}
}

func TestResolveICMInputs_MissingCustomField(t *testing.T) {
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"subscription_id": {From: "icm.customFields.SubscriptionId"},
	}
	resolved := resolveICMInputs(inputs, inc)
	if _, ok := resolved["subscription_id"]; ok {
		t.Error("should not resolve a missing custom field")
	}
}

func TestResolveICMInputs_FullLoginSuccessRateRunbook(t *testing.T) {
	// End-to-end: exactly the inputs from login-success-rate-below-target.runbook.yaml
	inc := testIncident()
	inputs := map[string]*schema.InputDef{
		"server_name": {
			From:        "icm.customFields.ServerName",
			Description: "Logical SQL server name from the incident.",
		},
		"database_name": {
			From:        "icm.customFields.DatabaseName",
			Description: "Database name from the incident.",
		},
		"environment": {
			From:        "icm.occuringLocation.instance",
			Description: "XTS environment from incident location.",
		},
		"login_failure_cause": {
			From:        "icm.title",
			Pattern:     `LoginFailureCause:\s*(\w+)`,
			Description: "Login failure cause from incident title.",
		},
	}

	resolved := resolveICMInputs(inputs, inc)

	expect := map[string]string{
		"server_name":         "devcp2-sql-server",
		"database_name":       "citrixm9vw6w4t14hz",
		"environment":         "ProdEus2a",
		"login_failure_cause": "IsUnplacedReplica",
	}

	if len(resolved) != len(expect) {
		t.Errorf("resolved %d inputs, want %d: %v", len(resolved), len(expect), resolved)
	}
	for k, want := range expect {
		assertResolved(t, resolved, k, want)
	}
}

// --- Title extraction tests ---

func TestExtractTitleFields(t *testing.T) {
	title := "[HighPriority] [Sev-2 LoginFailureCause: IsUnplacedReplica] [ProdEus2a/AIMS://AZURE SQL DB/Availability] Sterling CRGW0001: Login success rate is below 99%"

	fields := extractTitleFields(title)

	tests := map[string]string{
		"LoginFailureCause": "IsUnplacedReplica",
		"Sev":               "2",
		"ClusterName":       "CRGW0001",
	}
	for key, want := range tests {
		got, ok := fields[key]
		if !ok {
			t.Errorf("extractTitleFields: missing key %q", key)
			continue
		}
		if got != want {
			t.Errorf("extractTitleFields[%q] = %q, want %q", key, got, want)
		}
	}
}

func TestExtractTitleFields_NoMatch(t *testing.T) {
	fields := extractTitleFields("Just a plain title with no patterns")
	if len(fields) != 0 {
		t.Errorf("expected empty map, got %v", fields)
	}
}

func TestExtractTitleFields_SLO(t *testing.T) {
	title := "[Sev-3 SLO: GP_Gen5_2] Some alert"
	fields := extractTitleFields(title)
	assertField(t, fields, "SLO", "GP_Gen5_2")
	assertField(t, fields, "Sev", "3")
}

// --- snakeToPascal tests ---

func TestSnakeToPascal(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"login_failure_cause", "LoginFailureCause"},
		{"server_name", "ServerName"},
		{"database_name", "DatabaseName"},
		{"environment", "Environment"},
		{"a", "A"},
		{"", ""},
		{"already_pascal", "AlreadyPascal"},
		{"one_two_three_four", "OneTwoThreeFour"},
	}
	for _, tt := range tests {
		got := snakeToPascal(tt.in)
		if got != tt.want {
			t.Errorf("snakeToPascal(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func resolveICMInputs(inputs map[string]*schema.InputDef, inc *icm.Incident) map[string]string {
	return icm.ResolveInputs(inputs, inc)
}

func extractTitleFields(title string) map[string]string {
	return icm.ExtractTitleFields(title)
}

func snakeToPascal(s string) string {
	return icm.SnakeToPascal(s)
}

// --- helpers ---

func assertResolved(t *testing.T, resolved map[string]string, key, want string) {
	t.Helper()
	got, ok := resolved[key]
	if !ok {
		t.Errorf("resolved[%q] missing (have: %v)", key, resolved)
		return
	}
	if got != want {
		t.Errorf("resolved[%q] = %q, want %q", key, got, want)
	}
}

func assertField(t *testing.T, fields map[string]string, key, want string) {
	t.Helper()
	got, ok := fields[key]
	if !ok {
		t.Errorf("fields[%q] missing", key)
		return
	}
	if got != want {
		t.Errorf("fields[%q] = %q, want %q", key, got, want)
	}
}
