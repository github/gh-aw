---
"gh-aw": minor
---

Add the `untrusted-roles-detection` linter (CTR-005). Strict mode (the default) now rejects workflows that combine `on.roles: all` with a trigger untrusted users can fire (for example `issues`, `issue_comment`, or `pull_request`) when `safe-outputs.threat-detection` is disabled. With `strict: false`, the same combination is reported as a warning. Triggers that only trusted actors can fire (`schedule`, `workflow_dispatch`, `push`, `merge_group`) are exempt. The diagnostic also recommends the NVX microVM sandbox (`sandbox.agent.runtime: nvx`). The default container sandbox runs as the runner's user and shares the runner's `/tmp`, which exposes it to runner-escape attacks like the one Depi Security reported against the Codex issue-labeler workflow.

To fix this, remove `threat-detection: false` or restrict `on.roles` to trusted repository roles. A time-limited `threat-detection-suppress` entry for `CTR-005` can acknowledge the risk temporarily.
