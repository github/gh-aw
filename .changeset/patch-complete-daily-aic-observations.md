---
"gh-aw": patch
---

Persist verified daily AIC scan observations in activation artifacts instead of relying on a conclusion-only Actions cache. Stop on API failures and fail activation when daily accounting is incomplete, rather than reporting a partial total as under budget. Recompile workflows to use the new snapshot producer and restore steps.
