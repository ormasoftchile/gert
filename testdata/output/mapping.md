# Mapping Report: Pod CrashLoopBackOff Investigation

## Step Mapping Table

| Step ID                         | TSG Section                     | Type   | Justification |
|--------------------------------|----------------------------------|--------|---------------|
| check_pod_status               | 1. Check Pod Status              | cli    | Section contains an explicit kubectl command in a fenced bash code block. |
| get_pod_logs                   | 2. Get Pod Logs                  | cli    | Section provides a kubectl logs command in a fenced bash code block. |
| check_cluster_events           | 3. Check Events                  | cli    | Section includes a kubectl get events command in a fenced bash code block. |
| validate_monitoring_dashboard  | 4. Validate Monitoring Dashboard | manual | Section is prose-only and requires human judgment and external UI interaction (Grafana). |

## Extracted Variables

The following variables were extracted from the TSG and added to `meta.vars` with empty defaults:
- `NAMESPACE` → `namespace`
- `SERVICE` → `service`

These variables were replaced in CLI arguments using Go template syntax.

## Manual Steps and Evidence

- **validate_monitoring_dashboard**
  - Reason: Requires opening Grafana, visually confirming metrics, and taking a screenshot; cannot be automated via CLI.
  - Evidence required:
    - Checklist confirming error rate, latency, and traffic conditions.
    - Screenshot attachment of the Grafana dashboard.

## TODOs / Uncertainties

- No explicit Grafana URL, dashboard name, or access method was provided in the TSG. Operators must locate the correct dashboard manually based on the service context.
- No explicit success/failure thresholds beyond those listed; interpretation of “anomalous traffic patterns” relies on operator judgment.

## Notes

- Only commands explicitly present in the TSG were included.
- No timeouts, approvals, assertions, or destructive commands were inferred or added.
- Governance policy allows only the `kubectl` command, derived from CLI steps in the TSG.