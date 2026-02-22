# Mapping Report: connectivity-kusto-queries

## Overview
This TSG is a **reference catalog** of Kusto queries used for SQL connectivity troubleshooting.  
It does not represent a single executable incident workflow, so the runbook is modeled as `meta.kind: reference`.

## Step Mapping Table

| Step ID | TSG Section | Type | Justification |
|-------|-------------|------|---------------|
| setup_kusto_environment | Setup | manual | Pure prose guidance, no executable commands. |
| global_fanout_execute_for_prod_clusters | Global/Fanout queries – Using `_ExecuteForProdClusters` | xts | Inline Kusto query intended to be run in Kusto/XTS. |
| global_fanout_get_cross_cluster_data | Global/Fanout queries – Using `GetCrossClusterData` | xts | Inline Kusto query for cross-cluster fanout. |
| cluster_login_rate | Availability & Login – Cluster login rate | xts | Standard MonLogin Kusto query producing a timechart. |
| ring_login_traffic_birds_eye | Availability & Login – Ring Login Traffic Characteristics | xts | Multi-metric Kusto aggregation query. |
| get_login_traces_by_server_db_app | Availability & Login – Get all login traces given a DB/Server/Appname | xts | Parameterized lookup query over MonLogin. |
| login_errors_and_timeouts_by_db | Availability & Login – Get all login errors and potential timeouts given a DB | xts | Diagnostic Kusto query with parameters. |
| networksxml_whitelist_ip | NetworksXML troubleshooting – Get networks xml entries that whitelist a given IP address | xts | Investigative Kusto query using an input IP. |
| proxy_failures_by_target_ring | Gateway proxy debugging – Proxy failures by target ring | xts | Aggregation query for proxy failures. |
| escalation_and_ownership | Escalation / Ownership | manual | Non-executable contact and ownership information. |

## Extracted Variables

| Variable | Description | Default |
|---------|-------------|---------|
| logical_server_name | Logical SQL server name | "" |
| database_name | Database name | "" |
| appname | Application name filter | "" |
| instance_name | Instance name / fabric alias | "" |
| input_ip | IP address to check in Networks XML | "" |

## Manual Step Reasons

- **setup_kusto_environment**: Contains only prose instructions and external documentation links.
- **escalation_and_ownership**: Contact and ownership information cannot be executed programmatically.

## TODOs / Uncertainties

- The original TSG contains a very large number of independent Kusto queries.  
  This runbook includes a **representative subset** of commonly used and structurally distinct queries.
- Additional queries from the TSG can be incrementally added as further `xts` steps following the same pattern.
- XTS environment was not explicitly specified in the TSG; `Prod` was assumed as a reasonable default.

## Notes

- All Kusto queries were modeled as `type: xts` with `mode: query` and `query_type: kusto` per rules.
- The runbook intentionally avoids a linear flow, reflecting the reference nature of the source document.