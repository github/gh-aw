---
"gh-aw": patch
---

Use AWF's native protected command output for behavior-defined engines instead of a runner-side combined-stream tee. Parse only complete final-attempt stderr, keep raw streams private, preserve native exit outcomes, explicitly exec the recorder harness, and reject unsupported runtimes or incomplete collection without shared-log fallback.

Fail protected session publication when secret redaction would change structural fields, and remove stale publishable session artifacts on verification or publication failure.

Bind configured redaction secrets to the trusted parser and verify protected sessions before outcome collection. Disable shared-log recovery for protected outcomes, and require successful verification for both unified and fallback artifact publication.
