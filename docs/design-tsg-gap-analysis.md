!!!AI Generated.  To be verified!!!

# Design: TSG Gap Analysis

## Problem

TSGs cannot cover every case. When execution ends with `escalated` and the engineer mitigates manually, the gap between what the TSG covered and what actually worked is lost. The same gap gets hit again by the next on-call engineer.

## Goal

Given an ICM where the TSG's recommendation diverged from the actual resolution, produce:
1. A **gap report** — where the TSG stopped and what the engineer actually did
2. A **suggested patch** — concrete runbook YAML that would cover this case next time
3. A **confidence assessment** — single incident (low) vs pattern across many incidents (high)

## Data Sources

| Source | What it provides | Access |
|---|---|---|
| Execution trace | Steps executed, captures, branch taken, outcome | Saved scenario or replay log |
| ICM resolution | Actual mitigation notes, RCA, owner actions | `icm-remote-mcp` |
| Kusto data | What was visible at the time of the incident | `mcp_azure_mcp_kusto` |
| Runbook YAML | The decision tree that was followed | Local file |

## Architecture

```
┌─────────────────────┐     ┌──────────────────┐
│  Execution Trace    │     │  ICM Resolution   │
│  (captures, branch, │     │  (mitigation text,│
│   outcome=escalated)│     │   RCA, actions)   │
└────────┬────────────┘     └────────┬──────────┘
         │                           │
         └──────────┬────────────────┘
                    ▼
         ┌─────────────────────┐
         │  Gap Analysis Engine │
         │                     │
         │  1. Align trace to  │
         │     tree branches   │
         │  2. Compare outcome │
         │     vs resolution   │
         │  3. Identify missing│
         │     branch/condition│
         │  4. Generate patch  │
         └──────────┬──────────┘
                    │
         ┌──────────┴──────────┐
         │                     │
         ▼                     ▼
   Gap Report            YAML Patch
   (markdown)         (new branch/step)
```

## Integration Points

### 1. Post-Execution Button (Extension UI)

When outcome is `escalated`, show an **"Analyze Gap"** button in the outcome panel. Clicking it:
- Collects the execution trace (steps, captures, branch path)
- Prompts for the ICM ID if not already known
- Launches the gap analysis as a Copilot Chat request with full context

**Rationale:** The engineer who just ran the TSG has the freshest context. Low friction — one click.

### 2. Retroactive Skill (Copilot Chat)

A `analyze-gap` skill that can be invoked in chat:
```
@workspace /analyze-gap ICM 749270451 login-success-rate-below-target
```

Steps:
1. Fetch ICM resolution via MCP
2. Load the runbook YAML
3. Load the saved scenario (if exists) or replay from scratch
4. Walk the tree: identify the terminal branch
5. Compare terminal outcome vs ICM resolution
6. Generate gap report + suggested patch

### 3. Batch Mode (Future)

Scan N incidents that hit the same TSG:
- Cluster by outcome (escalated, resolved, no_action)
- For escalated: group by similar resolution text
- Rank gaps by frequency
- Output a prioritized list of patches

## Gap Report Format

```markdown
## TSG Gap Report

**ICM:** 749270451
**Runbook:** login-success-rate-below-target
**Execution outcome:** ESCALATED — "Recommend escalate to Gateway team"

### What the TSG covered
- Queried MonLogin for login failures → found 847 failures
- Top error: 40613 / state 10
- Branched to: error-40613-state-10 child TSG
- Child TSG reached: escalated (no specific mitigation for this pattern)

### What the engineer actually did
(From ICM resolution notes)
- Identified SNAT exhaustion on TR tr4729.westeurope1-a
- Engaged CloudNet to drain and replace the affected SLB VIP
- Issue resolved after SLB rotation

### Gap identified
The TSG has no branch for SNAT exhaustion when error 40613/state 10
occurs from multiple GW nodes in the same cluster. The current path
goes straight to "escalate to Gateway team" without checking SNAT.

### Suggested patch
Add a Kusto query step after error-40613-state-10 detection to check
for SNAT exhaustion patterns, with a branch to CloudNet escalation.

### Confidence
**Medium** — single incident, but the pattern (SNAT + 40613/10) is
well-documented in other TSGs.
```

## Suggested YAML Patch Format

The analysis produces a diff-ready YAML fragment:

```yaml
# Insert after step: query_error_pattern in error-40613-state-10.runbook.yaml
# New branch under condition: distinct_clusters == "1" && distinct_gw_nodes != "1"

- step:
    id: check_snat_exhaustion
    type: xts
    title: Check SNAT exhaustion on affected TR
    xts:
        mode: query
        query_type: kusto
        query: |
            // SNAT exhaustion check query here
    capture:
        snat_exhausted: "$.data[0].IsExhausted"
  branches:
    - condition: 'snat_exhausted == "True"'
      label: SNAT exhaustion confirmed
      steps:
        - step:
            id: escalate_snat
            type: manual
            title: Escalate to CloudNet for SLB rotation
            instructions: |
                SNAT exhaustion confirmed on the affected TR.
                Escalate to CloudNet\SLB for VIP drain and rotation.
            outcomes:
                - state: escalated
                  recommendation: SNAT exhaustion — CloudNet engaged.
```

## Implementation Phases

### Phase 1: Gap Report Generation (Skill)

- Build `analyze-gap` skill in `.github/skills/analyze-gap/SKILL.md`
- Input: ICM ID + runbook name (or path)
- Output: Gap report markdown
- Dependencies: `icm-remote-mcp` for resolution data, file read for runbook/scenario
- No code changes to gert engine

### Phase 2: Analyze Gap Button (Extension)

- Add button to outcome panel when `state === 'escalated'`
- Button triggers a Copilot Chat request with:
  - Full execution trace (steps, captures, branches)
  - ICM ID
  - Runbook path
  - Instruction to invoke the `analyze-gap` skill
- No server changes needed — pure extension UI

### Phase 3: YAML Patch Generation (Skill Enhancement)

- Extend the skill to produce a concrete YAML patch
- Validate the patch against the JSON schema
- Optionally auto-insert via file edit (with user confirmation)

### Phase 4: Batch Gap Analysis (CLI)

- `gert gaps --runbook <path> --incidents <icm1,icm2,...>`
- Fetches resolution for each incident
- Clusters gaps by pattern
- Outputs prioritized gap list with patch suggestions
- Could be scheduled as a periodic review job

## Open Questions

1. **ICM resolution quality:** Mitigation notes are free-text and often terse. How reliably can we extract the actual mitigation action?
2. **Kusto replay:** Should we re-run the same queries against historical data to see what was visible at incident time? Or rely on saved scenario captures?
3. **Auto-apply patches:** Should we offer to directly edit the runbook YAML, or always produce a report for human review?
4. **Feedback loop:** After a patch is applied and tested, should we auto-generate a scenario from the original ICM to validate the new branch?

## Non-Goals (for now)

- Automatic TSG rewriting without human review
- Real-time gap detection during live execution
- Cross-TSG gap analysis (comparing different TSGs for the same error)
