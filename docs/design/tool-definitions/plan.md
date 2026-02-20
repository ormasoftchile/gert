# Tool Definitions — Implementation Plan

## Overview

This plan implements the tool definitions design in 6 phases. Each phase is
independently shippable and adds value without breaking existing runbooks.
Estimated total: 3–4 weeks of focused work.

---

## Phase 1: Schema & Validation (2–3 days)

**Goal:** Parse `.tool.yaml` files, add `tools:` to runbooks, validate `type: tool` steps.

### Tasks

1. **Create `pkg/schema/tool.go`** — Go structs for `ToolDefinition`, `ToolMeta`,
   `ToolTransport`, `ToolAction`, `ToolArg`, `ToolCapture`, `ToolGovernance`,
   `ActionGovernance`
   - `LoadTool(path string) (*ToolDefinition, error)` — parse + strict YAML decode
   - Reuse the same `yaml.v3` strict-decode pattern from `schema.go`

2. **Add `ToolStepConfig` to `Step` struct** in `schema.go`:
   ```go
   Tool *ToolStepConfig `yaml:"tool,omitempty" json:"tool,omitempty"`
   ```
   Add `"tool"` to the `Type` enum: `jsonschema:"enum=cli,enum=manual,enum=xts,enum=invoke,enum=tool"`

3. **Add `Tools map[string]string` to `Runbook` struct** in `schema.go`:
   ```go
   Tools map[string]string `yaml:"tools,omitempty" json:"tools,omitempty"`
   ```

4. **Tool definition validation** in `pkg/schema/tool.go`:
   - `apiVersion` must be `tool/v0`
   - `meta.name` and `meta.binary` required
   - At least one action
   - Action has `argv` (stdio) or `method` (jsonrpc) or `mcp_tool` (mcp)
   - Required args don't have defaults
   - Enum args have `type: string`
   - `approval_min` requires `requires_approval`

5. **Step validation** — extend `validate.go`:
   - `type: tool` requires `tool.name` and `tool.action`
   - `tool.name` must exist in `tools:` map
   - Load referenced `.tool.yaml`, check action exists
   - Validate required args present, enum values valid

6. **JSON Schema** — update `schemas/runbook-v0.json`:
   - Add `tools` to top-level properties
   - Add `tool` step type
   - Create `schemas/tool-v0.json` for `.tool.yaml` files

7. **Tests:**
   - `pkg/schema/tool_test.go` — valid/invalid tool definitions
   - Update `validate_test.go` — tool step validation
   - Test fixtures: `testdata/tools/kubectl.tool.yaml`, `testdata/tools/invalid/`

### Deliverables
- `gert validate` catches tool reference errors
- VS Code extension gets red squiggles on invalid tool steps
- No execution yet — just parsing and validation

---

## Phase 2: stdio Execution (2–3 days)

**Goal:** `type: tool` steps execute via stdio (spawn per call) — the default transport.

### Tasks

1. **Create `pkg/tools/manager.go`**:
   ```go
   type Manager struct {
       defs      map[string]*schema.ToolDefinition
       baseDirs  map[string]string
       executor  providers.CommandExecutor
   }
   func NewManager(executor providers.CommandExecutor) *Manager
   func (m *Manager) Load(alias, path, baseDir string) error
   func (m *Manager) Execute(ctx context.Context, alias, action string, args map[string]string) (*ActionResult, error)
   func (m *Manager) ValidateStep(cfg *schema.ToolStepConfig) []string
   ```

2. **Create `pkg/tools/stdio.go`**:
   - Resolve action `argv` templates against provided args
   - Validate args (required, enum, type) before execution
   - Build full command: `[meta.binary] + resolved_argv`
   - Call `executor.Execute(ctx, binary, argv, nil)`
   - Apply capture rules to stdout
   - Apply arg-level redaction to evidence
   - Return `ActionResult{Stdout, Stderr, ExitCode, Captures, Duration}`

3. **Wire into engine** — `pkg/runtime/engine.go`:
   - Add `ToolManager *tools.Manager` field to `Engine`
   - Add `case "tool":` to `executeStep` switch → `e.executeToolStep(ctx, step, result)`
   - `executeToolStep`: resolve template vars in `tool.args`, call `ToolManager.Execute`,
     map captures to step result

4. **Wire into serve layer** — `pkg/serve/serve.go`:
   - On `exec/start`: load tool definitions from `runbook.Tools` map into manager
   - `type: tool` steps execute via `executeTreeStep` like CLI steps (no special handling)
   - Extension events include `tool: {name, action}` for rendering

5. **Governance integration**:
   - `tool_manager.Execute` checks action governance before dispatch
   - `requires_approval: true` → return `ActionResult{Status: "awaiting_approval"}`
   - Serve layer presents approval UI (same as existing `step.approvals`)
   - Arg `redact: true` → redact value in evidence and traces

