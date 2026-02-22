# Service Fabric Explorer Integration

## Status: collecting

## Problem
Several TSGs require checking Service Fabric Explorer (SFE) for cluster health, deployment status, node states, and application health. Today this is a browser-based tool. Rendering it inside VS Code would eliminate context switching and allow gert to capture SFE state as evidence.

## Proposed UX
- A VS Code extension that renders SFE inside a webview panel
- Gert manual steps that reference SFE could open the relevant cluster/node/application view
- User inspects state, then sends relevant data back to gert (similar to xts4vscode flow)
- SFE state snapshots captured as evidence (JSON health data + visual screenshot)

## Technical Notes
- SFE is a web app — could it be embedded in a VS Code webview iframe?
- SFE REST API: `GET /Applications`, `GET /Nodes`, `GET /$/GetClusterHealth`
- Alternative: skip rendering, just call the SFE REST API directly from a gert provider
- Need to understand SFE authentication (cluster certificates, AAD)

## Examples
<!-- Capture screenshots of SFE showing cluster health, node views, deployment status -->
<!-- Document SFE REST API endpoints used in TSGs -->

## Open Questions
- Is embedding SFE in an iframe viable (CORS, authentication)?
- Would a REST API provider be simpler and sufficient?
- What SFE data do TSGs actually reference? (cluster health, deployment status, node state)
- Is there an existing SFE VS Code extension?
