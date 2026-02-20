# Tool Definitions — Typed, Governed, Importable Tool Abstractions

## Status: proposed

## Problem

Today gert has three step types for executing commands:

1. **`cli`** — raw argv, no validation beyond governance allowlist, no structured args
2. **`xts`** — hardcoded provider with custom config, deeply coupled to gert internals
3. **`manual`** — no execution, just human guidance

This creates several pain points:

- **No reuse.** The same `kubectl get pods -n {{ .namespace }}` argv is copy-pasted
  across dozens of runbooks. A flag rename or auth change means updating them all.
- **No structured validation.** `cli` accepts any argv. A typo in `--namepsace` passes
  validation and blows up at runtime.
- **XTS is hardcoded.** Adding a new provider (DSConsole, Geneva Actions, Kusto CLI)
  requires modifying gert's Go source: new schema struct, new engine case, new serve
  handler, new extension rendering. This doesn't scale.
- **Governance is coarse.** The allowlist operates on `argv[0]` (binary name). There's
  no way to say "allow `kubectl get` but require approval for `kubectl delete`."
- **No extension UX.** `cli` steps render as opaque command text. There's no
  autocomplete, inline docs, or structured arg display.

## Proposal

A new file format `.tool.yaml` that declares a tool's binary, transport, actions,
typed arguments, capture rules, and governance constraints. Runbooks import tools by
alias (like `imports:` for sub-runbooks) and invoke actions via a new `type: tool` step.

### Goals

1. Tools are declared once, imported everywhere — single source of truth
2. Structured args with types, enums, defaults, required flags — validated at load time
3. Per-action governance: read-only, requires-approval, arg redaction
4. Three transport modes: stdio (spawn per call), jsonrpc (persistent), mcp (ecosystem)
5. XTS and future providers migrate to tool definitions — gert core becomes tool-agnostic
6. Extension can render typed args, autocomplete tool/action names, show inline docs

### Non-goals

- Replacing `type: cli` — it stays for ad-hoc one-off commands
- Replacing `type: manual` — human steps are not tool invocations
- Building a tool registry/marketplace (future work)

## Schema: Tool Definition File

```yaml
# tools/kubectl.tool.yaml
apiVersion: tool/v0

meta:
  name: kubectl
  version: "1.0"
  description: Kubernetes command-line tool
  binary: kubectl

transport:
  mode: stdio                  # stdio | jsonrpc | mcp

governance:
  read_only: true              # default for all actions
  redact:
    - pattern: "Bearer [A-Za-z0-9+/=]+"
      replace: "Bearer [REDACTED]"

actions:
  get-pods:
    description: List pods in a namespace with optional label selector
    argv: ["get", "pods", "-n", "{{ .namespace }}", "-l", "{{ .selector }}", "-o", "json"]
    args:
      namespace:
        type: string
        required: true
        description: Kubernetes namespace
      selector:
        type: string
        required: false
        default: ""
        description: Label selector (e.g. app=myservice)
    capture:
      stdout: { format: json }
    governance:
      read_only: true

  delete-pod:
    description: Delete a specific pod (destructive)
    argv: ["delete", "pod", "{{ .pod_name }}", "-n", "{{ .namespace }}"]
    args:
      pod_name:
        type: string
        required: true
      namespace:
        type: string
        required: true
    governance:
      read_only: false
      requires_approval: true
      approval_min: 1

  apply:
    description: Apply a manifest file
    argv: ["apply", "-f", "{{ .manifest }}"]
    args:
      manifest:
        type: string
        required: true
        description: Path to the YAML manifest
    governance:
      read_only: false
      requires_approval: true
```

### Transport Modes

#### `stdio` (default)

Spawn the binary per action call. The action's `argv` is appended to `meta.binary`.
Stdout/stderr are captured. Process exits after each call.

```yaml
transport:
  mode: stdio
```

Effective command: `kubectl get pods -n default -l app=web -o json`

This is the simplest mode and works for any CLI tool.

#### `jsonrpc`

