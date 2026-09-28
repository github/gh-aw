---
"gh-aw": patch
---

Fix `pushSignedCommits` unnecessarily failing the whole pull-request-creation operation when the agent's commits genuinely conflict with the base branch during the pre-replay rebase.

Previously, when the base branch advanced with content that conflicted with the agent's changes (e.g. both edited `CHANGELOG.md`), `pushSignedCommits` aborted the rebase and threw, which caused `create_pull_request` to give up on the pull request entirely and open a fallback issue instead — even though a normal (unsigned) `git push` of the un-rebased commits would have let GitHub create the pull request and simply report it as having conflicts, exactly like any other PR.

`pushSignedCommits` now falls back to an unsigned `git push` of the original, un-rebased commit range when a genuine (non-recoverable) rebase conflict is detected, unless `allowGitPushFallback: false` is explicitly requested. This lets the pull request still be created in a "has conflicts" state so it can be resolved normally, instead of failing the run and falling back to a GitHub issue.

If the unsigned push itself is rejected (for example, by a branch-protection rule that requires signed commits), `pushSignedCommits` now throws a combined error describing both the original rebase conflict and the push rejection, instead of letting the raw `git push` failure propagate on its own.

The un-rebased commit range still goes through the same preflight validation as any other unsigned push fallback (merge-commit detection, unsupported file modes such as symlinks/submodules, and file-protection/size policy) before being pushed, and a failed `git rebase --abort` cleanup is now treated as fatal rather than silently ignored, so this fallback never pushes content that should have been refused or operates on a possibly-corrupted worktree.

This same fallback (including the preflight checks and fatal-abort handling) now also applies when a genuine content conflict is only revealed *after* a shallow/partial-clone object backfill succeeds and the retried rebase still fails — previously that case unconditionally threw instead of falling back, even though it is the same class of "genuine conflict" as the non-backfill case.
