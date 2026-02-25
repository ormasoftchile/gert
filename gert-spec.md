# Project Definition: Governed Executable Runbooks for Prose TSGs  

(TSG → Runbook Compiler + Runtime + Debugger + Replay + Governance)

---

## 0) One-liner

Build a platform that converts prose-based TSG/runbooks (Markdown) into a governed, executable, debuggable runbook schema; executes steps (automated + manual) with traceability and evidence capture; supports deterministic replay via `cli-replay`; and provides a step-by-step debugger with state snapshots (“memory dumps”).

---

## 1) Primary User Story

As an on-call engineer (DRI), I want to run a text-based TSG in a governed way (guided + step-by-step), with a clear audit trail and evidence per step, so incident response is repeatable, reviewable, and safe—without rewriting all existing TSGs.

---

## 2) Goals

- Ingest existing Markdown TSGs and produce a structured, schema-valid runbook (draft + mapping report).

- Execute runbooks step-by-step with a debugger UX (CLI first), showing variables/captures/history.

- Support both automated steps (CLI calls, later providers) and manual steps (attestation + evidence).

- Record an immutable execution trace + periodic snapshots (“memory dumps”) for forensics and audit.

- Support deterministic offline replay/testing of CLI orchestration using `cli-replay`.

- Provide governance: allowed commands, redaction policies, evidence requirements, approvals/roles (v0 = minimal).

- Be extensible: provider model for AWS/Azure/etc. (v0 uses only CLI provider + `cli-replay` integration).

---

## 3) Non-Goals (v0)

- No full incident management suite (no paging, no ticketing integration required).

- No automatic remediation without human confirmation (safe by default).

- No complex workflow language (no Turing-complete DSL).

- No concurrent/parallel steps in v0.

- No fine-tuning/training required in v0 (use prompt + catalog + examples; later add RAG/fine-tune if needed).

---

## 4) System Components (repos or packages)

### 4.1 `runbook-schema`

Responsibilities:

- Define JSON Schema (and/or Go structs) for the runbook format.

- Strict validation (reject unknown fields).

- Validation rules enforced deterministically.

Includes:

- Step types: `cli`, `manual`, `group` (optional in v0).

- Governance: allowlist/denylist, redaction, evidence requirements.

- Captures, variables, assertions.

---

### 4.2 `runbook-compiler` (TSG → Runbook)

Input:

- Markdown TSG.

Output:

1. `runbook.yaml` (schema-valid)

2. `mapping.md` (explain how each step maps to TSG sections; list uncertainties/TODOs)

Pipeline:

- Stage A: Extract to TSG-IR (deterministic extraction of headings, code blocks, “Run:” lines).

- Stage B: LLM interpret IR → `runbook.yaml` + `mapping.md` (constrained prompt contract).

- Stage C: Deterministic validation (schema + policy + replay-compat checks).

---

### 4.3 `runbook-runtime`

Responsibilities:

- Deterministic execution of runbook as a state machine.

- Step scheduling.

- Variable resolution.

- Capture accumulation.

- Assertion evaluation.

- Trace + snapshot persistence.

Execution modes:

- `real`: execute actual CLI.

- `replay`: execute via `cli-replay`.

- `dry-run`: no side effects, only plan/preview.

Outputs:

- StepResult events (JSONL).

- State snapshots (memory dumps).

---

### 4.4 `runbook-debugger` (CLI-first UX)

Command:

```

runbook debug runbook.yaml

```

Capabilities:

- Step-by-step execution.

- Inspect variables and captures.

- View history.

- Submit manual evidence.

- Force snapshot.

Debugger commands:

- `next`

- `continue`

- `dump`

- `print vars`

- `print captures`

- `history`

- `evidence set`

- `evidence check`

- `evidence attach`

- `approve --as`

- `snapshot`

---

### 4.5 Integration: `cli-replay`

In replay mode:

- CLI steps are executed through `cli-replay exec`.

In real mode:

- CLI steps execute normally.

- Evidence captures command transcript hashes/previews.

---

## 5) Core Concepts & Data Models

---

### 5.1 Runbook YAML (v0)

```yaml

apiVersion: runbook/v0

meta:

  name: "example"

  description: "..."

  vars:

    namespace: "prod"

  governance:

    allowed_commands: ["kubectl", "az", "icm"]

    deny_env_vars: ["SECRET_*", "TOKEN", "AWS_*"]

    redact:

      - pattern: "(?i)password\\s*[:=]\\s*\\S+"

        replace: "password: <redacted>"

    evidence:

      require_for_manual: true

      store_full_stdout: false

steps:

  - id: check_pods

    type: cli

    with:

      argv: ["kubectl", "get", "pods", "-n", "{{ .namespace }}"]

    capture:

      pods: stdout

    assertions:

      - not_contains: "CrashLoopBackOff"

  - id: validate_portal

    type: manual

    title: "Validate metrics in portal"

    instructions: |

      Open dashboard X and confirm error_rate < 1%.

    required_evidence:

      - kind: checklist

        name: criteria

        items:

          - "Checked error_rate < 1%"

      - kind: text

        name: dashboard_link

      - kind: attachment

        name: screenshot

    approvals:

      min: 1

      roles: ["DRI"]

    replay_mode: reuse_evidence

```

