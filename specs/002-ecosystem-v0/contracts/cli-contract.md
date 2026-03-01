# CLI Contracts: Gert Ecosystem v0

## Core CLI (`cmd/gert/`)

Replaces `cmd/gert-kernel/`. Same commands, extended.

### gert validate <file>

Auto-detects runbook vs tool definition by `apiVersion`.

| Input | Output (success) | Output (failure) | Exit code |
|-------|-----------------|-------------------|-----------|
| Runbook YAML | `✓ <name> is valid (<N> steps)` + warnings to stderr | Error list to stderr | 0 / 1 |
| Tool YAML | `✓ tool <name> is valid (<N> actions)` + warnings | Error list to stderr | 0 / 1 |

New validations:
- `effects` + `side_effects` coexistence → error
- `side_effects` without `effects` → deprecation warning
- `secrets[].env` not in environment → warning
- `export` references undeclared `contract.outputs` → error
- `scope` path normalization (`/` → `.`)
- `visibility` glob syntax validation

### gert exec <file>

| Flag | Description |
|------|-------------|
| `--mode real\|dry-run\|probe` | Execution mode. Probe: execute read-only tools only. |
| `--var key=value` | Set input variable (repeatable) |
| `--trace <file>` | Write JSONL trace to file |
| `--as <actor>` | Actor identity for trace + approval requests |
| `--probe-allow-effects <list>` | Comma-separated effects allowed in probe mode (default: `network`) |

New trace output:
- `prev_hash` on every event
- `run_start` includes actor, host, gert_version, runbook_hash, tool_hashes
- `run_complete` includes chain_hash + signature (if signing key set)

### gert resume --run <id>

| Input | Output | Exit code |
|-------|--------|-----------|
| Run ID with persisted state | Continues execution from pending step | 0 (completed) / 1 (error) |

Loads `runs/<run-id>/state.json`, checks approval ticket status, resumes engine.

### gert test <file...>

Unchanged interface. Internally uses revised engine with `context.Context`.

### gert trace verify <file>

| Flag | Description |
|------|-------------|
| `--key-id <id>` | Verify signature against named key (from env var `GERT_TRACE_KEY_<id>`) |

| Output (valid) | Output (broken chain) | Output (bad signature) |
|-----------------|----------------------|----------------------|
| `✓ Chain integrity: N events, no breaks` | `✗ Chain broken at event M` | `✗ Signature invalid` |
| `✓ Signature valid: signed by key "<id>"` | | |

### gert watch <file>

| Flag | Description |
|------|-------------|
| `--interval <duration>` | Time between runs (e.g., `5m`) |
| `--stop-on <categories>` | Comma-separated outcome categories that stop the loop |
| `--var key=value` | Set input variable (repeatable) |

Output: one summary line per run with timestamp, outcome, duration.

### gert diff <file>

| Input | Output |
|-------|--------|
| Runbook with golden scenarios | Per-scenario outcome comparison: same / changed |

### gert outcomes

| Flag | Description |
|------|-------------|
| `--since <duration>` | Time window (e.g., `7d`, `30d`) |
| `--runbook <name>` | Filter by runbook name |
| `--json` | JSON output for dashboards |

### gert schema runbook / gert schema tool

Unchanged. Exports JSON Schema Draft 2020-12.

## TUI Binary (`cmd/gert-tui/`)

```
gert-tui <runbook.yaml> [--mode real|replay|dry-run] [--var key=value] [--scenario <name>] [--as <actor>]
```

Launches Bubble Tea interface. Same flags as `gert exec` minus `--trace` (TUI shows trace inline).

## MCP Server Binary (`cmd/gert-mcp/`)

```
gert-mcp [--stdio]
```

Starts MCP server on stdio (default). Registers tools:
- `gert/validate` → `{ path: string }`
- `gert/exec` → `{ path: string, vars: object, mode: string }`
- `gert/test` → `{ path: string, scenario?: string }`
- `gert/schema` → `{ type: "runbook" | "tool" }`

Registers resources:
- `gert://runbooks/{name}`
- `gert://tools/{name}`
- `gert://traces/{run_id}`
