---
"gh-aw": patch
---

Introduce `sanitized_logging.cjs` wrappers (`safeInfo`, `safeDebug`, `safeWarning`, `safeError`) and apply them to previously vulnerable `core.info()` calls so user-controlled strings can't inject workflow commands.
