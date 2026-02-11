---
"gh-aw": patch
---

Quote environment variables with `${VAR@Q}` in the shell scripts under `actions/setup/sh/` so echo statements cannot be abused by special characters or injection vectors.
