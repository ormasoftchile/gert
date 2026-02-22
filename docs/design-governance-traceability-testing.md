!!!AI Generated.  To be verified!!!

# Design: Governance, Traceability & Testing

## Overview

The gert runbook engine executes TSGs that query production systems and recommend (or execute) mitigation commands. As adoption grows, three capabilities become critical:

1. **Governance** — ensuring runbooks only do what they're authorized to do
2. **Traceability** — a durable, structured audit trail of every execution
3. **Testing** — verifying runbook logic before it reaches production incidents

This document defines the design for each capability and a phased implementation plan.

---

## 1. Governance

### Problem

Today, `governance` in the runbook schema is declarative only — `allowed_commands`, `denied_commands`, `redact`, and `evidence` fields exist but nothing enforces them. A runbook author could declare any command and the engine would execute it.

### Architecture

Three enforcement layers:

```
┌─────────────────────────────────────────────────────┐
│  Layer 1: Static Policy (author time)               │
│  gert validate --policy governance.yaml             │
│  Runs in CI — blocks PRs with unsafe steps          │
├─────────────────────────────────────────────────────┤
│  Layer 2: Runtime Policy (execution time)           │
│  Engine checks policy before dispatching each step  │
│  Denied commands → hard error (not skippable)       │
│  Approval-required commands → awaiting_approval     │
├─────────────────────────────────────────────────────┤
│  Layer 3: Post-Execution Audit (after completion)   │
│  Compare what ran vs what was declared              │
│  Flag evidence gaps, unsigned manual steps          │
└─────────────────────────────────────────────────────┘
```

### Policy Hierarchy

Policy is defined at three levels. Each level can only narrow (never widen) the permissions granted by its parent:

```
repo governance.yaml  →  runbook governance  →  runtime enforcement
(floor policy)           (can only narrow)       (engine enforces)
```

**Repo-level policy** (`governance.yaml` at repository root):
```yaml
apiVersion: governance/v0
deny_commands:
  - "Drop-Database"
  - "Remove-*"
require_approval_for:
  - pattern: "Kill-Process"
    min_approvers: 1
    roles: ["oncall-lead"]
  - pattern: "Set-*"
    min_approvers: 2
max_blast_radius: single_node
require_evidence: true
redact:
  - pattern: "password=([^&]+)"
    replace: "password=***"
```

**Runbook-level policy** (existing `meta.governance`):
```yaml
governance:
  allowed_commands:
    - Get-FabricNode
    - Kill-Process
  denied_commands: []
```

**Enforcement rule:** The engine computes the effective policy by intersecting repo policy with runbook policy. If the repo denies `Remove-*` and the runbook allows `Remove-Node`, the deny wins.

### Static Validation (Layer 1)

`gert validate --policy governance.yaml <runbook>` performs:

| Check | Description |
|---|---|
| Command allow/deny | Every `xts.activity` and CLI `argv[0]` matched against allow/deny patterns |
| Approval gates | Steps invoking approval-required commands must have `approvals` field |
| Evidence requirements | Manual steps with production impact must have `required_evidence` |
| Blast radius | Commands scoped to cluster/region rejected if policy says `single_node` |
| Redaction patterns | Runbook redaction must be superset of repo redaction |

This runs in CI as a PR gate.

### Runtime Enforcement (Layer 2)

Before dispatching any step, the engine:

1. Resolves template variables in the command/query
2. Matches the resolved command against the effective policy
3. If denied → emits `step_blocked` event with reason, step fails
4. If approval required → emits `awaiting_approval` status, pauses execution
5. If allowed → dispatches normally

### Approval Flow

When a step requires approval:

```
Engine: step requires approval (Kill-Process, min=1, roles=[oncall-lead])
   ↓
Extension: shows approval request UI with justification + command preview
   ↓
Approver: clicks "Approve" (or denies)
   ↓
Engine: records approval (who, when, justification) → dispatches step
```

For V1, the approval is in the same VS Code session (self-approval with explicit acknowledgment). Future: remote approval via Teams notification or API.

---

## 2. JIT (Just-In-Time Elevation)

### Problem

Some XTS commands require elevated access (e.g., PlatformServiceAdministrator). Engineers currently activate JIT manually before running the TSG, leading to over-provisioned sessions.

### Schema Extension

