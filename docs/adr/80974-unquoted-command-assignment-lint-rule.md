# ADR-80974: Detect Unquoted Command-Name Assignments in Shell Scripts

**Date**: 2026-11-25
**Status**: Draft
**Deciders**: pelikhan (PR author and actor)
**Related**: [Issue #80967](https://github.com/github/gh-aw/issues/80967), [PR #80974](https://github.com/github/gh-aw/pull/80974)

## Context

ShellCheck reports SC2209 when an unquoted assignment such as `QUERY_COMMAND=pr` has a value that is also a command name. The shared MCP script source `.github/workflows/shared/github-queries-mcp-script.md` contained two such assignments (`QUERY_COMMAND=pr` in `github-pr-query` and `QUERY_COMMAND=issue` in `github-issue-query`). They were compiled into six workflows: daily-performance-summary, daily-regulatory, smoke-copilot, smoke-copilot-aoai-apikey, smoke-copilot-aoai-entra, and smoke-copilot-arm. Issue #80967 reports that the ShellCheck total rose from 2 to 8 between two scans.

Forces:

- The finding appears only in compiled shell (`.lock.yml` `run:` blocks), so authors who edit the source Markdown cannot see it directly.
- The compiler already has a lint pipeline (`pkg/workflow/lint`) with a rule registry and a rule index in its README. A check there runs on both Actions `run:` steps and compiled mcp-scripts.
- Detecting the pattern needs the shell to be parsed, not matched as text. The `mvdan.cc/sh` parser is already available, and `taint.VisitInterpolations` can mask `${{ }}` expressions before parsing.

Unknown from PR evidence: whether ShellCheck runs by default in CI. The PR evidence shows it only as the opt-in `gh aw compile --shellcheck` flag. The PR body also says tests were not run because the change was "markdown-only", but the diff includes Go changes, so test coverage for the new code is not confirmed by the PR.

## Decision

Add a lint rule and wire its inputs through the compiler:

- `UnquotedCommandAssignmentRule()` in `pkg/workflow/lint/unquoted_command_assignment.go`. Its ID is `unquoted-command-assignment`, its severity is Warning, and it targets `ActionsAST | AWAST`. It parses POSIX `run:` steps and mcp-scripts bodies with `mvdan.cc/sh/v3/syntax`, masks `${{ }}` interpolations, and reports any call-level assignment whose single unquoted literal is one of ten command names (`awk cat date echo grep ls mkdir pr printf sed`).
- `lint.Input.ResolvedMCPScripts map[string]string` in `pkg/workflow/lint/lint.go`. The compiler fills it from the merged mcp-scripts tools in `pkg/workflow/compiler_lint.go`. A nil map means the rule falls back to the authored frontmatter.
- The rule is listed in `pkg/workflow/lint/README.md`.
- The fix itself quotes the literals in the shared source (`QUERY_COMMAND="pr"`, `QUERY_COMMAND="issue"`), and the six affected lock files are regenerated.

## Alternatives Considered

1. **Rely on the existing opt-in ShellCheck pass** (`gh aw compile --shellcheck`). This needs no new code. But it is opt-in, and the PR evidence reports the finding only after running it manually, so a regression can reach a merge without anyone seeing it.
2. **Fix the six lock files and the source only, with no rule.** This is the smallest change. It gives no protection against the same pattern being added to another mcp-script or `run:` step.
3. **Line-based regex over `run:` text.** This avoids a shell parser, but it cannot tell an assignment in a command from text inside quotes, and it would need its own handling of `${{ }}` masking. The chosen design reuses the existing `taint.VisitInterpolations` helper and the existing parser.

## Consequences

Positive:

- Regressions in this class are caught inside the compiler's lint pipeline for both Actions `run:` steps and compiled mcp-scripts, without needing ShellCheck to be enabled.
- Matching on parsed `CallExpr` assignments means a quoted value (`QUERY_COMMAND="pr"`) is not flagged, because its word part is a double-quoted node rather than a bare literal. The fix therefore passes the rule the same way it passes ShellCheck.
- The change to generated scripts is value-preserving. The literal's value is unchanged, so runtime behavior does not change.

Negative:

- The ten-name allowlist is hard-coded. New command names must be added by hand, and the rule can fire on a variable whose value merely equals a command name (for example `MODE=date`), so it can produce false positives.
- The rule is a Warning. It blocks a merge only if warnings are promoted to errors, so it is advisory by default.
- The PR body describes only the quoting fix. It does not mention the new rule, the `ResolvedMCPScripts` input, or the compiler plumbing, so reviewers relying on the PR description would miss this scope. The description should be updated to match the diff.
- `ResolvedMCPScripts` adds a second source of mcp-script bodies that must stay consistent with tool resolution, and nil and empty maps have different meanings.
