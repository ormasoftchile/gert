# Feature Specification: Gert Ecosystem v0

**Feature Branch**: `002-ecosystem-v0`  
**Created**: 2026-02-28  
**Status**: Draft  
**Input**: Implement gert ecosystem v0 — kernel hardening (Track 1) and host surfaces (Track 2) as defined in ecosystem-v0.md
## User Scenarios & Testing *(mandatory)*

### User Story 1 — Tool Pack Author Creates Contract-Governed Tools (Priority: P1)

A tool author creates tool definition YAML files with the new `effects` taxonomy (replacing deprecated `side_effects`), declares secrets requirements, and validates them. The kernel correctly classifies risk from effects + writes, and governance rules evaluate using effects rather than legacy boolean flags.

**Why this priority**: The `effects` taxonomy is the foundation every other Track 1 item depends on. Without it, governance is inconsistent and no other kernel hardening can proceed reliably.

**Independent Test**: Author writes `kubectl.tool.yaml` with `effects: [kubernetes]`, `writes: [pods]` for delete action and `effects: [kubernetes]`, `writes: []` for get action. `gert validate` passes, dry-run shows correct risk classification, and governance rules matching on `effects: [kubernetes]` trigger correctly.

**Acceptance Scenarios**:

1. **Given** a tool definition with `effects: [network]` and no `side_effects` field, **When** `gert validate` runs, **Then** validation passes with no warnings.
2. **Given** a tool definition with `side_effects: true` and no `effects` field, **When** `gert validate` runs, **Then** validation passes with a deprecation warning and auto-migrates to `effects: [unknown]`.
3. **Given** a tool definition with both `side_effects` and `effects` declared, **When** `gert validate` runs, **Then** validation fails with an error.
4. **Given** a tool with `effects: [kubernetes]` and `writes: [production]`, **When** governance rule `effects: [kubernetes], writes: [production] → require-approval` exists, **Then** the engine triggers approval for that step.
5. **Given** a tool with `effects: [network]` and `writes: []`, **When** the engine evaluates risk, **Then** derived risk is "Low" (effects but no writes).
6. **Given** a tool definition with `secrets: [{env: API_KEY, required: true}]`, **When** `gert validate` runs and `API_KEY` is not set, **Then** validation emits a warning listing the missing secret.

---

### User Story 2 — Operator Runs Runbook with Resumable Approval Gate (Priority: P1)

An operator executes a runbook containing a step that requires approval (critical risk). The engine submits an approval ticket, persists state, and either waits or exits for later resume. The approver responds asynchronously, and the run completes.

**Why this priority**: Approval is the governance interaction point for all high-risk operations. Without resumable approvals, gert cannot support Teams/Slack/PagerDuty workflows. Blocking approvals limit gert to terminal-only use.

**Independent Test**: Run a runbook with a `require-approval` governance rule. Engine emits `approval_submitted` trace event with ticket ID, persists state, and can be resumed with `gert resume --run <id>` after approval arrives.

**Acceptance Scenarios**:

1. **Given** a step with critical risk requiring approval, **When** the engine evaluates governance, **Then** it calls `ApprovalProvider.Submit()` and receives an `ApprovalTicket` immediately (non-blocking).
2. **Given** a pending approval ticket in synchronous mode (stdin), **When** the operator types "y", **Then** `Wait()` returns approved and the step executes.
3. **Given** a pending approval ticket in async mode, **When** the engine is configured for resume, **Then** it persists state to `runs/<run-id>/state.json` and exits with code 0.
4. **Given** a persisted run state, **When** `gert resume --run <id>` is invoked after approval, **Then** the engine loads state, verifies ticket status, and continues execution from the pending step.
5. **Given** an approval response with `require_verified_approval: true`, **When** the signature is invalid, **Then** the approval is treated as rejected and a `contract_violation` trace event is emitted.
6. **Given** an approval with `min_approvers: 2`, **When** only 1 approval is received, **Then** the engine waits for the second approval before proceeding.

---

### User Story 3 — Runbook Author Uses Scoped State and Keyed Fan-Out (Priority: P2)

A runbook author writes a multi-agent or multi-endpoint pattern using `for_each` with `key`, `scope` for isolation, `visibility` for information control, and `export` to promote results to global scope.

**Why this priority**: Scoped state, keyed outputs, and visibility are the MAD-enabling primitives. They also support common operational patterns (multi-endpoint sweeps, parallel diagnostics with isolated results).

**Independent Test**: Write a runbook with `for_each.key` producing a map, `scope: "round.0"` for isolation, `export: ["response"]` to promote, and `visibility` constraints. `gert validate` passes, engine produces map-structured outputs, and trace includes `scope_export` and `visibility_applied` events.

**Acceptance Scenarios**:

