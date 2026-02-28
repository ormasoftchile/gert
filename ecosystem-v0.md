# Gert Ecosystem v0 — Design Document

> **Everything outside the kernel that makes gert useful.**

**Date**: 2026-02-28
**Status**: Draft
**Depends on**: `kernel-v0.md` (all kernel packages assumed stable)

---

## 1. Overview

The kernel executes runbooks. The ecosystem makes runbooks usable by humans, AI agents, and organizations. This document defines the ecosystem components, their dependencies, and the interfaces they use to extend the kernel.

### Build Order

```
Phase A: Real Runbooks + Tools         ← validate the kernel works
Phase B: Approval Provider Interface   ← kernel extension for governance UX
Phase C: TUI                           ← interactive experience (separate binary)
Phase D: MCP Server                    ← AI-agent-facing interface (separate binary)
Phase E: VS Code Extension             ← richest human experience
Phase F: Input Providers               ← pluggable input resolution
```

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

**Install what you need:**

```bash
go install github.com/ormasoftchile/gert/cmd/gert@latest       # everyone
go install github.com/ormasoftchile/gert/cmd/gert-tui@latest    # operators
go install github.com/ormasoftchile/gert/cmd/gert-mcp@latest    # AI agents
```

**Why separate binaries:**

- Core CLI stays scriptable — pipes, `--json`, `jq`, CI-friendly, no terminal dependencies
- TUI pulls Bubble Tea + lipgloss + glamour — unnecessary for automation
- MCP server has its own SDK dependency — unnecessary for humans
- Follows industry pattern: `kubectl` (core) + `k9s` (TUI), `git` (core) + `lazygit` (TUI), `docker` (core) + `lazydocker` (TUI)
- Same kernel underneath, different front-ends

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
| `curl.tool.yaml` | curl | all | `side_effects: false`, `reads: [network]` |
| `kubectl.tool.yaml` | kubectl | all | Multiple actions: `get` (read-only), `delete` (side_effects, writes: [kubernetes]), `apply` (idempotent) |
| `az.tool.yaml` | az | all | Azure CLI actions: `vm list`, `vm restart` |
| `ping.tool.yaml` | ping | all | `side_effects: false`, `deterministic: false` |
| `jq.tool.yaml` | jq | all | `side_effects: false`, `deterministic: true` — JSON transformation |

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

## 3. Phase B — Approval Provider Interface

### Problem

The kernel's `requestApproval()` currently reads from stdin. This blocks adoption for any non-terminal use case. Approvals need to route to Teams, Slack, PagerDuty, email, or any async system — without the kernel knowing about any of them.

### Design

#### Kernel interface (added to engine)

```go
// ApprovalProvider routes approval requests to external systems.
type ApprovalProvider interface {
    RequestApproval(req ApprovalRequest) (*ApprovalResponse, error)
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
}

type ApprovalResponse struct {
    Approved   bool      `json:"approved"`
    ApproverID string    `json:"approver_id"`
    Method     string    `json:"method"`
    Timestamp  time.Time `json:"timestamp"`
    Signature  string    `json:"signature,omitempty"`
    Reason     string    `json:"reason,omitempty"`
}
```

#### Engine changes

- Add `ApprovalProvider` to `RunConfig` (like `ToolExecutor`)
- Default implementation: `stdinApprovalProvider` (current behavior)
- `requestApproval()` delegates to the provider instead of reading stdin
- Trace records the full `ApprovalResponse` in `governance_decision` events

#### Ecosystem implementations

| Provider | Transport | How it works |
|----------|-----------|-------------|
| **stdin** | Terminal | Current behavior — prompt and read y/n (default) |
| **Teams** | HTTP webhook + callback | POST adaptive card, block until webhook callback |
| **Slack** | HTTP API + events | Post message with approve/reject buttons, listen for interaction |
| **HTTP callback** | HTTP POST + poll | POST approval request to a URL, poll/callback for response |
| **API store** | Database/queue | Write request to store, poll until responded; for async workflows |
| **Signed URL** | HTTP | Send approver a time-limited HMAC-signed URL; click = approve |

