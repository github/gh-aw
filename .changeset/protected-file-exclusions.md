---
"gh-aw": patch
---

Add repository-wide protected-file path glob exclusions through `protected_files.exclude` in `.github/workflows/aw.json`. Reject wildcard-only catch-all patterns in repository and handler exclusion lists.
