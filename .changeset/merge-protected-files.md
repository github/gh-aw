---
"gh-aw": major
---

Block `merge-pull-request` when the target PR changes protected files, using the same protected set and exclusions as `create-pull-request`. Check the complete live file list, including deleted and renamed paths, and revalidate before live or staged merges. Support `protected-files: allowed` as an explicit opt-out and object-form exclusions; all existing merge gates remain enforced.

**Breaking change:** Protection is enabled by default. Existing `merge-pull-request` workflows that previously merged protected-file changes now refuse those merges unless explicitly configured otherwise.

**Migration:** Review affected workflows and recompile them after updating trusted configuration. Keep the default and merge protected-file PRs manually, use `safe-outputs.merge-pull-request.protected-files.exclude` for scoped exceptions, or set `safe-outputs.merge-pull-request.protected-files: allowed` when the workflow intentionally permits protected-file changes. The opt-out does not bypass caution notices or other merge gates.
