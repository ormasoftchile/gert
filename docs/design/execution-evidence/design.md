# Execution Evidence & Postmortem

## Status: collecting

## Problem
When a DRI runs a TSG via gert during an incident, the execution produces trace data
and step responses. But this evidence:
1. Sits in `.runbook/runs/<run_id>/` with no link to the ICM
2. Isn't posted back to the ICM timeline
3. Isn't automatically saved as a replayable scenario
4. Doesn't handle chained runbooks (triage → cause-specific)

For postmortem, you need: "for ICM 747870160, here's everything that happened during triage."

## Run Manifest

Every run produces a `run.yaml` manifest:

```yaml
run_id: 20260214T095753-e0943cea
icm_id: 747870160
runbook: login-success-rate-below-target.runbook.yaml
actor: cristiano
mode: real
started_at: "2026-02-14T09:57:53Z"
ended_at: "2026-02-14T10:03:12Z"
outcome:
  state: needs_rca
  step_id: check_login_failures_kusto
  recommendation: "Route to IsReplicaInBuild TSG"
inputs_resolved:
  server_name: sobeys-sql01
  database_name: AlarmDB
  environment: ProdEus1a
steps_summary:
  total: 1
  passed: 1
  failed: 0
  skipped: 0
# Chaining fields (if applicable)
parent_run_id: ""
child_runs: []
```

## Chained Runs

When an outcome has `next_runbook`, the parent run spawns a child run.
Both are linked by ICM ID and parent/child run IDs.

### Folder structure

```
.runbook/runs/
├── 20260214T095753-parent/              # triage runbook
│   ├── run.yaml
│   │   icm_id: 747870160
│   │   outcome:
│   │     state: needs_rca
│   │     next_runbook: IsReplicaInBuild.runbook.yaml
│   │   child_runs:
│   │     - run_id: 20260214T100312-child
│   │       runbook: IsReplicaInBuild.runbook.yaml
│   │       outcome: resolved
│   ├── trace.jsonl
│   └── steps/
│       └── 001-check-login-failures-kusto.json
│
└── 20260214T100312-child/               # cause-specific runbook
    ├── run.yaml
    │   icm_id: 747870160               # same ICM
    │   parent_run_id: 20260214T095753-parent
    │   inherited_captures:
    │     server_name: sobeys-sql01
    │     failure_count: "1"
    │   outcome:
    │     state: resolved
    │     recommendation: "Replica build completed"
    ├── trace.jsonl
    └── steps/
        └── 001-check-replica-build-status.json
```

### Anchor: ICM ID, not run ID

All runs for the same ICM are linked. Postmortem starts from ICM → finds all runs → replays the chain.

## ICM Evidence Posting

After a run completes, gert posts a summary back to the ICM discussion:

```
[gert] Runbook execution completed
Runbook: login-success-rate-below-target
Run ID: 20260214T095753-e0943cea
Outcome: needs_rca
Steps: 1 passed, 0 failed
Captures: failure_count=1
Recommendation: Route to IsReplicaInBuild TSG
```

For chained runs:

```
[gert] Runbook chain completed for ICM 747870160

1. login-success-rate-below-target (20260214T095753)
   → 1 step passed
   → Outcome: needs_rca → chained to IsReplicaInBuild

2. IsReplicaInBuild (20260214T100312)
   → 3 steps passed
   → Outcome: resolved
   → Recommendation: Replica build completed, login rate recovered

Total: 4 steps, 2 runbooks, final state: resolved
```

This uses the ICM MCP tools to add a discussion entry, making execution visible
in the incident timeline alongside the DRI's manual notes and Kusto queries.

## Auto-Save as Scenario

After a real-mode run, automatically save step responses to a scenario folder
so the run is replayable:

```
scenarios/2026/02/14/login-failure-747870160/
├── icm.json                    # captured at run start via ICM MCP
├── inputs.yaml                 # resolved inputs
├── scenario.yaml               # manifest
├── runs/
│   ├── parent/
│   │   ├── run.yaml
│   │   └── steps/
│   │       └── 001-check-login-failures-kusto.json
│   └── child/
│       ├── run.yaml
│       └── steps/
│           └── 001-check-replica-build-status.json
```

The evidence IS the scenario. A postmortem reviewer opens the scenario folder
and sees exactly what the DRI saw, step by step, with original timestamps.

## The Full Chain

```
ICM alert fires
  → vscode:// link (passes ICM ID)
  → gert loads runbook from TSG link
  → Binds inputs from ICM custom fields (routing-to-fields mapping)
  → Resolves environment via xts-cli env resolve
  → Executes runbook (real mode)
      → Each XTS step response auto-saved to steps/*.json
      → Each manual step evidence recorded in trace.jsonl
  → Outcome triggers next_runbook (if applicable)
      → Child run starts with inherited captures
      → Child step responses saved separately
  → Run manifest written with ICM link
  → Summary posted to ICM discussion
  → Scenario folder ready for postmortem replay
```

## Replay for Postmortem

```bash
# Exact reproduction — original timestamps
gert exec login-success-rate.runbook.yaml \
  --mode replay \
  --scenario scenarios/2026/02/14/login-failure-747870160/

# Demo/training — shifted timestamps
gert exec login-success-rate.runbook.yaml \
  --mode replay \
  --scenario scenarios/2026/02/14/login-failure-747870160/ \
  --rebase-time now
```

## Open Questions

- Should the ICM evidence post be automatic or opt-in (--post-to-icm flag)?
- How to handle failed runs — still post to ICM? Still save scenario?
- Should the scenario auto-save be a separate --record flag or always-on in real mode?
- How to handle sensitive data in saved scenarios (customer names, connection strings)?
- Should the run manifest include the full trace.jsonl inline or just reference it?
- How does the VS Code extension visualize the run chain? Timeline view?
