---
"gh-aw": patch
---

Authorize allowlisted GitHub App actors on `repository_dispatch` even though Apps are never repository collaborators. The installation lookup used by the `on.bots:` allowlist reports Apps as inactive, so App-triggered dispatches were skipped at pre-activation; posting a `repository_dispatch` already requires `contents: write` on the repository, so the lookup is now optional for that event. Other events still require the bot to be installed.
