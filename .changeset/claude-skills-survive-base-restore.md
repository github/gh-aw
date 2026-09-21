---
"gh-aw": patch
---

Keep skills installed by workflow steps (for example the APM package restore) when the agent job restores agent config folders from the base branch. The restore step now deletes only the git-tracked files of each agent folder, so untracked files written by earlier trusted steps into `.claude/skills/` or `.github/skills/` survive. Also allow Claude's `Skill` tool by default so skills can be invoked without `permission-mode: bypassPermissions`.
