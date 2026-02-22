# Mapping Report — Executing a CAS Command

## Step Mapping Table

| Step ID | TSG Section | Type | Justification |
|-------|-------------|------|---------------|
| open_ds_consolidated_console | Opening a CAS command window | manual | Pure prose instructions with UI actions; no executable commands. |
| open_sterling_servers_databases_view | Executing a CAS command | xts | Explicit reference to opening a `.xts` view to obtain context and CAS commands. |
| select_database_and_open_cas_commands | Executing a CAS command | manual | UI-driven selection steps described in prose; no CLI commands provided. |
| execute_cas_command | Executing a CAS command | manual | CAS commands are potentially destructive and not explicitly specified; requires human judgment. |

## Extracted Variables

The following variables were identified from the TSG and added to `meta.vars` with empty defaults:

- `environment`
- `cluster`
- `tenant_ring_name`
- `fabric_cluster_name`
- `node_name`
- `app_name`
- `instance_name`
- `logical_server_name`
- `database_name`

## Manual Step Reasons

- No explicit CLI commands were provided in the TSG.
- CAS commands can be destructive (e.g., killing instances, DBCC operations).
- The TSG emphasizes UI-based workflows (XTS, DS Consolidated Console) and human selection of commands.

## TODOs / Uncertainties

- **CAS command validation**: The exact CAS command to execute is not specified and must be validated for safety and correctness before execution.
- **Approvals**: The TSG does not define approval requirements, but organizational policy may require them for high-impact CAS commands.
- **XTS environment**: The specific XTS environment is context-dependent and left unspecified.