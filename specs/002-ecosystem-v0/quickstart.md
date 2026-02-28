# Quickstart: Gert Ecosystem v0

**Feature**: 002-ecosystem-v0

## Prerequisites

- Go 1.25+
- gert kernel/v0 built and tests passing (`go test ./pkg/kernel/... -count=1`)
- Tool binaries on PATH: `curl`, `ping` (for basic runbooks)

## Build

```bash
# Build core CLI (replaces gert-kernel)
go build -o gert ./cmd/gert

# Build TUI (optional)
go build -o gert-tui ./cmd/gert-tui

# Build MCP server (optional)
go build -o gert-mcp ./cmd/gert-mcp
```

## Validate a runbook with effects taxonomy

```bash
# Tool with new effects field
cat tools/curl.tool.yaml
# contract:
#   effects: [network]
#   writes: []

./gert validate tools/curl.tool.yaml
# ✓ tool curl is valid (2 actions)

./gert validate runbooks/service-health-diagnostic.yaml
# ✓ service-health-diagnostic is valid (5 steps)
```

## Execute with trace + identity

```bash
./gert exec runbooks/service-health-diagnostic.yaml \
  --var hostname=srv1.example.com \
  --as oncall@company.com \
  --trace traces/run-001.jsonl

# run_start includes actor, host, gert_version, runbook_hash, tool_hashes
# Every event has prev_hash for tamper evidence
```

## Verify trace integrity

```bash
GERT_TRACE_SIGNING_KEY=mykey123 \
./gert exec runbooks/health-check.yaml --trace run.jsonl --var hostname=srv1

./gert trace verify run.jsonl
# ✓ Chain integrity: 12 events, no breaks
# ✓ Signature valid
```

## Run scenario tests

```bash
./gert test runbooks/service-health-diagnostic.yaml
#   service-health-diagnostic
#     ✓ healthy (2ms)
#     ✓ degraded (3ms)
#     ✓ unknown (1ms)
#   3 passed, 0 failed
```

## Resume after approval

```bash
# Run hits require-approval gate
./gert exec runbooks/k8s-pod-restart.yaml --var pod=web-api-7f8b9
# ⚠ Step delete_pod requires approval (risk: critical)
# Approval pending. Resume with: gert resume --run run-abc123

# After approval arrives:
./gert resume --run run-abc123
# ✓ Outcome: resolved (pod_restarted)
```

## TUI

```bash
./gert-tui runbooks/service-health-diagnostic.yaml --mode replay --scenario healthy
# Launches terminal UI with step list, output panel, status bar
```

## MCP Server (for AI agents)

```bash
# Start MCP server on stdio
./gert-mcp --stdio

# AI agent sends:
# {"jsonrpc":"2.0","method":"tools/call","params":{"name":"gert/validate","arguments":{"path":"runbooks/health.yaml"}},"id":1}
```

## Watch mode

```bash
./gert watch runbooks/health-check.yaml --interval 5m --var hostname=srv1 --stop-on escalated
# 14:30:00  ✓ resolved (service_healthy)   2.1s
# 14:35:00  ✓ resolved (service_healthy)   1.8s
```

## Development workflow

```bash
# Run all kernel tests (must pass before every commit)
go test ./pkg/kernel/... -v -count=1

# Run all scenario tests
./gert test examples/service-health-check.yaml
./gert test runbooks/*.yaml

# Validate all tool definitions
for f in tools/*.tool.yaml; do ./gert validate "$f"; done
```