---

### 5.2 Execution State (“Memory Dump”)

`RunState`:

- `run_id`

- `runbook_path`

- `mode`

- `started_at`

- `actor`

- `current_step_index`

- `vars`

- `captures`

- `history[]` (StepResults)

Snapshots stored as:

```

.runbook/runs/<run_id>/snapshots/step-0003.json

```

---

### 5.3 StepResult (Uniform Envelope)

```json

{

  "run_id": "...",

  "step_id": "check_pods",

  "status": "passed|failed|skipped",

  "actor": "engine|human",

  "started_at": "...",

  "ended_at": "...",

  "evidence": { "...": "..." },

  "captures": { "pods": "<hash or preview>" },

  "error": null

}

```

---

## 6) Extensibility Model (Providers)

Each step has a `type`.

Providers implement:

```

Validate(step)

Execute(step)

```

v0 providers:

- `cli`

- `manual`

Future:

- `aws.*`

- `azure.*`

- `k8s.*`

- `ssh.*`

Providers may register schema fragments for autocomplete.

Engine must remain sovereign:

- Providers cannot mutate global state outside StepResult.

- Providers cannot alter execution flow.

- Providers cannot bypass governance.

---

## 7) Manual Steps & Evidence (v0)

Manual steps:

- Pause execution.

- Require structured evidence.

Evidence types (v0):

- `text`

- `checklist`

- `attachment`

Attachments:

- Stored locally.

- sha256 + size recorded in trace.

Approvals:

- CLI flag `--as <name>`.

- No external identity integration in v0.

---

## 8) Replay & Validation Semantics

Replay mode:

- Requires scenario.yaml.

- CLI steps routed through `cli-replay`.

Real mode:

- Executes real CLI.

- Captures transcript hashes/previews.

Verify:

- All required steps satisfied.

- All manual evidence present.

- All assertions passed.

---

## 9) Governance & Safety (v0)

- Command allowlist enforced before execution.

- Deny env vars during template resolution.

- Redaction applied before storing output.

- Store hashes + previews by default.

- Optional blocklist for dangerous commands.

---

## 10) CLI Interface (v0)

### Compiler

```

runbook compile tsg.md --out runbook.yaml --mapping mapping.md

runbook validate runbook.yaml

```

### Runtime

```

runbook exec runbook.yaml --mode real

runbook exec runbook.yaml --mode replay --scenario scenario.yaml

runbook exec runbook.yaml --mode dry-run

```

### Debugger

```

runbook debug runbook.yaml --mode real|replay

```

---

## 11) Acceptance Criteria (v0)

- Compile Markdown TSG → schema-valid runbook.yaml.

- Generate mapping.md with section-to-step mapping.

- Execute runbook with:

  - ≥3 CLI steps.

  - ≥1 manual step.

- Produce:

  - JSONL trace.

  - ≥1 snapshot.

- Replay mode works via cli-replay.

- Validation fails on:

  - Missing evidence.

  - Disallowed command.

  - Assertion failure.

---

## 12) Implementation Notes

Language: Go (align with cli-replay).

Suggested libraries:

- Cobra (CLI)

- yaml.v3

- text/template

- JSON schema validation or strict struct decoding

Filesystem layout:

```

.runbook/

  runs/<run_id>/

    trace.jsonl

    snapshots/

    attachments/

```

---

## 13) Work Plan (Agent-Executable Milestones)

### Milestone 1 — Schema + Validator

- Define structs.

- Strict YAML parsing.

- JSON schema export.

- Validation rules.

### Milestone 2 — Runtime Core

- Step scheduler.

- CLI executor (real mode).

- Manual executor.

- Trace + snapshot writer.

### Milestone 3 — Debugger

- Interactive CLI loop.

- Step inspection.

- Evidence submission.

- Snapshot command.

### Milestone 4 — cli-replay Integration

- Replay mode via cli-replay.

- Verification integration.

### Milestone 5 — Compiler

- TSG-IR extractor.

- Agent prompt contract.

- Golden test set (5–10 TSGs).

- Diff-based validation harness.

---

## 14) Compiler Agent Prompt Contract

### Inputs

- Raw Markdown TSG.

- Runbook schema definition.

- Command ontology.

- Governance defaults.

### Outputs (MUST)

1) `runbook.yaml` (strictly schema-valid)

2) `mapping.md` with:

   - Step ID → TSG section mapping.

   - Extracted commands.

   - Inferred variables.

   - Manual steps.

   - TODOs/uncertainties.

### Rules

- If automation is unsafe or ambiguous → use `type: manual`.

- Never invent credentials or destructive commands.

- Explicit commands only when present in prose/code blocks.

- Uncertain variables → `meta.vars` placeholder + TODO.

---

## 15) Implementation Order (Recommended)

1. Schema + validation.

2. Runtime + trace.

3. Debugger.

4. cli-replay integration.

5. Compiler last (schema/runtime must be stable first).

---