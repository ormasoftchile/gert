# Gert Ecosystem v0 — Design Document

> **Extension points, packaging, and host surfaces for the gert kernel.**

**Date**: 2026-02-28
**Status**: Draft
**Depends on**: `kernel-v0.md`

**Stability notice:** Kernel interfaces (`ToolExecutor`, contract model, trace format) are **unstable** until hardened by real adoption. Expect breaking changes in: approval semantics, contract taxonomy, input resolution, extension runner. The kernel is functional but not frozen.

---

## 1. Overview

The kernel executes runbooks. The ecosystem defines:
1. **Extension points** — interfaces the kernel exposes for external behavior (approval, input resolution, tool transports)
2. **Contract taxonomy** — how effects/resources are classified for governance
3. **Packaging** — distribution, versioning, tool packs
4. **Host surfaces** — TUI, MCP, VS Code, webhooks

This document is about **extension points and packaging first, surfaces second.**

### Build Order — Two Tracks

```
Track 1: Primitives (must do first — kernel hardening)
  1a. Real runbooks + tool packs        ← validate the kernel
  1b. Contract taxonomy                 ← effects vs. writes, governance clarity
  1c. Resumable approval                ← kernel interface, not blocking
  1d. Input resolution semantics        ← kernel-owned semantics, ecosystem implementations
  1e. Secrets convention                ← declaration, validation, redaction
  1f. Extension step runner             ← kernel dispatches to external executors
  1g. Trace integrity                   ← hash chaining, verification policy
  1h. Contract violation detection      ← runtime and probe mode

Track 2: Surfaces (after Track 1 is solid)
  2a. TUI (gert-tui)                   ← separate binary
  2b. MCP Server (gert-mcp)            ← separate binary
  2c. VS Code Extension                ← uses MCP or JSON-RPC backend
  2d. Auto-record + scenario diffing   ← replay excellence
  2e. Outcome intelligence             ← aggregation CLI, webhooks
  2f. Watch mode                       ← lightweight scheduling loop
```

Track 1 changes kernel packages. Track 2 is ecosystem-only (imports kernel, never modifies it).

### Distribution Model

Three separate binaries from one repo. Core CLI stays lean and scriptable.

```
cmd/
  gert/            Core CLI (validate, exec, test, schema)
                   Lean, no TUI deps. Designed for scripts, CI, pipes.
  gert-tui/        Terminal UI (imports kernel + Bubble Tea)
                   For operators during incidents. Optional install.
  gert-mcp/        MCP server for AI agents
                   Exposes kernel operations as MCP tools. Optional install.
```

**Versioning and distribution:**

```bash
# Tagged releases — never @latest in production
go install github.com/ormasoftchile/gert/cmd/gert@v0.1.0
go install github.com/ormasoftchile/gert/cmd/gert-tui@v0.1.0
go install github.com/ormasoftchile/gert/cmd/gert-mcp@v0.1.0
```

- All binaries share a version tag from the monorepo
- GitHub Releases with checksums for each binary (linux/darwin/windows, amd64/arm64)
- `gert version` prints the tag + commit hash for reproducibility
- For approved toolchains: organizations pin to a release tag in their CI config or internal package manager

**Why separate binaries:**

- Core CLI stays scriptable — pipes, `--json`, `jq`, CI-friendly, no terminal dependencies
- TUI pulls Bubble Tea + lipgloss + glamour — unnecessary for automation
- MCP server has its own SDK dependency — unnecessary for humans
- Follows industry pattern: `kubectl` (core) + `k9s` (TUI), `git` (core) + `lazygit` (TUI)

### Package Layout

```
pkg/kernel/                  ← library (Phase 1-5, stable)
  contract/
  schema/
  validate/
  engine/
  eval/
  executor/
  governance/
  trace/
  replay/
  testing/

pkg/ecosystem/               ← ecosystem packages (never imported by kernel)
  tui/                       ← Bubble Tea wrapper (Phase C)
  mcp/                       ← MCP server handlers (Phase D)
  serve/                     ← JSON-RPC server for VS Code (Phase E)
  approval/
    stdin/                   ← default terminal approval (current)
    teams/                   ← Teams adaptive cards
    slack/                   ← Slack
    http/                    ← HTTP callback
  providers/
    prompt/                  ← stdin prompt (default)
    pagerduty/               ← PagerDuty
    servicenow/              ← ServiceNow

cmd/
  gert/                      ← core CLI: validate, exec, test, schema
  gert-tui/                  ← TUI binary
  gert-mcp/                  ← MCP server binary

vscode/                      ← VS Code extension (TypeScript)
```

**Dependency rule:** `pkg/kernel/` never imports `pkg/ecosystem/`. The arrow is always `ecosystem → kernel`.

### Component Map

| Component | Package / Location | Imports from kernel | New interfaces |
|-----------|--------------------|---------------------|----------------|
| Core CLI | `cmd/gert/` | engine, schema, validate, trace, replay, testing | — |
| Tools | `tools/*.tool.yaml` | — | — |
| Runbooks | `runbooks/*.yaml` | — | — |
| Approval Provider | `pkg/kernel/engine/` (interface) + `pkg/ecosystem/approval/` | engine, trace | `ApprovalProvider` |
| TUI | `cmd/gert-tui/` + `pkg/ecosystem/tui/` | engine, schema, trace | — |
| MCP Server | `cmd/gert-mcp/` + `pkg/ecosystem/mcp/` | engine, schema, validate | — |
| VS Code Extension | `vscode/` (rewrite) | via MCP or JSON-RPC | — |
| Input Providers | `pkg/ecosystem/providers/` | schema | `InputProvider` |

---

## 2. Phase A — Real Runbooks & Tools

### Goal

Build 3–5 operational runbooks that exercise the full kernel surface. Find bugs before building UI layers.

### Tools to build

| Tool | Binary | Platform | Contract highlights |
|------|--------|----------|---------------------|
| `curl.tool.yaml` | curl | all | `effects: [network]`, `writes: []`, `deterministic: false` |
| `kubectl.tool.yaml` | kubectl | all | Multiple actions: `get` (`effects: [kubernetes]`, no writes), `delete` (`effects: [kubernetes]`, `writes: [pods]`), `apply` (idempotent) |
| `az.tool.yaml` | az | all | Azure CLI actions: `vm list` (`effects: [azure]`), `vm restart` (`effects: [azure]`, `writes: [vm]`) |
| `ping.tool.yaml` | ping | all | `effects: [network]`, `deterministic: false` |
| `jq.tool.yaml` | jq | all | `effects: []`, `deterministic: true` — pure JSON transformation |

### Runbooks to build

| Runbook | Exercises |
|---------|-----------|
| **Service health diagnostic** | Tool calls, branching on status, retry loop (`next` with `max`), structured outcomes |
| **Multi-endpoint health sweep** | `for_each parallel: true` across a list of endpoints, output accumulation |
| **Kubernetes pod restart** | Governance-heavy: `require-approval` for pod deletion, manual evidence collection |
| **DNS + HTTP chain diagnostic** | Sequential tool steps with variable threading, assert steps, multiple outcome paths |
| **Incident triage** | Multi-branch with `default`, manual investigation step, escalation outcome, extension metadata (`x-incident-id`) |

### Success criteria

- All runbooks: `gert validate` passes
- All runbooks: `gert exec --mode dry-run` shows correct contract/governance
- Each runbook has 2+ scenarios with `gert test` passing
- At least one runbook exercises parallel execution
- At least one runbook triggers `require-approval` in governance

---

## 3. Track 1b — Contract Taxonomy

### Problem

The current contract model conflates "effects" with "resources". A tool declaring `side_effects: false, reads: [network]` is incoherent — a network call *is* an effect in many governance models. Authors will produce inconsistent contracts because the taxonomy is unclear.

### Design: Split effects from resources

```yaml
contract:
  effects: [network]                    # what external systems does this touch?
  writes: [service]                     # what domain resources does this mutate? (for parallel safety)
  reads: [service_status]               # what domain resources does this observe? (for parallel safety)
  idempotent: true
  deterministic: false
```

**`effects`** — Observable interactions with external systems. Used by governance for risk classification.

| Effect | Meaning |
|--------|---------|
| `network` | Makes network calls (HTTP, DNS, TCP) |
| `filesystem` | Reads/writes local files |
| `process` | Spawns or kills processes |
| `kubernetes` | Interacts with K8s API |
| `azure` | Interacts with Azure Resource Manager |
| `aws` | Interacts with AWS APIs |
| `database` | Queries or mutates a database |

