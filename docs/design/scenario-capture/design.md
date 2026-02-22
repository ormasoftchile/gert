# Scenario Capture

## Status: collecting

## Problem
To demo a full TSG end-to-end or to test runbooks without hitting production, we need a way to capture a complete scenario: ICM incident data, XTS query responses, manual evidence, and visual captures. This extends the existing cli-replay mechanism to cover the full gert execution surface.

## Proposed UX
- **Record mode**: Run a runbook in real mode while recording all inputs/outputs to a scenario folder
- **Replay mode**: Load a scenario folder and replay the entire runbook with zero network calls
- **Demo mode**: Step through slowly with realistic data, showing each panel's output
- Start from an ICM ID → capture ICM context → capture each step's response → bundle as scenario

## Proposed Structure
```
scenarios/
└── 2026/02/13/
    └── alias-db-failure-747241872/
        ├── icm.json                  # ICM incident snapshot (details + context + location)
        ├── inputs.yaml               # Resolved input values (from ICM binding + prompts)
        ├── steps/
        │   ├── 001-assess-failures.json     # XTS query JSON response
        │   ├── 002-assess-logins.json       # XTS query JSON response
        │   ├── 003-review-dashboard.json    # Manual step evidence
        │   ├── 004-check-replicas.json      # Manual step evidence
        │   └── 005-investigate.json         # Manual step evidence
        ├── attachments/
        │   ├── jarvis-heatmap.png           # Screenshot evidence
        │   └── xts-replicas-table.png       # XTS view capture
        └── scenario.yaml                    # Manifest: maps step IDs to response files
```

The `yyyy/mm/dd` date prefix is the incident date (from `icm.impactStartTime`), making it
easy to browse scenarios chronologically and correlate with on-call shifts.

## Technical Notes
- `scenario.yaml` ties step IDs to their response files — the replay engine loads this
- ICM.json captured via ICM MCP tools (get_incident_details + get_incident_context + get_incident_location)
- XTS responses are the raw `--format json` output from xts-cli
- Manual step evidence is the collected evidence values (text, checklist items, attachment refs)
- cli-replay already handles command → response matching; extend to XTS and manual steps

## Examples
<!-- Capture a real scenario from ICM 747241872 or similar -->

## Open Questions
- Should recording be automatic (intercept during real execution) or manual (user saves responses)?
   The least resistance path. Intercept when possible, fill missing when not.
- How to handle time-sensitive data (Kusto queries that return different results later)?
   Behave like the data was current. Intercept time fields from the captured data and update to make them relative to the current specified time range (probably we'll have to store the originally recorded time to calculate the time delta).
- Should scenarios be version-controlled alongside TSGs?
  Yes
- How to sanitize scenarios for sharing (redact customer data, connection strings)?
  We need to define or use the governance components/policies to redact data / passwords.