Spawn a long-lived process that speaks JSON-RPC 2.0 over stdio. The process is
started once per runbook execution and reused across all steps that reference the tool.

```yaml
transport:
  mode: jsonrpc
  binary: xts-server
  startup:
    argv: ["--mode", "server", "--cluster", "{{ .cluster }}"]
    ready_signal: "listening"    # wait for this line on stderr
    timeout: 10s
    shutdown_method: "shutdown"  # JSON-RPC method sent before SIGTERM
```

Each action maps to a JSON-RPC method:

```yaml
actions:
  query:
    method: xts/query            # JSON-RPC method name (instead of argv)
    args:
      query_type: { type: string, required: true }
      query: { type: string, required: true }
    capture:
      result: { from: result.data, format: json }
```

Request sent to the tool process:
```json
{"jsonrpc":"2.0","id":1,"method":"xts/query","params":{"query_type":"wql","query":"SELECT ..."}}
```

**Lifecycle:** gert spawns the process on first use, waits for `ready_signal`,
sends requests, and sends `shutdown_method` + SIGTERM when the runbook completes.

#### `mcp`

Connect to an MCP (Model Context Protocol) server. The tool's actions map to MCP
tool names.

```yaml
transport:
  mode: mcp
  binary: npx
  startup:
    argv: ["-y", "@azure/mcp-server"]
    ready_signal: "MCP server"
    timeout: 30s
```

Or connect to an existing MCP server:

```yaml
transport:
  mode: mcp
  connect: http://localhost:3000/mcp
```

Each action maps to an MCP tool call:

```yaml
actions:
  query-resources:
    mcp_tool: azure_resources_query    # MCP tool name
    args:
      query: { type: string, required: true }
      subscription: { type: string, required: false }
    capture:
      result: { from: result, format: json }
```

This makes every MCP server in the ecosystem available as a gert tool with
typed args, governance, and evidence capture layered on top.

## Schema: Runbook Usage

### `tools:` top-level block

Peer of `imports:`, maps aliases to `.tool.yaml` file paths:

```yaml
apiVersion: runbook/v0

imports:
  dns-check: ../checks/dns-check.runbook.yaml

tools:
  kubectl: ../tools/kubectl.tool.yaml
  xts: ../tools/xts.tool.yaml
  copilot: ../tools/copilot.tool.yaml

meta:
  name: diagnose-pod-crash
  kind: mitigation
```

### `type: tool` step

```yaml
tree:
  - step:
      id: check_pods
      type: tool
      title: Check pod status in namespace
      tool:
        name: kubectl            # references tools.kubectl alias
        action: get-pods         # references actions.get-pods
        args:                    # typed, validated at load time
          namespace: "{{ .namespace }}"
          selector: "app={{ .app_name }}"
      capture:
        pod_list: stdout
      outcomes:
        - when: '{{ eq (len (fromJson .pod_list).items) 0 }}'
          state: escalated
          recommendation: No pods found — deployment may have failed

  - step:
      id: restart_pod
      type: tool
      title: Delete the crashing pod
      tool:
        name: kubectl
        action: delete-pod
        args:
          pod_name: "{{ .crashing_pod }}"
          namespace: "{{ .namespace }}"
      # governance from tool def: requires_approval=true, approval_min=1
      # gert will require human approval before executing this step
```

## Go Types

### Tool Definition