1. **Given** a step with `for_each.key: "{{ .agent.id }}"`, **When** executed with 3 agents, **Then** outputs are a map keyed by agent ID, not a list.
2. **Given** two steps in `scope: "round.0"`, **When** both complete, **Then** scope variables are visible to each other but not to steps in `scope: "round.1"`.
3. **Given** a step with `export: ["decision"]`, **When** executed, **Then** `decision` is available globally as `{{ .decision }}` to subsequent steps.
4. **Given** `export: ["response"]` on a step where `response` is not in `contract.outputs`, **When** `gert validate` runs, **Then** validation fails with an error.
5. **Given** a step with `visibility: {allow: ["question"], deny: ["scope.round.0.*"]}`, **When** executed, **Then** the trace includes a `visibility_applied` event with allow/deny globs and a filtered view hash.
6. **Given** YAML `scope: "round/0"`, **When** the kernel processes it, **Then** it normalizes to dot form `scope.round.0` for storage and template access.

---

### User Story 4 — All Hosts Resolve Inputs Via Kernel API (Priority: P2)

CLI, MCP server, and TUI all resolve runbook inputs through the kernel's `ResolveInputs()` API, ensuring consistent resolution order, trace provenance, and determinism across all host surfaces.

**Why this priority**: Input resolution affects determinism and trace meaning. Host-specific input pre-processing would fragment behavior and break replay.

**Independent Test**: Call `ResolveInputs()` with a runbook that has `from: provider/cmdb.hostname`, `from: prompt`, and `default` inputs. Verify resolution order, trace events record source per input, and replay reproduces the same resolution.

**Acceptance Scenarios**:

1. **Given** a runbook input with `from: provider/cmdb.server.hostname`, **When** `ResolveInputs()` is called with a configured CMDB resolver, **Then** the input is resolved from the provider and the trace records `{ source: "provider/cmdb" }`.
2. **Given** a runbook input with CLI override `--var hostname=x`, **When** the same input has a `from: provider/...` binding, **Then** the CLI value wins.
3. **Given** a required input with no CLI override, no provider, and no default, **When** `ResolveInputs()` is called, **Then** it returns an error with the input name and available sources.
4. **Given** a replay scenario with recorded input sources, **When** replayed, **Then** the same resolved values are produced regardless of current provider state.

---

### User Story 5 — Auditor Verifies Trace Integrity (Priority: P2)

An auditor receives a JSONL trace file and verifies it has not been tampered with by checking the hash chain and verifying the run's signature against a known key.

**Why this priority**: Trace integrity is required for compliance (SOC2, FedRAMP) and is the basis for trust in gert's audit trail.

**Independent Test**: Run a runbook with `GERT_TRACE_SIGNING_KEY` set. Verify `run.jsonl` contains `prev_hash` on every event, `run_complete` includes `chain_hash` + `signature`. Run `gert trace verify` and confirm chain integrity + signature validity.

**Acceptance Scenarios**:

1. **Given** a trace file from a completed run, **When** `gert trace verify` is called, **Then** it walks the hash chain and reports "N events, no breaks."
2. **Given** a trace file where one event has been modified, **When** `gert trace verify` is called, **Then** it reports the break point and affected event index.
3. **Given** a trace with a `signing_key_id`, **When** `gert trace verify --key-id prod-2026` is called with the correct key, **Then** it reports "Signature valid."
4. **Given** a trace signed with key A, **When** verified with key B, **Then** verification fails.
5. **Given** a `run_start` event, **Then** it includes `actor`, `host`, `gert_version`, `runbook_hash`, and `tool_hashes`.

---

### User Story 6 — Operator Uses TUI for Incident Response (Priority: P3)

An operator launches `gert-tui` during an incident, sees the step list with status icons, watches tool output in real-time, and approves governance gates via the TUI interface.

**Why this priority**: The TUI is the primary interactive experience for incident responders. However, it depends on stable kernel interfaces from Track 1, so it's sequenced after primitives.

**Independent Test**: Run `gert-tui examples/service-health-check.yaml --mode replay --scenario healthy`. Verify step list panel shows progress, output panel shows tool responses, and the run completes with outcome displayed.

**Acceptance Scenarios**:

1. **Given** `gert-tui <runbook>` is launched, **When** the engine executes steps, **Then** the step list updates with status icons per step.
2. **Given** a tool step executes, **When** stdout is captured, **Then** the output panel displays it in real-time.
3. **Given** a step requires approval, **When** governance triggers, **Then** the TUI shows an approval prompt with risk level and contract info.
4. **Given** `--mode replay --scenario healthy`, **When** the TUI runs, **Then** it replays using canned responses with no interactive prompts.

---

### User Story 7 — AI Agent Validates and Executes Runbooks via MCP (Priority: P3)

