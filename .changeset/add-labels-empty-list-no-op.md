---
"gh-aw": patch
---

Treat an `add_labels` safe output with an empty label list as a no-op with a warning instead of a failure, so runs are no longer marked as failed when the agent emits `labels: []`. The `add_labels` tool schema now declares `minItems: 1` on `labels` so agents are steered away from emitting an empty list.
