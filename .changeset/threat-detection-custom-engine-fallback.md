---
"gh-aw": minor
---

Fix threat detection failing with `config_error` for custom engines: detection now falls back to a built-in engine (with a compile-time warning), and engine definitions can declare their own `detection-engine`.
