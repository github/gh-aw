---
"gh-aw": patch
---

Retry transient worker source, lock, and registration reads with bounded attempts and timeouts. Emit always-visible, secret-safe retry timing and recovery diagnostics without retrying dispatches or queue writes.
