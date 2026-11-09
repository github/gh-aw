---
"gh-aw": patch
---

Populate Gemini run metadata `version` and `agent_version` with the default CLI installation version when `engine.version` is omitted. Explicit version pins remain unchanged. Recompile affected workflows to apply the correction to future runs; existing run artifacts are not rewritten.
