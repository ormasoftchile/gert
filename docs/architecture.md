# Gert — Architecture Document

**Module**: `github.com/ormasoftchile/gert`
**Version**: v0 (runbook/v0 schema)

---

## System Overview

Gert is a Governed Executable Runbook Engine. It validates, executes, debugs, replays, and compiles incident-response runbooks defined in YAML. The CLI (`gert`) orchestrates all operations; a VS Code extension provides real-time schema validation in the editor.

```
┌───────────────────────────────────────────────────────────────────────┐
│                            cmd/gert (CLI)                             │
│  validate │ exec │ debug │ compile │ schema export │ version          │
│           │ --var │       │         │               │                 │
└────┬──────┴──┬───┴───┬───┴────┬────┴───────────────┴──────────────────┘
     │         │       │        │
     ▼         ▼       ▼        ▼
 ┌────────┐ ┌──────┐ ┌───────┐ ┌──────────┐
 │ schema │ │engine│ │debuggr│ │ compiler │
 └────────┘ └──┬───┘ └───┬───┘ └────┬─────┘
               │         │          │
        ┌──────┴─────────┘          │
        ▼                           ▼
 ┌─────────────┐             ┌────────────┐
 │  providers  │◄────────────│  schema    │
 │ cli,manual, │             │  (types)   │
 │ xts         │             └────────────┘
 └──────┬──────┘
        │
   ┌────┼────────────┐
   ▼    ▼            ▼
┌─────┐┌──────────┐┌──────┐
│gov. ││assertions││evid. │
└─────┘└──────────┘└──────┘

 External dependencies:
   xts-cli.exe ◄── XTSProvider (query, activity, view)
   .env        ◄── XTS_CLI_PATH, XTS_VIEWS_ROOT, XTS_ENVIRONMENT
   ICM API     ◄── meta.inputs (from: icm.*) [future binding]
```

---

## Package Inventory

| Package | Path | Purpose |
|---------|------|---------|
| **schema** | `pkg/schema/` | Runbook data model, YAML parsing, 3-phase validation, JSON Schema export |
| **providers** | `pkg/providers/` | Provider interfaces (`CommandExecutor`, `EvidenceCollector`, `Provider`) and all concrete implementations |
| **runtime** | `pkg/runtime/` | Execution engine, run state, JSONL trace, snapshots, resume |
| **governance** | `pkg/governance/` | Command allow/deny lists, env-var blocking, output redaction |
| **assertions** | `pkg/assertions/` | Post-execution assertion evaluation (7 types) |
| **evidence** | `pkg/evidence/` | Evidence value factory, SHA256 file hashing |
| **compiler** | `pkg/compiler/` | TSG→runbook compilation (IR extraction, LLM, validation) |
| **replay** | `pkg/replay/` | Scenario file parsing, deterministic `ReplayExecutor` |
| **debugger** | `pkg/debugger/` | Interactive REPL for step-by-step execution and inspection |
| **CLI** | `cmd/gert/` | Cobra command tree, flag wiring, `.env` loading |
| **VS Code ext.** | `vscode/` | Real-time YAML validation against `schemas/runbook-v0.json` |

---

## Component Details

### 1. `pkg/schema` — Data Model & Validation

The leaf package — everything depends on it, it depends on nothing internal.

**Structs** define the canonical runbook shape:

| Struct | Role |
|--------|------|
| `Runbook` | Top-level document (`apiVersion`, `meta`, `steps`) |
| `Meta` | Name, kind, description, source, vars, inputs, defaults, governance, xts |
| `SourceMeta` | Provenance: source file path, compiled_at timestamp, LLM model used |
| `InputDef` | External variable binding: `from` (icm.*, prompt, enrichment), `pattern`, `default` |
| `XTSMeta` | Global XTS config: environment, views_root, cli_path |
| `Step` | Single unit of work — `type: cli`, `type: manual`, or `type: xts` |
| `CLIStepConfig` | `argv` array for CLI steps |
| `XTSStepConfig` | XTS step config: mode (query/activity/view), file, activity, query_type, query, params |
| `GovernancePolicy` | Allowed/denied commands, env-var deny, redaction rules, evidence policy |
| `Assertion` | One of 7 check types (contains, matches, exit_code, etc.) |
| `EvidenceRequirement` | Required evidence for manual steps (text, checklist, attachment) |

**Step fields added**:

