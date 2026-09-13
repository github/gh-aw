---
"gh-aw": patch
---

Bind operational-value graders to the workflow run creation time and preserve the full grader result contract in `gh aw logs` output. `generate_aw_info` now normalizes and retries the run-created-at lookup, the graders step falls back to Actions run metadata and reports an explicit acquisition failure instead of grading an invalid request, and grader results keep `source`, `implementation`, `observation`, `diagnostics`, `baselineValue`, `deltaFromBaseline`, and their declared unit when serialized. The conclusion job's usage activity summary now embeds the full grader result document, and `gh aw logs` falls back to it when `grader_results.json` is unavailable.
