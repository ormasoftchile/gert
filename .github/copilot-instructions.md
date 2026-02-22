# Gert — Copilot Instructions

Gert is a Governed Executable Runbook Engine: a Go CLI that validates, executes, debugs, replays, and compiles incident-response runbooks defined in YAML.

## Build & Test

```bash
# Build
go build -o bin/gert ./cmd/gert

# Run all tests
go test ./...

# Run tests for a single package
go test ./pkg/schema/...

# Run a single test by name
go test ./pkg/runtime/... -run TestDryRunZeroSideEffects

# Verbose output
go test ./... -v

# Regenerate the JSON Schema from Go structs
go run tools/gen-schema.go
```

There is no Makefile, linter config, or CI pipeline — just `go build` and `go test`.

## Architecture

The CLI entry point is `cmd/gert/main.go` (single file, Cobra commands). All domain logic lives in `pkg/` packages with a strict DAG dependency structure:

- **`pkg/schema`** — the leaf package. Defines all runbook Go structs (`Runbook`, `Meta`, `Step`, etc.), strict YAML parsing via `yaml.v3 KnownFields(true)`, and a 3-phase validation pipeline (structural → JSON Schema → domain rules). Everything depends on it; it depends on nothing internal.
- **`pkg/providers`** — the interface hub. Defines `CommandExecutor`, `EvidenceCollector`, and `Provider` interfaces plus shared result types (`CommandResult`, `StepResult`, `EvidenceValue`). Almost every package references its types.
- **`pkg/runtime`** — the orchestrator. The only package that imports governance, assertions, and evidence together. Runs steps via the provider interfaces, writes JSONL traces, saves per-step snapshots, and supports run resumption.
- **`pkg/governance`** — command allowlist/denylist checks, env-var blocking, output redaction. Evaluated before every CLI step.
- **`pkg/assertions`** — 7 post-execution assertion evaluators (contains, not_contains, matches, exit_code, equals, not_equals, json_path).
- **`pkg/evidence`** — evidence value factory with SHA256 file hashing.
- **`pkg/compiler`** — 3-stage TSG→runbook compilation: deterministic Markdown IR extraction (goldmark), LLM interpretation (Azure OpenAI), then `schema.ValidateFile` post-check.
- **`pkg/replay`** — `ReplayExecutor` matches argv against canned scenario responses (fail-closed on no match). `ScenarioCollector` returns pre-recorded evidence.
- **`pkg/debugger`** — readline-based interactive REPL wrapping the runtime engine.
- **`pkg/tools`** — MCP tool server, JSON-RPC transport, XTS built-in tool definitions.
- **`pkg/testing`** — scenario-based runbook test runner (`gert test` command). Tests live alongside runbooks in `scenarios/` directories.
- **`pkg/inputs`** — input resolution manager for runtime variable binding.
- **`pkg/icm`** — ICM incident integration.
- **`pkg/serve`** — HTTP/JSON-RPC server mode.

### Execution modes and provider wiring

The engine programs against interfaces. CLI wires the concrete implementations:

| Mode | `CommandExecutor` | `EvidenceCollector` |
|------|-------------------|---------------------|
| `real` | `RealExecutor` (os/exec) | `InteractiveCollector` (stdin) |
| `replay` | `ReplayExecutor` (scenario match) | `ScenarioCollector` (canned) |
| `dry-run` | `DryRunExecutor` (no-op, prints) | `DryRunCollector` (placeholders) |

### Schema versions

The project supports `runbook/v0` and `runbook/v1` API versions. JSON Schema files live in `schemas/` (`runbook-v0.json`, `runbook-v1.json`, `tool-v0.json`). Schemas are generated from Go struct tags via `tools/gen-schema.go`.

### Step types

Steps can be `cli` (command execution), `manual` (human evidence collection + approvals), `xts` (XTS provider queries/activities), or `tool` (MCP tool invocation).

## Key Conventions

- **Strict YAML parsing**: `yaml.v3` with `KnownFields(true)` — unknown fields are rejected at parse time.
- **3-phase validation**: structural (YAML decode) → semantic (JSON Schema Draft 2020-12) → domain (custom Go rules: unique step IDs, undefined vars, governance consistency).
- **Template variables**: Go `text/template` syntax (`{{ .varname }}`). Variables come from `meta.vars`, `meta.inputs`, `--var` flags, and step captures.
- **Test patterns**: tests use stdlib `testing` only (no testify). Test doubles are defined in `_test.go` files (e.g., `mockLLMClient`, `dryRunExecutor`). Tests construct `schema.Runbook` structs in-line rather than loading YAML fixtures.
- **Interface-driven design**: the provider abstraction (`CommandExecutor`, `EvidenceCollector`) enables real/replay/dry-run modes without changing engine logic.
- **Append-only traces**: execution produces JSONL trace files and per-step JSON snapshots under `.runbook/runs/<run_id>/`.
- **Version via ldflags**: `version` and `commit` vars in `main.go` are set at build time.
- **`.env` loading**: `main.go` loads `.env` from the working directory at startup (gitignored). Used for `XTS_CLI_PATH`, `AZURE_OPENAI_*`, etc.
- **Runbook test scenarios**: test scenarios live alongside runbooks in `scenarios/<runbook-name>/<scenario>/` with `inputs.yaml`, recorded step responses, and `test.yaml` assertions.

## VS Code Extension

A TypeScript extension lives in `vscode/`. It validates open YAML files against the JSON Schema using ajv 2020. It is a separate project from the Go CLI.
