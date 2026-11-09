---
"gh-aw": patch
---

Correct the unsupported Cursor sample's native session mapping: exclude hook metadata from tool counts, normalize reasoning and timestamps, retain model-call correlations and distinct terminal answers, and preserve cache read/write usage through unified-session projection.

Include separate cache usage in Cursor token totals, keep rejected aliases unavailable, ignore unknown overflow markers safely, and scope terminal-answer deduplication to the corresponding observation.