**`reads` / `writes`** — Domain-specific resource tags for parallel safety (unchanged from kernel-v0). Opaque strings. The kernel does set intersection math, nothing more.

**`side_effects`** — Derived, not declared. A tool has side effects if `effects` is non-empty AND `writes` is non-empty. Or: governance policy can define what combination of effects + writes constitutes "side effects" for their organization.

### Governance uses `effects` first

```yaml
governance:
  rules:
    - effects: [kubernetes]
      writes: [production]
      action: require-approval
      min_approvers: 2
    - effects: [network]
      action: allow                     # network reads are fine
    - default: allow
```

### Risk classification (revised)

| Condition | Risk |
|-----------|------|
| No effects, no writes | Low |
| Effects but no writes (read-only) | Low |
| Effects + writes + idempotent | Medium |
| Effects + writes + not idempotent + deterministic | High |
| Effects + writes + not idempotent + not deterministic | Critical |

**Important:** Derived risk is **informational only** — a heuristic for runbooks without explicit governance rules. **Derived risk MUST NOT be used as the sole basis for production governance decisions.** Organizations must write explicit `governance.rules` matching their specific `effects` and `writes` for any environment where governance matters.

### Migration

`side_effects` is **deprecated** and will be **removed in the next major schema version**:
- If `effects` is present, `side_effects` is **ignored** (with a validation warning)
- If only `side_effects` is declared: kernel auto-migrates (`true` → `effects: [unknown]`, `false` → `effects: []`) and emits a deprecation warning
- New tool definitions using `side_effects` are a validation **error** if `effects` is also declared
- Do not describe `side_effects` as part of the current design. It exists only for backward compatibility with pre-kernel/v0 tool packs.

### Schema change

- Add `Effects []string` to `contract.Contract`
- `SideEffects *bool` accepted in YAML only for migration; deprecated, will be removed
- Governance rules use `Effects` matching. `SideEffects` is not used by governance in the new model.
- Validation: error if both `side_effects` and `effects` declared; warning on any `side_effects` use

---

## 4. Track 1c — Resumable Approval

### Problem

The current `ApprovalProvider` is synchronous: `RequestApproval() → blocks → returns response`. This works for stdin but deadlocks or creates ugly poll loops for async systems (Teams, Slack, PagerDuty). Worse — it turns every approval provider into a mini workflow engine.

### Design: Ticket-based, resumable

The kernel doesn't block on approval. It:
1. Requests approval → gets a **ticket** back immediately
2. Persists the run state as "pending approval"
3. Exits (or waits, depending on mode)
4. Resumes when the approval arrives

#### Kernel interface

```go
// ApprovalProvider submits approval requests and optionally waits for responses.
type ApprovalProvider interface {
    // Submit sends an approval request and returns a ticket immediately.
    Submit(ctx context.Context, req ApprovalRequest) (*ApprovalTicket, error)

    // Wait blocks until the ticket is resolved or context is cancelled.
    // For synchronous providers (stdin), Submit+Wait happen atomically.
    // For async providers (Teams), Wait polls or listens for callbacks.
    Wait(ctx context.Context, ticket *ApprovalTicket) (*ApprovalResponse, error)
}

type ApprovalRequest struct {
    RunID        string         `json:"run_id"`
    StepID       string         `json:"step_id"`
    RunbookName  string         `json:"runbook_name"`
    StepType     string         `json:"step_type"`
    Description  string         `json:"description"`
    RiskLevel    string         `json:"risk_level"`
    Contract     map[string]any `json:"contract"`
    Inputs       map[string]any `json:"inputs"`
    MinApprovers int            `json:"min_approvers"`
    RequestedBy  string         `json:"requested_by,omitempty"`
    Extensions   map[string]any `json:"extensions,omitempty"`
    Timeout      time.Duration  `json:"timeout,omitempty"`
}

type ApprovalTicket struct {
    TicketID string    `json:"ticket_id"`
    Status   string    `json:"status"`    // pending, approved, rejected, expired
    Created  time.Time `json:"created"`
}

type ApprovalResponse struct {
    TicketID   string    `json:"ticket_id"`
    Approved   bool      `json:"approved"`
    ApproverID string    `json:"approver_id"`
    Method     string    `json:"method"`
    Timestamp  time.Time `json:"timestamp"`
    Signature  string    `json:"signature,omitempty"`
    Reason     string    `json:"reason,omitempty"`
}
```

#### Engine flow

```
Engine                        ApprovalProvider            External System
  │                                │                           │
  │── Submit(req) ───────────────→ │── send notification ─────→ │
  │←── ApprovalTicket{pending} ───│                           │
  │                                │                           │
  │── trace: approval_submitted { ticket_id, step_id }       │
  │── persist state snapshot                                 │
  │                                │                           │
  │  Mode A: engine waits (with ctx timeout)                │
  │  Mode B: engine exits, gert resume --ticket <id> later  │
  │                                │                           │
  │                                │                           │  user approves
  │                                │←── callback ──────────────│
  │  (resumed via Wait or gert resume)                      │
  │←── ApprovalResponse ──────────│                           │
  │                                │                           │
  │── verify signature (if require_verified_approval=true)   │
  │── trace: approval_resolved { approved, approver, verified }
  │── execute step
```

#### Modes

| Mode | Behavior |
|------|----------|
| **Synchronous** (stdin, TUI) | `Submit` + `Wait` happen atomically. Provider blocks internally. |
| **Async with wait** (Teams + CLI) | `Submit` returns ticket. `Wait` polls/listens until response or timeout. Engine stays running. |
| **Async with resume** (Teams + CI) | `Submit` returns ticket. Engine persists state and exits. Later: `gert resume --run <id> --ticket <id>` resumes from the pending step. |

#### State persistence for resume

When the engine encounters a pending approval in async mode:
- Snapshot current state (vars, step index, trace position) to `runs/<run-id>/state.json`
- Exit with code 0 and message: "Approval pending. Resume with: gert resume --run <run-id>"
- `gert resume` loads the state snapshot, checks ticket status, continues execution

This is the key insight: **the kernel doesn't need a workflow engine**. It needs persistence + resume. The approval provider is stateless from the kernel's perspective.

#### Trace events

- `approval_submitted`: `{ ticket_id, step_id, risk_level }`
- `approval_resolved`: `{ ticket_id, approved, approver_id, method, signature }`

#### Cryptographic signing + verification

- Provider generates HMAC-SHA256 over `ticket_id + approved + timestamp + request_hash` using a shared secret
- `request_hash` = SHA-256 of `runbook_hash + step_id + contract_json + inputs_json` — this binds the approval to the specific action, preventing replay if the request changes
- The kernel supports a **verification policy**: if `governance.require_signed_approvals: true`, the engine verifies the signature before accepting the approval
- Verification keys are provided via environment variable (`GERT_APPROVAL_VERIFY_KEY`)
- If verification fails → approval treated as rejected, `contract_violation` trace event

This addresses the critique that "if the kernel doesn't verify, anyone can feed fake signatures."

---

## 5. Track 1d — Input Resolution Semantics (Kernel-Owned)

### Problem

Input resolution is currently ecosystem-only, but it changes execution determinism, evaluation order, and trace meaning. If the kernel executes runbooks, it must own the semantics.

### Design: Kernel defines semantics, ecosystem provides implementations

#### Kernel-level spec (in `kernel-v0.md`)

Input `from:` bindings have a defined resolution order:

```yaml
meta:
  inputs:
    hostname:
      type: string
      from: provider/cmdb.server.hostname   # resolved by named provider
    zone:
      type: string
      from: prompt                           # resolved by prompting the user
    threshold:
      type: int
      default: 200                           # fallback if not resolved
```

**Resolution order (kernel-defined):**

1. CLI flags (`--var hostname=x`) — always wins
2. Provider resolution (`from: provider/...`) — if provider is configured
3. Prompt (`from: prompt`) — interactive input
4. Default value — fallback
5. Missing required → execution halts with clear error

**Kernel contract:**
- Input resolution is a **kernel API**, not host-specific pre-processing
- The kernel exposes `ResolveInputs(runbook, hostVars, resolvers) -> (ResolvedVars, []TraceEvent)`
- All hosts (CLI, MCP, TUI) call the same kernel function — no host reimplements resolution semantics
- Resolved values + their sources are returned as trace events emitted by the kernel
- Provider failures are returned as errors with structured context

#### Kernel API

