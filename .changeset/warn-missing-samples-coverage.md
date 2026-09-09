---
"gh-aw": patch
---

Warn when samples replay is enabled but a safe output has no samples

Compiling with `--use-samples` (or `features.samples: true`) now emits a warning listing every enabled safe output that declares no `samples:` entries. Previously such workflows compiled with an empty `GH_AW_SAMPLES` array and the run succeeded without performing any safe output, which made sampled end-to-end suites silently skip the operation under test (for example `replace-label`, which left its fixture label unchanged).

The `test-copilot-replace-label` test workflow now carries a canonical `replace-label` sample so it is exercised deterministically under samples replay.
