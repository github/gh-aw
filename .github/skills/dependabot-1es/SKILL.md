---
name: dependabot-1es
description: Review open Dependabot npm pull requests in github/gh-aw, verify exact dependency updates against the Microsoft 1ES feed, and optionally merge eligible PRs.
---

# Dependabot updates through 1ES

Run from a `github/gh-aw` worktree with authenticated `gh`, Git, Node.js
22.18+ (or 24.2+), and npm.
Use the script in this skill directory:

```sh
node .github/skills/dependabot-1es/update.mjs
node .github/skills/dependabot-1es/update.mjs --pr 12345
node .github/skills/dependabot-1es/update.mjs --apply --pr 12345
```

Run without `--apply` for a report. Use `--apply` only when the user asks to
merge or apply updates. Omit `--pr` to process all open Dependabot PRs against
`main`; repeat `--pr` to select a batch. Apply mode squash-merges the original
PRs, in order, without admin overrides or auto-merge. It does not edit the
worktree or create replacement PRs.

The script handles manifest/lockfile-only npm updates in the docs and setup
action projects. It compares the lockfiles in GitHub's test merge to
its base parent, checks metadata and downloads tarballs for the exact added
or changed versions on 1ES (including optional packages), and runs
`npm ci --ignore-scripts` against the feed in a temporary directory. It does
not run project tests or verify browser downloads or other lifecycle effects;
GitHub checks must pass before the script will merge.

Report which PRs are eligible, merged, or skipped and why. Leave empty merges, conflicts,
non-npm changes, unsupported dependency sources, missing feed versions, and
failed or pending checks untouched. If the feed is unreachable, stop and use
the [npm skill](../npm/SKILL.md) to diagnose; do not change committed registry
settings or fall back to npmjs. If the base or PR head moved, rerun once GitHub
has refreshed the test merge. A nonzero exit means at least one selected
candidate could not be inspected, verified, or merged, or no selected PR matched.