```go
// pkg/schema/tool.go

// ToolDefinition represents a .tool.yaml file.
type ToolDefinition struct {
    APIVersion string                `yaml:"apiVersion"  json:"apiVersion"  jsonschema:"const=tool/v0"`
    Meta       ToolMeta              `yaml:"meta"        json:"meta"`
    Transport  ToolTransport         `yaml:"transport,omitempty" json:"transport,omitempty"`
    Governance *ToolGovernance       `yaml:"governance,omitempty" json:"governance,omitempty"`
    Actions    map[string]ToolAction `yaml:"actions"     json:"actions"     jsonschema:"required,minProperties=1"`
}

type ToolMeta struct {
    Name        string `yaml:"name"        json:"name"        jsonschema:"required"`
    Version     string `yaml:"version,omitempty" json:"version,omitempty"`
    Description string `yaml:"description,omitempty" json:"description,omitempty"`
    Binary      string `yaml:"binary"      json:"binary"      jsonschema:"required"`
}

type ToolTransport struct {
    Mode    string          `yaml:"mode,omitempty"    json:"mode,omitempty"    jsonschema:"enum=stdio,enum=jsonrpc,enum=mcp,default=stdio"`
    Binary  string          `yaml:"binary,omitempty"  json:"binary,omitempty"` // override meta.binary for server mode
    Connect string          `yaml:"connect,omitempty" json:"connect,omitempty"` // URL for remote MCP
    Startup *ToolStartup    `yaml:"startup,omitempty" json:"startup,omitempty"`
}

type ToolStartup struct {
    Argv           []string `yaml:"argv,omitempty"           json:"argv,omitempty"`
    ReadySignal    string   `yaml:"ready_signal,omitempty"   json:"ready_signal,omitempty"`
    Timeout        string   `yaml:"timeout,omitempty"        json:"timeout,omitempty"  jsonschema:"pattern=^[0-9]+(s|m|h)$"`
    ShutdownMethod string   `yaml:"shutdown_method,omitempty" json:"shutdown_method,omitempty"`
}

type ToolAction struct {
    Description string                 `yaml:"description,omitempty" json:"description,omitempty"`
    Argv        []string               `yaml:"argv,omitempty"        json:"argv,omitempty"`        // stdio mode
    Method      string                 `yaml:"method,omitempty"      json:"method,omitempty"`      // jsonrpc mode
    MCPTool     string                 `yaml:"mcp_tool,omitempty"    json:"mcp_tool,omitempty"`    // mcp mode
    Args        map[string]ToolArg     `yaml:"args,omitempty"        json:"args,omitempty"`
    Capture     map[string]ToolCapture `yaml:"capture,omitempty"     json:"capture,omitempty"`
    Governance  *ActionGovernance      `yaml:"governance,omitempty"  json:"governance,omitempty"`
}

type ToolArg struct {
    Type        string   `yaml:"type"                  json:"type"        jsonschema:"required,enum=string,enum=int,enum=bool,enum=float"`
    Required    bool     `yaml:"required,omitempty"    json:"required,omitempty"`
    Default     string   `yaml:"default,omitempty"     json:"default,omitempty"`
    Description string   `yaml:"description,omitempty" json:"description,omitempty"`
    Enum        []string `yaml:"enum,omitempty"        json:"enum,omitempty"`
    Redact      bool     `yaml:"redact,omitempty"      json:"redact,omitempty"`
}

type ToolCapture struct {
    From   string `yaml:"from,omitempty"   json:"from,omitempty"`   // jsonpath for jsonrpc/mcp results
    Format string `yaml:"format,omitempty" json:"format,omitempty"` // text | json
}

type ToolGovernance struct {
    ReadOnly bool            `yaml:"read_only,omitempty" json:"read_only,omitempty"`
    Redact   []RedactionRule `yaml:"redact,omitempty"    json:"redact,omitempty"`
}

type ActionGovernance struct {
    ReadOnly         bool `yaml:"read_only,omitempty"         json:"read_only,omitempty"`
    RequiresApproval bool `yaml:"requires_approval,omitempty" json:"requires_approval,omitempty"`
    ApprovalMin      int  `yaml:"approval_min,omitempty"      json:"approval_min,omitempty"`
}
```

### Step additions

```go
// In schema.go — Step struct gets a new field
type ToolStepConfig struct {
    Name   string            `yaml:"name"             json:"name"   jsonschema:"required"`
    Action string            `yaml:"action"           json:"action" jsonschema:"required"`
    Args   map[string]string `yaml:"args,omitempty"   json:"args,omitempty"`
}

type Step struct {
    // ... existing fields ...
    Type  string          `yaml:"type" json:"type" jsonschema:"required,enum=cli,enum=manual,enum=xts,enum=invoke,enum=tool"`
    Tool  *ToolStepConfig `yaml:"tool,omitempty" json:"tool,omitempty"`
}
```

