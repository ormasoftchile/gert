---
name: walkthrough-runbook
description: Run a runbook step by step, describe each step, take screenshots, evaluate correctness, and generate a development review document
---

# Walkthrough Runbook

Executes a runbook step by step (dry-run or replay mode), producing a structured Markdown review document with step descriptions, screenshots (if Playwright is available), correctness evaluation, and development feedback.

## When to Use

Use this skill when:
- Reviewing a new or modified runbook for correctness
- Generating documentation/walkthrough for a runbook
- Validating that the TUI renders steps correctly
- Producing a development review artifact before merging
- Onboarding someone to a runbook's logic and flow

## Prerequisites

- `gert` must be built (`go build -o bin/gert.exe ./cmd/gert` in the gert workspace)
- The runbook must pass `gert validate`
- For screenshots: Playwright MCP tools should be available (`browser_navigate`, `browser_take_screenshot`, etc.)
- For replay mode: a scenario directory must exist for the runbook

## Variables

- **{RunbookPath}**: Path to the `.runbook.yaml` file to walk through
- **{Mode}**: Execution mode — `dry-run` (default) or `replay`
- **{ScenarioDir}**: Path to scenario directory (required for replay mode, optional otherwise)
- **{OutputPath}**: Where to save the review document (default: `walkthrough-{runbook-name}.md` next to the runbook)
- **{TakeScreenshots}**: Whether to take TUI screenshots via Playwright (default: false)
- **{Vars}**: Optional `--var key=value` flags to pass to execution

## How It Works

### Phase 1: Analyze the Runbook Structure

1. Read the runbook YAML file at {RunbookPath}
2. Parse and understand the structure:
   - `meta`: name, kind, description, inputs, vars, governance rules
   - `tree` (or `steps`): the step hierarchy with branches, iterations, and outcomes
3. Count total steps, identify step types (cli/manual/tool/xts), note branches and conditions
4. Identify template variables and their sources (vars, inputs, captures)
5. Validate the runbook: run `gert validate {RunbookPath}` and capture output

Write a brief structural summary to include at the top of the walkthrough document.

### Phase 2: Execute Step by Step

Run the runbook using `gert exec` and capture output:

```bash
gert exec --mode {Mode} {RunbookPath} [--scenario {ScenarioDir}] [--var key=value ...] 2>&1
```

For dry-run mode:
- CLI steps will print the command but not execute it
- Manual steps will show instructions and use placeholder evidence
- The full terminal output is captured

For replay mode:
- CLI steps will use recorded scenario responses
- Manual steps will use recorded choices/evidence
- Output matches what a real execution would look like

**Capture the complete terminal output.** This is the raw data for analysis.

### Phase 3: Per-Step Analysis

For each step discovered in Phase 1, analyze and document:

#### For CLI steps:
- **Command**: The resolved `argv` (with template variables filled in)
- **Purpose**: Why this command is run (from instructions + title)
- **Expected behavior**: What the command does and what output to expect
- **Captures**: What variables are extracted from the output
- **Governance check**: Is the command in the allowed list? Any redaction rules apply?

#### For Manual steps:
- **Instructions**: The rendered instructions (with template variables resolved)
- **Choices**: If choices are defined, list the options and what each means
- **Evidence requirements**: What evidence types are required
- **Human judgment needed**: What the operator needs to decide

#### For each step (all types):
- **Correctness evaluation**: 
  - Does the step title accurately describe what it does?
  - Are instructions clear and actionable?
  - Are capture patterns correct?
  - Could this step fail in ways not handled?
- **Development feedback**:
  - Missing error handling or edge cases
  - Unclear instructions or missing context
  - Opportunities to automate manual steps
  - Template variable issues (undefined, unused)
  - Governance gaps (commands not in allowlist)

### Phase 4: Take Screenshots (Optional)

If {TakeScreenshots} is true and Playwright MCP tools are available:

1. Build `gert` if needed: `go build -o bin/gert.exe ./cmd/gert`
2. Launch `gert tui --mode {Mode} {RunbookPath}` in a VS Code terminal
3. Wait for the TUI to render (2-3 seconds)
4. Take a screenshot of the initial state (step list visible)
5. For each step:
   a. Press Enter to advance to the next step
   b. Wait for the step to execute (1-2 seconds)
   c. Take a screenshot
   d. Note what's visible: step list status, output panel, detail bar
6. Save screenshots alongside the walkthrough document
7. Reference screenshots in the Markdown output

**Note**: If Playwright is not available, skip this phase entirely. The walkthrough
is still valuable from the structural analysis alone.

### Phase 5: Branch & Outcome Analysis

For each branch point in the tree:
- List the condition expression
- Explain what triggers each branch
- Note which branch would be taken with the current variable values
- Flag branches that might never be reachable (dead code)

For each outcome:
- List the `when` condition
- Explain what state it reaches (resolved, escalated, etc.)
- Evaluate whether the recommendation is actionable
- Check if `next_runbook` chaining is configured and valid

### Phase 6: Generate Walkthrough Document

Produce a Markdown document at {OutputPath} with this structure:

```markdown
# Walkthrough: {runbook-name}

**Generated**: {date}  
**Mode**: {Mode}  
**Runbook**: {RunbookPath}

## Overview

{meta.description}

- **Kind**: {meta.kind}
- **Total steps**: {N} ({cli_count} CLI, {manual_count} manual, {tool_count} tool)
- **Branches**: {branch_count}
- **Outcomes**: {outcome_count}
- **Inputs required**: {list of inputs}
- **Governance**: {summary of allowed_commands, redaction rules}

## Validation

{output of gert validate}

## Step-by-Step Walkthrough

### Step 1: {step.title} [{step.id}]

**Type**: {step.type}  
**When guard**: {step.when or "always"}

{screenshot if available}

{For CLI: command, expected output, captures}
{For Manual: instructions, choices, evidence}

**Correctness**: {evaluation}  
**Feedback**: {development suggestions}

---

### Step 2: ...

## Branch Analysis

{For each branch point, explain the logic}

## Outcome Analysis

{For each possible outcome, explain what leads to it}

## Summary

### Correctness Score
{Overall assessment: how well-formed is the runbook?}

### Issues Found
{Numbered list of issues, ordered by severity}

### Recommendations
{Prioritized list of improvements for the development process}
```

### Phase 7: Present Results

1. Save the walkthrough document to {OutputPath}
2. Print a summary to the user:
   - Number of steps walked through
   - Number of issues found
   - Top 3 recommendations
3. If screenshots were taken, note how many and where they're saved

## Example Usage

User says: "Walk through dri-prerequisites.runbook.yaml"

The skill will:
1. Read and analyze the 8-step DRI prerequisites runbook
2. Run it in dry-run mode
3. Describe each step (identity check, group memberships, VPN, share access)
4. Evaluate branch logic (share access failure → clear + re-auth)
5. Check outcome conditions (all-pass vs any-fail)
6. Generate `walkthrough-dri-prerequisites.md`

## Notes

- Dry-run mode is the default because it requires no scenario data and no real execution
- Replay mode produces richer output but requires a recorded scenario
- Screenshots are optional — the structural analysis is the primary value
- The walkthrough document is a point-in-time snapshot, regenerate after changes
- For runbooks with `inputs`, provide default values via {Vars} or accept defaults
- Template variables that can't be resolved in dry-run mode are noted as `<unresolved>`