```yaml
- step:
    id: kill_gw
    type: xts
    title: Kill gateway process
    elevation:
      role: PlatformServiceAdministrator
      scope: "{{ .top_cluster }}/{{ .top_node }}"
      justification: "Kill gateway to resolve 40613/22 — ICM {{ .icm_id }}"
      ttl: 15m
    xts:
      mode: activity
      activity: Kill-Process
      params:
        ProcessName: xdbgatewaymain.exe
```

### Execution Flow

```
1. Engine reaches step with 'elevation' field
2. Resolves scope + justification templates
3. Requests elevation: POST to JIT API with (role, scope, justification, ttl)
4. Waits for grant (status: awaiting_jit)
5. On grant → dispatches step with elevated token
6. On step completion → releases elevation
7. Records grant/release in trace (token_id, scope, duration)
```

### Scope Minimization

The `scope` field uses template variables so elevation is always scoped to the specific resource identified during triage:
- Single node: `{{ .top_cluster }}/{{ .top_node }}`
- Single TR: `{{ .top_cluster }}`
- Never cluster-wide unless policy explicitly permits

### Security Properties

- Elevation is step-scoped, not session-scoped
- TTL is declared per-step (default: 15m, max from policy)
- Justification includes ICM ID for audit correlation
- If the step fails or times out, elevation is released immediately

---

## 3. Traceability

### Problem

Today, execution history is ephemeral — it exists only in the VS Code webview and the Copy Summary text. There is no structured, durable, queryable audit trail.

### Design: OpenTelemetry (OTEL)

Each runbook execution maps to the OTEL tracing model:

| OTEL Concept | Gert Concept |
|---|---|
| **Trace** | Full runbook execution (including chain) |
| **Span** | Individual step execution |
| **Span Link** | TSG chain transition (parent → child) |
| **Attributes** | Captures, input values, outcome state |
| **Events** | Approval granted, evidence collected, elevation activated |

### Trace Structure

```
Trace: run-abc123
│ attr: runbook=login-success-rate-below-target
│ attr: icm_id=749270451
│ attr: engineer=alias
│ attr: mode=real
│
├─ Span: query_login_failures [xts/kusto] 2.3s PASSED
│   attr: failure_row_count=1
│   attr: top_error=40613
│   attr: top_state=22
│   attr: query_hash=sha256:abc...
│
├─ Span: verify_scope [manual] 0.1s PASSED
│
├─ Span: route_to_mitigation [manual] 0.0s PASSED
│   event: outcome_reached {state=resolved, next_runbook=is-gw-proxy-throttled...}
│   link → child_trace: run-def456
│
└─ Child Trace: run-def456
    │ attr: runbook=is-gw-proxy-throttled-tcp-timeout-to-backend
    │ attr: parent_run=run-abc123
    │
    ├─ Span: query_gw_throttle [xts/kusto] 1.8s PASSED
    │   attr: throttle_rows=165
    │
    ├─ Span: query_scope [xts/kusto] 1.2s PASSED
    │   attr: distinct_clusters=2
    │
    └─ Span: escalate_networking [manual] 0s ESCALATED
        event: outcome_reached {state=escalated}
```

### Implementation

The Go engine emits OTEL spans using the `go.opentelemetry.io/otel` SDK:

```go
// In engine.go, wrapping step execution
ctx, span := tracer.Start(ctx, step.ID,
    trace.WithAttributes(
        attribute.String("step.type", step.Type),
        attribute.String("step.title", step.Title),
        attribute.String("runbook", rb.Meta.Name),
    ),
)
defer span.End()

// After capture
for k, v := range captures {
    span.SetAttributes(attribute.String("capture."+k, v))
}

// On outcome
span.AddEvent("outcome_reached", trace.WithAttributes(
    attribute.String("state", outcome.State),
))
```

### Exporter Configuration

Configured via environment variable or gert config:

| Exporter | Use Case |
|---|---|
| `stdout` | Local development, debugging |
| `otlp` (gRPC/HTTP) | Production → Jaeger, Azure Monitor, Datadog |
| `file` (JSON) | Offline analysis, CI test runs |

```yaml
# gert config or environment
OTEL_EXPORTER_OTLP_ENDPOINT=https://monitor.azure.com/...
OTEL_SERVICE_NAME=gert
```

