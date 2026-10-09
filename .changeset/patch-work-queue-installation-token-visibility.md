---
"gh-aw": patch
---

Determine native work-queue read visibility from successful Git API reads rather than repository collaborator permission flags, which can be false for workflow installation tokens. Preserve repository identity checks and real API denials, and require a readable default ref before reporting a missing queue in a nonempty repository.
