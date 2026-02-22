# XTS4VSCode Integration

## Status: collecting

## Problem
Many TSG steps say "open view X.xts and find Y." The LLM correctly emits these as `type: manual` because the view requires interactive navigation. But if xts4vscode renders XTS views inside VS Code, we can make this semi-automated: gert opens the view, the user navigates and selects data, then sends it back to the runbook as a capture.

## xts4vscode Current State (from screenshots)

Screenshots in this folder show xts4vscode rendering the "sterling servers and databases" view:

### Architecture (screenshot 1: waiting state, screenshot 2: populated)
- **Left sidebar**: XTS EXPLORER with VIEWS tree (per-team folders: sterling/576 items, etc.) + ENVIRONMENTS
- **Main editor area**: Wave-by-wave activity panels cascading left→right, top→bottom
  - Each activity is a named panel with its own table, toolbar icons, and row count
  - Parameters on the left ("Search String" input + OK button)
  - Data cascades through waves: Servers → Databases → Partition info → Replicas → Hadron DMV
- **Each panel toolbar**: has copy/snapshot icon buttons (top-right of each table)
- **Navigation**: "Double click to open Database Replicas" — interactive chaining between waves
- **DETAILS section**: bottom-left, shows view file path

### Data visible in the populated view (screenshot 2)
The full sterling chain for `testsvr`:
```
Servers:        testsvr → logical_server_id, state=Ready
Databases:      master → state, parent_state, sql_instance_name=d1676c3410ea
Partition info: partition_id=7FC31B20..., physical_database_id=a7086e1f..., SQLMasterDb, Ready
Instance:       d1676c3410ea, state=Ready, type=SQLLogicalServer
Replicas:       DB1, READY, PRIMARY, health=OK, UP, pid, replica_id, conn_string
Hadron DMV:     replication_endpoint_url, last_hardened_lsn, catchup_progress=100%
```

### View explorer (screenshots 3-4)
- VIEWS tree organized by team/person alias (albain, chongliu, jimoha, sterling...)
- Per-user folders with item counts
- ENVIRONMENTS section below

## Proposed UX
1. Gert hits a manual XTS view step
2. VS Code opens the specified .xts view in xts4vscode (extension-to-extension call)
3. xts4vscode populates the first parameter and auto-executes the view
4. User navigates the wave cascade, finding the relevant data
5. User clicks a cell/row and hits a toolbar button: "📋 Send to Runbook" (per-cell) or "📸 Snapshot Table" (full table)
6. Selected data flows back to gert with full context (activity name, column, value)
7. Toast message confirms: "Sent tenant_ring_name → gert runbook"
8. Runbook resumes with the captured data

## Proposed API Surface

### Commands (gert → xts4vscode)

```typescript
// Open a view with pre-filled parameters and auto-execute
vscode.commands.executeCommand('xts4vscode.openView', {
  file: 'sterling/sterling servers and databases.xts',
  environment: 'LocalSterlingOnebox',
  params: { search_string: 'testsvr' },
  autoExecute: true,          // trigger execution immediately
  callerId: 'gert-runbook',   // identifies the calling extension
  sessionId: 'run-20260214T...' // ties captures back to a specific run
});

// Request focus on a specific activity panel (optional, for guidance)
vscode.commands.executeCommand('xts4vscode.focusActivity', {
  activityName: 'Replicas for...',
  highlightColumns: ['tenant_ring_name', 'node_name']  // hint which columns gert needs
});
```

### Events (xts4vscode → gert)

```typescript
// User sends a single cell value back to the runbook
interface CellSentEvent {
  source: 'xts4vscode';
  sessionId: string;
  viewFile: string;
  activityName: string;     // "Servers" or "Partition info for 86130219..."
  column: string;           // "tenant_ring_name"
  value: string;            // "tr123"
  rowIndex: number;         // 0
}

// User snapshots an entire table
interface TableSnapshotEvent {
  source: 'xts4vscode';
  sessionId: string;
  viewFile: string;
  activityName: string;
  columns: string[];
  data: Record<string, any>[];
  rowCount: number;
}

// User captures a chart/panel as PNG
interface ScreenshotEvent {
  source: 'xts4vscode';
  sessionId: string;
  viewFile: string;
  activityName: string;
  png: Uint8Array;
}
```