### Why OTEL (Not Custom Logging)

- Standard protocol — any observability backend works without vendor lock-in
- Trace context propagation across TSG chains for free
- Existing visualization tools (Jaeger, Azure Monitor Application Map)
- Structured attributes, not text to grep
- Sampling and export batching built in

---

## 4. Testing

### 4.1 Unit Tests: Branch Logic

**What:** Test individual conditions and branch routing without executing queries.

**How:** A `gert test` subcommand loads a runbook, injects canned variable values, and asserts which branches are taken and which outcomes fire.

**Test file format** (`*.test.yaml` alongside the runbook):

```yaml
apiVersion: test/v0
runbook: is-gw-proxy-throttled-tcp-timeout-to-backend.runbook.yaml
tests:
  - name: single_gw_node
    vars:
      distinct_gw_nodes: "1"
      distinct_clusters: "1"
      top_node: "node1"
      top_cluster: "cluster1"
    expect:
      branch_taken: "Single GW node affected"
      steps_reached: [mitigate_single_gw]

  - name: multi_cluster
    vars:
      distinct_gw_nodes: "5"
      distinct_clusters: "3"
    expect:
      branch_taken: "Multiple clusters affected — networking issue"
      steps_reached: [escalate_networking]

  - name: no_failures
    vars:
      throttle_rows: "0"
    expect:
      outcome: no_action
```

**Command:** `gert test <runbook-or-test-file>`

