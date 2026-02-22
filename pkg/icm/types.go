package icm

import "time"

// Incident represents the fields we care about from the ICM OData API.
type Incident struct {
	Id           int64     `json:"Id"`
	Title        string    `json:"Title"`
	Severity     int       `json:"Severity"`
	Status       string    `json:"Status"` // Active, Mitigated, Resolved, etc.
	CreateDate   time.Time `json:"CreateDate"`
	ModifiedDate time.Time `json:"ModifiedDate"`
	MitigateDate *IcmTime  `json:"MitigatedDate"`
	ResolveDate  *IcmTime  `json:"ResolvedDate"`

	ImpactStartDate *IcmTime `json:"ImpactStartDate"`

	OwningTeamId string `json:"OwningTeamId"`
	OwningTeam   *Team  `json:"OwningTeam,omitempty"`

	RoutingId     string `json:"RoutingId"`
	CorrelationId string `json:"CorrelationId"`

	// Occurring location fields from ICM
	OccuringLocation    *OccuringLocation `json:"OccuringLocation,omitempty"`
	OccuringDatacenter  string            `json:"OccuringDatacenter"`
	OccuringEnvironment string            `json:"OccuringEnvironment"`

	// Resolution / mitigation text
	MitigationData *MitigationData `json:"MitigationData,omitempty"`

	// Custom field groups — where server names, subscriptions, etc. live.
	// The ICM API returns an array of groups, each containing a CustomFields array.
	CustomFieldGroups []CustomFieldGroup `json:"CustomFieldGroups,omitempty"`

	// Flattened custom field values (populated by client after fetch)
	Fields map[string]string `json:"-"`
}

// Team is the owning team info.
type Team struct {
	Name string `json:"Name"`
	Id   string `json:"Id"`
}

// MitigationData holds resolution/mitigation notes.
type MitigationData struct {
	Entry string `json:"Entry"`
}

// OccuringLocation holds the occurring location details from the ICM API.
type OccuringLocation struct {
	Environment string `json:"Environment"`
	DataCenter  string `json:"DataCenter"`
	DeviceGroup string `json:"DeviceGroup"`
	DeviceName  string `json:"DeviceName"`
	Instance    string `json:"ServiceInstanceId"`
}

// CustomFieldGroup is a group of custom fields from the ICM API.
type CustomFieldGroup struct {
	GroupName string        `json:"GroupName"`
	Fields    []CustomField `json:"CustomFields"`
}

// CustomField is a single custom field key-value pair.
type CustomField struct {
	Name         string   `json:"Name"`
	StringValue  string   `json:"StringValue"`
	NumberValue  *float64 `json:"NumberValue"`
	BooleanValue *bool    `json:"BooleanValue"`
	Description  string   `json:"Description"`
}

// IcmTime handles the ICM OData date format which can be null.
type IcmTime struct {
	time.Time
}

// SearchResult wraps the OData response envelope.
type SearchResult struct {
	Value    []Incident `json:"value"`
	NextLink string     `json:"odata.nextLink,omitempty"`
	Count    int        `json:"odata.count,omitempty"`
}

// GetResult wraps a single incident response (v2 OData uses "d" wrapper).
type GetResult struct {
	D *Incident `json:"d,omitempty"`
}