An AI agent connects to `gert-mcp` and invokes `gert/validate`, `gert/exec`, and `gert/test` operations. The MCP server returns structured results that the agent can reason about.

**Why this priority**: MCP positions gert as the execution backend for AI-driven incident response. Depends on stable kernel interfaces.

**Independent Test**: Start `gert-mcp` via stdio, send MCP `tools/call` for `gert/validate` with a valid runbook path, verify structured response with validation status.

**Acceptance Scenarios**:

1. **Given** an MCP client connects to `gert-mcp`, **When** it calls `gert/validate` with a runbook path, **Then** it receives validation result with error/warning list.
2. **Given** an MCP client calls `gert/exec` with dry-run mode, **When** the runbook has 3 steps, **Then** the response includes per-step contract and governance info.
3. **Given** an MCP client calls `gert/test`, **When** 3 scenarios exist, **Then** the response includes per-scenario pass/fail status and assertion details.

---

### User Story 8 — Developer Uses Watch Mode for Health Monitoring (Priority: P3)

A developer runs `gert watch` to repeatedly execute a health check runbook on an interval, with automatic trace output per run and stop-on-escalation semantics.

**Why this priority**: Developer convenience for soak testing and light monitoring. No kernel changes required.

**Independent Test**: Run `gert watch` with `--interval 5s --stop-on escalated` in replay mode and verify it runs multiple times, prints one-line summaries, and stops on the specified outcome.

**Acceptance Scenarios**:

1. **Given** `gert watch` with `--interval 5s`, **When** the runbook resolves successfully three times, **Then** console shows three one-line summaries with timestamps.
2. **Given** `--stop-on escalated`, **When** a run produces an `escalated` outcome, **Then** the loop stops with a clear message.
3. **Given** a run that errors before reaching an outcome, **When** the loop detects the error, **Then** it stops (errors are not retried).

---

### Edge Cases

- What happens when a `for_each.key` produces duplicate keys across iterations? → Runtime error, no silent overwrite.
- What happens when `export` collides with an existing global variable? → Runtime error, documented collision.
- What happens when a provider fails during `ResolveInputs()`? → Error with structured context, trace records failure.
- What happens when an extension runner crashes mid-execution? → Step status `error`, trace records crash.
- What happens when `gert resume` is called with an expired approval ticket? → Treated as rejection, run halts.
- What happens when a `repeat` block hits max iterations without `until` being true? → Block exits normally, last iteration state persists.
- What happens when probe mode encounters a tool with `effects: [database]` and only `[network]` is allowed? → Tool is skipped (dry-run behavior).

## Requirements *(mandatory)*

### Functional Requirements

**Track 1 — Kernel Hardening**

- **FR-001**: Kernel MUST support `effects` field on contracts as a list of system-level effect categories
- **FR-002**: Kernel MUST deprecate `side_effects` boolean — accept with warning, error if both declared
- **FR-003**: Governance rules MUST support matching on `effects` and `writes` alongside existing risk classification
- **FR-004**: Derived risk MUST be informational only; enforcement MUST be policy-driven via governance rules
- **FR-005**: Kernel MUST expose `ApprovalProvider` interface with `Submit()` returning a ticket and `Wait()` for optional blocking
- **FR-006**: Engine MUST persist run state when approval is pending in async mode, enabling `gert resume`
- **FR-007**: Kernel MUST expose `ResolveInputs()` API that all hosts call for input resolution
- **FR-008**: Resolution order MUST be: CLI vars → provider → prompt → default → error if required
- **FR-009**: Each resolved input MUST produce a trace event recording its source
- **FR-010**: Tool and runbook schemas MUST support `secrets` block declaring required environment variables
- **FR-011**: Secret values MUST never appear in traces — only secret names
- **FR-012**: Values of declared secrets MUST be auto-redacted from tool stdout/stderr in traces
- **FR-013**: Extension step runner MUST communicate via JSON-RPC 2.0 over stdio with initialize/execute/shutdown methods
- **FR-014**: Extension runner outputs MUST be checked against `contract.outputs` — undeclared outputs stripped with violation event
- **FR-015**: Every trace event MUST include `prev_hash` (SHA-256 of previous event JSON) for hash chaining
- **FR-016**: `run_complete` event MUST include `chain_hash` and optional `signature` for trace signing
- **FR-017**: `run_start` event MUST include actor, host, gert_version, runbook_hash, and tool_hashes
- **FR-018**: Engine MUST detect contract violations at runtime (undeclared outputs, missing outputs, deterministic inconsistency)
- **FR-019**: Governance MUST support `contract_violations: deny` rule to promote violations to step errors
- **FR-020**: Probe mode MUST execute only tools with `writes == []` and effects in an allowed list (default: `[network]`)
- **FR-021**: Schema MUST support `scope` field on steps for variable namespace isolation
- **FR-022**: Schema MUST support `export` field on steps to promote scope-local outputs to global namespace
- **FR-023**: Schema MUST support `visibility` field with `allow`/`deny` glob patterns, recorded in trace
- **FR-024**: `for_each` MUST support `key` field producing map-structured outputs instead of list
- **FR-025**: Trace events MUST support `principal` attribution with kind/id/role/model fields
- **FR-026**: Schema MUST support `repeat` block with `max` and `until` for bounded multi-step iteration
- **FR-027**: All provider interfaces MUST accept `context.Context` for cancellation and timeout propagation
- **FR-028**: Scope paths MUST use dot-separated canonical form; YAML `/` is normalized to `.` by kernel
- **FR-029**: Visibility globs MUST use `*` (one segment) and `**` (any depth); deny overrides allow

