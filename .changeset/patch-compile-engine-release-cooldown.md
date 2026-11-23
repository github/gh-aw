---
"gh-aw": patch
---

Validate literal Copilot and npm engine release ages during compilation with cached public metadata and manual-validation warnings when metadata is unavailable. Avoid runtime Copilot age lookups for literal releases, and allow engine.cooldown: 0 to disable cooldown in strict mode.
