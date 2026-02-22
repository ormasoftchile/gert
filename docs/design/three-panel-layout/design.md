# Three-Panel Layout

## Status: collecting

## Problem
When running a TSG, the engineer needs to see three things simultaneously:
1. The source material (prose TSG or runbook YAML) — reference context
2. The execution flow — where am I, what branches exist, what's done/pending
3. The current step detail — instructions, evidence forms, query results, actions

## Target UX (from TSG-ToolKit prototype)

The TSG-ToolKit prototype (7 screenshots in this folder) established the target experience.
The prototype was built purely to feel the DRI experience — it is not a product to integrate with.

### Layout (validated by prototype)

```
┌──────────────────┬─────────────────────────┬──────────────────────┐
│  LEFT PANEL      │  MIDDLE PANEL           │  RIGHT PANEL         │
│                  │                         │                      │
│  Source material │  WORKFLOW MAP (GUIDED)  │  ACTIVE STEP         │
│  (user chooses): │                         │                      │
│  • prose TSG     │  Collapsible tree of    │  Step type badge:    │
│  • runbook YAML  │  steps with:            │  COMMAND / MANUAL /  │
│                  │  • state dots (●○)      │  QUERY               │
│  Standard VS Code│  • OR branch connectors │                      │
│  text editor     │  • "one of N paths"     │  Instructions text   │
│                  │    annotations          │                      │
│                  │  • collapsible sections │  Evidence fields:    │
│                  │                         │  • text inputs       │
│                  │  Webview panel          │  • checkboxes        │
│                  │                         │  • choice/radio      │
│                  │                         │                      │
│                  │                         │  Action buttons:     │
│                  │                         │  • Submit Evidence   │
│                  │                         │  • Next Step         │
│                  │                         │  • Retry / Skip /    │
│                  │                         │    Abort (commands)  │
└──────────────────┴─────────────────────────┴──────────────────────┘
                   │                         │
                   │  SIDEBAR (left edge)    │
                   │  TSG EXPLORER tree      │
                   │  + INCIDENT section     │
                   │    (loads ICM data)     │
                   └─────────────────────────┘
```

### What the prototype proved

1. **WORKFLOW MAP** is the primary navigation surface — not raw YAML.
   It renders the decision tree as a collapsible list with visual state indicators
   (red = failed, blue = current, green = completed, white = pending).
   Branch points show `OR` connectors and `— one of N paths:` labels.

2. **ACTIVE STEP** panel adapts to step type:
   - **COMMAND**: shows the command text, stdout/stderr, exit code, Retry/Skip/Abort buttons
   - **MANUAL**: shows instructions, evidence input fields (text, checkbox, choice/radio), Submit Evidence + Next Step buttons
   - **QUERY**: shows query type badge, query text, results (future: inline table)

3. **Evidence types** need to include `choice` (radio buttons for mutually exclusive options)
   in addition to text/checklist/attachment. This is critical for decision points
   where the engineer picks one path (e.g., "User Pool" vs "Kernel Mode" vs "System Pool").

4. **Branch structure** must be explicit in the schema — not implicit via `when:` guards on flat steps.
   The prototype uses `branches:` with `condition:` which directly maps to the collapsible
   tree rendering. Gert's current flat `steps[]` + `when:` cannot represent:
   - Nested branches (branch within a branch, e.g. datacenter-outage-response screenshot)
   - `OR` connectors between sibling branches
   - "one of N paths" annotations
   - Collapsible sections in the workflow map

5. **INCIDENT sidebar section** loads ICM data directly, feeding into `meta.inputs`.
   This is the UI surface for the ICM binding layer.

6. **Step state tracking** across the workflow map uses colored indicators that update
   in real-time as execution progresses. The map auto-highlights the current step.

### Screenshots (in this folder)

| File | TSG | What it shows |
|------|-----|--------------|
| image.png | catch-process-terminator | COMMAND step failed (exit code 1), Retry/Skip/Abort |
| image copy.png | data-loss-guidelines | MANUAL step, nested branches (WARN CUSTOMER, PITR REQUESTED), checkbox evidence |
| image copy 2.png | datacenter-outage-response | Deep branching: SQL IMPACTED → FULL/PARTIAL OUTAGE → RECOVERING/RECOVERED |
| image copy 3.png | debug-ha | QUERY step, long HA step list (HA140, HA200, HA1000...), red/blue/white state dots |
| image copy 4.png | get-kusto-cluster-info | MANUAL step with text input fields (kusto_cluster_uri, kusto_database) |
| image copy 5.png | alias-db-high-backoff-duration | MANUAL step with checkbox, ESCALATE TO GATEWAY QUEUE branch |
| image copy 6.png | cas-with-psa | URGENT vs STANDARD branching with is_urgent checkbox, R2D compliance flow |
| image copy 7.png | GitHub PR layout | Reference: timeline spine, inline status, progressive disclosure, conflict banner |