### Runbook struct additions

```go
type Runbook struct {
    APIVersion string            `yaml:"apiVersion" json:"apiVersion"`
    Imports    map[string]string `yaml:"imports,omitempty" json:"imports,omitempty"`
    Tools      map[string]string `yaml:"tools,omitempty"  json:"tools,omitempty"`  // alias → path
    Meta       Meta              `yaml:"meta" json:"meta"`
    // ...
}
```

## New Package: `pkg/tools`

```go
// pkg/tools/manager.go

// Manager handles tool lifecycle: loading definitions, spawning processes,
// routing action calls, and shutdown.
type Manager struct {
    defs       map[string]*schema.ToolDefinition  // loaded tool definitions by alias
    processes  map[string]*toolProcess             // live jsonrpc/mcp processes
    baseDirs   map[string]string                   // tool alias → directory for relative path resolution
    executor   providers.CommandExecutor            // for stdio mode — shares replay executor
    mu         sync.Mutex
}

// Load parses and validates a .tool.yaml file.
func (m *Manager) Load(alias string, path string) error

// Execute runs a tool action and returns the result.
// Dispatches to stdio/jsonrpc/mcp based on transport mode.
func (m *Manager) Execute(ctx context.Context, alias string, action string, args map[string]string) (*ActionResult, error)

// Shutdown gracefully stops all persistent tool processes.
func (m *Manager) Shutdown(ctx context.Context) error

// ValidateStep checks that a tool step references a loaded tool with a valid action
// and that all required args are present with valid types/enums.
func (m *Manager) ValidateStep(step *schema.ToolStepConfig) []string
```

```go
// pkg/tools/stdio.go   — spawn binary, collect stdout/stderr
// pkg/tools/jsonrpc.go — manage persistent process, send/receive JSON-RPC
// pkg/tools/mcp.go     — MCP protocol client (spawn or connect)
```

### ActionResult

```go
type ActionResult struct {
    Stdout   string            // raw stdout (stdio) or serialized result (jsonrpc/mcp)
    Stderr   string
    ExitCode int
    Captures map[string]string // extracted via capture rules
    Duration time.Duration
}
```

## Execution Flow

### stdio mode

```
1. Engine hits type:tool step
2. Manager.Execute("kubectl", "get-pods", {namespace: "default", selector: "app=web"})
3. Manager looks up tool def → kubectl.tool.yaml
4. Finds action "get-pods", validates args
5. Resolves argv templates: ["get", "pods", "-n", "default", "-l", "app=web", "-o", "json"]
6. Checks governance: read_only=true → ok, no approval needed
7. Applies arg redaction rules
8. Calls executor.Execute("kubectl", argv, nil) — same as cli steps
9. Applies capture rules to stdout
10. Applies output redaction rules
11. Returns ActionResult
```

### jsonrpc mode

```
1. Engine hits type:tool step referencing "xts"
2. Manager.Execute("xts", "query", {query_type: "wql", query: "SELECT ..."})
3. Manager checks processes["xts"] — not running yet
4. Spawns: xts-server --mode server --cluster mycluster
5. Reads stderr until "listening" appears (ready_signal)
6. Sends: {"jsonrpc":"2.0","id":1,"method":"xts/query","params":{...}}
7. Reads response: {"jsonrpc":"2.0","id":1,"result":{"data":"..."}}
8. Extracts captures via "from" jsonpath: result.data
9. Returns ActionResult

   ... later steps reuse the same process ...

10. Runbook ends → Manager.Shutdown()
11. Sends: {"jsonrpc":"2.0","method":"shutdown"}
12. Waits grace period → SIGTERM
```

### mcp mode

