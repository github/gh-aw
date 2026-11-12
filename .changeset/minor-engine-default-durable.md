---
"gh-aw": minor
---

Allow behavior-defined engine definitions to enable durable continuation by default with `behaviors.durable.default-retention-days` (1 to 90), and let workflows opt out with `engine.durable: false` or override `retention-days`. Attribute engine-default durability in the experimental-feature warning, and compile incompatible workflow shapes (`workflow_call`, `work-queue`, explicit concurrency, or custom command, driver, or harness overrides) non-durably with a warning when durability was not requested explicitly.
