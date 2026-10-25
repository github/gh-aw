---
"gh-aw": minor
---

Add a shared `health --timeout` collection budget (default `2m`, `0` disables it), also available through the health MCP tool. Cancel in-flight API and Git reads when the budget expires, retain collected evidence in explicitly partial reports, and log request progress and timings with `DEBUG=cli:health`.
