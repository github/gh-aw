---
"gh-aw": patch
---

Fix Codex configuration merging, shell environment inheritance, plugin registration, inline skill discovery, inference endpoint routing, runtime recovery, process shutdown, and native JSONL accounting.

Codex search and page fetching now use the CLI's shared browsing capability instead of an unsupported fetch setting. Invalid native provider combinations fail compilation with actionable guidance. Custom TOML is structurally merged; use `engine.env` and `${ENV_VAR}` rather than embedding Actions expressions, and configure MCP transports and AWF upstream endpoints through their dedicated frontmatter fields.

Custom shell filters replace inherited legacy arrays, short model options do not receive duplicate defaults, forced shutdown completes descendant termination, and framed tool-result JSON cannot override compatibility or OpenTelemetry accounting.

Unified conclusion sessions retain Codex turn outcomes, native terminal event types, and reasoning-token accounting without duplicating cache or output totals.
