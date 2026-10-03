---
"gh-aw": major
---

Make Claude unattended execution enforce deterministic tool permissions and retry lifecycle controls.

**Breaking change:** Claude now defaults to `permission-mode: dontAsk` instead of `acceptEdits` or `auto`. Safe outputs no longer grant unrestricted file writes. Disabled Bash/web tools are removed, `tools.edit: false` denies repository edits, and memory/sandbox file grants use absolute recursive Claude `Read`/`Edit` rules.

Claude retries resume the captured session ID. A restart after partial execution is refused by default to prevent replaying tool work and staged outputs. Replay-safe workflows can explicitly set `engine.env.GH_AW_CLAUDE_ALLOW_FRESH_RESTART: "true"`.

**Migration:** Recompile Claude workflows. Enable `tools.edit` explicitly for repository changes. Use `engine.permission-mode: acceptEdits`, `auto`, or `bypassPermissions` only when approval beyond the declared pre-approved tool rules is intentional; retain sandbox and gateway policies for isolation. Custom harnesses must implement their own runtime policies.

Resolve tool/startup timeout expressions at runtime, deliver prompts through stdin, honor post-result watchdogs and active soft deadlines, stop on structured proxy guardrail errors, and preserve provider/authentication/bare configuration in detection jobs. Bare-mode workflow-declared skills load explicitly through a `gh-aw-workflow` plugin without restoring ambient discovery. Use `bare: false` for automatic Skill-tool invocation or subagent delegation.

Correct Claude retry cost/token accounting and tool-result size attribution, with Go and JavaScript regression coverage.