| Field | Purpose |
|-------|--------|
| `when` | Conditional guard — Go template expression, step skipped if empty/false |
| `terminal` | Endpoint marker: resolved, escalated, no_action, needs_rca |
| `xts` | XTS step configuration (mode, file, activity, query_type, query, params) |

**Meta fields added**:

| Field | Purpose |
|-------|--------|
| `kind` | Runbook purpose: mitigation, reference, composable, rca |
| `source` | Provenance tracking (source file, compile timestamp, model) |
| `inputs` | ICM-bound variables (from: icm.*, prompt, enrichment) |
| `xts` | Global XTS configuration (environment, views_root, cli_path) |

**Validation pipeline** (`ValidateFile`) runs 3 phases:

```
Phase 1: Structural    →  yaml.v3 KnownFields(true) — reject unknown fields
Phase 2: Semantic      →  JSON Schema Draft 2020-12 via santhosh-tekuri/jsonschema
Phase 3: Domain        →  Custom rules: unique step IDs, undefined vars, governance consistency
```

**Domain validation rules** (Phase 3) include:
- Step ID uniqueness
- Undefined variable references (checks meta.vars + meta.inputs + captures)
- Governance consistency (allow/deny overlap)
- meta.kind validation (mitigation, reference, composable, rca)
- meta.inputs validation (valid `from:` prefixes, regex pattern compilation)
- meta.xts required when any step has type=xts
- XTS mode-specific field validation (activity needs file+activity, query needs query_type+query, view needs file)
- XTS params and query template variable references

**Key functions**:
- `LoadFile(path) → (*Runbook, error)` — strict YAML parse
- `ValidateFile(path) → (*Runbook, []*ValidationError)` — full 3-phase pipeline
- `GenerateJSONSchema() → ([]byte, error)` — outputs `schemas/runbook-v0.json`

---

### 2. `pkg/providers` — The Provider Abstraction Layer

The **central interface hub**. Every execution mode plugs in here.

#### Interfaces

```go
// CommandExecutor runs a command and captures output.
type CommandExecutor interface {
    Execute(ctx context.Context, command string, args []string, env []string) (*CommandResult, error)
}

// EvidenceCollector gathers human evidence for manual steps.
type EvidenceCollector interface {
    PromptText(name, instructions string) (string, error)
    PromptChecklist(name string, items []string) (map[string]bool, error)
    PromptAttachment(name, instructions string) (*AttachmentInfo, error)
    PromptApproval(roles []string, min int) ([]Approval, error)
}

// Provider validates and executes a single step.
type Provider interface {
    Validate(step schema.Step) ValidationResult
    Execute(ctx context.Context, execCtx *ExecutionContext, step schema.Step) (*StepResult, error)
}
```

#### Concrete Implementations

| Type | Interface | Behaviour | File |
|------|-----------|-----------|------|
| `RealExecutor` | `CommandExecutor` | `os/exec.CommandContext` with timeout | `cli.go` |
| `InteractiveCollector` | `EvidenceCollector` | CLI prompts (stdin/readline) | `manual.go` |
| `DryRunCollector` | `EvidenceCollector` | Returns placeholder values, no prompts | `manual.go` |
| `ScenarioCollector` | `EvidenceCollector` | Returns pre-recorded evidence from scenario | `manual.go` |
| `DryRunExecutor` | `CommandExecutor` | Prints resolved command, returns exit 0 | `cmd/gert/main.go` |
| `ReplayExecutor` | `CommandExecutor` | Matches argv → canned response | `pkg/replay/replay.go` |
| `XTSProvider` | (standalone) | Wraps xts-cli.exe for query/activity/view modes | `xts.go` |

#### Provider Interaction Diagram

