# DS Consolidated Console Provider

## Status: collecting

## Problem
212 TSGs reference CAS commands (Get-FabricNode, Kill-Process, Dump-Process, DBCC, etc.). These run in the DS Consolidated Console, which requires:
1. JIT access to the target resource (JIT claim lifecycle)
2. A DSConsole session with the correct environment and cluster set
3. After JIT renewal, re-launching DSConsole to pick up updated claims

A generic `type: cli` provider can't handle this because:
- CAS commands aren't standalone executables — they're PowerShell cmdlets inside a DSConsole session
- The session has state (environment, cluster context) that must be set before commands run
- JIT claims expire and need refresh mid-session

## Proposed UX
- `type: dsc` step type with a `DscStepConfig` specifying the CAS command, target environment, cluster
- Provider manages session lifecycle: JIT → launch DSConsole → set context → execute → capture output
- Automatic JIT claim refresh detection and DSConsole relaunch
- Read-only CAS commands (Dump-Process, Get-FabricNode) execute automatically
- Destructive CAS commands (Kill-Process, Remove-Replica) pause for confirmation

## Technical Notes
- DSConsole is a PowerShell environment — likely need to manage a persistent PowerShell session
- JIT via SAW tooling — need to understand the JIT API/CLI
- Session context: `Set-Environment`, `Set-Cluster` commands within DSConsole
- Output capture: pipe CAS command output to a variable or file

## Examples
<!-- Capture screenshots of DSConsole session -->
<!-- Document a real CAS command sequence with context setup -->
<!-- Show JIT claim flow -->

## Open Questions
- How is DSConsole launched programmatically? PowerShell module import?
- What's the JIT claim lifecycle (duration, renewal API)?
- Can we detect when claims have expired (error pattern)?
- Is there a headless/non-interactive DSConsole mode?
- How to capture structured output from CAS commands (object output vs. text)?