```
1. Engine hits type:tool step referencing "azure"
2. Manager.Execute("azure", "query-resources", {query: "..."})
3. Manager checks processes["azure"] — not running
4. If transport.connect set: connect to http://localhost:3000/mcp
   Else: spawn binary with startup.argv, wait for ready_signal
5. Sends MCP tool call: {tool: "azure_resources_query", arguments: {...}}
6. Reads MCP response
7. Extracts captures, applies redaction
8. Returns ActionResult
```

## Governance Model

Governance evaluates in three layers:

```
 ┌──────────────────────────────────┐
 │ 1. Runbook-level governance      │  allowed_commands, denied_commands
 │    (existing — schema.go)        │  redact rules, deny_env_vars
 ├──────────────────────────────────┤
 │ 2. Tool-level governance         │  read_only default, tool-wide redaction
 │    (tool.yaml → governance:)     │
 ├──────────────────────────────────┤
 │ 3. Action-level governance       │  read_only override, requires_approval,
 │    (action → governance:)        │  approval_min, per-arg redact
 └──────────────────────────────────┘
```

**Resolution order:**
1. If action declares `governance`, use it
2. Else inherit from tool-level `governance`
3. Runbook-level `governance.allowed_commands` still applies to the binary name
4. Arg-level `redact: true` → value is redacted in logs, evidence, and traces

**Approval enforcement:**

When `requires_approval: true`, the serve layer presents the step as `awaiting_approval`
instead of executing immediately. The extension shows an approval dialog. The step
executes only after `approval_min` approvals are recorded via the evidence collector.

This is the same mechanism as `step.approvals` today, but declared at the tool level
so every runbook that uses `kubectl.delete-pod` gets approval enforcement automatically.

## Validation Rules

### Tool definition validation (`gert validate-tool`)

1. `apiVersion` must be `tool/v0`
2. `meta.name` and `meta.binary` required
3. `actions` must have at least one entry
4. Each action must have `argv` (stdio), `method` (jsonrpc), or `mcp_tool` (mcp)
   matching the transport mode
5. `args` with `required: true` must not have `default`
6. `args` with `enum` must have `type: string`
7. `governance.approval_min` requires `requires_approval: true`
8. `transport.connect` only valid for `mode: mcp`
9. `startup` only valid for `mode: jsonrpc` or `mode: mcp` without `connect`

### Runbook step validation (`gert validate`)

