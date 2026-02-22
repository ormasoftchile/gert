---
mode: agent
---

After merge cleanup: return local repo to latest main.

Do the following in order:
1) Ensure we are in the repository root.
2) Show current branch and status.
3) Checkout `main`.
4) Fetch from `origin`.
5) Pull latest `origin/main`.
   - Prefer fast-forward (`git pull --ff-only origin main`).
6) Show final branch, latest commit (`git log -1 --oneline`), and status.

Constraints:
- Do not create commits.
- Do not stash automatically.
- If local changes prevent checkout/pull, stop and report blockers with exact next commands to resolve.
