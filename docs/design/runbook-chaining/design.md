# Runbook Chaining via Outcomes

## Status: collecting

## Problem
TSGs often reference other TSGs: "follow the cause-specific TSG" or "see the CAS command guide."
Today, outcomes have a `recommendation` that tells the DRI what to do next in prose.
This could be machine-actionable: an outcome triggers another runbook automatically,
passing captures as inputs.

## Proposed Schema

```yaml
outcomes:
  - when: '{{ ne .failure_count "" }}'
    state: needs_rca
    next_runbook:
      file: "connection/login-failure-causes/{{ .login_failure_cause }}.runbook.yaml"
      inputs:
        server_name: "{{ .server_name }}"
        database_name: "{{ .database_name }}"
        app_name: "{{ .app_name }}"
    recommendation: |
      Active login failures detected (cause: {{ .login_failure_cause }}).
      Invoking cause-specific runbook.
```

### Outcome struct extension

```go
type Outcome struct {
    When           string            `yaml:"when,omitempty"`
    State          string            `yaml:"state" jsonschema:"required,enum=resolved,enum=escalated,enum=no_action,enum=needs_rca"`
    Recommendation string            `yaml:"recommendation,omitempty"`
    NextRunbook    *NextRunbook       `yaml:"next_runbook,omitempty"`
}

type NextRunbook struct {
    File   string            `yaml:"file"   jsonschema:"required"`
    Inputs map[string]string `yaml:"inputs,omitempty"`
}
```

## Execution Flow

```
ICM alert
  → login-success-rate-below-target.runbook.yaml
      step 1: Kusto query → failure_count > 0, cause = IsReplicaInBuild
      outcome: next_runbook → IsReplicaInBuild.runbook.yaml
          → loads child runbook
          → merges parent captures into child inputs
          → starts new engine run (same trace, continuous run ID)
          → child reaches its own outcome: resolved / escalated
```

## How Runbook Kinds Connect

| Parent kind | Outcome action | Child kind |
|---|---|---|
| **mitigation** (triage) | `next_runbook` → cause-specific | **mitigation** (specialized) |
| **mitigation** | `next_runbook` → sub-procedure | **composable** |
| **mitigation** | reference to query library | **reference** (not invoked, just linked) |

## Engine Changes

When an outcome has `next_runbook`:
1. Resolve template vars in `file` and `inputs` against current captures + vars
2. Load the child runbook from the resolved file path
3. Validate the child runbook
4. Merge: child `meta.inputs` satisfied by parent's `next_runbook.inputs` mapping
5. Create a child engine run (options: same run ID with sub-ID, or new run ID linked to parent)
6. Execute child runbook
7. Child's trace appends to parent's trace (or separate trace linked by parent run ID)
8. Child's outcome becomes the overall outcome

## Call Stack / Recursion

- Maximum chain depth should be limited (e.g., 5) to prevent infinite loops
- The trace records the chain: `parent_run_id` field on child runs
- Each link in the chain is visible in the workflow map as a "handoff" node

## Open Questions

- Should the child runbook's outcome replace or augment the parent's outcome?
- How deep can chaining go? Fixed limit or configurable?
- Should `next_runbook.file` support template vars (dynamic dispatch) or only static paths?
- How does the workflow map visualize the handoff to a child runbook?
- Should the child inherit the parent's `meta.xts` config or have its own?
- What happens if the child runbook doesn't exist at runtime (file not found)?
- How does resume work across a chain? Resume the parent at the handoff point?
