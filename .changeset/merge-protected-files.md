---
"gh-aw": minor
---

Block `merge-pull-request` when the target PR changes protected files, using the same protected set and exclusions as `create-pull-request`. Check the complete live file list, including deleted and renamed paths, and revalidate before live or staged merges. Support `protected-files: allowed` as an explicit opt-out and object-form exclusions; all existing merge gates remain enforced.