### UX Patterns from GitHub PR Layout (image copy 7)

The GitHub PR view is a reference for elegant information-dense UX. Key patterns to adopt:

**1. Timeline spine with inline status**
GitHub shows `✓ 8040fe3` on every commit — status + identifier in one glance, no expansion needed.
Apply to workflow map: each step line shows `✓ assess_failures` / `✗ check_replicas` / `○ escalate`.
Status dot + step ID + optional one-line summary, all visible without clicking.

**2. Progressive disclosure**
The PR timeline is collapsed by default — you see the shape (how many commits, what events)
before drilling into detail. The workflow map should do the same: collapsed branch groups
show a summary ("3 steps, 1 failed") and expand on click to reveal individual steps.

**3. Warning banner with action button**
GitHub's "This branch has conflicts that must be resolved" banner is prominent, inline, and
has a clear "Resolve conflicts" CTA. Adopt for gert:
- `⚠ 3 inputs unresolved` → "Resolve inputs" button (opens input form)
- `✗ Step failed` → "Retry / Skip / Abort" inline banner at top of workflow map
- `⏸ Manual step waiting` → "Open Active Step" button
These banners sit at the TOP of the workflow map, not buried in the active step panel.

**4. File list as change summary**
The PR's conflict file list shows exactly what's in scope — filenames only, clean, clickable.
For gert's "run summary" after completion: list of step IDs with status icons, clickable
to view that step's detail in the active step panel:
```
✓ assess_failures          (0.8s, 0 rows)
✓ assess_logins            (1.2s, 0 rows)  
✗ check_replica_health     (failed: view not found)
○ investigate_auth_dns     (skipped)
○ escalate                 (not reached)
```

**5. Event type differentiation**
GitHub uses distinct icons per event type (commit dot, merge arrow, CI gear, force-push arrow).
The workflow map should visually distinguish step types:
- `⚡` or terminal icon → COMMAND (automated)
- `👤` or hand icon → MANUAL (human)
- `🔍` or query icon → QUERY (data retrieval)
- `⊘` → SKIPPED (when: guard was false)

## Schema Implications

**Decision: Explicit tree schema.** The webview renders a tree — the schema must BE a tree.

### Schema changes needed

**1. Tree structure** — replace flat `steps[]` with recursive tree:
```yaml
tree:
  - step:
      id: assess_failures
      type: xts
      ...
    branches:
      - condition: '{{ ne .failure_count "" }}'
        label: "Failures detected"
        steps:
          - step:
              id: check_replicas
              ...
      - condition: '{{ eq .failure_count "" }}'
        label: "No failures"
        steps:
          - step:
              id: mitigate_transient
              ...
              outcomes:
                - state: no_action
```

**2. `kind: choice` evidence** — add to EvidenceRequirement for mutually exclusive options:
```yaml
required_evidence:
  - kind: choice
    name: cpu_category
    label: "Primary CPU consumer"
    options:
      - "User Pool"
      - "Kernel Mode"
      - "System Pool"
```

**3. Existing features that map directly** (no change needed):
| Feature | Already in schema |
|---|---|
| Step state indicators | `status` in StepResult (passed/failed/skipped) |
| Outcomes with recommendations | `outcomes[]` with state + recommendation + next_runbook |
| Chained runbooks | `outcome.next_runbook` with file + inputs |
| ICM binding | `meta.inputs` with `from: icm.*` |
| Source provenance | `meta.source` with file + compiled_at + model |

### Migration path
- Flat `steps[]` remains valid (degenerate tree = linear sequence, no branches)
- Compiler emits `tree:` for new compilations
- Engine supports both: walks tree recursively, or falls back to flat steps

## Technical Notes
- WORKFLOW MAP: Lit webview rendering the tree structure
- ACTIVE STEP: Lit webview with typed forms per step type
- LEFT PANEL: standard VS Code text editor (readonly for prose in run mode)
- Backend: `gert serve` JSON-RPC over stdio