1. `type: tool` requires `tool.name` and `tool.action`
2. `tool.name` must be a key in `tools:` (or a warning if inline path)
3. Tool definition file must exist and parse
4. `tool.action` must exist in the tool definition's `actions`
5. All `required: true` args must be present in `tool.args`
6. Args with `enum` must have a value in the enum list (or a template expression)
7. Unknown args (not in action's `args`) produce a warning

## Extension UX

### Autocomplete

The VS Code extension reads imported tool definitions and provides:
- `tool.name:` → completion list of imported tool aliases
- `tool.action:` → completion list of actions for the selected tool
- `tool.args:` → completion list of arg names with descriptions and types
- Enum args → completion list of allowed values

### Step rendering

Tool steps in the Active Step panel show structured information:

```
┌─────────────────────────────────────────────────┐
│  🔧  Check pod status in namespace    passed    │
│  check_pods                                     │
│                                                 │
│  TOOL     kubectl                               │
│  ACTION   get-pods                              │
│                                                 │
│  ARGS                                           │
│  namespace    default                           │
│  selector     app=myservice                     │
│                                                 │
│  CAPTURES                                       │
│  pod_list     [{"name":"web-abc","status":...}] │
│                                                 │
│  🔒 read-only                                   │
└─────────────────────────────────────────────────┘
```

Destructive actions show an approval banner:

```
┌─────────────────────────────────────────────────┐
│  ⚠️  Delete the crashing pod       awaiting     │
│  restart_pod                                    │
│                                                 │
│  TOOL     kubectl                               │
│  ACTION   delete-pod  ⚠ REQUIRES APPROVAL       │
│                                                 │
│  ARGS                                           │
│  pod_name     web-abc-12345                     │
│  namespace    production                        │
│                                                 │
│  [ Approve & Execute ]  [ Reject ]              │
└─────────────────────────────────────────────────┘
```

### Workflow map

Tool steps show the tool icon and action name:

```
✓  🔧 1  Check pod status          kubectl.get-pods
●  🔧 2  Delete crashing pod       kubectl.delete-pod  ⚠ approval
○  🔧 3  Verify recovery           kubectl.get-pods
```

## XTS Migration Path

| Phase | What happens | Breaking? |
|---|---|---|
| **1. Ship `type: tool`** | New step type alongside existing. Tool manager, validation, extension UX. | No |
| **2. Publish `xts.tool.yaml`** | Standard tool definition for XTS. Teams can import it. | No |
| **3. Compiler emits `type: tool`** | New compilations produce `type: tool` for XTS steps. | No |
| **4. Desugar `type: xts`** | Engine internally converts `type: xts` to `type: tool` + built-in xts.tool.yaml. Old runbooks keep working. | No |
| **5. Deprecate `type: xts`** | Validation warns on `type: xts`. Migration guide published. | Soft |
| **6. Remove XTS provider** | Delete `pkg/providers/xts.go`, XTS-specific schema fields, engine/serve special cases. | Yes (major) |

After phase 6, gert's core knows only: `cli`, `manual`, `invoke`, `tool`.
Everything else is a tool definition file.

## Testing Strategy

### Tool definition loading

- Valid tool definitions parse without error
- Missing required fields fail validation
- Invalid enum values, arg types, transport combos fail
- Governance inheritance resolves correctly

### stdio execution

- Tool step spawns binary with resolved argv
- Required args missing → validation error
- Enum arg with invalid value → validation error
- Capture extracts stdout correctly
- Arg redaction masks values in evidence/logs
- Governance `requires_approval` blocks execution until approved

### jsonrpc execution

- First use spawns process, waits for ready_signal
- Second use reuses existing process
- Timeout during startup → error with context
- Shutdown sends method and SIGTERM
- Process crash mid-runbook → error, auto-restart on next use

### mcp execution

- Spawn mode: starts binary, connects via MCP protocol
- Connect mode: connects to existing URL
- Tool call maps args to MCP tool arguments
- Response captured and extracted

### Replay/scenario support

- Tool steps in scenario files mock responses like CLI steps
- jsonrpc and mcp modes fall through to mock executor in replay

### Extension

- Autocomplete for tool names, actions, args
- Structured rendering in Active Step panel
- Approval dialog for destructive actions
- Workflow map shows tool.action

## Open Questions

- [ ] Should tool definitions support versioning/semver constraints?
- [ ] Should there be a standard library of tool definitions (e.g. `gert-tools` repo)?
- [ ] How do tool definitions compose with `invoke`? (invoke a runbook that uses tools)
- [ ] Should `capture.from` support jsonpath for nested result extraction?
- [ ] How does `gert compile` handle tools? Inline the definition? Reference by hash?
- [ ] Should tool processes be shared across `invoke` boundaries? (parent + child share kubectl process)
- [ ] Auth: should tool definitions declare auth requirements (kubeconfig, tokens, certs)?
- [ ] Should `mode: jsonrpc` support SSE/streamable HTTP transport in addition to stdio?

## File Layout

```
gert/
  pkg/
    tools/
      manager.go         # Tool lifecycle, loading, dispatch
      manager_test.go
      stdio.go           # Spawn-per-call executor
      jsonrpc.go         # Persistent JSON-RPC client
      mcp.go             # MCP protocol client
      validate.go        # Tool definition + step validation
  schemas/
    tool-v0.json         # JSON Schema for .tool.yaml
  docs/
    design/
      tool-definitions/
        design.md        # this document
  testdata/
    tools/
      kubectl.tool.yaml  # test fixture
      xts.tool.yaml      # test fixture
    testing/
      tool-step/         # scenario tests
```