### Communication mechanism
- Extension-to-extension via `vscode.commands.executeCommand` (for gert → xts4vscode)
- `vscode.commands.registerCommand('gert.receiveCellData', handler)` — xts4vscode calls gert's registered command
- This is the simplest pattern: both extensions register commands, call each other's commands

### Session routing (multi-instance support)

Multiple runbook sessions and multiple xts4vscode views can be active simultaneously.
The `sessionId` (= gert run ID) ensures data routes to the correct receiver.

```
gert session A (run-001) ──opens──► xts4vscode view X (sessionId=run-001)
gert session B (run-002) ──opens──► xts4vscode view Y (sessionId=run-002)

user clicks "Send" on view X ──► event.sessionId=run-001 ──► routed to session A
user clicks "Send" on view Y ──► event.sessionId=run-002 ──► routed to session B
```

**Lifecycle:**
1. `xts4vscode.openView` receives `sessionId` from gert → stores it on the view instance
2. All toolbar buttons (Send Cell, Send Table, Send Screenshot) include `sessionId` in their events
3. Gert registers `gert.receiveCellData` / `gert.receiveTableSnapshot` / `gert.receiveScreenshot`
4. On receive, gert routes by `sessionId` to the correct running engine:

```typescript
// gert side
const activeSessions = new Map<string, RunbookSession>();

vscode.commands.registerCommand('gert.receiveCellData', (event: CellSentEvent) => {
  const session = activeSessions.get(event.sessionId);
  if (!session) {
    vscode.window.showWarningMessage(`Runbook session ${event.sessionId} expired or not found.`);
    return;
  }
  session.handleCapture(event);
  vscode.window.showInformationMessage(`✓ ${event.column} → runbook capture`);
});
```

**Edge cases:**
- View opened manually (not by gert) → no `sessionId` → send buttons hidden/disabled
- Gert session ends while view still open → next send attempt shows "session expired" toast
- Multiple sends from same view → each send is independent, gert accumulates captures

## UI Elements Needed in xts4vscode

On each activity panel's toolbar (next to existing copy/snapshot icons):

| Button | Icon | Action | When |
|--------|------|--------|------|
| Send Cell | 📋 | Sends selected cell's {activity, column, value, row} to gert | Cell is selected |
| Send Table | 📸 | Sends full table as JSON to gert | Always available |
| Send Screenshot | 🖼️ | Captures panel rendering as PNG, sends to gert | Always available |

Each button:
- Only visible/enabled when a gert runbook session is active (sessionId exists)
- Shows toast on success: "✓ Sent tenant_ring_name to runbook"
- Handles "no gert session" gracefully (toast: "No active runbook session")

## Integration with Runbook Schema

When gert compiles a TSG that references an XTS view as a manual step, the step should include
hints about what data gert expects from the view:

```yaml
- id: check_alias_db_health
  type: manual
  title: Check Alias DB replica health
  instructions: |
    Open SqlAliasCacheReplicas.xts and verify all replicas are healthy.
  xts_view_hint:               # new field — hints for xts4vscode integration
    file: sterling/sqlaliascachereplicas.xts
    expected_captures:
      - column: replica_health_state_desc
        capture_as: replica_health
      - column: node_name
        capture_as: replica_node
  capture:
    replica_health: ""         # populated by xts4vscode send-cell
    replica_node: ""           # populated by xts4vscode send-cell
```

This allows the gert extension to:
1. Call `xts4vscode.openView` with the file from `xts_view_hint`
2. Call `xts4vscode.focusActivity` with `highlightColumns` from expected_captures
3. Match incoming `CellSentEvent.column` to `capture_as` mapping
4. Auto-populate captures without the user needing to know the variable names

## Open Questions

### Answered
- Does xts4vscode have an extension API today? → **No. Will be built.**
- What data formats does it expose? → **All (JSON, XML, HTML).**
- Can it open a view with pre-filled parameters? → **Yes for first param. Auto-execute TBD.**
- Can we capture table snapshots? → **Will add. Per-panel snapshot button.**
- Can we capture chart/graph as PNG? → **Will add as we define.**

### Remaining
- How to handle multi-row selection? (e.g., "send all unhealthy replicas" → filter + send)
- Should the `sessionId` be tied to a gert run ID or to the VS Code window session?
- How to handle the case where the user navigates away from the view before sending data?
- Should xts4vscode auto-detect that a gert runbook is waiting and show a banner?
- Performance: will large tables (1000+ rows) cause issues with JSON serialization?
