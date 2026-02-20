# gert — Governed Executable Runbook Engine

[![Go](https://img.shields.io/badge/Go-1.23+-blue.svg)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

A platform for governed, executable, debuggable runbooks with traceability and evidence capture.

## Features

- **Validate** runbook YAML against a strict JSON Schema (Draft 2020-12)
- **Execute** runbooks in real mode with CLI and manual steps
- **Debug** interactively with a step-through REPL
- **Replay** executions deterministically with pre-recorded scenarios
- **Dry-run** preview what a runbook will do without side effects
- **Compile** Markdown TSGs into schema-valid runbooks
- **Governance** enforcement: command allowlists/denylists, env var blocking, output redaction
- **Evidence capture**: text, checklists, attachments with SHA256 hashing
- **Traceability**: append-only JSONL traces, per-step state snapshots, run resumption

## Installation

```bash
go install github.com/ormasoftchile/gert/cmd/gert@latest
```

Or build from source:

```bash
git clone https://github.com/ormasoftchile/gert.git
cd gert
go build -o bin/gert ./cmd/gert
```

## Quick Start

### 1. Validate a runbook

```bash
gert validate runbook.yaml
```

### 2. Execute a runbook

```bash
# Real execution
gert exec runbook.yaml --as engineer@company.com

# Dry-run (no side effects)
gert exec runbook.yaml --mode dry-run

# Replay with pre-recorded scenario
gert exec runbook.yaml --mode replay --scenario scenario.yaml
```

### 3. Debug interactively

```bash
gert debug runbook.yaml --as engineer@company.com
```

Available debugger commands: `next`, `continue`, `print vars`, `print captures`, `history`, `evidence set/check/attach`, `approve`, `snapshot`, `dump`, `help`, `quit`

### 4. Compile a TSG

```bash
gert compile guide.md --out runbook.yaml --mapping mapping.md
```

### 5. Export JSON Schema

```bash
gert schema export > runbook-v0.json
```

## Runbook Format

```yaml
apiVersion: runbook/v0
meta:
  name: pod-crashloop-investigation
  description: Investigate CrashLoopBackOff pods
  vars:
    namespace: prod
    service: api-gateway
  defaults:
    timeout: "120s"
  governance:
    allowed_commands: [kubectl, az, curl]
    deny_env_vars: ["SECRET_*", "TOKEN"]
    redact:
      - pattern: "(?i)password\\s*[:=]\\s*\\S+"
        replace: "password: <redacted>"

steps:
  - id: check_pods
    type: cli
    title: Check pod status
    with:
      argv: ["kubectl", "get", "pods", "-n", "{{ .namespace }}"]
    capture:
      pods: stdout
    assertions:
      - not_contains: "CrashLoopBackOff"

  - id: validate_dashboard
    type: manual
    title: Validate monitoring dashboard
    instructions: |
      Check Grafana dashboard for {{ .service }}.
    required_evidence:
      - kind: checklist
        name: dashboard_check
        items: ["Error rate < 1%", "P99 < 500ms"]
    approvals:
      min: 1
      roles: ["DRI"]
```

## Architecture

```
cmd/gert/          CLI entry point (Cobra)
pkg/
  schema/          YAML parsing, JSON Schema validation, domain rules
  runtime/         Execution engine, trace writer, snapshots, resume
  governance/      Command allowlist/denylist, redaction, env blocking
  assertions/      7 assertion evaluators
  evidence/        Evidence types, SHA256 hashing
  providers/       CommandExecutor, EvidenceCollector implementations
  debugger/        Interactive REPL debugger
  replay/          ReplayExecutor, scenario parsing
  compiler/        TSG-to-runbook compilation
schemas/           JSON Schema (Draft 2020-12)
vscode/            VS Code extension (TypeScript)
testdata/          Test fixtures
```

## Testing

```bash
go test ./... -v
```

## License

MIT
