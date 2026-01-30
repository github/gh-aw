---
"gh-aw": patch
---

Use runtime-import macros for the main workflow markdown so the lock file can stay small and workflows remain editable without recompiling; frontmatter imports stay inlined and the compiler/runtime-import helper now track the original markdown path, clean expressions, and cache recursive imports while the updated tests verify the new behavior.
