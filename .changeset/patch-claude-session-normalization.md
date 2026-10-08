---
"gh-aw": patch
---

Normalize Claude session message attribution, MCP tool identities, reasoning token usage, and terminal status. Preserve streamed message blocks and corrected snapshots without duplicating answers, and keep permission-denial notices separate from assistant messages.

Normalize Claude local-agent lifecycle events and preserve nested caller scope through unified traces. Isolate child messages, tool pairing, initialization, and usage from root accounting, including reused IDs and grandchildren, and label nested conversations separately in summaries.
