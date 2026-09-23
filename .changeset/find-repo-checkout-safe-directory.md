---
"gh-aw": patch
---

Fix `find_repo_checkout` so the git-scan fallback can discover repositories cloned into the workspace by a `steps:` entry or a manual `actions/checkout`. The `remote.origin.url` read now runs with a per-invocation `safe.directory` override, which previously failed silently inside the safe-outputs container because of git's dubious-ownership protection, without relaxing git trust process-wide. Scanned repositories are bound to an `owner/repo` slug only when their remote host matches `GITHUB_SERVER_URL` or `github.com`, so an agent-planted repository configuration cannot claim an allowlisted repository. The not-found error message now describes both `checkout:` frontmatter entries and workspace clones.
