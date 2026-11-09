---
"gh-aw": patch
---

Capture DeepSeek Harness AWF timeouts and failed wrapper exits as canonical session errors. Preserve failed-step diagnostics when stdout is incomplete, and prevent a contradictory wrapper exit zero from masking observed failures in live detection or unified sessions without inventing an exit code.

Register the DeepSeek host detector, preserve parsed errors alongside init-only canonical artifacts without duplicates, and retain verified wrapper failures after partial output.
