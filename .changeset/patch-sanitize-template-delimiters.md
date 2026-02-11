---
"gh-aw": patch
---

Document that `sanitize_content_core.cjs` now neutralizes common template delimiters (Jinja2, Liquid, ERB, JavaScript, Jekyll) to prevent downstream template injection bypasses.
