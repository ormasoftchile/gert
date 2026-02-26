# gert — Governed Executable Runbook Engine

[![Go](https://img.shields.io/badge/Go-1.25+-blue.svg)](https://go.dev)
[![License](https://img.shields.io/badge/license-MIT-green.svg)](LICENSE)

A platform for governed, executable, debuggable runbooks with full traceability, evidence capture, and pluggable tool/provider integrations.

## Features

- **Validate** runbook YAML against a strict JSON Schema (Draft 2020-12)
- **Execute** runbooks in real, replay, or dry-run mode with CLI, manual, tool, and invoke steps
- **Debug** interactively with a step-through REPL
- **TUI** — full terminal UI (Bubble Tea) with step list, output, and status panels; no VS Code required
- **Diagram** — generate Mermaid flowcharts or ASCII diagrams from any runbook
- **Replay** executions deterministically with pre-recorded scenarios
- **Test** runbooks with scenario replay tests and rich assertions (`gert test`)
- **Migrate** runbooks from v0 to v1 (`gert migrate`)
- **Serve** — JSON-RPC 2.0 server powering the VS Code extension and other clients
- **Tool definitions** (`.tool.yaml`): typed actions over stdio, JSON-RPC, or MCP transports
- **Input providers** (`.provider.yaml`): pluggable resolution of `from:` bindings (incident management, PagerDuty, etc.)
- **Governance** enforcement: command allowlists/denylists, env var blocking, output redaction, per-action approval gates
- **Evidence capture**: text, checklists, attachments with SHA256 hashing
- **Traceability**: append-only JSONL traces, per-step state snapshots, run resumption
- **VS Code extension**: interactive runbook execution, validation, and test running

## Installation

```bash
go install github.com/ormasoftchile/gert/cmd/gert@latest
```

Or build from source:

```bash
git clone https://github.com/ormasoftchile/gert.git
cd gert
go build -o gert ./cmd/gert
```

## CLI Commands

| Command | Description |
|---------|-------------|
| `gert validate <runbook.yaml>` | Validate a runbook against the JSON Schema and domain rules |
| `gert exec <runbook.yaml>` | Execute a runbook (flags: `--mode`, `--scenario`, `--as`, `--resume`, `--var`, `--record`) |
| `gert debug <runbook.yaml>` | Launch interactive debugger (flags: `--mode`, `--scenario`, `--as`, `--var`) |
| `gert tui <runbook.yaml>` | Launch terminal UI (flags: `--mode`, `--scenario`, `--as`, `--var`, `--compact`) |
| `gert diagram <runbook.yaml>` | Generate a visual diagram (flags: `-f mermaid\|ascii`, `-o file`) |
| `gert test <runbook.yaml...>` | Run scenario replay tests (flags: `--scenario`, `--json`, `--fail-fast`, `--timeout`) |
| `gert migrate <runbook.yaml>` | Migrate a runbook from v0 to v1 (flags: `--dry-run`) |
| `gert schema export` | Export JSON Schema to stdout |
| `gert serve` | Start JSON-RPC server for VS Code extension (stdio) |
| `gert version` | Print version information |

### Execution modes

```bash
# Real execution
gert exec runbook.yaml --as engineer@company.com

# Dry-run (no side effects)
gert exec runbook.yaml --mode dry-run

# Replay with pre-recorded scenario
gert exec runbook.yaml --mode replay --scenario scenario-dir/

# Record execution for later replay
gert exec runbook.yaml --as engineer@company.com --record
```

### Debugger

```bash
gert debug runbook.yaml --as engineer@company.com
```

Commands: `next`, `continue`, `print vars`, `print captures`, `history`, `evidence set/check/attach`, `approve`, `snapshot`, `dump`, `help`, `quit`

### Terminal UI (TUI)

```bash
gert tui runbook.yaml --as engineer@company.com
```

A full interactive terminal interface (Bubble Tea) with three-panel layout: step list, command output, and status detail. Supports real, replay, and dry-run modes. Use `--compact` for narrow terminals.

### Diagrams

```bash
# Mermaid flowchart (default) — paste into GitHub, docs, VS Code preview
gert diagram runbook.yaml

# ASCII for terminals
gert diagram runbook.yaml -f ascii

# Write to file
gert diagram runbook.yaml -o flow.md
```

Diagrams show step type icons (⚡ cli, 🧑 manual, 🔧 tool, 📎 invoke), branch conditions, capture annotations, and color-coded outcome terminals. Also available via the `runbook/diagram` JSON-RPC method.

### Scenario testing

```bash
# Test all scenarios for a runbook
gert test service-health.runbook.yaml

# With JSON output and fail-fast
gert test *.runbook.yaml --json --fail-fast --timeout 60s
```

Scenarios are discovered by convention at `{runbook-dir}/scenarios/{runbook-name}/*/`. Each scenario directory contains an `inputs.yaml` (variable overrides) and `scenario.yaml` (replayed command responses). A `test.yaml` defines assertions:

```yaml
description: "DNS OK, HTTP returns 200"
expected_outcome: resolved
expected_captures:
  http_response: "HTTP/2 200\r\n..."
must_reach:
  - dns_gate
  - check_http
must_not_reach: []
```

## Runbook Format

```yaml
apiVersion: runbook/v0

imports:
  dns-check: ../dns-check/dns-check.runbook.yaml

tools:
  - curl
  - nslookup

meta:
  name: service-health
  kind: mitigation
  description: Check DNS resolution and HTTP endpoint health
  vars:
    hostname: github.com
  inputs:
    server_name:
      from: prompt
      description: Server to check
  defaults:
    timeout: "120s"
  governance:
    allowed_commands: [kubectl, az, curl, nslookup]
    deny_env_vars: ["SECRET_*", "TOKEN"]
    redact:
      - pattern: "(?i)password\\s*[:=]\\s*\\S+"
        replace: "password: <redacted>"

tree:
  - step:
      id: resolve_dns
      type: tool
      title: Resolve DNS for hostname
      tool:
        name: nslookup
        action: lookup
        args:
          host: "{{ .hostname }}"
      capture:
        dns_output: stdout
    branches:
      - condition: '{{ contains .dns_output "Address" }}'
        label: DNS resolved
        steps:
          - step:
              id: check_http
              type: tool
              title: Check HTTP endpoint
              tool:
                name: curl
                action: head
                args:
                  url: "https://{{ .hostname }}"
              capture:
                http_response: stdout
              outcomes:
                - state: resolved
                  recommendation: "Service is healthy."

  - step:
      id: manual_review
      type: manual
      title: Validate monitoring dashboard
      instructions: |
        Check Grafana dashboard for {{ .hostname }}.
      required_evidence:
        - kind: checklist
          name: dashboard_check
          items: ["Error rate < 1%", "P99 < 500ms"]
      approvals:
        min: 1
        roles: ["DRI"]
```

### Step types

| Type | Description |
|------|-------------|
| `cli` | Execute a shell command (`with.argv`) |
| `tool` | Execute a tool action (`tool.name`, `tool.action`, `tool.args`) |
| `manual` | Prompt operator for instructions, evidence, or approval |
| `invoke` | Call a sub-runbook with input mapping and gate control |

### Step features

- **`capture`**: Capture `stdout`/`stderr`/`exit_code` into template variables
- **`assertions`**: Post-execution checks (`contains`, `not_contains`, `matches`, `exit_code`, `equals`, `not_equals`, `json_path`)
- **`when`**: Conditional guard expression (Go template)
- **`precondition`**: Probe check that auto-skips the step if already satisfied
- **`choices`**: Present operator with a choice menu that sets a variable
- **`gate`**: Control flow after `invoke` steps (`stop_if`, `on_error: skip`)
- **`outcomes`**: Terminal states (`resolved`, `escalated`, `no_action`, `needs_rca`) with optional `next_runbook` chaining

### Tools

Tools are declared as a list of names in the runbook and resolved by convention as `tools/<name>.tool.yaml`:

```yaml
tools:
  - curl
  - nslookup
```

Tool definitions (`.tool.yaml`) describe typed actions with governance:

```yaml
apiVersion: tool/v0
meta:
  name: curl
  binary: curl
transport:
  mode: stdio      # stdio | jsonrpc | mcp
actions:
  head:
    description: "HTTP HEAD request"
    argv: ["-s", "-I", "{{ .url }}"]
    args:
      url:
        type: string
        required: true
    capture:
      stdout: response_headers
```

Three transport modes are supported:

| Mode | Description |
|------|-------------|
| `stdio` | Spawn binary per call with resolved argv |
| `jsonrpc` | Persistent JSON-RPC 2.0 process (spawn once, reuse) |
| `mcp` | MCP (Model Context Protocol) server with tool discovery |

### Input providers

External input providers resolve `from:` bindings before execution. Providers are defined in `.provider.yaml` files and configured per-workspace or per-project:

```yaml
# .gert/config.yaml
providers:
  svc:
    binary: gert-svc-provider
    config: configs/svc.provider.yaml
```

Providers communicate over JSON-RPC 2.0 stdio and resolve prefixed bindings (e.g. `svc.customFields.ServerName`).

### Project manifest

Multi-package workspaces use a `gert.yaml` project manifest for cross-package tool and runbook resolution:

```yaml
name: gert-tools
paths:
  tools: tools
require:
  dep-pkg: ../dep-pkg
```

Qualified references like `dep-pkg/query` resolve through `require` dependencies.

## Architecture

```
cmd/gert/          CLI entry point (Cobra)
pkg/
  schema/          YAML parsing, JSON Schema validation, domain rules, tool & provider schemas
  runtime/         Execution engine, trace writer, snapshots, resume, expression evaluation
  governance/      Command allowlist/denylist, redaction, env blocking
  assertions/      7 assertion evaluators
  evidence/        Evidence types, SHA256 hashing
  providers/       CommandExecutor, EvidenceCollector implementations
  debugger/        Interactive REPL debugger
  diagram/         Mermaid and ASCII diagram generation from runbook trees
  replay/          ReplayExecutor, scenario parsing
  inputs/          Input provider framework, JSON-RPC provider, workspace config
  tools/           Tool manager, stdio/jsonrpc/mcp transports
  serve/           JSON-RPC server for VS Code extension
  testing/         Scenario replay test runner and assertion engine
  tui/             Bubble Tea terminal UI
schemas/           JSON Schema (Draft 2020-12): runbook-v0, runbook-v1, tool-v0
tools/             Built-in tool definitions (curl, ping, nslookup)
vscode/            VS Code extension (TypeScript)
  src/             Extension source, JSON-RPC client, runbook panel webview
  schemas/         Bundled JSON schemas for in-editor validation
  syntaxes/        Kusto syntax injection grammar
```

## VS Code Extension

The `gert` VS Code extension provides interactive runbook execution:

- **Run TSG**: Execute runbooks step-by-step with a webview panel
- **Validate TSG**: Schema + domain validation with inline diagnostics
- **Run Tests**: Execute scenario replay tests from the editor
- In-editor JSON schema validation for `.runbook.yaml` and `.tool.yaml` files
- Kusto syntax highlighting inside runbook YAML

Build the extension:

```bash
cd vscode && npm install && npm run compile
```

## Testing

```bash
# Run all Go tests
go test ./... -v

# Run scenario replay tests for runbooks
gert test service-health.runbook.yaml

# Run VS Code extension tests
cd vscode && npm test
```

## License

MIT
