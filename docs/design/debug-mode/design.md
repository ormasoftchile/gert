# Debug Mode

## Status: collecting

## Problem
Engineers need to:
1. Step through a runbook interactively (F5 experience)
2. Dry-run with zero side effects (mock data)
3. Inspect/watch variables and captures as they evolve
4. Set breakpoints on specific steps
5. Replay with recorded scenario data for training/demos

The CLI debugger (`gert debug`) exists but it's terminal-based. The VS Code extension should provide a rich visual debugging experience.

## Proposed UX
- **F5 to start**: Open a .runbook.yaml, press F5 → launches debug session
- **Debug toolbar**: Step (next), Continue, Pause, Stop — standard VS Code debug controls
- **Variable watch**: Variables panel shows meta.vars, meta.inputs (resolved values), and captures (updated per step)
- **Step highlighting**: Current step highlighted in the YAML editor (uses DecorationTypes)
- **Breakpoints**: Click gutter to set breakpoint on a step → execution pauses there
- **Mode selector**: Real / Dry-run / Replay (pick scenario folder)
- **Output panel**: Step results, XTS query output, evidence collected

## Technical Notes
- VS Code Debug Adapter Protocol (DAP): implement a custom debug adapter for gert
- Debug adapter translates DAP messages (next, stepIn, variables, evaluate) to gert engine calls
- Alternative: use the gert CLI debugger as the backend, DAP adapter as the frontend
- Variables panel: three scopes — Inputs, Vars, Captures
- Breakpoints: map line numbers in YAML to step indices
- Launch configurations in `.vscode/launch.json`:
  ```json
  {
    "type": "gert",
    "request": "launch",
    "name": "Debug Runbook",
    "runbook": "${file}",
    "mode": "dry-run",
    "scenario": "scenarios/alias-db-failure/"
  }
  ```

## Examples
<!-- Screenshots of desired variable watch panel -->
<!-- Mockup of step highlighting in YAML editor -->
<!-- Reference: VS Code debug UI for other languages -->

## Reference: TSG-ToolKit Prototype

The prototype screenshots (see `docs/design/three-panel-layout/`) show the execution UX
that debug mode should build on. Key patterns:

- **Retry / Skip / Abort** buttons on COMMAND steps (screenshot: catch-process-terminator)
- **Submit Evidence / Next Step** buttons on MANUAL steps
- **Step state dots** in workflow map (red=failed, blue=current, green=done, white=pending)
- **QUERY step** with active step detail showing query text and type badge

Debug mode adds to this: breakpoints, variable inspection, and mode selection (real/dry-run/replay).

## Open Questions
- Full DAP implementation vs. simpler custom webview-based debugger?
- How to handle manual steps in debug mode (pause with input form vs. auto-fill from scenario)?
- Can we reuse the existing gert CLI debugger as the backend process?
- Should dry-run mock XTS responses with placeholder data or require a scenario?
