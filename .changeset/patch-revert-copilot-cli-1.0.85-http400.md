---
"gh-aw": patch
---

Pin the default Copilot CLI version back to 1.0.83 (from 1.0.85). Copilot CLI 1.0.85 was reported to return an instant HTTP 400 "Bad Request" on the very first `/responses` request for some workflow shapes, failing agent runs before any tokens are consumed (github/gh-aw#62363). The `agent-compat-v1` copilot compatibility window in `.github/aw/compat.json` was also capped at `max-agent: 1.0.83` so workflows resolving a Copilot CLI version through the compatibility matrix do not pick up 1.0.85 either.