**Track 2 — Host Surfaces**

- **FR-030**: `gert-tui` MUST be a separate binary importing kernel packages + Bubble Tea
- **FR-031**: TUI MUST display step list with status icons, output panel, and status bar
- **FR-032**: `gert-mcp` MUST be a separate binary exposing gert operations as MCP tools
- **FR-033**: MCP server MUST support `gert/validate`, `gert/exec`, `gert/test`, and `gert/schema` tools
- **FR-034**: Auto-record mode MUST capture tool responses and evidence into a replayable scenario directory
- **FR-035**: Recorded scenarios MUST apply trace redaction rules — secret values never in scenario files
- **FR-036**: `gert diff` MUST re-run golden scenarios against current runbook and report outcome changes
- **FR-037**: `gert outcomes` MUST aggregate structured outcomes from trace files with configurable time window
- **FR-038**: `gert watch` MUST run a runbook in a loop with configurable interval and stop conditions

### Key Entities

- **Contract**: Behavioral declaration — `effects`, `reads`, `writes`, `idempotent`, `deterministic`, contract outputs
- **ApprovalTicket**: Pending approval — ticket ID, status (pending/approved/rejected/expired), creation time
- **ApprovalResponse**: Resolution — approver ID, method, signature (with alg + key ID), timestamp
- **Scope**: Named variable namespace isolating state within a run (e.g., `round.0`)
- **Principal**: Actor attribution — kind (system/human/agent), id, role, model
- **SecretRef**: Required environment variable — name, description, required flag
- **InputBinding**: Input resolution spec — name, `from` source, type, default, required
- **TraceChain**: Sequence of hash-linked JSONL events with optional final signature

## Success Criteria *(mandatory)*

### Measurable Outcomes

- **SC-001**: All 5 planned runbooks validate and pass 2+ scenario tests each
- **SC-002**: Governance rules matching on `effects` produce correct decisions for 100% of test cases
- **SC-003**: Resumable approval round-trips complete successfully for synchronous and async modes
- **SC-004**: `ResolveInputs()` produces identical results across CLI, MCP, and TUI for same inputs
- **SC-005**: `gert trace verify` detects single-event tampering in 100% of test cases
- **SC-006**: All trace files include complete hash chains with no genesis gaps
- **SC-007**: `gert-tui` completes a full runbook replay with visual step progression
- **SC-008**: `gert-mcp` responds correctly to all 4 standard MCP tool calls
- **SC-009**: Auto-recorded scenarios replay identically to the original run
- **SC-010**: Extension runner protocol completes successfully for at least one extension
- **SC-011**: Probe mode executes read-only tools and skips write tools with correct trace output

## Assumptions

- The kernel/v0 implementation (Phases 1-5) is stable and all 72 existing tests pass
- Tool binaries (curl, kubectl, az, ping, jq) are available on PATH in the development environment
- Go 1.25+ is the build toolchain
- The `effects` taxonomy replaces `side_effects` — old packs must migrate
- MCP SDK for Go is available and functional
- Bubble Tea and lipgloss are the TUI framework
- Extension runners are trusted executables — the kernel does not sandbox them

## Scope Boundaries

**In scope**: All Track 1 kernel changes (1a-1i), all Track 2 ecosystem surfaces (2a-2f), MAD-ready primitives, 5 tool packs + 5 runbooks with scenario tests

**Out of scope**: Distributed execution, built-in secrets management, web UI, governance DSL v2, plugin marketplace, MAD-specific step types, full visibility enforcement (v0 is intent + trace only)

## Dependencies

- `kernel-v0.md` — kernel types and engine behavior
- `ecosystem-v0.md` — design document defining Track 1 + Track 2
- Go MCP SDK — for `gert-mcp` binary
- Bubble Tea — for `gert-tui` binary
- Existing 72 kernel tests must continue to pass