### INCIDENT Sidebar
- Uses ICM MCP tools (get_incident_details, get_incident_context, get_incident_location) for now
- Loads ICM by ID from the `--icm` flag or a vscode:// URI parameter
- Resolves inputs using `configs/icm-field-mapping.yaml` (routing → available custom fields)
- Resolves environment via `xts-cli env resolve`
- Future: direct ICM REST API calls from the extension (no MCP dependency)

### Chained Runbooks in Workflow Map
- Child runbook renders **inline** in the parent's workflow map
- The chain point (outcome with `next_runbook`) shows a handoff indicator: `→ IsReplicaInBuild.runbook.yaml`
- Child steps appear indented below the handoff, with a visual separator
- Child outcome becomes the final outcome shown in the workflow map
- Collapsible: the entire child section can be collapsed to one line

### Left Panel Follows Active Runbook
- The left panel always shows the **source TSG prose** for the runbook that owns the currently selected step
- When the chain switches from parent to child, the left panel swaps to the child's source file (`meta.source.file`)
- When the user clicks back on a parent step in the workflow map, the left panel swaps back to the parent's source
- Within the same runbook, the left panel scrolls to the step's source line range (from mapping.md)
- The link chain: **workflow map step → step's runbook → `meta.source.file` → left panel prose, scrolled to source line**
- If the user chose to show YAML instead of prose, the same logic applies — left panel shows the child runbook's YAML

### Execution Mode
- Mode (real / dry-run / replay) is set at session start — cannot change mid-run
- Mode selector appears as a dropdown in the toolbar before the first step executes
- If replay: scenario folder picker (file dialog)
- If replay: optional `--rebase-time now` toggle for demos
- Once a mode is selected and the run starts, all chained runbooks inherit the same mode

### `gert serve` JSON-RPC Protocol

```jsonc
// === Extension → gert serve ===

// Start execution with mode, vars, optional scenario
{"method": "exec/start", "params": {
  "runbook": "path.yaml",
  "mode": "real",           // real | dry-run | replay
  "vars": {"server_name": "test", ...},
  "icmId": "747870160",
  "scenarioDir": "",        // for replay mode
  "rebaseTime": ""          // "now" or timestamp, for replay
}}

// Advance to next step
{"method": "exec/next", "params": {}}

// Submit evidence for a manual step
{"method": "exec/submitEvidence", "params": {
  "stepId": "check_health",
  "evidence": {"dashboard_check": {"kind": "text", "value": "All green"}}
}}

// Get current variable/capture state
{"method": "exec/getVariables", "params": {}}

// Get run manifest
{"method": "exec/getManifest", "params": {}}

// === gert serve → Extension ===

// Step execution started
{"method": "event/stepStarted", "params": {
  "stepId": "assess_failures",
  "index": 0,
  "type": "xts",
  "title": "Assess gateway failures"
}}

// Step completed
{"method": "event/stepCompleted", "params": {
  "stepId": "assess_failures",
  "status": "passed",
  "captures": {"failure_count": "0"},
  "duration": "1.2s"
}}

// Step skipped (when: guard was false)
{"method": "event/stepSkipped", "params": {
  "stepId": "investigate_auth",
  "reason": "when: {{ .failure_count }} → empty"
}}

// Manual step waiting for evidence
{"method": "event/inputRequired", "params": {
  "stepId": "check_health",
  "instructions": "Open SqlAliasCacheReplicas.xts...",
  "evidence": [{"kind": "checklist", "name": "health_check", "items": [...]}]
}}

// Outcome reached (terminal state)
{"method": "event/outcomeReached", "params": {
  "stepId": "assess_failures",
  "state": "no_action",
  "recommendation": "No active failures found. Mitigate as transient."
}}

// Chain initiated (outcome.next_runbook)
{"method": "event/chainStarted", "params": {
  "parentStepId": "assess_failures",
  "childRunbook": "causes/IsReplicaInBuild.runbook.yaml",
  "childRunId": "20260214T100312-child",
  "inheritedCaptures": {"server_name": "sobeys-sql01", ...}
}}

// Chain completed
{"method": "event/chainCompleted", "params": {
  "childRunId": "20260214T100312-child",
  "outcome": "resolved",
  "recommendation": "Replica build completed"
}}
```

## Open Questions + Analysis