```
                          ┌───────────────────────┐
                          │   runtime.Engine      │
                          │                       │
                          │  step.Type dispatch   │
                          └──┬──────────┬───────┬─┘
                             │          │       │
                    ┌────────▼──┐ ┌─────▼─────┐ ┌▼──────────┐
                    │  CLI Step │ │Manual Step│ │ XTS Step  │
                    └────┬──────┘ └─────┬─────┘ └─────┬─────┘
                         │              │             │
                         ▼              ▼             ▼
                  CommandExecutor  EvidenceCol.  XTSProvider
                                                (xts-cli.exe)
                          │                   │
              ┌───────────▼────────┐   ┌──────▼───────────────┐
              │  CommandExecutor   │   │  EvidenceCollector   │
              │  .Execute(...)     │   │  .PromptText(...)    │
              └───────┬────────────┘   │  .PromptChecklist()  │
                      │                │  .PromptAttachment() │
         ┌────────────┼──────────┐     │  .PromptApproval()   │
         │            │          │     └───────┬──────────────┘
         ▼            ▼          ▼             │
  ┌────────────┐┌──────────┐┌────────┐  ┌──────┼──────────────┐
  │ RealExec.  ││ReplayExec││DryRun  │  │      ▼        ▼     │
  │ os/exec    ││ scenario ││ no-op  │  │Interactive Scenario │
  │            ││ match    ││ print  │  │Collector  Collector │
  └────────────┘└──────────┘└────────┘  │  (stdin)  (canned)  │
                                        │     ▼               │
                                        │  DryRunCollector    │
                                        │  (placeholders)     │
                                        └─────────────────────┘
```

#### Execution Modes → Provider Wiring

| Mode | `CommandExecutor` | `EvidenceCollector` |
|------|-------------------|---------------------|
| `real` | `RealExecutor` | `InteractiveCollector` |
| `replay` | `ReplayExecutor` | `ScenarioCollector` |
| `dry-run` | `DryRunExecutor` | `DryRunCollector` |

#### Shared Result Types

| Type | Emitted By | Consumed By |
|------|-----------|-------------|
| `CommandResult` | `CommandExecutor.Execute()` | Engine (capture extraction, assertions) |
| `StepResult` | `Provider.Execute()` | Engine → TraceWriter, SnapshotWriter |
| `EvidenceValue` | `EvidenceCollector.Prompt*()` | StepResult → Trace |
| `AssertionResult` | `assertions.Evaluate()` | StepResult → Trace |
| `ExecutionContext` | Engine | Providers (vars, captures, governance ref) |

---

### 3. `pkg/runtime` — Execution Engine

Orchestrates step-by-step execution. The engine **does not know** which concrete executor or collector it's using — it programs against the `CommandExecutor` and `EvidenceCollector` interfaces.

#### Engine Execution Flow

```
CLI: resolve --var flags + meta.inputs → prompt for unresolved inputs
   │
   ▼
NewEngine(runbook, executor, collector, mode, actor)
   │  └── Initialize XTSProvider from meta.xts + env vars (XTS_CLI_PATH, etc.)
   │
   ▼
Engine.Run(ctx)
   │
   ├── for each step (from CurrentStepIndex):
   │   │
   │   ├── Evaluate step.when (Go template → skip if empty/false/0)
   │   │   └── Skipped steps recorded in trace with status="skipped"
   │   │
   │   ├── step.Type == "cli":
   │   │   ├── Resolve template variables in argv
   │   │   ├── governance.CheckCommand(argv[0])
   │   │   ├── executor.Execute(ctx, argv[0], argv[1:], env)
   │   │   ├── governance.RedactOutput(stdout)
   │   │   ├── Extract captures (stdout/stderr → vars)
   │   │   └── assertions.Evaluate(step.Assertions, output, exitCode)
   │   │
   │   ├── step.Type == "manual":
   │   │   ├── Resolve template variables in instructions
   │   │   ├── For each RequiredEvidence:
   │   │   │   ├── kind=text      → collector.PromptText()
   │   │   │   ├── kind=checklist → collector.PromptChecklist()
   │   │   │   └── kind=attachment→ collector.PromptAttachment()
   │   │   └── If step.Approvals → collector.PromptApproval()
   │   │
   │   ├── step.Type == "xts":
   │   │   ├── Resolve templates in xts.params, xts.query, xts.environment
   │   │   ├── Inject default environment from meta.xts (with template resolution)
   │   │   ├── Dry-run: show xts-cli command, return placeholder captures
   │   │   ├── Real: XTSProvider.Execute() → xts-cli --format json
   │   │   ├── Parse JSON: { success, rowCount, columns, data[], metadata }
   │   │   ├── Extract captures: $.data[N].field, $.data[*].field, row_count, stdout
   │   │   └── Empty results (0 rows): captures set to "" (not failure)
   │   │
   │   ├── Write step result → TraceWriter (JSONL append)
   │   ├── Save snapshot → snapshots/step-NNNN.json
   │   ├── Merge captures into RunState.Captures
   │   ├── If step failed → HALT (unless dry-run)
   │   └── If step.terminal set → END runbook (reached terminal state)
   │
   └── Done
```