**What it validates:**
- Condition expressions evaluate correctly for given variable combinations
- The expected branch is taken (and others are skipped)
- The expected outcome fires (or doesn't)
- Template resolution in titles/instructions doesn't error

**What it does NOT do:** Execute queries, call XTS, hit the network.

### 4.2 Functional Tests: Scenario Replay

**What:** Replay a saved scenario end-to-end and verify the execution matches expectations.

**We already have:** Replay mode with scenario directories (`scenarios/{runbook}/icm-{id}/`). What's missing is an **assertions file**.

**Expected file** (`scenarios/{runbook}/icm-{id}/expected.yaml`):

```yaml
apiVersion: expected/v0
outcome: escalated
branch_path:
  - query_login_failures
  - verify_scope
  - route_to_mitigation
  - query_gw_throttle
  - query_scope
  - escalate_networking
captures:
  top_error: "40613"
  distinct_clusters: "2"
chain:
  - runbook: login-success-rate-below-target
    outcome: resolved   # routing outcome
  - runbook: is-gw-proxy-throttled-tcp-timeout-to-backend
    outcome: escalated
```

**Command:** `gert test --scenario scenarios/login-success-rate-below-target/icm-749270451/`

**Execution:**
1. Start replay with the scenario's cached responses
2. Auto-advance all steps (no manual interaction)
3. Record: branch path, captures, final outcome, chain transitions
4. Compare against `expected.yaml`
5. Exit 0 on match, exit 1 on divergence with diff output

**Time drifting for live tests:**

For testing against real Kusto (not cached), queries that use `ago(4h)` won't return the original incident data. Options:

| Approach | How | Trade-off |
|---|---|---|
| **Query rewrite** | Engine replaces `ago(Xh)` with explicit `between(start..end)` using incident timestamp | Requires parsing Kusto; fragile |
| **Time variable** | Runbook uses `{{ .query_end_time }}` / `{{ .query_start_time }}` instead of `ago()` | Clean; requires runbook convention |
| **Cache-only** | Functional tests always use cached responses | Simple; no time drift needed |

**Recommendation:** Cache-only for functional tests (P0). Time-variable convention for live regression tests (P2).

### 4.3 Coverage

**What:** Measure which branches and outcomes in a runbook are exercised by existing scenarios.

**Command:** `gert coverage <runbook>`

**Algorithm:**
1. Load runbook, walk tree, enumerate all branches and outcomes → `total_set`
2. Discover all scenarios for this runbook (convention-based)
3. For each scenario, dry-run replay, record which branches were entered → `covered_set`
4. Report: `covered_set / total_set` with details

**Output:**

```
is-gw-proxy-throttled-tcp-timeout-to-backend.runbook.yaml
  Branches: 5 total, 3 covered (60%)
  ✓ Single GW node affected                      icm-749306075
  ✓ Multiple clusters affected                    icm-749270451
  ✓ outcome: no_action (throttle_rows == "0")     icm-749408176
  ✗ Single cluster, multi-GW → single_db_node    NO SCENARIO
  ✗ Single cluster, multi-GW → single_tr          NO SCENARIO

  Outcomes: 4 total, 2 covered (50%)
  ✓ no_action     icm-749408176
  ✓ escalated     icm-749270451
  ✗ resolved      NO SCENARIO
  ✗ escalated (single_db_node path)   NO SCENARIO
```

**CI integration:**

```yaml
# PR validation pipeline
steps:
  - run: gert validate --policy governance.yaml TSG/**/*.runbook.yaml
  - run: gert test TSG/**/*.test.yaml
  - run: |
      gert coverage TSG/**/*.runbook.yaml --min-branch 70 --min-outcome 50
```

PRs that add new branches without scenarios would fail the coverage threshold.

**Coverage format:** JSON output for programmatic consumption:

```json
{
  "runbook": "is-gw-proxy-throttled-tcp-timeout-to-backend",
  "branches": { "total": 5, "covered": 3, "percentage": 60 },
  "outcomes": { "total": 4, "covered": 2, "percentage": 50 },
  "uncovered": [
    { "type": "branch", "label": "Single cluster, multi-GW → single_db_node", "path": "query_scope.branches[2].steps[0].branches[0]" },
    { "type": "branch", "label": "Single cluster, multi-GW → single_tr", "path": "query_scope.branches[2].steps[0].branches[1]" }
  ]
}
```

---

## Implementation Plan

### Phase 1 — Testing Foundation (P0)

| Item | Effort | Deliverable |
|---|---|---|
| `gert test` with `*.test.yaml` unit tests | 1 week | Branch logic validation without network |
| `expected.yaml` format for scenario replay | 2 days | Assertions on replay outcome |
| `gert test --scenario` replay with assertions | 3 days | End-to-end scenario validation |
| `gert coverage` branch/outcome reporting | 3 days | Coverage report + JSON output |

### Phase 2 — Governance (P1)

| Item | Effort | Deliverable |
|---|---|---|
| `governance.yaml` repo-level schema | 2 days | Policy definition format |
| `gert validate --policy` static checks | 3 days | CI-time policy enforcement |
| Runtime policy enforcement in engine | 1 week | Pre-dispatch deny/allow checks |
| Approval flow (local, same-session) | 3 days | `awaiting_approval` step status |

### Phase 3 — Traceability (P2)

| Item | Effort | Deliverable |
|---|---|---|
| OTEL instrumentation in Go engine | 1 week | Spans for steps, attributes for captures |
| Exporter config (stdout, OTLP, file) | 2 days | Configurable trace export |
| Chain trace linking (span links) | 2 days | Cross-TSG trace correlation |
| Extension trace timeline view | 1 week | Visual trace in VS Code |

### Phase 4 — JIT Elevation (P2)

| Item | Effort | Deliverable |
|---|---|---|
| `elevation` schema field | 1 day | Declarative JIT in runbook YAML |
| JIT request/release in engine | 1 week | Scoped elevation lifecycle |
| Extension JIT status UI | 3 days | `awaiting_jit` step display |
| Elevation audit in OTEL trace | 1 day | Grant/release events in spans |

### Phase 5 — Advanced Testing (P3)

| Item | Effort | Deliverable |
|---|---|---|
| Time-variable convention for live replay | 3 days | `query_start_time`/`query_end_time` |
| Coverage thresholds in CI | 1 day | `--min-branch`, `--min-outcome` flags |
| Auto-scenario generation from coverage gaps | 1 week | Suggest missing scenario inputs |

---

## Open Questions

1. **Remote approval:** Should V1 support remote approval (e.g., via Teams), or is same-session acknowledgment sufficient?
2. **OTEL backend:** Which backend for production traces — Azure Monitor (native integration) or Jaeger (open source, self-hosted)?
3. **Coverage enforcement:** Should coverage thresholds be per-runbook or per-repository? Should new runbooks be exempt initially?
4. **JIT provider:** Which JIT API to integrate with — Azure PIM, a custom elevation service, or XTS's built-in elevation?
5. **Test execution in CI:** Can replay scenarios run in CI without XTS access (cache-only), or do we need a test XTS environment?
