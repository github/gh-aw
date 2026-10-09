---
"gh-aw": major
---

Replace the work-queue Issue `status-field` projection with repository labels.

**Breaking change:** `tools.work-queue.issues.status-field` is no longer
accepted. Remove it from workflow frontmatter. Use `issues: true` for the
default `work` label prefix or `issues: { label: cookie }` for a custom prefix;
status labels such as `work: Queued` are created in the repository.
