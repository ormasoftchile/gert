# ICM Custom Field Analysis

Analyzed 5 incidents from the SQL DB Availability team (Feb 2026).

## Custom Field Availability

| Custom Field | Field ID | 747241872 | 747870160 | 747822325 | 747007036 | 746631362 |
|---|---|---|---|---|---|---|
| ServerName | 28388 | 510014259 | sobeys-sql01 | smi-shs-sea-prd-01 | irt-ppdi-eastus-prod | credit-accounting-... |
| DatabaseName | 28389 | DedicatedContent_535_100 | AlarmDB | ApplicationData_1 | irt-ppdi | 429DCB66-... |
| AppName | 28392 | - | d32e1b0ec027 | - | aed2ea4bdbf8 | c100441c59c7 |
| PrimaryTenantRing | 28393 | - | tr48699.eastus1-a... | - | tr56909.eastus1-a... | tr21582.centralus1-a... |
| PrimaryNodeName | 28394 | - | _DB_48 | - | _DB_11 | _DB_19 |
| SLO | 26648 | - | SQLDB_HS_Gen5_2 | - | P11 | - |

## Key Findings

1. **ServerName + DatabaseName**: Available on 100% of incidents. Always populated by alert connector.

2. **AppName + PrimaryTenantRing + PrimaryNodeName**: Available on ~80% of incidents.
   - Present: Availability-routed, GeoDR, Socrates incidents
   - Missing: StorageEngine-routed, MI/Availability incidents
   - These are populated by a secondary enrichment step, not the initial alert.

3. **SLO**: Available on ~60%. Populated by yet another enrichment step.

4. **Implication for meta.inputs**: The `from: icm.customFields.*` binding needs a fallback mechanism.
   Some fields are guaranteed, others are probabilistic.

## Recommended Input Binding Strategy

```yaml
inputs:
  # Tier 1: Always available (100%)
  server_name:
    from: icm.customFields.ServerName
  database_name:
    from: icm.customFields.DatabaseName
  environment:
    from: icm.occuringLocation.instance
  start_time:
    from: icm.impactStartTime

  # Tier 2: Usually available (80%) — need fallback
  app_name:
    from: icm.customFields.AppName
    fallback: prompt
  tenant_ring:
    from: icm.customFields.PrimaryTenantRing
    fallback: prompt
  node_name:
    from: icm.customFields.PrimaryNodeName
    fallback: prompt

  # Tier 3: Sometimes available (60%) — always need fallback
  slo:
    from: icm.customFields.SLO
    fallback: prompt
```

## Incident Diversity

| ICM | Monitor | Routing | Team | TSG in title |
|---|---|---|---|---|
| 747241872 | LoginFailure | Storage Engine | Capacity Mgmt | - |
| 747870160 | LoginFailure | Availability | SQL DB Availability | - |
| 747822325 | AvailabilityLoss | MI/Availability | SQL DB Availability | TRDB0002 |
| 747007036 | GeoDrReplicationLag | GEODR | GeoDR | - |
| 746631362 | SocratesFrequentReconfig | Socrates | Socrates Data Plane | SOC036 |

## Schema Enhancement: `fallback` on InputDef

Add `fallback` field to `InputDef` struct:
```go
type InputDef struct {
    From        string `yaml:"from" json:"from" jsonschema:"required"`
    Fallback    string `yaml:"fallback,omitempty" json:"fallback,omitempty" jsonschema:"enum=prompt,enum=enrichment"`
    Pattern     string `yaml:"pattern,omitempty" json:"pattern,omitempty"`
    Description string `yaml:"description,omitempty" json:"description,omitempty"`
    Default     string `yaml:"default,omitempty" json:"default,omitempty"`
}
```
When `from: icm.customFields.AppName` returns empty, fall back to `prompt` (ask engineer).