```go
// ResolveInputs is the kernel's input resolution service.
// All hosts (CLI, MCP, TUI) call this — never reimplement resolution logic.
// Lives in pkg/kernel/engine/ or pkg/kernel/resolve/.
func ResolveInputs(
    ctx context.Context,
    rb *schema.Runbook,
    hostVars map[string]string,      // CLI flags, env vars
    resolvers []InputResolver,        // ecosystem-provided resolvers
) (*ResolvedInputs, error)

type ResolvedInputs struct {
    Vars   map[string]string          // final resolved values
    Events []trace.Event              // input_resolved events for trace
}
```

#### InputResolver interface (kernel-defined, ecosystem-implemented)

```go
// InputResolver is the kernel interface for input resolution.
// Lives in pkg/kernel/schema/ or pkg/kernel/engine/.
type InputResolver interface {
    Resolve(ctx context.Context, binding InputBinding) (string, error)
}

type InputBinding struct {
    Name     string // input name
    From     string // "prompt", "provider/cmdb.server.hostname", etc.
    Type     string // string, int, bool
    Default  any    // fallback value
    Required bool
}
```

#### Ecosystem implementations

| Resolver | `from:` prefix | Implementation |
|----------|---------------|----------------|
| CLI flags | — | Built into `cmd/gert/` (not a resolver, always wins) |
| Prompt | `prompt` | Built into `cmd/gert/` (reads stdin) |
| Provider | `provider/<name>.<path>` | JSON-RPC stdio to external binary |
| Environment | `env/<VAR_NAME>` | Reads environment variable |
| File | `file/<path>` | Reads value from a file |

#### Why this belongs in the kernel spec

- **Determinism:** replay must produce the same results. If input resolution isn't specified, replay can't reproduce the original run.
- **Trace completeness:** the trace must record where each input came from, or audit is incomplete.
- **Validation:** `gert validate` should check that all `from:` bindings reference configured providers.

#### InputProvider ↔ InputResolver adapter rule

The kernel interface (`InputResolver`) is binding-oriented (resolves one binding at a time). The ecosystem `InputProvider` interface is batch-oriented (resolves many bindings in one call over JSON-RPC). These are bridged:

- `ResolveInputs` groups bindings by provider prefix (e.g., all `provider/cmdb.*` bindings go to the `cmdb` provider)
- Each group is sent as a single batch `Resolve` call to the `InputProvider`
- The provider returns a map; the kernel distributes results back to individual bindings
- The kernel remains binding-oriented for trace granularity (one `input_resolved` event per binding)
- Batching is an optimization; the semantic contract is per-binding resolution

---

## 6. Track 1 Cross-Cutting Decisions

### 6.1 Canonical Outcome Categories

One set, no drift. The kernel enforces this enum:

| Category | Meaning |
|----------|---------|
| `resolved` | Problem fixed |
| `escalated` | Handed off to another team/process |
| `no_action` | No intervention needed |
| `needs_rca` | Mitigated but root cause unknown |

Domain-specific meaning lives in `outcome.code` (free-form string) and `outcome.meta` (free-form map). Do NOT invent new categories mid-doc or mid-runbook. Governance keys on categories; extending the enum breaks policy contracts.

**Engine errors are not outcomes.** Runs that error before reaching an `end` step (validation failure, tool crash, contract violation) are represented by `run status` (`error`, `failed`), not by an outcome category. The outcome enum is reserved for runs that reached a terminal `end` step.

**Governance scope:** Governance rules evaluate **each step independently**. Run-level policies (e.g., "no production writes after hours") are outside kernel scope and belong to host enforcement.

### 6.2 Run Identity (run_start event)

The `run_start` trace event must include identity + provenance for audit-grade traceability:

```json
{
  "type": "run_start",
  "data": {
    "runbook": "service-health-check",
    "runbook_hash": "sha256:a1b2c3...",
    "actor": "oncall@company.com",
    "host": "runner-prod-03.internal",
    "gert_version": "v0.1.0 (abc1234)",
    "tool_hashes": {
      "health-check": "sha256:d4e5f6...",
      "restart-service": "sha256:789abc..."
    },
    "inputs": { "hostname": "srv1.example.com" },
    "input_sources": { "hostname": "cli" },
    "constants": { "health_endpoint": "/healthz" }
  }
}
```

This proves: who ran it, where it ran, what exact runbook + tools were used, and what version of gert executed it. Supply chain for runbooks.

**Schema change:** `RunConfig` gains `Actor string` and `Host string`. `run_start` event in trace writer includes runbook/tool content hashes computed at load time.

**Hash scope:** `tool_hashes` and `runbook_hash` are computed from the **YAML definition file content**, not from referenced binaries. Binary integrity is outside gert's scope — supply-chain verification of tool binaries belongs to the host platform (e.g., signed binaries, container image hashes).

### 6.3 Extension Step Runner Protocol (Track 1f)

Extension steps dispatch to external executors via JSON-RPC 2.0 over stdio.

**Packaging:** An extension runner is a binary on `PATH` or referenced by absolute path in the step:

```yaml
- id: custom_check
  type: extension
  extension: my-runner          # resolved as binary "gert-ext-my-runner" on PATH
  contract:
    effects: [network]
    writes: []
    inputs:
      target: { type: string, required: true }
    outputs:
      status: { type: string }
  inputs:
    target: "{{ .hostname }}"
```

**Protocol:**

```
Kernel                          Extension Runner (stdio)
  │                                    │
  │── spawn gert-ext-my-runner         │
  │── JSON-RPC: initialize ──────────→ │
  │←── { capabilities } ──────────────│
  │                                    │
  │── JSON-RPC: execute ─────────────→ │
  │   { inputs, vars, contract }       │
  │←── { outputs, exit_code } ────────│
  │                                    │
  │── JSON-RPC: shutdown ────────────→ │
  │── process exits                    │
```

**Methods:**

| Method | Request | Response |
|--------|---------|----------|
| `initialize` | `{ protocol_version: "1" }` | `{ capabilities: {} }` |
| `execute` | `{ inputs: {}, vars: {}, contract: {} }` | `{ outputs: {}, exit_code: 0, stderr: "" }` |
| `shutdown` | `{}` | `{}` (then process exits) |

**Kernel enforcement:**
- Extension runner outputs are checked against `contract.outputs` — undeclared outputs are stripped and a `contract_violation` event is emitted
- `contract.effects` and `contract.writes` are enforced by governance the same way as tool steps — the kernel doesn't trust the runner, it trusts the declared contract
- If the runner process crashes or times out → step status `error`

**Trust boundary:** Extension runners are **trusted executables**. gert enforces contracts at the interface level (inputs/outputs/governance) but **cannot prevent undeclared side effects inside runners**. A runner could make network calls, write files, or leak secrets without gert's knowledge. Governance must treat extension runners as **privileged code** — the same trust level as tool binaries. Auditors should not assume gert sandboxes extensions.

**Statelessness invariant:** Extension runners **MUST behave deterministically given the same inputs** and **MUST NOT rely on hidden mutable state that changes outputs across calls**. Read-only caching for performance is allowed if it does not affect outputs and does not persist secrets. The kernel reuses the runner process for efficiency but does not guarantee isolation between calls.

**Contract honesty:** gert enforces declared contracts but **cannot verify undeclared side effects**. A tool declaring `effects: [network]` could also write to the filesystem. Governance must assume tools and extensions are trusted code. The contract model is a declaration of intent, not a sandbox.

**Tool vs extension — when to use which:**
- Use **tools** for external actions that produce data (run a command, call an API, query a database)
- Use **extensions** when the step needs custom execution semantics or lifecycle beyond tool invocation (multi-phase operations, stateful protocols, custom retry logic)
- If it looks like a tool, make it a tool. Extensions are the escape hatch, not the default.

**Lifecycle:** spawn on first use, reuse for subsequent extension steps referencing the same runner, shutdown on engine completion. Same pattern as jsonrpc tool transport.

### 6.4 Approval Signing — Algorithm Flexibility

HMAC-SHA256 for v0 bootstrap. The interface supports swapping to asymmetric signatures later:

```go
type ApprovalResponse struct {
    TicketID   string    `json:"ticket_id"`
    Approved   bool      `json:"approved"`
    ApproverID string    `json:"approver_id"`
    Method     string    `json:"method"`
    Timestamp  time.Time `json:"timestamp"`
    Signature  string    `json:"signature,omitempty"`
    SignatureAlg string  `json:"signature_alg,omitempty"` // "hmac-sha256", "ed25519", "rs256"
    KeyID      string    `json:"key_id,omitempty"`        // for key rotation
    Reason     string    `json:"reason,omitempty"`
}
```

- `signature_alg` and `key_id` make the format future-proof for asymmetric (Ed25519, RSA)
- Kernel verification policy (`require_verified_approval: true`) calls a `SignatureVerifier` interface — ecosystem provides the implementation (HMAC, Ed25519, etc.)
- Don't hard-code "HMAC only" into governance language — the kernel checks `verified: bool`, not the algorithm

