# Implementation Plan: Gert Ecosystem v0

**Branch**: `002-ecosystem-v0` | **Date**: 2026-02-28 | **Spec**: [spec.md](spec.md)
**Input**: Feature specification from `/specs/002-ecosystem-v0/spec.md`

## Summary

Harden the gert kernel with Track 1 primitives (effects taxonomy, resumable approvals, kernel-owned input resolution, secrets, extension runner protocol, trace integrity, scoped state, visibility, keyed fan-out, principal attribution, repeat blocks) and build Track 2 host surfaces (TUI, MCP server, auto-record, outcome intelligence, watch mode). 38 functional requirements across 18 kernel changes and 9 ecosystem components.

## Technical Context

**Language/Version**: Go 1.25+
**Primary Dependencies**: gopkg.in/yaml.v3, github.com/spf13/cobra, github.com/invopop/jsonschema, github.com/santhosh-tekuri/jsonschema/v6, github.com/charmbracelet/bubbletea (TUI), github.com/mark3labs/mcp-go (MCP)
**Storage**: JSONL trace files (append-only), JSON state files (run persistence for resume)
**Testing**: Go `testing` package, scenario replay tests via `pkg/kernel/testing`
**Target Platform**: Linux, macOS, Windows (cross-platform CLI + libraries)
**Project Type**: Library (kernel) + CLI + TUI + MCP server
**Performance Goals**: Validation < 100ms, step execution overhead < 10ms, trace verify linear in event count
**Constraints**: Kernel packages must not import ecosystem packages; all kernel interfaces take `context.Context`; 72 existing tests must continue to pass
**Scale/Scope**: ~20 kernel source files modified, ~15 new ecosystem source files, 5 tool packs, 5 runbooks with 10+ scenarios

## Constitution Check

*GATE: Must pass before Phase 0 research. Re-check after Phase 1 design.*

| Principle | Status | Notes |
|-----------|--------|-------|
| I. Kernel-First Architecture | ✅ Pass | Track 1 modifies kernel; Track 2 is ecosystem-only. Dependency arrow correct. |
| II. Contract-Driven Governance | ✅ Pass | `effects` taxonomy replaces `side_effects`. Governance matches on contract properties. |
| III. Test-Driven Quality | ✅ Pass | Spec requires tests for every kernel change. SC-001 through SC-011 are test-based. |
| IV. Deterministic Execution | ✅ Pass | Keyed fan-out uses declaration order. Scope merge rules defined. Input resolution is host-independent. |
| V. Trace Everything | ✅ Pass | FR-009, FR-015, FR-016, FR-017, FR-025 add new trace events. Hash chaining on all events. |
| VI. Simplicity and YAGNI | ✅ Pass | MAD support via composable primitives, not special features. Visibility is intent-only in v0. |

No violations. No complexity justifications needed.

## Project Structure

### Documentation (this feature)

```text
specs/002-ecosystem-v0/
├── plan.md              # This file
├── research.md          # Phase 0 output
├── data-model.md        # Phase 1 output
├── quickstart.md        # Phase 1 output
├── contracts/           # Phase 1 output
│   ├── kernel-api.md    # Kernel interface contracts
│   └── cli-contract.md  # CLI command contracts
└── tasks.md             # Phase 2 output (from /speckit.tasks)
```

### Source Code (repository root)

```text
pkg/kernel/                      # Modified — kernel hardening (Track 1)
  contract/                      # Add Effects field, deprecate SideEffects
  schema/                        # Add scope, export, visibility, for_each.key, repeat, secrets, principal
  validate/                      # Add effects validation, secrets validation, scope/export rules
  engine/                        # Add ApprovalProvider, ResolveInputs, scoped state, keyed fan-out, repeat, probe mode
  governance/                    # Add effects matching, contract_violations matcher
  trace/                         # Add prev_hash, chain signing, principal, new event types
  executor/                      # Add extension runner (JSON-RPC stdio)
  replay/                        # No changes
  testing/                       # No changes

pkg/ecosystem/                   # New — ecosystem packages (Track 2)
  tui/                           # Bubble Tea wrapper
  mcp/                           # MCP server handlers
  approval/stdin/                # Default stdin approval provider
  recorder/                      # Auto-record tool response capture

cmd/
  gert/                          # New core CLI (replaces gert-kernel)
  gert-tui/                      # New TUI binary
  gert-mcp/                      # New MCP server binary

tools/                           # New tool packs
  curl.tool.yaml
  kubectl.tool.yaml
  az.tool.yaml
  ping.tool.yaml
  jq.tool.yaml

runbooks/                        # New stress-test runbooks
  service-health-diagnostic.yaml
  multi-endpoint-sweep.yaml
  k8s-pod-restart.yaml
  dns-http-chain.yaml
  incident-triage.yaml
```

**Structure Decision**: Kernel modifications in existing `pkg/kernel/` packages. New ecosystem code in `pkg/ecosystem/`. Three CLI binaries in `cmd/`. Tool packs and runbooks at repo root.

## Complexity Tracking

No constitution violations — no complexity justifications needed.
