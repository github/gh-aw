---
"gh-aw": patch
---

Reconcile the original reservation after an uncertain worker dispatch response, including GitHub server errors and lost transport responses, without resending the launch or cancelling the worker. Add sanitized recovery diagnostics and retain the durable start fence when discovery or publication remains unavailable.
