---
"gh-aw": patch
---

Fix runtime cache-memory prompts to use workflow-defined cold-start fallbacks without reporting optional missing history as a failure, while preserving reports for missing or unusable required inputs. Clarify that a successful cache restore does not guarantee every expected file exists.
