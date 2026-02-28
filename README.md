# gert — Governed Executable Runbook Engine

[![Go](https://img.shields.io/badge/Go-1.25+-blue.svg)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

A lean execution kernel for structured, governed, replayable runbooks with full traceability and safe extensibility.

## Features

- **Contract-driven governance** — risk classification from behavioral contracts (side_effects, deterministic, idempotent), not command names
- **7 step types** — tool, manual, assert, branch, parallel, end, extension
- **3-phase validation** — structural (strict YAML) → semantic → domain (21 rules including variable resolution, end-step reachability, contract tightening)
- **Parallel execution** — concurrent branches with state isolation and contract-based conflict detection
- **Structured outcomes** — every run ends with a categorized outcome (resolved / escalated / no_action / needs_rca)
- **Append-only JSONL trace** — 14 event types for full auditability
- **Scenario replay testing** — deterministic re-execution with canned tool responses and declarative assertions
- **Cross-platform** — stdout normalization, `meta.platform` constraints, `meta.binary` resolution
- **Tool definitions** (`.tool.yaml`) — typed actions over stdio with contracts (reads/writes, side_effects, idempotent)
- **Constants & `inputs_from`** — DRY input spreading from object-valued constants

## Installation

```bash
go install github.com/ormasoftchile/gert/cmd/gert-kernel@latest
```

Or build from source:

```bash
git clone https://github.com/ormasoftchile/gert.git
cd gert
go build -o gert-kernel ./cmd/gert-kernel
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `gert validate <file>` | 3-phase validation (runbook or tool definition). Exit 0/1. |
| `gert exec <file>` | Execute a runbook. Produce trace + structured outcome. |
| `gert test <file...>` | Run scenario replay tests with assertions. |
| `gert schema runbook` | Export kernel/v0 runbook JSON Schema (Draft 2020-12). |
| `gert schema tool` | Export tool/v0 JSON Schema. |
| `gert version` | Print version info. |

### Execution

```bash
# Real execution with variables
gert exec runbook.yaml --var hostname=srv1.example.com

# Dry-run — shows resolved inputs, contracts, governance per step
gert exec runbook.yaml --mode dry-run --var hostname=srv1.example.com

# With JSONL trace output
gert exec runbook.yaml --var hostname=srv1.example.com --trace run.jsonl
```

### Validation

```bash
# Validate a runbook (auto-detects tool definitions by apiVersion)
gert validate examples/service-health-check.yaml
gert validate examples/tools/health-check.tool.yaml
```

### Scenario Testing

```bash
# Run all scenarios for a runbook
gert test examples/service-health-check.yaml

# Single scenario, JSON output, fail-fast
gert test examples/service-health-check.yaml --scenario healthy --json --fail-fast
```

Scenarios are discovered by convention at `scenarios/<runbook-name>/*/`:

```
examples/
├── service-health-check.yaml
├── tools/
│   ├── health-check.tool.yaml
│   └── restart-service.tool.yaml
└── scenarios/
    └── service-health-check/
        ├── healthy/
        │   ├── scenario.yaml      # inputs + canned tool responses
        │   └── test.yaml          # assertions
        ├── degraded/
        │   ├── scenario.yaml
        │   └── test.yaml
        └── unknown/
            ├── scenario.yaml
            └── test.yaml
```

Test specs are declarative — all fields optional:

```yaml
description: "Service is up — should take no action"
expected_status: completed
expected_outcome: no_action
expected_code: service_healthy
must_reach: [check_health, evaluate, healthy_end]
must_not_reach: [restart, investigate]
expected_outputs:
  status_code: "200"
```

## Runbook Format

```yaml
apiVersion: kernel/v0

meta:
  name: service-health-check
  description: Diagnose and remediate service health issues
  inputs:
    hostname: { type: string, required: true }
  constants:
    health_endpoint: "/healthz"
    max_retries: 2
  governance:
    rules:
      - risk: critical
        action: require-approval
      - default: allow

tools:
  - health-check
  - restart-service

steps:
  - id: check_health
    type: tool
    tool: health-check
    action: check
    inputs:
      url: "https://{{ .hostname }}{{ .health_endpoint }}"

  - id: evaluate
    type: assert
    continue_on_fail: true
    assert:
      - type: equals
        value: "{{ .status_code }}"
        expected: "200"

  - id: triage
    type: branch
    branches:
      - condition: '{{ eq .status_code "200" }}'
        label: healthy
        steps:
          - type: end
            outcome:
              category: no_action
              code: service_healthy
      - condition: default
        label: unknown
        steps:
          - type: end
            outcome:
              category: escalated
              code: unknown_failure
```

### Step Types

| Type | Purpose |
|------|---------|
| `tool` | Execute a tool action (stdio transport) |
| `manual` | Human evidence collection + approvals |
| `assert` | First-class assertion evaluation |
| `branch` | Conditional flow fork — multiple paths, one executes |
| `parallel` | Concurrent execution with state isolation |
| `end` | Structured outcome declaration |
| `extension` | Escape hatch for custom behavior with declared contract |

### Flow Control

| Mechanism | Purpose |
|-----------|---------|
| `when` | Step-level guard — run or skip |
| `branch` | Flow-level fork — exactly one arm executes |
| `next` | Constrained goto — forward always, backward bounded (`max`) |
| `for_each` | List iteration — sequential or parallel |

### Tool Definitions

```yaml
apiVersion: tool/v0
meta:
  name: health-check
  transport: stdio
  binary: curl
  platform: [linux, darwin, windows]  # optional OS constraint
contract:
  inputs:
    url: { type: string, required: true }
  outputs:
    status_code: { type: string }
  side_effects: false
  deterministic: true
  idempotent: true
  reads: [network]
  writes: []
actions:
  check:
    argv: ["curl", "-s", "-o", "/dev/null", "-w", "%{http_code}", "{{ .url }}"]
    extract:
      status_code: { from: stdout, pattern: "^(\\d+)$" }
```

## Architecture

```
cmd/gert-kernel/       Kernel CLI (validate, exec, test, schema, version)

pkg/kernel/
  contract/            Contract model (risk, tightening, conflicts, merge)
  schema/              Runbook + Tool types, YAML loader, JSON Schema export
  validate/            3-phase validation pipeline (21 domain rules)
  engine/              Sequential + parallel execution engine
  eval/                Go text/template expression evaluator
  executor/            Tool dispatch (stdio transport, binary resolution)
  governance/          Contract-driven policy engine
  trace/               Append-only JSONL audit trail (14 event types)
  replay/              Scenario-based replay with canned responses
  testing/             Test harness (spec, assertions, runner)

examples/
  service-health-check.yaml     Example runbook
  tools/                        Tool definitions
  scenarios/                    Test scenarios (healthy, degraded, unknown)
```

### Kernel Boundary

The kernel is responsible for: executing steps, enforcing governance, maintaining state, producing traces, and validating structure.

Everything else is **ecosystem** — debugger, TUI, JSON-RPC server, diagram generation, VS Code extension, TSG compilation. These import kernel packages but live outside the kernel.

## Testing

```bash
# Run all kernel tests (72 tests across 9 packages)
go test ./pkg/kernel/... -v

# Run scenario replay tests for the example runbook
./gert-kernel test examples/service-health-check.yaml
```

## Design

See [kernel-v0.md](kernel-v0.md) for the full design document covering:
- Contract model and governance
- 7 step types with rationale
- Flow control (when, branch, next, for_each)
- Variable model (inputs, constants, `inputs_from`)
- Parallel execution with contract-based conflict detection
- Structured outcomes
- Error model
- Trace event types
- Platform awareness

## License

MIT
