---
"gh-aw": patch
---

Sort safe output tool messages by their temporary ID dependencies before dispatching them so single-pass handlers can resolve every reference without multiple retries.