6. **Tests:**
   - `pkg/tools/manager_test.go` — load, validate, execute
   - `pkg/tools/stdio_test.go` — argv resolution, capture, redaction
   - Integration scenario: `testdata/testing/tool-step/` with mock commands

### Deliverables
- `type: tool` steps work end-to-end in `gert exec` and VS Code
- Governance (approval, redaction) enforced from tool definition
- Replay/scenario support via shared executor

---

## Phase 3: Extension UX (2–3 days)

**Goal:** VS Code extension renders tool steps with structured information.

### Tasks

1. **Serve layer enrichment** — include tool metadata in events:
   ```json
   {
     "stepId": "check_pods",
     "type": "tool",
     "tool": {
       "name": "kubectl",
       "action": "get-pods",
       "args": {"namespace": "default", "selector": "app=web"},
       "governance": {"read_only": true}
     }
   }
   ```

2. **Active Step panel** — `runbookPanel.ts`:
   - Detect `type === 'tool'` steps
   - Render tool name, action, structured args table
   - Show governance badge: 🔒 read-only / ⚠ requires approval
   - Approval dialog for destructive actions

3. **Workflow map** — render tool steps with action name:
   ```
   ✓  🔧 1  Check pod status          kubectl.get-pods
   ●  🔧 2  Delete crashing pod       kubectl.delete-pod  ⚠
   ```

4. **YAML language support** — `syntaxes/`:
   - Syntax highlighting for `.tool.yaml` files
   - Register `tool/v0` as a recognized `apiVersion`

5. **Tests:**
   - Extension render tests for tool step HTML
   - Manual smoke test with kubectl.tool.yaml fixture

### Deliverables
- Tool steps look distinct from raw CLI steps in the UI
- Governance constraints visible before execution
- Approval workflow works for destructive actions

---

## Phase 4: jsonrpc Transport (3–4 days)

**Goal:** Tools can run as persistent JSON-RPC servers, spawned once and reused.

### Tasks

1. **Create `pkg/tools/jsonrpc.go`**:
   ```go
   type jsonrpcProcess struct {
       cmd      *exec.Cmd
       stdin    io.WriteCloser
       stdout   *bufio.Reader
       nextID   int64
       mu       sync.Mutex
   }
   func spawnJSONRPC(ctx context.Context, binary string, argv []string, readySignal string, timeout time.Duration) (*jsonrpcProcess, error)
   func (p *jsonrpcProcess) Call(method string, params map[string]interface{}) (json.RawMessage, error)
   func (p *jsonrpcProcess) Shutdown(method string, gracePeriod time.Duration) error
   ```

2. **Process lifecycle in Manager**:
   - `processes map[string]*jsonrpcProcess` — keyed by tool alias
   - First `Execute` for a jsonrpc tool → `spawnJSONRPC`
   - Subsequent calls → reuse process
   - `Shutdown()` → iterate all processes, send shutdown method, SIGTERM

3. **Action dispatch for jsonrpc**:
   - Build params from validated args
   - Call `process.Call(action.Method, params)`
   - Parse response, extract captures via `capture.from` (dot-path into JSON result)
   - Apply redaction

4. **Ready signal detection**:
   - Read stderr line-by-line until `ready_signal` substring appears
   - Timeout → kill process, return error with context

5. **Error recovery**:
   - Process crash detection (stdout EOF)
   - Auto-respawn on next `Execute` call
   - Error surfaced to user with "tool process crashed, restarting"

6. **Wire into Manager.Execute**:
   - Check `transport.mode`
   - `"stdio"` → existing stdio path
   - `"jsonrpc"` → get-or-spawn process, call method

7. **Engine/serve integration** — transparent, just works via Manager

8. **Tests:**
   - Mock JSON-RPC server binary for testing (Go test helper)
   - Test spawn, ready wait, call/response, shutdown
   - Test crash recovery
   - Test concurrent calls (mutex)

### Deliverables
- Stateful tools work (connection pooling, expensive startup amortized)
- XTS can be wrapped as a JSON-RPC server behind a tool definition
- Process lifecycle managed automatically

---

## Phase 5: MCP Transport (3–4 days)

**Goal:** MCP servers usable as gert tools — spawn or connect.

### Tasks

1. **Create `pkg/tools/mcp.go`**:
   - MCP client that speaks the tool-call subset of the protocol
   - Two modes:
     - **Spawn:** start binary, initialize MCP session over stdio
     - **Connect:** HTTP/SSE to existing server URL

2. **MCP session management**:
   ```go
   type mcpProcess struct {
       transport mcpTransport  // stdio or http
       tools     map[string]mcpToolSchema  // discovered via tools/list
   }
   func spawnMCP(ctx context.Context, ...) (*mcpProcess, error)
   func connectMCP(ctx context.Context, url string) (*mcpProcess, error)
   func (p *mcpProcess) CallTool(name string, args map[string]interface{}) (json.RawMessage, error)
   ```

