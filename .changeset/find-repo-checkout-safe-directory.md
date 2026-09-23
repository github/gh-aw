---
"gh-aw": patch
---

Fix `find_repo_checkout` so the git-scan fallback can discover repositories cloned into the workspace by a `steps:` entry or a manual `actions/checkout`. Each scanned repository is now trusted via `safe.directory` before reading `remote.origin.url`, which previously failed silently inside the safe-outputs container because of git's dubious-ownership protection. The not-found error message now describes both `checkout:` frontmatter entries and workspace clones.
