# ADR-80766: Flag Public File Permissions on Sensitive Writes

**Date**: 2026-11-24
**Status**: Draft
**Deciders**: pelikhan (PR actor); PR author gh-aw-bot
**Related**: [Issue #80765](https://github.com/github/gh-aw/issues/80765), [PR #80766](https://github.com/github/gh-aw/pull/80766)

## Context

`pkg/constants` defines `FilePermPublic` (world-readable) alongside `FilePermSensitive` (0600). The linked issue (#80765) reports a code-pattern scan that found roughly 67 uses of `FilePermPublic` under `pkg/` and `cmd/`. Some of those writes may target paths or content that hold tokens, secrets, credentials, env files, or lock state. No lint analyzer currently flags that combination.

Forces:

- Reviewing about 67 call sites by hand does not scale, and new call sites are easy to miss in review.
- Paths and variables are usually named after what they hold (`token`, `env`, `lock`), so a name-based signal is cheap to compute. It cannot prove that data is sensitive.
- The repository already enforces conventions through `pkg/linters` analyzers. `doc_sync_test.go` tracks analyzers that are registered but not yet enforced in CI.

Unknown from the PR evidence: how many of the ~67 existing call sites would be flagged by the heuristic, and whether any are real exposures. The PR does not report an audit.

## Decision

Add a custom `go/analysis` analyzer, `writefilepublicpermsensitive` (`pkg/linters/writefilepublicpermsensitive`). It reports `os.WriteFile`, `os.OpenFile`, and `os.MkdirAll` calls that pass `constants.FilePermPublic` when:

- the path argument, or the content argument for `WriteFile`, contains an identifier or constant string that matches one of `token`, `secret`, `key`, `credential`, `password`, `auth`, `env`, or `lock` (case-insensitive);
- the file is not generated and the line has no `nolint` directive for this analyzer.

The analyzer is registered in `pkg/linters/registry.go` and documented in `pkg/linters/README.md` and `pkg/linters/doc.go`. It is listed in `notYetEnforced` in `doc_sync_test.go`, so CI does not enforce it yet. Enforcement stays deferred until the heuristic findings have been reviewed.

## Alternatives Considered

1. **Flag every `FilePermPublic` use.** This is simpler and has no false negatives for sensitive data. At roughly 67 existing call sites, though, most diagnostics would not be actionable, and developers would be pushed toward blanket suppressions.
2. **Data-flow (taint) analysis from known secret sources.** This would be more precise than keyword matching, but it needs source and sink modeling across packages. That is considerably more code and upkeep than the keyword analyzer in this PR.
3. **Documentation-only guidance** that tells contributors to prefer `FilePermSensitive` for secrets. This costs nothing to build, but nothing enforces it, and existing call sites stay as they are.

## Consequences

Positive:

- Sensitive-looking world-readable writes get a named diagnostic that suggests `FilePermSensitive`, and the rule is documented alongside the other linters.
- The analyzer ships with `analysistest` fixtures covering aliases, constant strings, suppressions, and non-matching calls.
- Deferring CI enforcement lets the team audit findings before the rule can block merges.

Negative:

- Keyword matching is heuristic. It can raise false positives (for example, `keyboard` contains `key`, and `environment` contains `env`) and false negatives (sensitive data whose names contain none of the listed terms).
- Because enforcement is deferred, the analyzer is advisory only until a follow-up enables it. This PR does not change the existing `FilePermPublic` call sites.
- The analyzer count goes from 102 to 103. The README, `doc.go`, and registry must stay in sync, and the doc sync tests check that.
