# Research: Gert Ecosystem v0

**Feature**: 002-ecosystem-v0
**Date**: 2026-02-28
**Status**: Complete

## Research Tasks

### R1: context.Context propagation through engine step dispatch

**Decision**: Thread `context.Context` through `Engine.Run()` → `executeSteps()` → `executeStep()` → all provider calls (ToolExecutor, ApprovalProvider, InputResolver).

**Rationale**: Go's standard cancellation pattern. All long-running operations (tool execution, approval wait, input resolution) must respect context cancellation. The engine creates a per-step child context with the step's timeout if specified.

**Alternatives considered**:
- Global timeout on `RunConfig` only → rejected: per-step timeouts are specified in the schema
- Channel-based cancellation → rejected: context.Context is idiomatic Go and composes with stdlib

**Impact**: `Engine.Run()` signature changes to `Run(ctx context.Context)`. All `ToolExecutor`, `ApprovalProvider`, `InputResolver` interfaces already take `context.Context` per the spec.

### R2: JSON-RPC 2.0 over stdio for extension runner protocol

**Decision**: Use `github.com/sourcegraph/jsonrpc2` (well-maintained, stdio-friendly) or implement minimal JSON-RPC 2.0 client using `encoding/json` + `bufio.Scanner` over stdin/stdout pipes.

**Rationale**: Extension runner protocol has only 3 methods (initialize, execute, shutdown). A full JSON-RPC library is overkill. A ~100-line custom implementation using `json.Encoder`/`json.Decoder` over `exec.Cmd.StdinPipe()`/`StdoutPipe()` is simpler, has zero dependencies, and matches the existing tool executor pattern.

**Alternatives considered**:
- `github.com/sourcegraph/jsonrpc2` → viable but adds a dependency for 3 methods
- `github.com/creachadair/jrpc2` → heavier, designed for servers not clients
- gRPC → rejected: too heavy for extension runners, requires protobuf compilation

**Impact**: New file `pkg/kernel/executor/extension.go` with ~150 lines. No new dependencies.

### R3: SHA-256 hash chaining in trace writer

**Decision**: Compute `prev_hash` inline in `trace.Writer.Emit()`. After encoding the event JSON (without `prev_hash`), compute SHA-256 of the previous event's JSON bytes (cached), insert `prev_hash` field, then write.

**Rationale**: The trace writer already serializes events sequentially under a mutex. Adding a SHA-256 computation (~1μs per event) is negligible overhead. Caching the previous event's JSON bytes avoids re-serialization.

**Alternatives considered**:
- Post-processing hash chain (compute after run) → rejected: breaks append-only semantics; can't verify partial traces
- Streaming hash (hash entire trace as one blob) → rejected: doesn't allow per-event verification
- External hash chain service → rejected: unnecessary complexity

**Impact**: `trace.Writer` gains a `prevJSON []byte` field and `prevHash string` field. `Emit()` computes SHA-256 before writing. ~20 lines changed in `trace.go`.

### R4: MCP SDK for gert-mcp server

**Decision**: Use `github.com/mark3labs/mcp-go` — the most mature Go MCP SDK as of 2026. It supports stdio transport, tool registration, and resource serving.

**Rationale**: Building MCP from scratch would require implementing the full protocol (tool discovery, call/response, resources, notifications). The SDK handles protocol compliance. `gert-mcp` would register 4-6 tools and serve them over stdio.

**Alternatives considered**:
- Custom MCP implementation → rejected: protocol is non-trivial (capability negotiation, pagination, notifications)
- HTTP-only MCP (no SDK) → rejected: VS Code and most AI agents use stdio transport

**Impact**: New dependency in `go.mod`: `github.com/mark3labs/mcp-go`. New package `pkg/ecosystem/mcp/` with handler registration. New binary `cmd/gert-mcp/main.go`.

### R5: Bubble Tea model architecture for TUI

**Decision**: Single Bubble Tea `Model` that owns an `engine.Engine` and receives trace events via a channel. The model dispatches engine execution in a background goroutine; trace events drive UI updates via `tea.Cmd`.

**Rationale**: Bubble Tea's architecture is message-driven. The engine runs asynchronously; trace events are converted to Bubble Tea messages that update the step list, output panel, and status bar. This matches the existing old TUI pattern in `pkg/tui/app.go`.

**Alternatives considered**:
- Synchronous engine execution (block UI per step) → rejected: freezes the TUI during tool execution
- Separate process (spawn gert-kernel, parse stdout) → rejected: loses type safety and adds IPC complexity
- Direct engine polling → rejected: Bubble Tea is event-driven, not poll-driven

**Impact**: New package `pkg/ecosystem/tui/` with `model.go`, `views.go`, `keys.go`. Imports `bubbletea`, `lipgloss`, kernel packages. New binary `cmd/gert-tui/main.go`.

### R6: State persistence format for gert resume

**Decision**: JSON file at `runs/<run-id>/state.json` containing: variable state, current step index, trace file path, pending approval ticket, runbook path. Resume loads this file, re-creates the engine with saved state, and continues from the pending step.

**Rationale**: JSON is simple, human-readable, and debuggable. The state is small (variable map + position). No database or binary format needed.

**Alternatives considered**:
- Binary gob encoding → rejected: not debuggable, Go-specific
- SQLite → rejected: overkill for single-run state
- Trace replay (re-execute from start using trace) → rejected: slow for long runs, requires full replay infrastructure

**Impact**: New file `pkg/kernel/engine/state.go` with `SaveState()` and `LoadState()`. ~80 lines.