**Approval invariants:**
- **Validity window:** approval responses must arrive within `Timeout` of the request. Expired approvals are treated as rejections.
- **Clock skew tolerance:** 30 seconds. If `response.Timestamp` is more than 30s before `request.Created`, reject as stale.
- **Multi-approver aggregation:** when `min_approvers > 1`, the kernel collects N approvals before proceeding. Each approval is a separate `ApprovalResponse`. The engine resumes only when N responses with `approved: true` are received, or any single `approved: false` halts with rejection.
- **Replay protection:** approval responses are bound to `ticket_id`. A response for a different ticket is ignored. Tickets are single-use — once resolved (approved/rejected/expired), the ticket cannot be reused.

### 6.5 Watch Mode Framing

`gert watch` is a **developer convenience**, not a scheduler. It literally calls `engine.New() + engine.Run()` in a loop. No new kernel interfaces. No new engine state. It's a for-loop in `cmd/gert/`.

**Stop semantics:**
- `--stop-on` triggers only on **terminal outcomes** (the run reached an `end` step)
- If a run fails before reaching an outcome (engine error) → loop stops (errors are not recoverable by retrying)
- If a run enters `approval_pending` state → loop stops (can't auto-resume async approvals in a loop)
- Trace signing failures → loop stops (integrity cannot be guaranteed)\n- Probe mode failures and contract violations do **not** stop the loop unless they result in a terminal outcome or engine error

---

_Track 1 remainder: Secrets (§12), Contract Violations (§12.2), Probes (§12.3), Trace Integrity (§15)._

---

## 7. TUI (Track 2a)

### Goal

A Bubble Tea interface wrapping the kernel engine for interactive step-by-step execution. **Separate binary** (`gert-tui`), not bundled with the core CLI.

### Why separate

The core `gert` CLI is designed for scripts, CI, and pipes. The TUI is for operators sitting at a terminal during an incident. Different audience, different dependencies:

- `gert` — no Bubble Tea, no lipgloss, no terminal UI deps. ~5MB binary.
- `gert-tui` — imports Bubble Tea + lipgloss + glamour. ~12MB binary.
- Follows `kubectl`/`k9s`, `git`/`lazygit`, `docker`/`lazydocker` pattern.

### Architecture

```
                  cmd/gert-tui/main.go
                        │
User Input ──→ pkg/ecosystem/tui/ ──→ pkg/kernel/engine/
                  │                          │
                  │←── events ───────────────│ (step_start, step_complete, ...)
                  │
                  ├── Step List Panel (left)
                  ├── Output Panel (center/right)
                  └── Status Panel (bottom)
```

### Design

- **Import:** `pkg/kernel/engine`, `pkg/kernel/schema`, `pkg/kernel/trace`
- **Custom ToolExecutor:** wraps the default executor, feeds stdout/stderr to the output panel in real-time
- **Custom ApprovalProvider:** renders approval prompts in the TUI instead of raw stdin
- **Trace listener:** subscribes to trace events to update step status icons (✓, ✗, ○, ⚠) in the step list
- **Modes:** real, replay, dry-run — same as CLI, but visual

### Layout

```
┌─────────────────┬──────────────────────────────────┐
│ Steps           │ Output                           │
│                 │                                  │
│ ✓ check_health  │ $ curl -s https://srv1/healthz   │
│ ✓ evaluate      │ 200                              │
│ → triage        │                                  │
│   ○ restart     │ [branch: healthy]                │
│   ○ investigate │                                  │
│   ○ healthy_end │                                  │
│                 │                                  │
├─────────────────┴──────────────────────────────────┤
│ Status: executing triage │ Risk: low │ Duration: 2s│
└────────────────────────────────────────────────────┘
```

### Key interactions

| Key | Action |
|-----|--------|
| Enter | Advance to next step (manual steps) |
| `q` | Quit |
| `d` | Toggle dry-run mode |
| `v` | Show variables |
| `c` | Show contract for current step |
| `t` | Show trace events |
| `/` | Search steps |

### Reuse from old TUI

The old `pkg/tui/` has Bubble Tea components worth porting:
- `app.go` — main model structure, key handling
- `steps.go` — step list rendering with status icons
- `output.go` — scrollable output panel
- `detail.go` — step detail view
- `styles.go` — lipgloss styling
- `evidence.go` — evidence entry forms

Port the layout and rendering; replace the engine calls with kernel's `engine.New()` + `engine.Run()`.

---

## 8. MCP Server (Track 2b)

### Goal

Expose gert operations as MCP (Model Context Protocol) tools so AI agents can validate, execute, and test runbooks.

### MCP Tools

| Tool name | Description | Parameters |
|-----------|-------------|------------|
| `gert/validate` | Validate a runbook file | `{ path: string }` |
| `gert/exec` | Execute a runbook | `{ path: string, vars: object, mode: "real"\|"dry-run" }` |
| `gert/test` | Run scenario tests | `{ path: string, scenario?: string }` |
| `gert/list-tools` | List available tool definitions | `{ dir: string }` |
| `gert/schema` | Export JSON Schema | `{ type: "runbook"\|"tool" }` |
| `gert/step-contract` | Show resolved contract for a step | `{ path: string, step_id: string }` |

### Architecture

```
AI Agent (Claude, GPT, etc.)
    │
    │  MCP protocol (stdio or HTTP)
    │
    ▼
cmd/gert-mcp/ ──→ pkg/ecosystem/mcp/
                        │
                        ├── validate handler  → pkg/kernel/validate
                        ├── exec handler      → pkg/kernel/engine
                        ├── test handler      → pkg/kernel/testing
                        └── schema handler    → pkg/kernel/schema
```

### Implementation

- Use the Go MCP SDK (`github.com/mark3labs/mcp-go` or similar)
- `cmd/gert-mcp/main.go` — **standalone MCP server binary**, separate from core CLI
- Follows same pattern: `gert` (core CLI), `gert-tui` (humans), `gert-mcp` (AI agents)
- Each handler imports kernel packages directly — no intermediate layer
- Exec handler uses a custom `ToolExecutor` that records output for the MCP response
- Approval requests routed via `ApprovalProvider` — could prompt the AI agent for a decision or delegate to an external system

### MCP Resource types

| Resource | URI pattern | Description |
|----------|-------------|-------------|
| Runbook | `gert://runbooks/{name}` | Runbook YAML content + validation status |
| Tool | `gert://tools/{name}` | Tool definition + contract summary |
| Scenario | `gert://scenarios/{runbook}/{name}` | Scenario YAML + test result |
| Trace | `gert://traces/{run_id}` | JSONL trace for a completed run |

---

## 9. VS Code Extension (Track 2c)

### Goal

Interactive runbook authoring, validation, execution, and testing inside VS Code.

### Features

| Feature | How it works |
|---------|-------------|
| **Validate on save** | File watcher → call `gert/validate` via MCP or JSON-RPC → inline diagnostics |
| **Run runbook** | Command palette → `gert/exec` → webview panel with step progress |
| **Run tests** | Command palette → `gert/test` → test results in a tree view |
| **Contract lens** | CodeLens above each step showing resolved risk level |
| **Approval UX** | When `require-approval` triggers, show a VS Code notification with approve/reject buttons |
| **Trace viewer** | Open JSONL trace file → timeline visualization |
| **Schema completion** | JSON Schema for `kernel/v0` and `tool/v0` → YAML autocomplete |
| **Scenario scaffolding** | Right-click runbook → "New Scenario" → creates scenario directory structure |

### Architecture

```
VS Code Extension (TypeScript)
    │
    │  Backend protocol (MCP or JSON-RPC, chosen per feature)
    │
    ▼
gert-mcp / gert serve (Go)
    │
    └── kernel packages
```

### Backend

**MCP as one option, not the only one.** MCP is still evolving and may not map cleanly to all VS Code UX needs (streaming logs, partial results, file watching, cancellations, approval prompts).

| Feature | Best backend | Why |
|---------|-------------|-----|
| Validate, exec, test | MCP (`gert-mcp`) | Standard operations — same as AI agents |
| Streaming execution output | JSON-RPC (`gert serve`) | MCP notification support is SDK-dependent |
| File watch + diagnostics push | JSON-RPC (`gert serve`) | Needs server-initiated messages |
| Approval prompts in VS Code | JSON-RPC or MCP | Depends on which supports bidirectional prompts |

Start with MCP for the standard operations. Add a thin JSON-RPC layer only for features that MCP can't handle after practical testing. Don't commit to MCP-only until the full lifecycle is proven.

### Reuse from old extension

The old `vscode/` has:
- `extension.ts` — activation, command registration
- `src/serve/` — JSON-RPC client and session management
- `src/views/` — webview panel for step-by-step execution
- `syntaxes/` — YAML syntax injection for Kusto blocks
- `schemas/` — bundled JSON schemas

Port the extension shell and webview layout; replace the JSON-RPC calls with MCP tool invocations.

---

## 10. Input Providers (Track 1d implementations)

### Goal

Pluggable resolution of runbook inputs from external systems (incident management, CMDB, PagerDuty, ServiceNow).

### Design

#### Interface

```go
// InputProvider resolves input values from an external system.
type InputProvider interface {
    Resolve(ctx context.Context, bindings []InputBinding) (map[string]string, error)
    Name() string
}

type InputBinding struct {
    InputName string // the runbook input name
    From      string // provider-specific path, e.g. "pagerduty.incident.title"
}
```

#### Runbook integration

```yaml
meta:
  inputs:
    hostname:
      type: string
      from: cmdb.server.hostname    # resolved by "cmdb" provider
    incident_id:
      type: string
      from: pagerduty.incident.id   # resolved by "pagerduty" provider
    manual_note:
      type: string
      from: prompt                   # resolved by prompting the user
```

#### Provider configuration

```yaml
# gert-project.yaml or .gert/config.yaml
providers:
  cmdb:
    transport: jsonrpc
    binary: gert-cmdb-provider
    config:
      endpoint: https://cmdb.internal/api
  pagerduty:
    transport: jsonrpc
    binary: gert-pd-provider
    config:
      api_key_env: PAGERDUTY_API_KEY
```

#### Provider protocol

Providers communicate over JSON-RPC 2.0 stdio (same as old `pkg/inputs/`):

```json
// Request
{"jsonrpc": "2.0", "method": "resolve", "params": {"bindings": [{"input": "hostname", "path": "server.hostname"}]}, "id": 1}

// Response
{"jsonrpc": "2.0", "result": {"hostname": "srv1.example.com"}, "id": 1}
```

#### Engine integration

Providers are invoked via the kernel's `ResolveInputs` API (§5). Hosts supply resolver implementations, but resolution order, tracing, and error semantics are kernel-defined. Hosts do **not** pre-process inputs — they call `ResolveInputs()` and pass the result to the engine.

---

## 11. Tool as MCP Consumer

### Problem

The kernel supports `transport: mcp` in tool definitions, but it's not implemented. This would let a tool definition reference an MCP server and invoke its tools as gert tool actions.

### Design

```yaml
apiVersion: tool/v0
meta:
  name: ai-search
  transport: mcp
  connect: stdio                    # or http://localhost:8080
  binary: mcp-search-server        # for stdio transport
contract:
  inputs:
    query: { type: string, required: true }
  outputs:
    results: { type: string }
  effects: [network]
  writes: []
  reads: [search_index]
actions:
  search:
    mcp_tool: search_documents      # MCP tool name on the server
```

### Implementation

- Add an MCP client to `pkg/kernel/executor/` alongside the stdio executor
- For `transport: mcp` + `connect: stdio`: spawn the binary, initialize MCP session, call `tools/call`
- For `transport: mcp` + `connect: http://...`: connect to running MCP server
- Map tool action inputs to MCP tool arguments, map MCP results back to contract outputs
- MCP tool servers are initialized once per engine run and reused for all actions targeting that server. Connection failure triggers restart and step retry (once). If restart also fails → step status `error`. Session is closed on engine completion.

### Priority

Lower than Phases A–F. Can be added when AI agent integration demands it.

---

## 12. Secrets + Contract Hardening (Track 1e/1h)

### 9.1 Secrets Convention

#### Problem

gert delegates secrets to the host platform, but there's no way to declare what secrets a tool or runbook needs. Users discover missing secrets at runtime via cryptic tool errors.

#### Design: `secrets` block

**On tool definitions:**

```yaml
apiVersion: tool/v0
meta:
  name: az-cli
  binary: az
secrets:
  - env: AZURE_CLIENT_SECRET
    description: "Azure service principal secret"
    required: true
  - env: AZURE_TENANT_ID
    description: "Azure tenant ID"
    required: true
```

**On runbooks:**

```yaml
meta:
  name: deploy-service
  secrets:
    - env: DEPLOY_TOKEN
      description: "Deploy token for production"
    - env: SLACK_WEBHOOK
      description: "Slack webhook for notifications"
      required: false  # optional — notification skipped if missing
```

#### Behavior

| Phase | What happens |
|-------|-------------|
| **Validation** | `gert validate` reports: "This runbook requires 3 secrets: AZURE_CLIENT_SECRET, AZURE_TENANT_ID, DEPLOY_TOKEN." Warning if any are missing from current environment. |
| **Dry-run** | Reports which secrets are present/missing alongside contract and governance info. |
| **Execution** | Missing required secret → step status `error` with clear message: "tool `az-cli` requires secret `AZURE_CLIENT_SECRET`". |
| **Trace** | Secret **names** are recorded (for auditability). Secret **values** are never recorded. |
| **Redaction** | Values of declared `secrets[].env` vars are automatically redacted in tool stdout/stderr captured in the trace. |

#### Works with every secret store

| Store | How it feeds gert |
|-------|-------------------|
| Kubernetes Secrets | Mounted as env vars in the pod |
| Azure Key Vault | `az keyvault secret show` → env var in CI |
| HashiCorp Vault | `vault kv get` → env var via wrapper |
| GitHub Actions | `${{ secrets.X }}` → env var |
| AWS Secrets Manager | `aws secretsmanager get-secret-value` → env var |
| 1Password CLI | `op run -- gert exec runbook.yaml` |
| `.env` file | `source .env && gert exec runbook.yaml` (dev only) |

#### Schema changes

- Add `Secrets []SecretRef` to `schema.ToolMeta` and `schema.Meta`
- `SecretRef`: `{ Env string, Description string, Required bool }`
- Validation rule D22: check secret env var presence (warning, not error — may run in different environment)

### 9.2 Contract Violation Detection

#### Problem

Contracts are author-declared. An author can mark a destructive tool as `side_effects: false`. Nothing verifies this at runtime.

#### Design

After each tool step execution, the engine performs contract consistency checks:

| Check | How | Violation |
|-------|-----|-----------|
| **Undeclared outputs** | Tool produces output keys not listed in `contract.outputs` | `contract_violation` trace event (warning) |
| **Missing declared outputs** | Tool doesn't produce all keys listed in `contract.outputs` | `contract_violation` trace event (warning) |
| **Deterministic check** | If `deterministic: true` and the step was previously executed with the same inputs (in a retry loop), compare outputs. Different outputs → violation. | `contract_violation` trace event (error) |

The engine records violations in the trace but **does not halt by default** — the author may have legitimate reasons. Repeated violations across multiple runs signal a bad contract, surfaced via outcome aggregation (Phase I).\n\n**Escalation policy:** Governance may escalate contract violations to step failure. If a governance rule includes `contract_violations: deny`, the engine treats any violation as a step error and halts. This allows organizations to tighten enforcement without kernel changes:\n\n```yaml\ngovernance:\n  rules:\n    - contract_violations: deny      # strict mode — any violation halts\n    - default: allow\n```

#### Trace event

```json
{
  "type": "contract_violation",
  "data": {
    "step_id": "check_health",
    "kind": "undeclared_output",
    "message": "tool produced output 'body' not declared in contract.outputs",
    "severity": "warning"
  }
}
```

### 9.3 Dry-Run Contract Probes

#### Problem

Dry-run currently skips tool execution entirely. It can't verify that contracts are accurate because it never runs the tool.

#### Design: `--mode probe`

A new execution mode between dry-run and real:

```bash
gert exec runbook.yaml --mode probe --var hostname=test.example.com
```

**Behavior:**
- Executes tool steps where `writes == []` **and** `effects` are in an allowed list
- **Default allowed effects for probe: `[network]` only.** Database probes require explicit opt-in: `--probe-allow-effects network,database`
- Skips tools with any `writes` (reports contract + governance as dry-run does)
- For executed tools: applies contract violation detection (§12.2)
- For `deterministic: true` tools: runs twice with same inputs, verifies output consistency

This lets you validate contracts against real infrastructure without causing side effects.

---

## 13. Replay Excellence (Track 2d)

### 10.1 Auto-Record Mode

#### Problem

Scenario creation is manual. Users must write `scenario.yaml` files by hand after an incident. Most don't bother.

#### Design

Every `gert exec` can automatically record a replayable scenario:

```bash
# Record while executing
gert exec runbook.yaml --var hostname=srv1 --record scenarios/srv1-incident/

# Always record (via config or env var)
GERT_AUTO_RECORD=true gert exec runbook.yaml --var hostname=srv1
```

**What gets recorded:**

```
scenarios/srv1-incident/
├── scenario.yaml          # inputs + tool responses (captured from real execution)
├── test.yaml              # auto-generated: expected_status + expected_outcome + must_reach (from actual run)
└── trace.jsonl            # full trace for reference
```

The `scenario.yaml` is built during execution: every tool response is captured and written to the `tool_responses` section. Every manual step's evidence is captured to the `evidence` section. The `test.yaml` is generated from the actual outcome — it asserts that a replay produces the same result.

**Engine change:** Add a `Recorder` that wraps `ToolExecutor`, intercepts responses, and writes the scenario file at run completion.

**Secrets safety:** Recorded scenarios inherit trace redaction rules. Secret values declared in `secrets` blocks are redacted from captured tool stdout/stderr before writing to scenario files. Scenario files must never contain secret values.

### 10.2 Scenario Diffing

#### Problem

When a runbook changes, you don't know if historical incidents would produce different outcomes.

#### Design

```bash
gert diff runbook.yaml
```

Runs all recorded scenarios against the current runbook and reports outcome differences:

```
  service-health-check

  Scenario: srv1-incident-2026-02-15
    Before: resolved (service_restarted)
    After:  resolved (service_restarted)     ✓ same

  Scenario: srv2-incident-2026-02-20
    Before: escalated (unknown_failure)
    After:  resolved (dns_fixed)             ⚠ outcome changed!
    Steps changed:
      + dns_repair (newly reached)
      - investigate (no longer reached)

  1 unchanged, 1 changed
```

**Implementation:** Re-run each scenario with the current runbook in replay mode. Compare the `RunResult` (outcome category, code, visited steps, outputs) against the recorded `test.yaml`. Report differences.

### 10.3 Golden Scenario Promotion

#### Problem

Auto-recorded scenarios accumulate. Not all are worth keeping as permanent tests. Teams need a way to curate.

#### Design

```bash
# Promote a recorded run to a named golden scenario
gert scenario promote scenarios/srv1-incident/ --name healthy-restart

# List golden scenarios
gert scenario list runbook.yaml

# Demote (remove golden flag, scenario stays as archive)
gert scenario demote healthy-restart
```

**Mechanics:**
- "Golden" is a marker in `test.yaml`: `golden: true`
- `gert test` runs **only golden scenarios** by default
- `gert test --all` runs everything (golden + archived)
- `gert diff` runs only golden scenarios
- CI pipeline runs `gert test` → only curated, meaningful tests

---

## 14. Outcome Intelligence (Track 2e)

### 11.1 Outcome Aggregation CLI

#### Problem

Structured outcomes exist per-run, but there's no way to see trends across runs.

#### Design

```bash
# Summary of recent runs
gert outcomes --since 7d --runbook service-health-check

  service-health-check (23 runs, last 7 days)

  resolved:    17 (74%)  ████████████████░░░░░░
  escalated:    3 (13%)  ███░░░░░░░░░░░░░░░░░░
  no_action:    2 (9%)   ██░░░░░░░░░░░░░░░░░░░
  needs_rca:    1 (4%)   █░░░░░░░░░░░░░░░░░░░░

  Top outcome codes:
    service_restarted   12
    dns_fixed            5
    unknown_failure      3

# JSON output for dashboards
gert outcomes --since 30d --json
```

**Data source:** Reads `run_complete` and `outcome_resolved` events from trace files. Scans a configured trace directory.

**Configuration:**

```yaml
# gert-config.yaml or env var
traces:
  dir: /var/log/gert/traces    # or ~/.gert/traces
```

### 11.2 Outcome Webhooks

#### Problem

gert runs finish silently. Teams want to be notified of outcomes, especially escalations.

#### Design

```yaml
# In runbook meta or gert-config.yaml
meta:
  on_outcome:
    escalated:
      webhook: https://hooks.slack.com/services/xxx
      pagerduty_severity: critical
    needs_rca:
      webhook: https://hooks.slack.com/services/xxx
    resolved:
      webhook: https://hooks.slack.com/services/xxx  # optional — log successes too
```

**Implementation:**
- After `run_complete`, check if the outcome category has a configured webhook
- POST a JSON payload with: runbook name, outcome (category, code, meta), duration, run ID, trace path
- Fire-and-forget — webhook failure doesn't affect the run result
- Webhook payload includes enough context for Slack formatting or PagerDuty event creation

**Payload example:**

```json
{
  "runbook": "service-health-check",
  "run_id": "run-2026-02-28-001",
  "outcome": {
    "category": "escalated",
    "code": "unknown_failure",
    "meta": { "status_code": "418" }
  },
  "duration": "4.2s",
  "trace": "/var/log/gert/traces/run-2026-02-28-001.jsonl",
  "timestamp": "2026-02-28T14:30:00Z"
}
```

---

## 15. Trace Integrity (Track 1g)

### 12.1 Hash Chaining

#### Problem

The trace is append-only during a run, but nothing prevents post-hoc modification. For compliance (SOC2, FedRAMP, ISO 27001), auditors need tamper evidence.

#### Design

Each trace event includes a `prev_hash` field — the SHA-256 hash of the previous event's JSON:

```json
{"type":"run_start","timestamp":"...","run_id":"r1","data":{...},"prev_hash":"0000000000000000"}
{"type":"step_start","timestamp":"...","run_id":"r1","data":{...},"prev_hash":"a1b2c3d4e5f6..."}
{"type":"step_complete","timestamp":"...","run_id":"r1","data":{...},"prev_hash":"f6e5d4c3b2a1..."}
```

- First event: `prev_hash` is zero (genesis)
- Each subsequent event: `prev_hash = SHA256(previous_event_json)`
- Modifying any event breaks the chain for all subsequent events
- Verification: `gert trace verify run.jsonl` — walks the chain, reports breaks

**Engine change:** `trace.Writer.Emit()` computes and includes `prev_hash` before writing.

### 12.2 Trace Signing

#### Problem

Hash chaining proves internal consistency, but doesn't prove *who* produced the trace. An attacker could rewrite the entire chain.

#### Design

At run completion, the engine signs the final chain hash:

```bash
# Signing key from environment (convention, like secrets)
GERT_TRACE_SIGNING_KEY=base64-encoded-hmac-key gert exec runbook.yaml --trace run.jsonl
```

The `run_complete` event includes:

```json
{
  "type": "run_complete",
  "data": {
    "status": "completed",
    "chain_hash": "final-sha256-of-entire-chain",
    "signature": "hmac-sha256-of-chain-hash-with-signing-key",
    "signing_key_id": "prod-2026"
  }
}
```

**Verification:**

```bash
gert trace verify run.jsonl --key-id prod-2026
✓ Chain integrity: 47 events, no breaks
✓ Signature valid: signed by key "prod-2026"
```

- The kernel computes and records the signature
- The kernel does NOT store or manage keys — keys are env vars (same convention as secrets)
- `signing_key_id` is a label, not the key — for key rotation
- Verification tooling can run independently (auditors, compliance tools)

---

## 16. Watch Mode (Track 2f)

### Problem

gert has no scheduling, by design. But operators often want a simple "run this every 5 minutes" loop for monitoring runbooks, without setting up cron or Kubernetes CronJobs.

### Design

```bash
# Run a health check every 5 minutes
gert watch runbooks/health-check.yaml --interval 5m --var hostname=srv1

# Stop on first failure
gert watch runbooks/health-check.yaml --interval 5m --stop-on escalated,needs_rca

# With outcome webhook
gert watch runbooks/health-check.yaml --interval 5m --config gert-watch.yaml
```

**Behavior:**
- Runs `gert exec` in a loop with the specified interval
- Each run is independent — fresh engine, fresh variables
- Trace files written per-run: `traces/health-check-2026-02-28T14:30:00.jsonl`
- Console output: one summary line per run

```
14:30:00  ✓ resolved (service_healthy)   2.1s
14:35:00  ✓ resolved (service_healthy)   1.8s
14:40:00  ✗ escalated (unknown_failure)  4.3s  ← stopping (--stop-on escalated)
```

- `--stop-on <categories>`: stop the loop when an outcome matches
- Outcome webhooks fire per-run if configured
- Ctrl+C gracefully stops after the current run completes

**What this is NOT:**
- Not a daemon/service — it's a foreground loop
- Not a replacement for cron/K8s CronJobs — those are better for production scheduling
- Not highly available — single process, single machine

**What this IS:**
- A convenience for development and light monitoring
- A way to soak-test a runbook against real infrastructure
- A quick setup for "run this health check in a `tmux` session"

---

## 17. Summary — What the Kernel Needs

### Track 1 — Kernel changes (must do first)

| Change | Kernel package | Track | Notes |
|--------|---------------|-------|-------|
| Contract taxonomy (`effects`, deprecate `side_effects`) | contract, schema, validate, governance | 1b | `reads`/`writes` for parallel only, `effects` for governance |
| Resumable approval (ticket-based, Submit/Wait) | engine, trace | 1c | No blocking calls; supports sync + async + resume |
| `ResolveInputs` kernel API | engine (or resolve/) | 1d | All hosts call same function; ecosystem provides resolvers |
| `SecretRef` in schema | schema, validate | 1e | Declaration, validation, auto-redaction |
| Extension step runner (JSON-RPC stdio protocol) | engine, executor | 1f | §6.3 protocol spec |
| `prev_hash` in trace events | trace | 1g | Hash chain for tamper evidence |
| Trace signing + `SignatureVerifier` interface | trace, governance | 1g | Algorithm-flexible (HMAC → Ed25519) |
| Run identity in `run_start` | trace, engine | 1g | Actor, host, gert_version, runbook/tool hashes |
| Contract violation detection | engine, trace | 1h | Runtime + probe mode |
| Probe mode (`writes==[]` + allowed effects) | engine | 1h | Not keyed on deprecated `side_effects` |
| `context.Context` on all provider interfaces | engine | 1b (prereq) | ApprovalProvider, ToolExecutor, InputResolver |
| Canonical outcome enum (4 categories only) | schema | 1a | resolved, escalated, no_action, needs_rca. No drift. |
| Scoped state + export semantics (`scope`, `export`) | schema, engine, trace | 1i | Variable namespaces, merge rules, scope_export trace event |
| Visibility intent metadata (`visibility`) | schema, trace | 1i | allow/deny globs; recorded in trace; enforcement optional in v0 |
| Keyed fan-out outputs (`for_each.key`) | schema, engine, trace | 1i | Map-structured outputs; key collisions = runtime error |
| Principal attribution (`principal`) | trace, engine | 1i | kind/id/role/model on attributable trace events |
| `repeat` block (bounded multi-step iteration) | schema, engine, validate, trace | 1i | max + until; repeat_start/repeat_iteration trace events |
| `contract_violations` governance matcher | governance, schema | 1h | New rule type: `contract_violations: deny` promotes warnings to errors |

### Track 2 — Ecosystem-only (no kernel changes)

| Component | Packages | Track |
|-----------|----------|-------|
| TUI | `pkg/ecosystem/tui/`, `cmd/gert-tui/` | 2a |
| MCP Server | `pkg/ecosystem/mcp/`, `cmd/gert-mcp/` | 2b |
| VS Code Extension | `vscode/` | 2c |
| Auto-record + scenario diffing | `pkg/ecosystem/replay/` | 2d |
| Outcome intelligence | `pkg/ecosystem/outcomes/` | 2e |
| Watch mode | `cmd/gert/` (subcommand) | 2f |
| Approval providers (Teams, Slack, etc.) | `pkg/ecosystem/approval/` | 2a+ |
| Input providers (PagerDuty, CMDB, etc.) | `pkg/ecosystem/providers/` | 2a+ |

### CLI naming

- `cmd/gert-kernel/` (current) → **minimal reference binary**, kept for testing and embedding
- `cmd/gert/` (new, Track 1a) → **standard distribution**, replaces old broken `cmd/gert/`
- Old `cmd/gert/` (pre-kernel) → deleted when new `cmd/gert/` is built

### Stability

Kernel interfaces are **unstable** until Track 1 is complete. Expect breaking changes in:
- Contract model (adding `effects`)
- `ApprovalProvider` (moving to ticket-based)
- `ToolExecutor` (adding `context.Context`)
- Trace format (adding `prev_hash`)
- Input resolution (new kernel-level semantics)

Track 2 work should not start on a component until the kernel interfaces it depends on are hardened.

### Protocol unification direction

The kernel currently has three external execution models that use similar patterns (spawn binary, JSON-RPC stdio, structured results):

| Model | Used for | Protocol |
|-------|----------|----------|
| Tool transport (stdio) | Tool execution | spawn + argv + stdout capture |
| Extension runner | Custom step behavior | JSON-RPC stdio |
| Input provider | Input resolution | JSON-RPC stdio |

These are intentionally separate today — each has different method sets and lifecycle. However, the architecture signals a future **capability runtime** abstraction where a single JSON-RPC protocol handles all three roles via different method namespaces. Not required for v0, but avoid designs that make future unification impossible.

### What NOT to add next

- No distributed execution
- No built-in secrets manager
- No web UI before adoption
- No governance DSL v2 before real usage
- No plugin marketplace before pack format stabilizes
- No MAD-specific step types (the primitives compose into MAD patterns naturally)

Less design, more usage is the correct next move.

---

## 18. MAD-Ready Patterns (Multi-Agent Debate)

gert does not implement MAD as a feature. The kernel primitives — `for_each` with keyed outputs, `repeat`, scoped state, visibility intent, principal attribution, extension runners — compose into MAD patterns naturally. This section documents conventions, not new kernel features.

### 15.1 Reducer / Judge Extension Runner Convention

A canonical pattern for aggregating parallel outputs into a decision.

**Runner naming convention:**
- `gert-ext-reduce-*` — aggregation runners (merge, vote, score)
- `gert-ext-judge-*` — evaluation runners (pick winner, assess quality)

**Contract convention:**

```yaml
# judge.tool.yaml or as extension inline contract
contract:
  effects: [network]              # if calling an LLM for judgment
  writes: []
  deterministic: false
  inputs:
    items:
      type: object                # map of keyed outputs from fan-out
      required: true
    criteria:
      type: string
      required: false
  outputs:
    decision:
      type: string                # "agent_a" | "consensus" | "no_winner"
    scores:
      type: object                # { agent_a: 0.8, agent_b: 0.6 }
    winner:
      type: string
    summary:
      type: string
```

A judge runner receives all agent outputs as a map, evaluates them, and produces a structured decision. The kernel governs the judge step like any other — its contract declares effects, governance evaluates risk.

### 15.2 LLM Tool Convention

LLM calls are governed like any other tool. Recommended `llm.tool.yaml`:

```yaml
apiVersion: tool/v0
meta:
  name: llm
  description: Large language model inference
  transport: stdio
  binary: gert-llm-provider       # ecosystem binary
  platform: [linux, darwin, windows]
contract:
  effects: [network]              # LLM APIs are network calls
  writes: []                      # LLMs don't mutate infrastructure
  deterministic: false            # same prompt → different outputs
  idempotent: true                # safe to re-call
  inputs:
    prompt:
      type: string
      required: true
    model:
      type: string
      default: "claude-4-opus"
    temperature:
      type: float
      default: 0.7
  outputs:
    response:
      type: string
    usage:
      type: object
secrets:
  - env: ANTHROPIC_API_KEY
    description: "API key for Anthropic"
    required: true
actions:
  complete:
    argv: ["gert-llm-provider", "complete", "--model", "{{ .model }}", "--temp", "{{ .temperature }}"]
    extract:
      response: { from: stdout }
```

**Note:** This example uses `extract: { from: stdout }` mapping from the kernel tool schema (kernel-v0.md §12). The tool binary (`gert-llm-provider`) writes the LLM response to stdout; the kernel's extract plumbing maps it to the declared `response` contract output.

**Key governance point:** LLM calls are `effects: [network]`, `deterministic: false`. A governance rule like `effects: [network], deterministic: false → require-approval` would gate LLM usage — same as any other risky tool.

### 15.3 MAD-Ready Runbook Skeleton

Proof that the kernel primitives compose into a multi-agent debate without any MAD-specific features:

```yaml
apiVersion: kernel/v0

meta:
  name: multi-agent-review
  description: Three agents debate a question, judge picks winner
  inputs:
    question: { type: string, required: true }
  constants:
    agents:
      - { id: advocate, role: "argue in favor" }
      - { id: skeptic, role: "argue against" }
      - { id: analyst, role: "provide data-driven analysis" }

tools:
  - llm

steps:
  # Round 0: Blind fan-out — each agent answers independently
  - id: initial_responses
    type: tool
    tool: llm
    action: complete
    scope: "round/0"
    for_each:
      as: agent
      over: "{{ .agents }}"
      key: "{{ .agent.id }}"
      parallel: true
    visibility:
      allow: ["question"]
      deny: ["scope.round/0.*"]        # intent: hosts/executors SHOULD filter variable view
    inputs:
      prompt: "{{ .agent.role }}: {{ .question }}"
    export: ["response"]

  # Round 1: Critique — each agent sees all round 0 responses
  - id: critiques
    type: tool
    tool: llm
    action: complete
    scope: "round/1"
    for_each:
      as: agent
      over: "{{ .agents }}"
      key: "{{ .agent.id }}"
      parallel: true
    visibility:
      allow: ["question", "scope.round/0.*"]  # can see all round 0
      deny: ["scope.round/1.*"]               # can't see other critiques
    inputs:
      prompt: |
        You are {{ .agent.id }} ({{ .agent.role }}).
        Review these responses and critique:
        {{ .scope.round/0 }}
        Your critique:
    export: ["response"]

  # Judge: Evaluate all responses and critiques
  - id: judge
    type: extension
    extension: gert-ext-judge-llm
    contract:
      effects: [network]
      writes: []
      deterministic: false
      inputs:
        items: { type: object, required: true }
      outputs:
        decision: { type: string }
        winner: { type: string }
        summary: { type: string }
    inputs:
      items:
        round_0: "{{ .scope.round/0 }}"
        round_1: "{{ .scope.round/1 }}"
    export: ["decision", "winner", "summary"]

  # Outcome
  - type: end
    outcome:
      category: resolved
      code: debate_complete
      meta:
        winner: "{{ .winner }}"
        decision: "{{ .decision }}"
        summary: "{{ .summary }}"
```

**What this demonstrates:**
- `for_each` with `key` for named fan-out — schema defined in kernel-v0.md §6.4
- `scope` for round-based isolation — schema defined in kernel-v0.md §7
- `visibility` as **intent + trace** (hosts/executors SHOULD enforce by filtering variable view; kernel records intent but does not sandbox)
- Extension runner as judge (contract-governed like any step)
- `export` for promoting step outputs to global scope — export resolution rules in kernel-v0.md §7
- `principal` attribution in trace (each agent step records its agent ID) — defined in kernel-v0.md §11
- No MAD-specific kernel features — only existing primitives composed

**Note:** All schema additions used in this skeleton (`scope`, `export`, `visibility`, `for_each.key`, `repeat`) are kernel changes tracked in §17 Track 1i.

**Not shown (future via `repeat`):**
```yaml
  # Multi-round debate with convergence
  - id: debate
    type: repeat
    repeat:
      max: 3
      until: '{{ eq .decision "consensus" }}'
    steps:
      # ... fan-out + judge per round
```

---

## 19. Competitive Analysis

### Landscape

gert operates at the intersection of **runbook automation**, **incident response**, and **infrastructure orchestration**. No single competitor covers all of gert's surface. Here's how gert compares across dimensions:

### Direct competitors

| Product | Type | Governance | Contracts | Parallel | Replay/Test | TUI | AI Agent | Open Source |
|---------|------|-----------|-----------|----------|-------------|-----|----------|-------------|
| **gert** | Runbook engine | Contract-driven risk | ✅ Full | ✅ State-isolated | ✅ Scenario-based | Planned | Planned (MCP) | ✅ |
| **Rundeck** (PagerDuty) | Job scheduler + runbooks | ACL-based | ❌ | ✅ Workflow | ❌ | ❌ Web only | ❌ | ✅ (OSS edition) |
| **Ansible** | Config management + playbooks | Become/privilege escalation | ❌ | ✅ Forks | ❌ (check mode) | ❌ | ❌ | ✅ |
| **Temporal** | Workflow orchestration | Namespace/task queue | ❌ | ✅ Activities | ✅ Replay from history | ❌ | ❌ | ✅ |
| **Prefect** | Data/ML orchestration | ❌ | ❌ | ✅ Tasks | ❌ | ❌ Web UI | ❌ | ✅ |
| **Argo Workflows** | Kubernetes workflow engine | RBAC | ❌ | ✅ DAG | ❌ | ❌ Web UI | ❌ | ✅ |
| **Shoreline** | Incident automation | Policy-based | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ Proprietary |
| **Transposit** (now ServiceNow) | Runbook automation | Approval gates | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ Proprietary |
| **Rootly / Firehydrant** | Incident management | Workflow approvals | ❌ | ❌ | ❌ | ❌ | ✅ Slack-native | ❌ Proprietary |

### Where gert is strongest

**1. Governance derived from declared behavior, not identity (unique)**

This is the core thesis. Most systems govern *who* runs, *where* it runs, or *what command name* is invoked. gert governs **what the step does** — its effects, its writes, its idempotency, its determinism. Risk emerges from behavior, not from ACLs or command allowlists.

No competitor does this. Rundeck has ACLs (who). Ansible has `become` (privilege). Temporal has task queues (where). gert has contracts (what). This means governance scales with the runbook — add a dangerous step, governance automatically escalates. No admin intervention.

**2. Deterministic replay + scenario testing (rare)**

Temporal has replay from event history, but it's for debugging distributed workflows, not for runbook validation. gert's scenario testing — canned tool responses + declarative assertions on outcomes, step visits, and outputs — has no equivalent in the runbook automation space. Ansible's `check mode` is the closest, but it doesn't support assertions or multiple scenarios per playbook.

**3. Structured outcomes (unique)**

Every gert run produces a categorized outcome (resolved/escalated/no_action/needs_rca) with domain-specific codes and metadata. Competitors produce exit codes or unstructured logs. This enables dashboards, trend analysis, and governance policies keyed on outcomes.

**4. YAML-native, single-file runbooks**

Unlike Temporal (requires writing Go/Python/Java workers), Argo (Kubernetes CRDs), or Ansible (inventory + playbook + roles), gert runbooks are self-contained YAML files with inline governance. Lower barrier to entry for SRE teams.

**5. Append-only audit trace**

gert's JSONL trace records every decision — contract evaluation, governance decision, branch taken, approval received. Most competitors log execution but don't produce a structured, verifiable audit trail with 14 typed events.

### Where gert is weakest

**1. No distributed orchestration**

Temporal, Argo Workflows, and Prefect handle distributed execution across workers/pods. gert runs on a single machine. This is by design (kernel boundary), but it means gert can't orchestrate multi-machine deployments natively. Mitigation: gert can call tools that trigger remote operations (kubectl, az, ssh), and distributed orchestration is explicitly outside the kernel.

**2. No built-in scheduling**

Rundeck is a job scheduler first. Ansible has Tower/AWX. gert has no cron, no queue, no recurring runs. Mitigation: host platform provides scheduling (Azure Pipelines, GitHub Actions, cron, Kubernetes CronJobs). gert is invoked, not scheduled.

**3. No web UI (yet)**

Rundeck, Argo, and Prefect ship with web dashboards. gert has CLI + TUI (planned) + VS Code (planned). No standalone web UI for non-developers. Mitigation: the MCP server could back a web frontend, but it's not planned in the ecosystem doc.

**4. Small ecosystem / no marketplace**

Ansible has Galaxy (thousands of roles). Rundeck has plugins. gert has a handful of example tools. Mitigation: tool definitions are trivial YAML files wrapping existing binaries — the barrier to contribution is low. But discovery and sharing aren't solved.

**5. No native secrets management**

gert explicitly delegates secrets to the host platform. Competitors like Rundeck have built-in key storage. Ansible has Vault integration. gert reads environment variables. Fine for Kubernetes (secrets mounted as env vars), but missing for standalone use cases.

### Unique positioning

gert occupies a space no competitor fills: **governed, contract-driven runbook execution with deterministic testing**. The closest pairing would be Temporal (replay) + Rundeck (governance) + Ansible (YAML playbooks) — but that's three products, none of which share data models.

The AI-agent integration via MCP (Phase D) is forward-looking — no runbook tool today exposes operations as MCP tools. This positions gert as the execution backend for AI-driven incident response, where agents need governed, auditable operations with human-in-the-loop approval.

### Strategic gaps to address

| Gap | Priority | Mitigation |
|-----|----------|------------|
| No web UI | Medium | MCP server could back a lightweight web frontend (future phase) |
| No tool marketplace | Low | GitHub-based tool sharing + `gert-project.yaml` dependency resolution |
| No secrets integration | Medium | Add `secrets:` section to tool definitions that references env vars or external secret stores by convention |
| Single-machine execution | Low | By design — gert orchestrates remote operations via tools, not local processes |
| No Windows-native tool library | Low | Most SRE tools (curl, kubectl, az) are cross-platform; platform field handles the rest |
