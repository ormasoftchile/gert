# Runbook YAML Schema Contract v0

**Branch**: `001-runbook-engine-v0` | **Date**: 2026-02-11

---

## Schema Overview

The canonical schema is generated from Go struct definitions and exported as `schemas/runbook-v0.json` (JSON Schema Draft 2020-12). This document is the human-readable reference.

## Document Structure

```yaml
apiVersion: runbook/v0                  # required, fixed value
meta:                                    # required
  name: string                           # required, unique identifier
  description: string                    # optional
  vars:                                  # optional
    <key>: <value>                       # string key-value pairs
  defaults:                              # optional
    timeout: string                      # duration (e.g., "300s", "5m")
  governance:                            # optional
    allowed_commands: [string]           # command allowlist
    denied_commands: [string]            # command denylist
    deny_env_vars: [string]             # glob patterns
    redact:                              # output redaction rules
      - pattern: string                  # regex (required)
        replace: string                  # replacement text (required)
    evidence:
      require_for_manual: boolean        # default: false
      store_full_stdout: boolean         # default: false
steps:                                   # required, min 1 item
  - id: string                           # required, unique within runbook
    type: cli | manual                   # required
    title: string                        # optional

    # CLI step fields (required when type=cli)
    with:
      argv: [string]                     # required, min 1 element

    # Manual step fields (required when type=manual)
    instructions: string                 # required for manual
    required_evidence:                   # optional
      - kind: text | checklist | attachment  # required
        name: string                     # required, unique within step
        items: [string]                  # required when kind=checklist
    approvals:                           # optional
      min: integer                       # minimum approvals (default: 0)
      roles: [string]                    # authorized roles
    replay_mode: reuse_evidence          # optional

    # Shared step fields
    capture:                             # optional
      <name>: stdout | stderr            # capture source
    assertions:                          # optional, list of assertion objects
      - contains: string                 # substring check
      # OR
      - not_contains: string             # negative substring check
      # OR
      - matches: string                  # regex match
      # OR
      - exit_code: integer               # exit code check
      # OR
      - equals: string                   # exact match
      # OR
      - not_equals: string               # negative exact match
      # OR
      - json_path:                       # structured JSON query
          path: string                   # JSON path expression
          equals: string                 # expected value
    timeout: string                      # per-step timeout override (CLI only)
```

## Validation Phases

### Phase 1: Structural (yaml.v3 strict decode)
- Reject unknown fields at any nesting level
- Enforce YAML type correctness

### Phase 2: Semantic (JSON Schema validation)
- Required fields present
- Enum values valid
- Pattern constraints (regex validity)
- Conditional requirements (type=cli → with.argv required)

### Phase 3: Domain (custom Go validation)
- Step ID uniqueness
- Variable reference resolution (all `{{ .var }}` resolve to `meta.vars` keys)
- Governance consistency (allowed_commands and denied_commands don't overlap)
- Redaction pattern regex validity
- Capture name validity (valid identifiers)

## Example Document

```yaml
apiVersion: runbook/v0
meta:
  name: pod-crashloop-investigation
  description: Investigate CrashLoopBackOff pods in production
  vars:
    namespace: prod
    service: api-gateway
  defaults:
    timeout: "120s"
  governance:
    allowed_commands:
      - kubectl
      - az
      - curl
    deny_env_vars:
      - "SECRET_*"
      - "TOKEN"
      - "AWS_*"
    redact:
      - pattern: "(?i)password\\s*[:=]\\s*\\S+"
        replace: "password: <redacted>"
    evidence:
      require_for_manual: true
      store_full_stdout: false
steps:
  - id: check_pods
    type: cli
    title: Check pod status
    with:
      argv: ["kubectl", "get", "pods", "-n", "{{ .namespace }}", "-l", "app={{ .service }}"]
    capture:
      pods: stdout
    assertions:
      - not_contains: "CrashLoopBackOff"

  - id: get_logs
    type: cli
    title: Retrieve pod logs
    with:
      argv: ["kubectl", "logs", "-n", "{{ .namespace }}", "-l", "app={{ .service }}", "--tail=100"]
    capture:
      logs: stdout
    timeout: "60s"

  - id: check_events
    type: cli
    title: Check cluster events
    with:
      argv: ["kubectl", "get", "events", "-n", "{{ .namespace }}", "--sort-by=.lastTimestamp"]
    capture:
      events: stdout

  - id: validate_dashboard
    type: manual
    title: Validate metrics in monitoring dashboard
    instructions: |
      Open the Grafana dashboard for {{ .service }} in {{ .namespace }}.
      Confirm that:
      1. Error rate is below 1%
      2. P99 latency is below 500ms
      3. No anomalous traffic patterns
    required_evidence:
      - kind: checklist
        name: metrics_validation
        items:
          - "Error rate < 1%"
          - "P99 latency < 500ms"
          - "No anomalous traffic"
      - kind: text
        name: dashboard_url
      - kind: attachment
        name: dashboard_screenshot
    approvals:
      min: 1
      roles: ["DRI"]
    replay_mode: reuse_evidence
```