### 1. Tree structure vs flat steps with richer `when:`?

| Approach | Pros | Cons |
|----------|------|------|
| **Explicit tree** (`tree:` + `branches:`) | Direct 1:1 mapping to workflow map rendering. Compiler emits nested branches naturally from TSG prose hierarchy. Visual structure is data, not derived. | Bigger schema change. Engine needs recursive step walker. Existing flat runbooks need migration. Harder to hand-edit. |
| **Flat steps + richer `when:`** | Minimal schema change. Current engine mostly works. Easy to hand-edit. | Workflow map must reconstruct tree from flat conditions — fragile. Can't represent "one of N paths." Nested 3-level branches become unreadable `when:` chains. |

**Recommendation**: Explicit tree. The prototype proved the tree IS the product — deriving it from flat conditions is working backwards. The compiler already understands TSG heading hierarchy, so emitting a tree is natural. Flat `steps[]` remains as a degenerate case (tree with no branches = linear sequence).

### 2. YAML ↔ tree rendering sync?

> TSGs are immutable. No updates.

This simplifies everything. The flow is one-directional:
```
TSG prose (immutable) → gert compile → runbook YAML (immutable) → webview renders tree
```
No bidirectional sync needed. The YAML is a compile artifact, not editable during execution. The left panel shows it as readonly reference. The webview reads it once and renders.

One nuance: a runbook author might hand-edit the compiled YAML to fix compilation errors — that's "author mode," separate from "run mode." For run mode, immutable is correct.

### 3. Webview framework choice?

| Framework | Pros | Cons |
|-----------|------|------|
| **Raw HTML/CSS/JS** | Zero deps. Full control. No build step. Smallest bundle. | Verbose. Manual DOM manipulation. State management is DIY. |
| **Lit** (web components) | Lightweight (~5KB). Reactive templates. Native web components. Used by VS Code's own webview-ui-toolkit. | Smaller ecosystem than React. |
| **Svelte** | Compiles to vanilla JS (tiny bundle). Reactive by default. Clean syntax. | Needs build step. Less common in VS Code extension ecosystem. |
| **React** | Huge ecosystem. Most devs know it. Rich component libraries. | Heavy runtime (~40KB). Overkill for a tree + form. Build complexity. |

**Recommendation**: Lit. It's what Microsoft's `@vscode/webview-ui-toolkit` is built on. Tiny footprint, reactive, web-standard. The workflow map is a tree of custom elements — Lit's sweet spot.

### 4. Gert backend architecture?

| Approach | Pros | Cons |
|----------|------|------|
| **Go backend (gert) + stdio JSON-RPC** | Reuse all 18K+ lines of tested Go logic. Single source of truth. Extension is thin UI. Stdio is standard (DAP uses it). | Cross-process communication. Need a protocol. Go binary must ship with extension. Startup latency. |
| **Go backend + DAP** | Standard protocol. VS Code has native DAP support. Variables panel + breakpoints come free. | DAP is line-level debugging, not step-level runbook execution. Impedance mismatch. |
| **Port gert to TypeScript** | Single language. No binary distribution. Direct webview ↔ engine calls. Faster iteration. | Duplicate logic or full rewrite. Lose Go tests. Two codebases or abandon Go. |
| **Hybrid: Go for heavy lifting, TS for UI** | Best of both. Go handles: compile, validate, XTS provider, governance. TS handles: webview, step flow, evidence forms. Stdio JSON-RPC. | Two processes. Protocol design overhead. |

**Recommendation**: Hybrid with stdio JSON-RPC. The extension spawns `gert serve` (new subcommand) which speaks JSON-RPC over stdio. This is exactly how language servers work.

The protocol shape:
```jsonc
// Extension → gert serve
{"method": "exec/start", "params": {"runbook": "path.yaml", "mode": "real", "vars": {...}}}
{"method": "exec/next", "params": {}}
{"method": "exec/submitEvidence", "params": {"stepId": "x", "evidence": {...}}}
{"method": "exec/getVariables", "params": {}}

// gert serve → Extension
{"method": "event/stepStarted", "params": {"stepId": "x", "index": 0}}
{"method": "event/stepCompleted", "params": {"stepId": "x", "status": "passed", "captures": {...}}}
{"method": "event/inputRequired", "params": {"stepId": "x", "evidence": [...]}}
{"method": "event/terminalReached", "params": {"stepId": "x", "terminal": "escalated"}}
```
