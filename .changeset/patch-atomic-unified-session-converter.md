---
"gh-aw": patch
---

Switch the experimental Atomic sample to `--mode json` and convert its JSONL event stream into the unified session format and step summaries with `sourceEngine: "atomic"`. Fix a cross-turn message and usage collision in the shared Pi v3 session parser when a proxy reuses response IDs across turns.
