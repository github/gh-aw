---
"gh-aw": patch
---

Ensure generated issue, comment, discussion, and pull request bodies always keep an empty line after a closing `</details>` tag so markdown that follows a collapsible block renders correctly on GitHub. Closing tags inside fenced code blocks or blockquotes are left untouched.
