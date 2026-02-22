# Mapping Report

## Step Mapping Table

| Step ID                       | TSG Section                       | Type   | Justification |
|------------------------------|-----------------------------------|--------|---------------|
| check_pod_status              | 1. Check Pod Status               | cli    | Contains a fenced bash code block invoking `kubectl get pods`. |
| get_pod_logs                  | 2. Get Pod Logs                   | cli    | Contains a fenced bash code block invoking `kubectl logs`. |
| check_cluster_events          | 3. Check Events                   | cli    | Contains a fenced bash code block invoking `kubectl get events`. |
| validate_monitoring_dashboard | 4. Validate Monitoring Dashboard  | manual | Pure prose instructions with a bullet list and evidence requirement (screenshot). |

## Extracted Variables

The following variables were extracted from the TSG and added to `meta.vars` with empty defaults:

- `NAMESPACE` → `namespace`
- `SERVICE` → `service`

All variable references in CLI commands were converted to Go template syntax.

## Manual Steps and Reasons

- **validate_monitoring_dashboard**: Marked as a manual step because it requires human judgment to review a Grafana dashboard and capture a screenshot. No CLI commands were provided in the TSG for this activity.

## TODOs / Uncertainties

- No explicit Grafana URL or dashboard identifier was provided; the operator must determine the correct dashboard for the service.
- No explicit success/failure thresholds beyond the listed metrics were defined for automated assertions, so none were included.

## Notes

- `kubectl` was the only executable present in the TSG and is the sole entry in `meta.governance.allowed_commands`.
- No explicit timeouts, approvals, or replay modes were mentioned in the TSG, so they were omitted per the rules.