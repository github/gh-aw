---
"gh-aw": patch
---

Report compile dry-run scope and scanner invocation coverage in text and an additive JSON batch summary, including the overall gate and explicit lack of execution authorization. Propagate observed model inventory refresh/collection warnings instead of silently accepting stale evidence; dry-run treats these warnings as errors, while ordinary model checking remains warning-only. Correct documentation for optional scanners, global staging precedence, and custom-effect exclusions.