#### Approval context flow

```
Runbook YAML                    Engine                 ApprovalProvider           External System
     │                            │                          │                         │
     │  (step with critical risk) │                          │                         │
     │                            │── resolve contract ──→   │                         │
     │                            │── evaluate governance ─→ │                         │
     │                            │   = require-approval     │                         │
     │                            │                          │                         │
     │                            │── RequestApproval({      │                         │
     │                            │     runbook, step,       │                         │
     │                            │     risk, contract,      │                         │
     │                            │     inputs, extensions   │                         │
     │                            │   }) ──────────────────→ │── render card ────────→ │
     │                            │   (blocks)               │                         │
     │                            │                          │                         │  user approves
     │                            │                          │←── callback ────────────│
     │                            │←── ApprovalResponse ─────│                         │
     │                            │                          │                         │
     │                            │── trace: governance_decision { approved, approver, signature }
     │                            │── execute step
```

#### Adaptive card example (Teams)

```
┌──────────────────────────────────────────────────┐
│ ⚠️  gert — Approval Required                     │
│                                                   │
│ Runbook:  service-health-check                    │
│ Step:     delete_production_pod                    │
│ Risk:     CRITICAL                                │
│                                                   │
│ Contract:                                         │
│   side_effects: true                              │
│   idempotent: false                               │
│   writes: [production, kubernetes]                │
│                                                   │
│ Action:                                           │
│   kubectl delete pod/web-api-7f8b9 -n production  │
│                                                   │
│ Requested by: oncall@company.com                  │
│ Run ID:       run-2026-02-28-001                  │
│ Team:         platform-eng (from x-team)          │
│                                                   │
│       [ ✅ Approve ]      [ ❌ Reject ]            │
└──────────────────────────────────────────────────┘
```

#### Cryptographic signing (optional, high-trust environments)

For SOC2/FedRAMP scenarios, the approval response includes a signature:

- Provider generates an HMAC-SHA256 over `run_id + step_id + approved + timestamp` using a shared secret
- The trace records the signature alongside the decision
- Audit tooling can verify signatures independently
- The kernel does not verify — it records. Verification is an ecosystem concern.

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
    │  MCP protocol (stdio) → cmd/gert-mcp
    │  (same binary AI agents use)
    │
    ▼
gert-mcp (Go)
    │
    └── kernel packages
```

### Backend

**MCP-first.** The extension uses `gert-mcp` as its backend — the same binary AI agents use. This means:
- One server implementation serves both VS Code and AI agents
- VS Code gets agent-compatible by default
- For VS Code-specific features (diagnostics push on file change, webview state sync), add a thin JSON-RPC layer alongside MCP — or use MCP notifications if the SDK supports them

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
  side_effects: false
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

## 9. Summary — What the Kernel Needs

| Interface | Kernel change | Phase |
|-----------|---------------|-------|
| `ApprovalProvider` | New interface in engine, replaces stdin prompt | B |
| `InputProvider` | Pre-engine step in CLI/MCP server, not in engine | F |
| MCP transport | New executor in `pkg/kernel/executor/` | After F |

Phases B and "after F" modify kernel packages. All other phases are ecosystem-only — they import kernel packages but never modify them.

### CLI naming

- `cmd/gert-kernel/` (current) becomes the **minimal reference binary** — useful for testing and embedding.
- `cmd/gert/` (new, Phase A) becomes the **standard distribution** — replaces the old broken `cmd/gert/` with one that imports only kernel packages.
- The old `cmd/gert/` (pre-kernel) is deleted when `cmd/gert/` is rebuilt on the kernel.

### Approval timeout

All `ApprovalProvider` implementations must respect a timeout. The `ApprovalRequest` should include a `Timeout time.Duration` field. If the provider doesn't respond within the timeout, the engine treats it as a rejection. Default: 5 minutes for interactive, 30 minutes for async (Teams/Slack).

### Context propagation

All provider interfaces (`ApprovalProvider`, `InputProvider`, `ToolExecutor`) should accept `context.Context` as their first parameter for cancellation and timeout propagation. This is a kernel-level decision to make before Phase B.

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
