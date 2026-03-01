# Data Model: Gert Ecosystem v0

**Feature**: 002-ecosystem-v0
**Date**: 2026-02-28

## Entity Relationship Overview

```
Contract (revised)
  ├── effects: []string          # NEW: system-level effect categories
  ├── reads: []string            # domain resources (parallel safety)
  ├── writes: []string           # domain resources (parallel safety)
  ├── idempotent: bool
  ├── deterministic: bool
  └── side_effects: *bool        # DEPRECATED: migration only

Step (extended)
  ├── scope: string              # NEW: variable namespace (dot-separated)
  ├── export: []string           # NEW: promote outputs to global
  ├── visibility: Visibility     # NEW: allow/deny glob patterns
  ├── principal: Principal       # NEW: actor attribution
  └── for_each.key: string       # NEW: map-structured outputs

Runbook Meta (extended)
  ├── secrets: []SecretRef       # NEW: required env vars
  └── governance.approval_timeout: duration  # NEW: per-runbook timeout

ApprovalTicket (new)
  ├── ticket_id: string
  ├── status: enum(pending, approved, rejected, expired)
  └── created: timestamp

ApprovalResponse (new)
  ├── ticket_id: string
  ├── approved: bool
  ├── approver_id: string
  ├── method: string
  ├── timestamp: time
  ├── signature: string
  ├── signature_alg: string
  ├── key_id: string
  └── reason: string

TraceEvent (extended)
  ├── prev_hash: string          # NEW: SHA-256 of previous event JSON
  └── principal: Principal       # NEW: on attributable events

RunStart data (extended)
  ├── actor: string              # NEW
  ├── host: string               # NEW
  ├── gert_version: string       # NEW
  ├── runbook_hash: string       # NEW
  ├── tool_hashes: map           # NEW
  └── input_sources: map         # NEW

RunState (new — for resume)
  ├── run_id: string
  ├── runbook_path: string
  ├── step_index: int
  ├── vars: map[string]any
  ├── trace_path: string
  ├── pending_ticket: *ApprovalTicket
  └── created: timestamp
```

## Entity Details

### Contract (revised)

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| effects | []string | no (but recommended) | System-level effect categories: network, filesystem, kubernetes, azure, etc. |
| reads | []string | no | Domain resource tags for parallel safety (set intersection) |
| writes | []string | no | Domain resource tags for parallel safety (set intersection) |
| idempotent | bool | no | Safe to re-run? Default: false |
| deterministic | bool | no | Same inputs → same outputs? Default: false |
| side_effects | *bool | no | DEPRECATED. Accepted for migration only. Error if both `effects` and `side_effects` declared. |

### SecretRef

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| env | string | yes | Environment variable name |
| description | string | no | Human-readable description |
| required | bool | no | Default: true. If false, missing secret is a warning not an error. |

### Visibility

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| allow | []string | no | Glob patterns of variable paths this step can access |
| deny | []string | no | Glob patterns explicitly hidden (applied after allow) |

Glob semantics: `*` matches one dot-segment; `**` matches any depth. Deny overrides allow. If allow is present, default for unlisted paths is deny.

### Principal

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| kind | string | yes | `system`, `human`, `agent` |
| id | string | yes | Stable identifier (email, agent ID, "kernel") |
| role | string | no | Functional role (e.g., "skeptic", "judge", "oncall") |
| model | string | no | For agents: model identifier (e.g., "claude-4-opus") |

### Scope Path Format

Canonical format: dot-separated segments. YAML `/` is sugar; kernel normalizes to `.` on load.

| YAML (author writes) | Kernel stores | Template access |
|----------------------|---------------|-----------------|
| `scope: "round/0"` | `scope.round.0` | `{{ .scope.round.0.var }}` |

### Export Resolution

- `export` takes output field names from `contract.outputs`
- Exported vars promoted to global namespace
- Name collision with existing global → runtime error
- With `for_each.key`: exported outputs are map-structured: `{{ .step_id.key.field }}`

### Repeat Block

| Field | Type | Required | Description |
|-------|------|----------|-------------|
| repeat.max | int | yes | Maximum iterations (guarantees termination) |
| repeat.until | string | no | Expression evaluated after each iteration; true = stop |
| steps | []Step | yes | Steps to execute per iteration |

Implicit variables: `{{ .repeat.index }}` (zero-based), `{{ .repeat.round }}` (alias).

## State Transitions

### ApprovalTicket Lifecycle

```
                 Submit()
                    │
                    ▼
    ┌──────────────────────────┐
    │       PENDING            │
    └──────────────────────────┘
         │          │         │
    approved   rejected   timeout
         │          │         │
         ▼          ▼         ▼
    ┌────────┐ ┌────────┐ ┌─────────┐
    │APPROVED│ │REJECTED│ │ EXPIRED │
    └────────┘ └────────┘ └─────────┘
```

- Tickets are single-use. Once resolved, cannot be reused.
- Expired tickets are treated as rejections.
- Timeout is configurable per-runbook via `governance.approval_timeout` (default: 30 minutes).

### Run State Lifecycle (for resume)

```
    Run()
      │
      ▼
   RUNNING ──→ approval_pending ──→ PERSISTED (state.json)
      │                                    │
      │                              gert resume
      │                                    │
      ▼                                    ▼
  COMPLETED                           RUNNING (continued)
```
