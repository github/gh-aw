---
"gh-aw": minor
---

Support `github-token-for-extra-empty-commit: none` on `create-pull-request` and `push-to-pull-request-branch` to skip the extra empty commit. When set, `GH_AW_CI_TRIGGER_TOKEN` is no longer emitted in the compiled lock file or listed in its `gh-aw-manifest`, so repositories that do not configure the magic secret can keep it out of their compiled workflows.
