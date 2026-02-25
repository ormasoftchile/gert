# Quickstart: gert — Governed Executable Runbook Engine v0

**Branch**: `001-runbook-engine-v0` | **Date**: 2026-02-11

---

## Prerequisites

- Go (latest stable) installed
- `cli-replay` binary on PATH (for replay mode only)

## Build

```bash
cd cmd/gert
go build -o gert .
```

## 1. Create a Runbook

Create `example-runbook.yaml`:

```yaml
apiVersion: runbook/v0
meta:
  name: healthcheck
  description: Basic service health check
  vars:
    namespace: prod
    service: api
  defaults:
    timeout: "60s"
  governance:
    allowed_commands: [curl, echo]
    evidence:
      require_for_manual: true
steps:
  - id: ping_service
    type: cli
    title: Ping the service endpoint
    with:
      argv: [curl, "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://{{ .service }}.{{ .namespace }}.svc:8080/health"]
    capture:
      status_code: stdout
    assertions:
      - equals: "200"

  - id: check_response
    type: cli
    title: Verify response body
    with:
      argv: [curl, "-s", "http://{{ .service }}.{{ .namespace }}.svc:8080/health"]
    capture:
      health: stdout
    assertions:
      - json_path:
          path: "$.status"
          equals: "healthy"

  - id: echo_version
    type: cli
    title: Report service version
    with:
      argv: [echo, "Service check complete for {{ .service }}"]
    capture:
      version_output: stdout

  - id: confirm_healthy
    type: manual
    title: Confirm service is operational
    instructions: |
      Review the health check results above.
      Verify the service dashboard shows no alerts.
    required_evidence:
      - kind: checklist
        name: health_confirmation
        items:
          - "Health endpoint returns 200"
          - "Dashboard shows no active alerts"
      - kind: text
        name: notes
```

## 2. Validate

```bash
gert validate example-runbook.yaml
```

Expected output:
```
example-runbook.yaml: valid
```

## 3. Execute (Real Mode)

```bash
gert exec example-runbook.yaml --as "jane.doe"
```

The system executes CLI steps, pauses at the manual step for evidence, then produces:
- `.runbook/runs/<run_id>/trace.jsonl` — execution trace
- `.runbook/runs/<run_id>/snapshots/` — state snapshots per step

## 4. Debug Interactively

```bash
gert debug example-runbook.yaml --as "jane.doe"
```

```
gert[step 1/4 | ping_service]> next
  ✓ ping_service: passed (0.3s)
gert[step 2/4 | check_response]> print vars
  namespace = prod
  service   = api
gert[step 2/4 | check_response]> next
  ✓ check_response: passed (0.2s)
gert[step 3/4 | echo_version]> print captures
  status_code = 200
  health      = {"status":"healthy","version":"1.2.3"}
gert[step 3/4 | echo_version]> next
  ✓ echo_version: passed (0.1s)
gert[step 4/4 | confirm_healthy]> evidence check health_confirmation "Health endpoint returns 200"
  ✓ checked
gert[step 4/4 | confirm_healthy]> evidence check health_confirmation "Dashboard shows no active alerts"
  ✓ checked
gert[step 4/4 | confirm_healthy]> evidence set notes "All clear, no issues observed"
  ✓ set
gert[step 4/4 | confirm_healthy]> next
  ✓ confirm_healthy: passed
All steps passed.
```

## 5. Dry-Run

```bash
gert exec example-runbook.yaml --mode dry-run
```

Shows what would happen without executing any commands.

## 6. Replay (Offline Testing)

Create `scenario.yaml`:
```yaml
commands:
  - argv: [curl, "-s", "-o", "/dev/null", "-w", "%{http_code}", "http://api.prod.svc:8080/health"]
    stdout: "200"
    stderr: ""
    exit_code: 0
  - argv: [curl, "-s", "http://api.prod.svc:8080/health"]
    stdout: '{"status":"healthy","version":"1.2.3"}'
    stderr: ""
    exit_code: 0
  - argv: [echo, "Service check complete for api"]
    stdout: "Service check complete for api"
    stderr: ""
    exit_code: 0
evidence:
  confirm_healthy:
    health_confirmation:
      kind: checklist
      items:
        "Health endpoint returns 200": true
        "Dashboard shows no active alerts": true
    notes:
      kind: text
      value: "Replayed — all checks verified"
```

```bash
gert exec example-runbook.yaml --mode replay --scenario scenario.yaml
```

Replay produces an identical execution trace without running real commands.

## 7. Compile a TSG

Given an existing Markdown TSG (`tsg.md`):

```bash
gert compile tsg.md --out runbook.yaml --mapping mapping.md
```

Produces:
- `runbook.yaml` — schema-valid runbook
- `mapping.md` — section-to-step mapping with explanations

## 8. Export Schema

```bash
gert schema export > runbook-v0.json
```

Outputs the canonical JSON Schema (Draft 2020-12) for integration with editors and CI validation.

## Verify Your Setup

Run all checks in sequence:
```bash
gert validate example-runbook.yaml && \
gert exec example-runbook.yaml --mode dry-run && \
echo "Setup verified"
```
