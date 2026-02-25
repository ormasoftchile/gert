---
mode: agent
---

Commit fixes in a dedicated branch.

Inputs:
- branch_name: <required>
- commit_message: <optional, default: "fix: apply requested changes">
- include_untracked: <optional, default: true>

Do the following in order:
1) Ensure we are in the repository root.
2) Create or switch to branch `${branch_name}`.
   - If it does not exist locally, create it from current HEAD.
3) Show status and summarize changed files.
4) Stage the current working changes:
   - If `include_untracked=true`, stage tracked + untracked files.
   - If false, stage tracked changes only.
5) Commit with `${commit_message}`.
   - If no staged changes exist, report and stop without error.
6) Report branch name, commit SHA, and committed file list.

Constraints:
- Do not push.
- Do not modify code beyond staging/commit actions.
- If merge conflicts/unmerged paths exist, stop and report exactly which files block commit.
