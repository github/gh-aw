---
"gh-aw": patch
---

Handle 404s from `add_comment` by warning instead of retrying the request as a discussion so deleted targets are skipped cleanly.