#### Input Resolution (pre-execution)

```
meta.inputs declared in runbook:
  ┌──────────────┐     ┌──────────────┐     ┌──────────────┐
  │  --var flag   │────►│   default    │────►│   prompt     │
  │  (CLI override│     │  (InputDef)  │     │  (stdin)     │
  └──────────────┘     └──────────────┘     └──────────────┘
         highest priority                      lowest priority

  from: icm.*     → [future] ICM API binding
  from: prompt    → interactive prompt at startup
  from: enrichment→ [future] pre-execution lookup step
```

#### XTS Provider Configuration Hierarchy

```
  runbook meta.xts.cli_path  →  .env XTS_CLI_PATH  →  PATH lookup
  runbook meta.xts.views_root →  .env XTS_VIEWS_ROOT
  runbook meta.xts.environment →  .env XTS_ENVIRONMENT
  step xts.environment         →  meta.xts.environment (per-step override)
```

#### State & Persistence

| Artifact | Format | Path | Purpose |
|----------|--------|------|---------|
| Run state | JSON | `.runbook/runs/<run_id>/snapshots/step-NNNN.json` | Per-step checkpoint for resume |
| Trace | JSONL | `.runbook/runs/<run_id>/trace.jsonl` | Immutable audit log |
| Run ID | `20260212T153045-abc` | — | Timestamp + 3-char suffix |

#### Resume

`ResumeEngine()` loads the latest snapshot, re-opens the trace file for append, and continues from the failed step.

---

### 4. `pkg/governance` — Safety Enforcement

Evaluated **before** every CLI step executes.

| Component | File | What It Does |
|-----------|------|--------------|
| `GovernanceEngine` | `allowlist.go` | `CheckCommand(argv[0])` — deny takes precedence over allow |
| Environment blocker | `envblock.go` | `FilterEnvVars(env)` — removes vars matching deny_env_vars globs |
| Redaction engine | `redaction.go` | `RedactOutput(stdout, rules)` — regex replace before persistence |

```
        ┌───────────────────┐
        │ GovernancePolicy  │ (from runbook YAML)
        │ allowed_commands  │
        │ denied_commands   │
        │ deny_env_vars     │
        │ redact[]          │
        └────────┬──────────┘
                 │
                 ▼
        ┌────────────────────┐
        │  GovernanceEngine  │
        │                    │
 argv ──►  CheckCommand()    │──► allow / deny error
        │                    │
 env  ──►  FilterEnvVars()   │──► filtered env, blocked list
        │                    │
        └────────────────────┘

        ┌────────────────────┐
 stdout►│ RedactOutput()     │──► sanitized output
        │  (compiled regex)  │
        └────────────────────┘
```

---

### 5. `pkg/assertions` — Post-Execution Checks

Called by the engine **after** a CLI step completes. Each assertion in the step's `assertions[]` array is evaluated against the captured output and exit code.

| Assertion Type | Evaluator | Checks |
|---------------|-----------|--------|
| `contains` | `EvalContains` | `stdout` contains substring |
| `not_contains` | `EvalNotContains` | `stdout` does NOT contain substring |
| `matches` | `EvalMatches` | `stdout` matches regex |
| `exit_code` | `EvalExitCode` | Process exit code == expected |
| `equals` | `EvalEquals` | `stdout` == exact string |
| `not_equals` | `EvalNotEquals` | `stdout` != string |
| `json_path` | `EvalJSONPath` | JSONPath query on stdout == expected |

Any failed assertion → `StepResult.Status = "failed"` → engine halts.

---

### 6. `pkg/evidence` — Evidence Value Factory

Constructs `EvidenceValue` objects from collected input. Used by both the engine and the debugger.

```
NewTextEvidence(value)            → EvidenceValue{Kind: "text", Value: "..."}
NewChecklistEvidence(items)       → EvidenceValue{Kind: "checklist", Items: map}
NewAttachmentEvidence(path)       → EvidenceValue{Kind: "attachment", Path, SHA256, Size}
                                         ↑
                                    HashFile(path) → SHA256 + file size
```

---

### 7. `pkg/compiler` — TSG Compilation Pipeline

Converts Markdown troubleshooting guides into schema-valid runbooks using a 3-stage pipeline.

