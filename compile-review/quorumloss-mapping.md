# Mapping Report — Recover from Quorum Loss (QL) with Auxiliary Replica

## Mapping Table

| Step ID | TSG Heading Path | Source Lines | Type | Justification |
|-------|-----------------|--------------|------|---------------|
| verify_prerequisites | ### Pre-requisites | L15–L43 | manual | Pure prose checklist of safety conditions; requires human validation. |
| initiate_recovery_from_aux | ### Recover Service from Aux replica | L52–L83 | manual | CAS command is risky and state-changing; requires judgment. |
| signal_data_loss_if_blocked | ### Recover Service from Aux replica | L85–L94 | manual | Data loss signaling is destructive and must remain manual. |
| track_restore_request_id | ### Track Progress and Debugging > Kusto Queries | L110–L126 | xts | Inline Kusto query provided to fetch restore_request_id. |
| track_restore_progress | ### Track Progress and Debugging > Kusto Queries | L128–L136 | xts | Inline Kusto query to monitor restore progress. |
| track_restore_details | ### Track Progress and Debugging > Kusto Queries | L138–L146 | xts | Inline Kusto query to inspect restore details. |
| check_management_operation | ### Management Service Telemetry | L154–L166 | xts | Kusto query to track management operation lifecycle. |
| check_seeding_failures | ### Seeding Failures | L180–L190 | xts | Kusto query against MonSQLSystemHealth for failure signals. |
| cms_recovery_state | ### CMS Query | L214–L232 | xts | CMS query explicitly provided in TSG. |
| cms_fabric_service_state | ### CMS Query | L234–L252 | xts | CMS query for fabric_services state machine. |
| assess_completion_or_escalate | ### Seeding Failures / Mitigation | L192–L205 | manual | Decision and escalation guidance is human-driven. |

## Source Excerpts

### verify_prerequisites
> “Below are the checks required to ensure if recovery from Aux should be used or not…”

### initiate_recovery_from_aux
> “Syntax: `Get-FabricService -ServiceName <fabric_service_uri> -ServiceClusterName <cluster_name> | Recover-FabricService`”

### signal_data_loss_if_blocked
> “If it is waiting on HADR_AsyncOpOnDataLoss then you need to fire Data loss signal.”

### track_restore_request_id
> “MonRestoreEvents | where AppName == … | where isnotempty(restore_request_id)”

### track_restore_progress
> “Grab the restore_request_id… and input that in the below queries”

### track_restore_details
> “| where isnotempty(details)”

### check_management_operation
> “MonManagementOperations | where operation_type contains ‘RecoverServicePartitions’”

### check_seeding_failures
> “If the seeding has failed… you can see the metrics in MonSQLSystemHealth”

### cms_recovery_state
> “The CMS table that tracks progress of recovery is recover_service_partitions_requests.”

### cms_fabric_service_state
> “You can query the fabric_services table… to determine at what point in the workflow it's in.”

### assess_completion_or_escalate
> “If seeding is failing continuously… reach out to the customer to do a point in time restore.”

## Extracted Variables

- environment (ICM occurring location)
- cluster_name (parsed from incident title)
- fabric_service_uri (prompt)
- app_name (prompt)
- aux_replica_node_name (prompt)
- partition_id (prompt)
- restore_request_id (runtime capture / prompt)
- management_request_id (prompt)

## Manual Step Reasons

- CAS recovery and data loss signaling are high-risk and require expert judgment.
- Final decision to escalate to point-in-time restore is non-automatable.

## TODOs / Uncertainties

- Validate auxiliary replica health and PRIMARY_AUXILIARY role visually in XTS/SFE.
- Ensure backups are behind auxiliary replica commit time before recovery.
- Confirm no safer recovery path exists before signaling data loss.