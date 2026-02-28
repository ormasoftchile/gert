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
  effects: [network]                    # what systems does this touch?
  writes: [service]                     # what domain resources does this mutate?
  reads: [network]                      # what domain resources does this observe?
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
| Effects but no writes (read-only network calls) | Low |
| Effects + writes + idempotent | Medium |
| Effects + writes + not idempotent + deterministic | High |
| Effects + writes + not idempotent + not deterministic | Critical |

### Migration

`side_effects: true/false` remains supported as shorthand. If `effects` is present, it takes precedence. If only `side_effects` is declared, the kernel infers `effects: [unknown]` for `true` and `effects: []` for `false`.

### Schema change

- Add `Effects []string` to `contract.Contract`
- `SideEffects` becomes computed from `Effects + Writes` (or stays as explicit override for backward compat)
- Governance rules gain `Effects` matching alongside `Reads`/`Writes`
- Validation rule: warn if `side_effects: false` but `effects` is non-empty

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

- Provider generates HMAC-SHA256 over `ticket_id + approved + timestamp` using a shared secret
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
- Inputs are resolved **before** engine execution starts (pre-processing)
- Resolved values are passed as `RunConfig.Vars` — the engine never sees `from:` bindings
- The trace `run_start` event records resolved inputs + their source (`cli`, `provider/cmdb`, `prompt`, `default`)
- Provider failures are recorded in trace as `input_resolution` events

#### Interface (kernel package, ecosystem implementations)

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

---

## 6. Phase B → Track 1 remainder (Secrets, Extension Runner, Trace Integrity, Contract Violations)

_Sections 9–12 below cover the remaining Track 1 items (secrets §9.1, contract violations §9.2, probes §9.3, trace integrity §12). Phase numbering retained for continuity._

---

## 4. Phase C — Terminal UI (TUI)

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

## 5. Phase D — MCP Server

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

## 6. Phase E — VS Code Extension

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

## 7. Phase F — Input Providers

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

Input resolution happens **before** engine execution:
1. Parse runbook → extract `from:` bindings that reference providers
2. Group bindings by provider
3. Call each provider's `Resolve()` method
4. Merge resolved values into `RunConfig.Vars`
5. Start engine execution

The kernel engine itself doesn't know about providers — it receives pre-resolved vars. Provider resolution is a pre-processing step in the CLI or MCP server.

---

## 8. Cross-Cutting: Tool as MCP Consumer

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
- MCP server lifecycle: spawn on first use, reuse for subsequent calls, shutdown on engine completion

### Priority

Lower than Phases A–F. Can be added when AI agent integration demands it.

---

## 9. Phase G — Secrets + Contract Hardening

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

The engine records violations in the trace but **does not halt** — the author may have legitimate reasons. Repeated violations across multiple runs signal a bad contract, surfaced via outcome aggregation (Phase I).

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
- Executes tool steps for **read-only tools only** (`side_effects: false`)
- Skips tools with `side_effects: true` (reports contract + governance as dry-run does)
- For executed tools: applies contract violation detection (§9.2)
- For `deterministic: true` tools: runs twice with same inputs, verifies output consistency

This lets you validate contracts against real infrastructure without causing side effects.

---

## 10. Phase H — Replay Excellence

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

## 11. Phase I — Outcome Intelligence

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

## 12. Phase J — Trace Integrity

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

## 13. Phase K — Watch Mode

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

## 14. Summary — What the Kernel Needs

### Track 1 — Kernel changes (must do first)

| Change | Kernel package | Track |
|--------|---------------|-------|
| Contract taxonomy (`effects` field) | contract, schema, validate, governance | 1b |
| `ApprovalProvider` (resumable, ticket-based) | engine, trace | 1c |
| `InputResolver` interface + resolution semantics | schema, engine | 1d |
| `SecretRef` in schema | schema, validate | 1e |
| Extension step runner | engine, executor | 1f |
| `prev_hash` in trace events | trace | 1g |
| Trace signing + verification policy | trace, governance | 1g |
| Contract violation detection | engine, trace | 1h |
| Probe mode | engine | 1h |
| `context.Context` on all provider interfaces | engine | 1b (prerequisite) |

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

---

## 10. Competitive Analysis

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

**1. Contract-driven governance (unique)**

No competitor has behavioral contracts on steps. Rundeck has ACLs. Ansible has `become`. Temporal has task queues. None derive risk classification from `side_effects × deterministic × idempotent` and auto-escalate to approval gates. This is gert's fundamental differentiator — governance emerges from declared behavior, not from command-name allowlists.

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
