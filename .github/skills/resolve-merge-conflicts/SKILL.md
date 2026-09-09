---
name: resolve-merge-conflicts
description: Merge a base ref and safely regenerate compiled workflow lock-file conflicts.
tools:
  bash:
    - "./.github/skills/resolve-merge-conflicts/resolve.sh *"
    - "git diff *"
    - "git status *"
---

# Resolve Merge Conflicts

Use this skill when merging `origin/main` into a branch, especially when the
only conflicts are generated `.github/workflows/*.lock.yml` files.

## One-step path

From the repository root, run:

```bash
./.github/skills/resolve-merge-conflicts/resolve.sh origin/main
```

The command works both before a merge and after another command has stopped on
conflicts. It:

1. Starts the merge with `--no-commit`, or resumes the current merge.
2. Refuses to auto-resolve if any conflict is not a workflow `.lock.yml`.
3. Scans `.github/workflows/*.md` for leftover conflict-marker lines
   (`<<<<<<<`, `|||||||`, `=======`, `>>>>>>>`) and aborts before compiling
   if any are found — see "Why the marker scan matters" below.
4. Runs `make recompile` once so generated files come from the merged Markdown.
5. Stages the regenerated conflicting lock files.
6. Verifies that no unresolved paths or whitespace errors remain.

The script does not fetch, commit, push, abort, or edit workflow Markdown.
Refresh `origin/main` first only when credentials are available. After success,
review the staged merge, run the repository's final validation gate, then
commit and push.

## Why the marker scan matters

A source `.md` conflict resolved manually (by a human or an agent) can leave
a stray conflict-marker line behind — most often the rarely-noticed
`||||||| base (original)` diff3 marker — inside a workflow's YAML
frontmatter. `git diff --check` only inspects lines touched by the current
diff/staged hunks, so a marker already committed in otherwise-unchanged file
content passes silently. The gh-aw compiler then parses the marker text as a
literal YAML header option (e.g. `invalid header option: "|||||| base
(original)"`), which fails compilation later — often in an unrelated
scheduled recompilation run, far from the original merge, making the root
cause hard to trace back.

Run the standalone check any time you suspect a workflow `.md` file went
through manual conflict resolution, even outside this script's merge flow:

```bash
./.github/skills/resolve-merge-conflicts/resolve.sh --verify-markers
```

It exits non-zero and lists the offending file(s)/line(s) if any marker is
found, and does not modify the working tree.

## Safety rules

- Never choose `ours` or `theirs` for compiled lock files; regenerate them.
- Never manually remove conflict markers from `.lock.yml` files.
- Never auto-resolve a source `.md`, Go, JavaScript, or other mixed conflict.
- If the script refuses a mixed conflict, resolve source conflicts on their
  merits, stage them, and rerun the same command. It will regenerate the
  remaining lock conflicts.
- Do not abort an existing merge unless the user explicitly requests it.

