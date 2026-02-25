# CLI Contract: `gert` Command Interface v0

**Branch**: `001-runbook-engine-v0` | **Date**: 2026-02-11

---

## Command Tree

```
gert
├── validate <runbook.yaml>              # Schema validation
├── exec <runbook.yaml>                  # Execute runbook
│   ├── --mode real|replay|dry-run       # Execution mode (default: real)
│   ├── --scenario <scenario.yaml>       # Scenario file (required for replay)
│   ├── --as <actor-name>                # Actor identity for approvals
│   └── --resume <run-id>               # Resume halted execution
├── debug <runbook.yaml>                 # Interactive debugger
│   ├── --mode real|replay               # Execution mode (default: real)
│   ├── --scenario <scenario.yaml>       # Scenario file (required for replay)
│   └── --as <actor-name>               # Actor identity for approvals
├── compile <tsg.md>                     # TSG → Runbook compilation
│   ├── --out <runbook.yaml>             # Output runbook path
│   └── --mapping <mapping.md>           # Output mapping report path
├── schema                               # Schema operations
│   └── export                           # Export JSON Schema to stdout
└── version                              # Print version information
```

---

## Commands

### `gert validate`

Validate a runbook YAML file against the schema and governance policies.

**Usage**: `gert validate <runbook.yaml>`

**Arguments**:
| Argument | Required | Description |
|----------|----------|-------------|
| `<runbook.yaml>` | yes | Path to the runbook file to validate |

**Exit codes**:
| Code | Meaning |
|------|---------|
| 0 | Runbook is valid |
| 1 | Validation errors found |
| 2 | File not found or unreadable |

**stdout** (success): `runbook.yaml: valid`

**stdout** (failure): Structured error list:
```
runbook.yaml: invalid (3 errors)
  error: steps[2].with.argv is required (line 45)
  error: unknown field "priority" at meta level (line 8)
  warn:  step "restart_pod" uses command "kubectl" not in allowed_commands (line 52)
```

**stderr**: Fatal errors only (file I/O failures).

---

### `gert exec`

Execute a runbook in the specified mode.

**Usage**: `gert exec <runbook.yaml> [flags]`

**Arguments**:
| Argument | Required | Description |
|----------|----------|-------------|
| `<runbook.yaml>` | yes | Path to the runbook file |

**Flags**:
| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `real` | Execution mode: `real`, `replay`, or `dry-run` |
| `--scenario` | — | Path to scenario YAML (required when `--mode=replay`) |
| `--as` | — | Actor identity for approval recording |
| `--resume` | — | Run ID to resume from last completed step |

**Exit codes**:
| Code | Meaning |
|------|---------|
| 0 | All steps passed |
| 1 | Step failure (assertion violation, non-zero exit, timeout) |
| 2 | Governance violation (blocked command, denied env var) |
| 3 | Invalid runbook or missing scenario |
| 4 | Resume failure (run ID not found, no snapshot) |

**stdout**: Step-by-step execution progress:
```
[1/5] check_pods ............ passed (1.2s)
[2/5] validate_portal ....... waiting for evidence
      ✓ criteria: 1/1 items checked
      ✓ dashboard_link: provided
      ✗ screenshot: missing
```

**Artifacts produced**:
- `.runbook/runs/<run_id>/trace.jsonl`
- `.runbook/runs/<run_id>/snapshots/step-NNNN.json`
- `.runbook/runs/<run_id>/attachments/<sha256>.bin` (if attachments provided)

---

### `gert debug`

Launch the interactive debugger for step-by-step execution.

**Usage**: `gert debug <runbook.yaml> [flags]`

**Arguments**:
| Argument | Required | Description |
|----------|----------|-------------|
| `<runbook.yaml>` | yes | Path to the runbook file |

**Flags**:
| Flag | Default | Description |
|------|---------|-------------|
| `--mode` | `real` | Execution mode: `real` or `replay` |
| `--scenario` | — | Path to scenario YAML (required when `--mode=replay`) |
| `--as` | — | Actor identity for approvals |

**Debugger commands**:
| Command | Description |
|---------|-------------|
| `next` | Execute the next step |
| `continue` | Execute all remaining steps |
| `dump` | Display full RunState as JSON |
| `print vars` | Display all current variables |
| `print captures` | Display all accumulated captures |
| `history` | Display executed step results |
| `evidence set <name> <value>` | Set text evidence for current manual step |
| `evidence check <name> <item>` | Check off a checklist item |
| `evidence attach <name> <path>` | Attach a file as evidence |
| `approve --as <name>` | Record an approval for current step |
| `snapshot` | Force a state snapshot to disk |
| `help` | Show available commands |
| `quit` | Exit the debugger |

**Prompt format**: `gert[step N/total | step_id]> `

**Exit codes**: Same as `gert exec`.

---

### `gert compile`

Compile a Markdown TSG into a schema-valid runbook.

**Usage**: `gert compile <tsg.md> [flags]`

**Arguments**:
| Argument | Required | Description |
|----------|----------|-------------|
| `<tsg.md>` | yes | Path to the Markdown TSG file |

**Flags**:
| Flag | Default | Description |
|------|---------|-------------|
| `--out` | `runbook.yaml` | Output path for the generated runbook |
| `--mapping` | `mapping.md` | Output path for the mapping report |

**Exit codes**:
| Code | Meaning |
|------|---------|
| 0 | Compilation successful, output is schema-valid |
| 1 | Compilation completed with warnings/TODOs |
| 2 | Input file not found or unreadable |
| 3 | Compilation failed (could not produce valid output) |

**stdout**: Compilation progress and summary:
```
Parsing TSG... 12 sections found
Extracting structure... 8 steps identified
  3 CLI steps, 4 manual steps, 1 TODO
Generating runbook.yaml... done
Generating mapping.md... done
Validating output... passed
```

---

### `gert schema export`

Export the canonical JSON Schema to stdout.

**Usage**: `gert schema export`

**Exit codes**:
| Code | Meaning |
|------|---------|
| 0 | Schema exported successfully |

**stdout**: JSON Schema document (Draft 2020-12).

---

### `gert version`

Print version information.

**Usage**: `gert version`

**stdout**: `gert v0.1.0 (build: <commit-hash>)`

---

## Error Output Convention

All commands follow this convention:
- **stdout**: Primary output (validation results, execution progress, schema JSON, etc.)
- **stderr**: Fatal errors, warnings, and diagnostic messages
- **Exit code 0**: Success
- **Exit code >0**: Failure (code indicates failure category)

Structured error output uses the format:
```
<context>: <severity> (<count> <items>)
  <severity>: <message> (<location>)
```
