# Implementation Plan: Governed Executable Runbook Engine v0

**Branch**: `001-runbook-engine-v0` | **Date**: 2026-02-11 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/001-runbook-engine-v0/spec.md`

## Summary

Build a governed, executable runbook platform that validates YAML runbook definitions against a strict schema, executes steps (CLI + manual) as a deterministic state machine with full governance enforcement, provides an interactive CLI debugger, supports offline replay via `cli-replay`, and compiles prose TSGs from Markdown into schema-valid runbooks. The Go CLI is the primary interface; a TypeScript VS Code extension shares schema contracts for future UX parity.

## Technical Context

**Language/Version**: Go (latest stable) for CLI; TypeScript (strict mode) for VS Code extension  
**Primary Dependencies**: Cobra (CLI framework), gopkg.in/yaml.v3 (strict YAML), text/template (variable resolution), VS Code Extension API  
**Storage**: Local filesystem — `.runbook/runs/<run_id>/` for traces, snapshots, attachments  
**Testing**: `go test` for Go; VS Code extension test framework for TypeScript; shared golden-file fixtures in `testdata/`  
**Target Platform**: Cross-platform CLI (Linux, macOS, Windows); VS Code extension (all VS Code platforms)  
**Project Type**: Dual-stack (Go CLI + TypeScript extension) sharing a canonical JSON Schema  
**Performance Goals**: Schema validation <5s for 500-step runbooks; compilation <60s for 50-section TSGs  
**Constraints**: Single-user local execution; sequential steps only; no network dependencies at runtime  
**Scale/Scope**: v0 targets individual on-call engineers; runbooks up to 500 steps; local artifact storage only

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| # | Principle | Status | Evidence |
|---|-----------|--------|----------|
| I | Schema-First Design | ✅ PASS | Canonical JSON Schema in `schemas/`; Go structs + TS types validated against it; strict parsing rejects unknowns (FR-001); apiVersion versioning in schema |
| II | Governance by Default | ✅ PASS | Allowlist/denylist enforced before execution (FR-012); env var blocking (FR-014); redaction before persistence (FR-013); evidence required for manual steps (FR-018, FR-022) |
| III | Deterministic Execution | ✅ PASS | Sequential state machine (FR-007); JSONL trace for all transitions (FR-015); snapshots at step boundaries (FR-016); replay parity (FR-028) |
| IV | Safe by Default | ✅ PASS | Halt on failure (FR-011a); dry-run with zero side effects (FR-008); compiler emits manual for unsafe steps (FR-032); no auto-remediation |
| V | Provider Sovereignty | ✅ PASS | Providers implement only Validate+Execute (FR-035); no global state mutation (FR-036); no flow alteration (FR-037) |
| VI | Dual-Stack Contract Parity | ✅ PASS | Single canonical JSON Schema shared; shared test fixtures in `testdata/`; identical validation accept/reject in both stacks |
| VII | Test-First Development | ✅ PASS | Contract tests for schema boundaries; integration tests for execution flows; replay parity tests; cross-stack golden-file tests |

**Gate result: PASS** — no violations. Proceeding to Phase 0.

### Post-Design Re-Check (after Phase 1)

| # | Principle | Status | Evidence |
|---|-----------|--------|----------|
| I | Schema-First Design | ✅ PASS | data-model.md defines all entities from schema; 3-phase validation pipeline (structural → semantic → domain); JSON Schema generated from Go structs |
| II | Governance by Default | ✅ PASS | CLI provider validates argv[0] against allowlist; governance in ExecutionContext; redaction in provider Execute flow |
| III | Deterministic Execution | ✅ PASS | RunState snapshots at each boundary; append-only JSONL trace; CommandExecutor interface enables replay determinism |
| IV | Safe by Default | ✅ PASS | Halt-on-failure; DryRunCollector for zero side effects; compiler emits manual for unsafe steps |
| V | Provider Sovereignty | ✅ PASS | 5 invariants in provider-contract.md; no mutation path outside StepResult |
| VI | Dual-Stack Contract Parity | ✅ PASS | Schema pipeline: Go structs → invopop/jsonschema → runbook-v0.json → ajv; shared testdata/ fixtures |
| VII | Test-First Development | ✅ PASS | testdata/ with valid/, invalid/, scenarios/, tsgs/; contract + integration + parity test strategy |

**Post-design gate result: PASS** — no new violations introduced by design artifacts.

## Project Structure

### Documentation (this feature)

```text
specs/001-runbook-engine-v0/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
└── tasks.md             # Phase 2 output (/speckit.tasks command)
```

### Source Code (repository root)

```text
cmd/
├── gert/
│   └── main.go              # CLI entry point
│
pkg/
├── schema/
│   ├── schema.go            # Runbook Go structs + strict YAML parsing
│   ├── validate.go          # Validation rules (governance, vars, uniqueness)
│   └── export.go            # JSON Schema export
├── runtime/
│   ├── engine.go            # State machine: step scheduling, variable resolution
│   ├── trace.go             # JSONL trace writer
│   ├── snapshot.go          # State snapshot persistence
│   └── resume.go            # Execution resumption from snapshot
├── governance/
│   ├── allowlist.go         # Command allowlist/denylist evaluation
│   ├── redaction.go         # Output redaction engine
│   └── envblock.go          # Denied env var pattern blocking
├── providers/
│   ├── provider.go          # Provider interface (Validate + Execute)
│   ├── cli.go               # CLI step provider (real + timeout)
│   └── manual.go            # Manual step provider (evidence + approvals)
├── debugger/
│   ├── debugger.go          # Interactive REPL loop
│   └── commands.go          # Debugger command handlers
├── replay/
│   ├── replay.go            # cli-replay integration
│   └── scenario.go          # Scenario file parsing
├── compiler/
│   ├── ir.go                # TSG-IR extraction (headings, code blocks)
│   ├── compile.go           # IR → runbook.yaml + mapping.md
│   └── prompt.go            # LLM prompt contract
├── evidence/
│   ├── evidence.go          # Evidence types (text, checklist, attachment)
│   └── hash.go              # SHA256 hashing + file size recording
└── assertions/
    └── assertions.go        # Assertion evaluation (contains, matches, json_path, etc.)

vscode/
├── src/
│   ├── extension.ts         # VS Code extension entry point
│   ├── schema/
│   │   └── validate.ts      # TypeScript schema validation (parity with Go)
│   └── views/
│       └── runbook.ts       # Runbook viewer/editor components
├── package.json
└── tsconfig.json

schemas/
└── runbook-v0.json          # Canonical JSON Schema (shared by Go + TS)

testdata/
├── valid/                   # Golden-file valid runbooks
├── invalid/                 # Golden-file invalid runbooks (expected errors)
├── scenarios/               # Replay scenario fixtures
└── tsgs/                    # Sample TSG Markdown files for compiler tests
```

**Structure Decision**: Dual-stack layout per constitution principle VI. Go code under `cmd/` + `pkg/` follows standard Go project conventions. TypeScript extension under `vscode/` with its own package.json. Canonical JSON Schema in `schemas/` consumed by both. Shared test fixtures in `testdata/`.

## Complexity Tracking

> No violations — all constitution gates pass. No complexity justifications needed.
