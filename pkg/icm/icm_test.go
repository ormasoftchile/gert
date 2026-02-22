package icm

import (
	"encoding/json"
	"testing"
)

// Real ICM API response shape for an Azure SQL DB incident.
// This is the contract we must not break.
const sampleICMJSON = `{
	"Id": 731796689,
	"Title": "[HighPriority] [Sev-2 LoginFailureCause: IsUnplacedReplica] [ProdEus2a/AIMS://AZURE SQL DB/Availability] Sterling CRGW0001: Login success rate is below 99%",
	"Severity": 2,
	"Status": "Active",
	"RoutingId": "AIMS://AZURE SQL DB/Availability",
	"CorrelationId": "ProdEus2a/AIMS://AZURE SQL DB/Availability",
	"OccuringDatacenter": "East US 2",
	"OccuringEnvironment": "Production",
	"OccuringLocation": {
		"Environment": "Production",
		"DataCenter": "East US 2",
		"DeviceGroup": "",
		"DeviceName": "",
		"ServiceInstanceId": "ProdEus2a"
	},
	"CustomFieldGroups": [
		{
			"GroupName": "SQL DB Connectivity",
			"CustomFields": [
				{"Name": "ServerName", "StringValue": "devcp2-sql-server", "NumberValue": null, "BooleanValue": null},
				{"Name": "DatabaseName", "StringValue": "citrixm9vw6w4t14hz", "NumberValue": null, "BooleanValue": null}
			]
		},
		{
			"GroupName": "Metrics",
			"CustomFields": [
				{"Name": "LoginSuccessRate", "NumberValue": 95.3, "StringValue": "", "BooleanValue": null},
				{"Name": "IsS500", "StringValue": "", "NumberValue": null, "BooleanValue": true}
			]
		}
	]
}`

func TestIncidentDeserialization(t *testing.T) {
	var inc Incident
	if err := json.Unmarshal([]byte(sampleICMJSON), &inc); err != nil {
		t.Fatalf("Unmarshal failed: %v", err)
	}

	// Basic fields
	if inc.Id != 731796689 {
		t.Errorf("Id = %d, want 731796689", inc.Id)
	}
	if inc.Severity != 2 {
		t.Errorf("Severity = %d, want 2", inc.Severity)
	}
	if inc.CorrelationId != "ProdEus2a/AIMS://AZURE SQL DB/Availability" {
		t.Errorf("CorrelationId = %q", inc.CorrelationId)
	}

	// OccuringLocation must not be nil
	if inc.OccuringLocation == nil {
		t.Fatal("OccuringLocation is nil")
	}
	if inc.OccuringLocation.Instance != "ProdEus2a" {
		t.Errorf("OccuringLocation.Instance = %q, want ProdEus2a", inc.OccuringLocation.Instance)
	}
	if inc.OccuringLocation.DataCenter != "East US 2" {
		t.Errorf("OccuringLocation.DataCenter = %q, want East US 2", inc.OccuringLocation.DataCenter)
	}
	if inc.OccuringDatacenter != "East US 2" {
		t.Errorf("OccuringDatacenter = %q, want East US 2", inc.OccuringDatacenter)
	}

	// CustomFieldGroups must parse nested structure
	if len(inc.CustomFieldGroups) != 2 {
		t.Fatalf("CustomFieldGroups = %d groups, want 2", len(inc.CustomFieldGroups))
	}
	g0 := inc.CustomFieldGroups[0]
	if g0.GroupName != "SQL DB Connectivity" {
		t.Errorf("Group[0].GroupName = %q", g0.GroupName)
	}
	if len(g0.Fields) != 2 {
		t.Fatalf("Group[0].Fields = %d, want 2", len(g0.Fields))
	}
	if g0.Fields[0].Name != "ServerName" || g0.Fields[0].StringValue != "devcp2-sql-server" {
		t.Errorf("Group[0].Fields[0] = %+v, want ServerName=devcp2-sql-server", g0.Fields[0])
	}
	if g0.Fields[1].Name != "DatabaseName" || g0.Fields[1].StringValue != "citrixm9vw6w4t14hz" {
		t.Errorf("Group[0].Fields[1] = %+v, want DatabaseName=citrixm9vw6w4t14hz", g0.Fields[1])
	}
}

func TestFlattenCustomFields(t *testing.T) {
	var inc Incident
	if err := json.Unmarshal([]byte(sampleICMJSON), &inc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}

	flattenCustomFields(&inc)

	tests := map[string]string{
		"ServerName":   "devcp2-sql-server",
		"DatabaseName": "citrixm9vw6w4t14hz",
		"LoginSuccessRate": "95.3",
		"IsS500":       "true",
	}
	for name, want := range tests {
		got, ok := inc.Fields[name]
		if !ok {
			t.Errorf("Fields[%q] missing", name)
			continue
		}
		if got != want {
			t.Errorf("Fields[%q] = %q, want %q", name, got, want)
		}
	}
}

func TestFlattenCustomFields_Empty(t *testing.T) {
	inc := Incident{}
	flattenCustomFields(&inc)
	if inc.Fields == nil {
		t.Error("Fields should be initialized even with no groups")
	}
	if len(inc.Fields) != 0 {
		t.Errorf("Fields should be empty, got %v", inc.Fields)
	}
}

func TestOccuringLocation_Nil(t *testing.T) {
	raw := `{"Id":1,"Title":"test","Severity":3}`
	var inc Incident
	if err := json.Unmarshal([]byte(raw), &inc); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if inc.OccuringLocation != nil {
		t.Error("OccuringLocation should be nil when not in JSON")
	}
}