3. **Action dispatch for MCP**:
   - Map `action.mcp_tool` to MCP tool name
   - Build arguments from validated step args
   - Send `tools/call` request
   - Parse response content, extract captures

4. **Tool discovery** (optional enhancement):
   - On connect, call `tools/list` to discover available tools
   - Validate that `mcp_tool` references exist on the server
   - Warn on mismatch

5. **Wire into Manager.Execute**:
   - `"mcp"` → get-or-spawn/connect, call tool

6. **Tests:**
   - Mock MCP server for testing
   - Test spawn + connect modes
   - Test tool call / response / error
   - Test with real Azure MCP server (integration, optional)

### Deliverables
- Any MCP server works as a gert tool
- Azure MCP, Playwright MCP, GitHub MCP importable with typed governance
- Runbooks can mix stdio, jsonrpc, and mcp tools

---

## Phase 6: XTS Migration (2–3 days)

**Goal:** Desugar `type: xts` to `type: tool` internally. Path to removing XTS provider.

### Tasks

1. **Create `tools/xts.tool.yaml`** — standard tool definition for XTS:
   - Actions: `query`, `view`, `activity`
   - Args map to current `XTSStepConfig` fields
   - Transport: `jsonrpc` (or `stdio` wrapping existing CLI)

2. **Desugar `type: xts` in engine**:
   - When engine encounters `type: xts`, convert to equivalent `type: tool` step:
     ```go
     func desugarXTS(step *schema.Step, xtsMeta *schema.XTSMeta) *schema.ToolStepConfig {
         return &schema.ToolStepConfig{
             Name:   "__xts_builtin",
             Action: step.XTS.Mode,
             Args:   map[string]string{
                 "query_type":  step.XTS.QueryType,
                 "query":       step.XTS.Query,
                 "environment": step.XTS.Environment,
                 // ...
             },
         }
     }
     ```
   - Register `__xts_builtin` as a built-in tool (loaded from embedded YAML, not a file)

3. **Compiler update** — `pkg/compiler/`:
   - New compilations emit `type: tool` + `tools:` import for XTS steps
   - Old `type: xts` output preserved behind a flag for backward compat

4. **Deprecation warnings** — `gert validate`:
   - `type: xts` → info: "Consider migrating to type: tool with xts.tool.yaml"
   - Not an error — old runbooks keep working

5. **Tests:**
   - Existing XTS test scenarios pass with desugared execution
   - New tool-based XTS scenarios side-by-side

### Deliverables
- `type: xts` still works but routes through tool manager internally
- New runbooks use `type: tool` for XTS
- Clear migration path documented

---

## Dependency Graph

```
Phase 1: Schema & Validation
    │
    ├──→ Phase 2: stdio Execution
    │        │
    │        ├──→ Phase 3: Extension UX
    │        │
    │        ├──→ Phase 4: jsonrpc Transport
    │        │        │
    │        │        └──→ Phase 6: XTS Migration
    │        │
    │        └──→ Phase 5: MCP Transport
    │
    └──(independent)
```

- Phases 1 → 2 are sequential (can't execute without schema)
- Phases 3, 4, 5 are parallel after Phase 2
- Phase 6 depends on Phase 4 (XTS uses jsonrpc transport)

## Testing Strategy Per Phase

| Phase | Unit Tests | Integration/Scenario | Extension |
|---|---|---|---|
| 1 | tool.go parse, validate.go rules | `gert validate` on fixtures | Schema squiggles |
| 2 | manager, stdio execution | `gert test` with tool scenarios | Runs in VS Code |
| 3 | — | — | Render tests, smoke test |
| 4 | jsonrpc spawn, call, shutdown | Persistent tool scenario | Transparent |
| 5 | mcp client, spawn/connect | MCP tool scenario | Transparent |
| 6 | desugar logic | Existing XTS scenarios pass | Transparent |

## Risk Assessment

| Risk | Impact | Mitigation |
|---|---|---|
| Tool process crashes mid-runbook | Step fails, user confused | Auto-restart + clear error message |
| MCP protocol drift | Connect mode breaks | Pin to stable MCP spec version, test against real servers |
| Governance bypass via raw `cli` | Users avoid tool governance | Lint rule: "this command has a tool definition, use `type: tool`" |
| Tool definition sprawl | Too many `.tool.yaml` files | Standard library repo, compiler auto-references |
| Performance: subprocess overhead | Slower than in-process XTS | jsonrpc transport eliminates per-call overhead |
| Approval UX friction | Engineers skip destructive checks | Approval bypass requires governance exception + audit trail |

## Success Criteria

- [x] `type: tool` steps execute correctly with all three transports
- [x] Existing `type: xts` runbooks continue working without modification
- [x] Tool governance (approval, redaction) enforced end-to-end
- [x] Extension renders tool steps with structured args and governance badges
- [x] At least one real tool definition (kubectl or XTS) validated by a team
- [x] `gert test` scenarios cover happy path, error, approval, and crash recovery
- [x] No regressions in existing cli/manual/invoke step types