```
┌──────────────┐     ┌──────────────────┐     ┌──────────────┐
│  Stage A     │     │  Stage B         │     │  Stage C     │
│ Deterministic│────►│  LLM Agent       │────►│ Deterministic│
│              │     │  (Azure OpenAI)  │     │              │
│  ParseTSG()  │     │  Complete()      │     │  ValidateFile│
│  → IR struct │     │  → YAML+mapping  │     │  → pass/fail │
└──────────────┘     └──────────────────┘     └──────────────┘
```

**Stage A** — `ParseTSG(source)`:
- Walks the Markdown AST (goldmark)
- Extracts headings → section boundaries
- Extracts fenced code blocks → CLI candidates
- Extracts `$VAR` references → `IR.Vars`
- Produces an `IR` struct (not a runbook yet)

**Stage B** — LLM interpretation (22 rules in system prompt):
- Renders system prompt (full JSON Schema + 22 rules + output format)
- Renders user prompt (TSG content + pre-extracted vars)
- Calls `LLMClient.Complete()` (Azure OpenAI chat completions)
- Parses response using `---RUNBOOK_YAML---` / `---MAPPING_MD---` delimiters
- Unmarshals YAML → `schema.Runbook`
- Injects `meta.source` provenance (source file, timestamp, model name)

**Key LLM rules** (system prompt):
| Rule | What it does |
|------|-------------|
| 5 | CAS command safety: read-only (Dump-Process) → cli, destructive (Kill-Process) → manual |
| 8 | Inputs vs vars: ICM-bound variables → meta.inputs, static defaults → meta.vars |
| 16 | XTS views → manual (interactive), activities/queries → type: xts |
| 17 | HttpRequest Query Tool / AdhocCMSQuery → mode: query, query_type: cms |
| 18 | Inline Kusto queries (Mon* tables) → mode: query, query_type: kusto |
| 20 | meta.kind inference: mitigation, reference, composable, rca |
| 21 | Conditional flow: when: guards using captures |
| 22 | Terminal states: every mitigation must have at least one terminal step |

**Output paths**: Derived from source TSG filename (e.g. `alias-db-failure-alert.md` → `.runbook.yaml` + `.mapping.md`).

**Mapping report** includes: TSG heading path, source line ranges, source excerpts (verbatim quotes), variable list, manual step reasons, TODOs.

**Stage C** — Validation:
- Passes generated runbook through `schema.ValidateFile()`
- Reports any errors back to the user

#### LLM Client Interface

```go
type LLMClient interface {
    Complete(ctx context.Context, systemPrompt, userPrompt string) (string, error)
    ModelName() string  // for provenance tracking
}
```

Implementations: `AzureOpenAIClient` (production), `mockLLMClient` (tests).

---

### 8. `pkg/replay` — Offline Deterministic Replay

**Scenario file** (YAML):
```yaml
commands:
  - argv: [kubectl, get, pods, -n, default]
    stdout: "NAME  READY  STATUS\npod-1  1/1  Running"
    exit_code: 0
evidence:
  validate_dashboard:
    dashboard_check:
      kind: text
      value: "All green"
```

`ReplayExecutor.Execute()` matches `command + args` against scenario entries. No match → error (fail-closed). Produces identical traces across multiple runs.

`ScenarioCollector` returns pre-recorded evidence for manual steps from the same scenario file.

---

### 9. `pkg/debugger` — Interactive REPL

Wraps the runtime engine in a readline-based REPL for step-by-step execution.

```
gert[step 1/4 | check_pod_status]>
```

| Command | Action |
|---------|--------|
| `next` / `n` | Execute current step, advance |
| `continue` / `c` | Run all remaining steps |
| `print vars` | Show resolved variables |
| `print captures` | Show accumulated captures |
| `history` | Show completed step results |
| `evidence set <step> <name> <value>` | Inject text evidence |
| `evidence check <step> <name> <item>` | Toggle checklist item |
| `evidence attach <step> <name> <path>` | Attach file evidence |
| `approve --as <id>` | Record approval |
| `snapshot` | Save current state |
| `dump` | Full state JSON dump |
| `help` | List commands |
| `quit` | Exit debugger |

---

### 10. VS Code Extension (`vscode/`)

Runtime-independent schema validation in the editor. Validates open `.yaml` files against `schemas/runbook-v0.json` (Draft 2020-12, ajv 2020).

