# gert — Governed Executable Runbook Engine

[![Go](https://img.shields.io/badge/Go-1.25+-blue.svg)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

A lean execution kernel for structured, governed, replayable runbooks with full traceability, safe extensibility, and multiple host surfaces (CLI, TUI, MCP server).

## Features

### Kernel (Track 1)

- **Effects-based governance** — `effects: [network, kubernetes]` + `writes: [pods]` taxonomy replaces boolean `side_effects`. Risk derived from effects + writes + idempotency + determinism. Governance rules match on contract properties, not tool names.
- **7 step types** — tool, manual, assert, branch, parallel, end, extension
- **Resumable approval** — `ApprovalProvider` with `Submit()/Wait()`, ticket-based flow, state persistence for async resume, multi-approver enforcement, signature verification
- **Scoped state** — `scope` for variable namespace isolation, `export` to promote outputs to global, `visibility` with allow/deny globs
- **Keyed fan-out** — `for_each.key` produces map-structured outputs instead of lists
- **Repeat blocks** — bounded multi-step iteration with `max` + `until` early-exit
- **Input resolution** — kernel-owned `ResolveInputs()` API with resolution order: CLI vars → providers → defaults → error. All hosts use the same path.
- **Trace integrity** — SHA-256 hash chain on every event, HMAC signing on `run_complete`, `gert trace verify` detects tampering
- **Contract violation detection** — undeclared outputs stripped, missing outputs warned, `contract_violations: deny` governance rule promotes to errors
- **Probe mode** — `--mode probe` executes read-only tools, skips writes
- **Extension runner** — JSON-RPC 2.0 over stdio (initialize/execute/shutdown)
- **Secret redaction** — declared secrets auto-redacted from traces and recorded scenarios
- **Run identity** — `run_start` includes actor, host, gert_version, runbook_hash, tool_hashes
- **3-phase validation** — structural (strict YAML) → semantic (JSON Schema) → domain (25+ rules including variable resolution, end-step reachability, contract tightening, scope/export/repeat validation)
- **Cross-platform** — Windows + Linux + macOS. Tool definitions declare `meta.platform` constraints.
- **131 tests** across 15 packages, 11 scenario replay tests across 5 runbooks

### Ecosystem (Track 2)

- **gert CLI** — exec, validate, test, resume, trace verify, watch, diff, outcomes
- **gert-tui** — Bubble Tea terminal UI with step list, status icons, real-time trace events, replay mode
- **gert-mcp** — MCP server exposing gert/validate, gert/exec, gert/test, gert/schema as tools for AI agents
- **Auto-record** — `Recorder` wrapping `ToolExecutor` captures responses into replayable scenarios with secret redaction

## Quick Start

```bash
# Build all binaries
go build -o gert ./cmd/gert
go build -o gert-tui ./cmd/gert-tui
go build -o gert-mcp ./cmd/gert-mcp

# Validate a runbook
./gert validate runbooks/service-health-diagnostic.yaml

# Execute against a real host
./gert exec runbooks/service-health-diagnostic.yaml --var hostname=google.com

# Dry-run (shows contracts, governance, inputs — no tools execute)
./gert exec runbooks/service-health-diagnostic.yaml --var hostname=google.com --mode dry-run

# With trace output + actor identity
./gert exec runbooks/service-health-diagnostic.yaml --var hostname=google.com --trace run.jsonl --as oncall@team.com

# Verify trace integrity
./gert trace verify run.jsonl

# Run scenario replay tests
./gert test runbooks/service-health-diagnostic.yaml

# TUI — real execution
./gert-tui runbooks/service-health-diagnostic.yaml --mode real --var hostname=google.com

# TUI — replay mode (canned responses, no tools needed)
./gert-tui runbooks/service-health-diagnostic.yaml --mode replay --scenario healthy

# Watch mode — repeat on interval
./gert watch runbooks/service-health-diagnostic.yaml --interval 30s --var hostname=google.com --stop-on escalated

# MCP server (for AI agents)
./gert-mcp
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `gert validate <file>` | 3-phase validation (runbook or tool). Auto-detects by `apiVersion`. |
| `gert exec <file>` | Execute a runbook. `--mode real\|dry-run\|probe`. `--var`, `--trace`, `--as`. |
| `gert test <file...>` | Run scenario replay tests. `--scenario`, `--json`, `--fail-fast`. |
| `gert resume --run <id>` | Resume a paused run from persisted state. |
| `gert trace verify <file>` | Verify hash chain integrity + optional HMAC signature. |
| `gert watch <file>` | Repeat execution on interval. `--interval`, `--stop-on`, `--var`. |
| `gert diff <file>` | Re-run scenarios and report outcome changes. |
| `gert outcomes` | Aggregate outcomes from trace files. `--json`. |
| `gert schema runbook\|tool` | Export JSON Schema (Draft 2020-12). |
| `gert version` | Print version info. |

## Runbooks

5 runbooks included with 11 scenario tests:

| Runbook | Description | Scenarios |
|---------|-------------|-----------|
| `service-health-diagnostic` | Ping + HTTP check with branching outcomes | healthy, degraded, unknown |
| `dns-http-chain` | DNS resolution → HTTP check with variable threading | reachable, unreachable |
| `k8s-pod-restart` | Kubernetes pod restart with approval gate | restart-success, not-running |
| `incident-triage` | Multi-branch severity routing with manual investigation | p1-critical, p3-low |
| `multi-endpoint-sweep` | Endpoint health check with branching | all-healthy, some-degraded |

## Tool Packs

6 tool definitions with effects-based contracts:

| Tool | Effects | Description |
|------|---------|-------------|
| `curl` | `[network]` | HTTP GET/POST/HEAD/download |
| `ping` | `[network]` | ICMP reachability check |
| `nslookup` | `[network]` | DNS resolution |
| `kubectl` | `[kubernetes]` | K8s get/delete/apply (writes: pods for delete) |
| `az` | `[azure]` | Azure VM list/restart |
| `jq` | `[]` | JSON processing (pure, no effects) |

## Runbook Format

```yaml
apiVersion: kernel/v0

meta:
  name: service-health-diagnostic
  inputs:
    hostname: { type: string, required: true }
  constants:
    health_path: "/"
  governance:
    rules:
      - effects: [network]
        action: allow
      - effects: [kubernetes]
        writes: [pods]
        action: require-approval
      - default: allow
  secrets:
    - env: SERVICE_AUTH_TOKEN
      required: false

tools:
  - curl
  - ping

steps:
  - id: check
    type: tool
    tool: curl
    action: get
    inputs:
      url: "https://{{ .hostname }}{{ .health_path }}"

  - id: triage
    type: branch
    branches:
      - condition: '{{ eq .status_code "200" }}'
        label: healthy
        steps:
          - type: end
            outcome: { category: no_action, code: service_healthy }
      - condition: default
        label: unknown
        steps:
          - type: end
            outcome: { category: escalated, code: unknown_status }
```

### Step Types

| Type | Purpose |
|------|---------|
| `tool` | Execute a tool action (stdio transport) |
| `manual` | Human evidence collection |
| `assert` | First-class assertion evaluation |
| `branch` | Conditional flow fork — one arm executes |
| `parallel` | Concurrent execution with state isolation |
| `end` | Structured outcome declaration |
| `extension` | Custom behavior via JSON-RPC runner with declared contract |

### Flow Control

| Mechanism | Purpose |
|-----------|---------|
| `when` | Step-level guard — run or skip |
| `branch` | Flow-level fork — one arm executes |
| `next` | Constrained goto — forward always, backward bounded (`max`) |
| `for_each` | List iteration — sequential or parallel, optional `key` for maps |
| `repeat` | Bounded multi-step loop with `max` + `until` |
| `scope` | Variable namespace isolation |
| `export` | Promote scope-local outputs to global |
| `visibility` | Allow/deny glob patterns on variable access |

## Architecture

```
cmd/
  gert/                Core CLI (exec, validate, test, resume, watch, diff, ...)
  gert-tui/            Bubble Tea terminal UI
  gert-mcp/            MCP server for AI agents

pkg/kernel/            Kernel packages (10 packages)
  contract/            Contract model (risk, effects, tightening, conflicts, merge)
  schema/              Runbook + Tool types, YAML loader, scope normalization, JSON Schema
  validate/            3-phase validation pipeline (25+ domain rules)
  engine/              Sequential + parallel execution, scoped state, repeat, probe mode
  eval/                Go text/template expression evaluator
  executor/            Tool dispatch (stdio) + extension runner (JSON-RPC)
  governance/          Contract-driven policy engine (effects + writes matching)
  trace/               Append-only JSONL audit trail (22 event types) + hash chain verification
  replay/              Scenario-based replay with canned responses
  testing/             Test harness (spec, assertions, runner)

pkg/ecosystem/         Ecosystem packages (4 packages)
  tui/                 Bubble Tea model + engine integration
  mcp/                 MCP server handlers + tool registration
  recorder/            Auto-record tool responses with secret redaction
  approval/stdin/      Default stdin approval provider

tools/                 6 tool packs (curl, ping, nslookup, kubectl, az, jq)
runbooks/              5 runbooks with scenarios/ test directories
examples/              Original kernel example (service-health-check)
```

### Kernel Boundary

The kernel (`pkg/kernel/`) is the single source of execution semantics. Ecosystem packages (`pkg/ecosystem/`) consume kernel interfaces but never modify kernel behavior. Dependency arrow: ecosystem → kernel, never reversed.

## Testing

```bash
# Run all tests (131 tests across 15 packages)
go test ./pkg/kernel/... ./pkg/ecosystem/... ./cmd/gert/ -v

# Run scenario replay tests for all runbooks
./gert test runbooks/service-health-diagnostic.yaml
./gert test runbooks/dns-http-chain.yaml
./gert test runbooks/k8s-pod-restart.yaml
./gert test runbooks/incident-triage.yaml
./gert test runbooks/multi-endpoint-sweep.yaml

# Validate all tools
./gert validate tools/curl.tool.yaml
./gert validate tools/kubectl.tool.yaml
```

## Design

- [kernel-v0.md](kernel-v0.md) — kernel design (contract model, step types, flow control, variable model, trace events)
- [specs/002-ecosystem-v0/](specs/002-ecosystem-v0/) — ecosystem specification, plan, data model, contracts, tasks

## License

MIT
