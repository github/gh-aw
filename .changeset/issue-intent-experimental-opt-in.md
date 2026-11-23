---
"gh-aw": major
---

Make `issue-intent` an experimental, opt-in option on `close-issue`, `add-labels`, `assign-to-agent`, `assign-to-user`, `set-issue-type`, and `set-issue-field`. Intent metadata (`rationale`, `confidence`, `suggest`) is now used only when `issue-intent: true` is set, and the compiler emits "Using experimental feature: issue-intent" when it is enabled.

**⚠️ Breaking Change**: Workflows that omit `issue-intent` previously forwarded intent metadata by default. They now strip the intent fields from the generated tool schemas, and the handlers ignore any intent metadata they receive (logging that `issue-intent` is not enabled).

**Migration guide:**
- To keep the previous metadata behavior, set `issue-intent: true` on each affected safe output and run `gh aw compile` to regenerate the lock file.
- With `issue-intent: true`, `add-labels` requires label objects with `rationale` and `confidence`; plain string labels are rejected.
- Workflows that set `issue-intent: false` or do not use intent metadata need no changes.