```
┌─────────────────────┐     ┌──────────────────┐
│  extension.ts       │     │  validate.ts     │
│                     │     │                  │
│  onDidOpenTextDoc ──┼────►│  Ajv2020.compile │
│  onDidSaveTextDoc ──┼────►│  (runbook-v0.json)│
│  gert.validateCmd ──┼────►│                  │
│                     │     │  validateRunbook()│──► Diagnostics
└─────────────────────┘     └──────────────────┘
```

Triggers: file open, file save, manual command. Results appear in the Problems panel.

---

## Dependency Graph

```
cmd/gert/main.go
 ├── pkg/schema           validate, load, export
 ├── pkg/providers        interfaces + RealExecutor, InteractiveCollector
 ├── pkg/runtime          Engine, ResumeEngine
 ├── pkg/replay           ReplayExecutor, LoadScenario
 ├── pkg/debugger         New, Run
 └── pkg/compiler         CompileTSG, AzureOpenAI client

pkg/runtime
 ├── pkg/schema
 ├── pkg/providers        CommandExecutor, EvidenceCollector, StepResult
 ├── pkg/assertions       Evaluate()
 ├── pkg/evidence         NewTextEvidence, NewChecklistEvidence
 └── pkg/governance       GovernanceEngine, RedactOutput

pkg/debugger
 ├── pkg/schema
 ├── pkg/providers
 ├── pkg/runtime          Engine, RunState, SaveSnapshot
 └── pkg/evidence         HashFile

pkg/compiler
 └── pkg/schema           Runbook types, GenerateJSONSchema, ValidateFile

pkg/assertions
 ├── pkg/providers        AssertionResult
 └── pkg/schema           Assertion

pkg/evidence
 └── pkg/providers        EvidenceValue

pkg/governance
 └── pkg/schema           GovernancePolicy, RedactionRule

pkg/replay
 └── pkg/providers        CommandResult, EvidenceValue

pkg/schema
 └── (leaf — no internal deps)
```

### Dependency Rules

1. **`pkg/schema`** is the leaf. All packages depend on it; it depends on none.
2. **`pkg/providers`** is the interface hub. Almost every package references its types.
3. **`pkg/runtime`** is the only package that imports governance, assertions, and evidence together — it is the orchestrator.
4. **No circular dependencies** exist. The dependency graph is a DAG.

---

## End-to-End Flows

### `gert validate <runbook.yaml>`

```
CLI → schema.ValidateFile(path)
         → Phase 1: yaml.v3 Decode (strict)
         → Phase 2: JSON Schema validation
         → Phase 3: Domain rules
       ← ValidationErrors or ✓
```

### `gert exec <runbook.yaml> --mode real --as engineer`

```
CLI → schema.LoadFile(path)
    → providers.RealExecutor + providers.InteractiveCollector
    → runtime.NewEngine(runbook, executor, collector, "real", "engineer")
    → engine.Run(ctx)
        → for each step:
            CLI:    governance → executor.Execute() → redact → capture → assert
            Manual: collector.Prompt*() → evidence → approvals
            → trace.Write() + snapshot.Save()
    ← exit 0 (all passed) or exit 2 (step failed)
```

### `gert exec <runbook.yaml> --mode replay --scenario s.yaml`

```
CLI → schema.LoadFile(path) + replay.LoadScenario(scenarioPath)
    → replay.ReplayExecutor + providers.ScenarioCollector
    → runtime.NewEngine(runbook, replayExec, scenarioCollector, "replay", "engine")
    → engine.Run(ctx)
        → steps matched against canned responses (deterministic)
    ← exit 0 or exit 2
```

### `gert compile <tsg.md> --out runbook.yaml`

```
CLI → compiler.CompileTSG(path, azureOpenAIClient)
        → Stage A: ParseTSG(source) → IR
        → Stage B: RenderPrompts() → client.Complete() → ParseLLMResponse()
        → Stage C: yaml.Unmarshal → schema.Runbook
    → compiler.WriteRunbook() + compiler.WriteMapping()
    → schema.ValidateFile(outPath) ← post-compilation check
```

### `gert debug <runbook.yaml>`

```
CLI → schema.LoadFile + create executor/collector
    → debugger.New(runbook, executor, collector, mode, actor)
    → debugger.Run(ctx) — REPL loop
        → user types "next" → engine.ExecuteStep(ctx, index)
        → user types "print vars" → display state.Vars
        → user types "evidence set ..." → inject evidence
        → user types "quit" → exit
```
