# ADR-80422: Add Five Actions/AW Lint Rules and a Daily Rule Minter

**Date**: 2026-11-23
**Status**: Accepted

## Context

`gh aw lint` (`pkg/workflow/lint`) did not cover several documented GitHub Actions vulnerability classes. External tools such as zizmor, poutine, and actionlint flag them, but they do not understand agentic workflow (AW) frontmatter. AI-generated workflows often contain the same unsafe patterns: bot-name trust checks, `contains()` checks on attacker-controlled strings, `secrets: inherit`, deprecated `set-env`/`add-path` commands, and `curl | sh` installers. New classes appear continuously, so writing every rule by hand does not keep pace.

## Decision

Add five AST-based rules to `Default()`. Each rule targets `ActionsAST`, `AWAST`, or both, and operates on parsed ASTs instead of scanning raw text:

- `bot-conditions`: `if:` conditions or `on.bots` trust based on spoofable actor or bot names.
- `unsound-contains`: `contains()` checks that attacker-controlled substrings can satisfy.
- `overprovisioned-secrets`: `secrets: inherit` and secrets broader than the job needs.
- `insecure-commands`: `ACTIONS_ALLOW_UNSECURE_COMMANDS` and deprecated workflow commands.
- `remote-script-execution`: piping downloaded content to a shell.

Severities follow exploitability. Patterns that are directly exploitable are errors. Patterns that depend on context are warnings. Diagnostics report structural paths only and offer no autofixes.

Add `.github/workflows/actions-aw-lint-rule-minter.md`. This daily workflow mints at most one new evidence-backed rule per run, or does nothing. It writes only through a **draft** `create-pull-request` restricted by `allowed-files: pkg/workflow/lint/**`, with `protected-files: blocked` except `/pkg/workflow/lint/README.md`. It treats all external content as untrusted data. A maintainer must review and merge every minted rule.

## Alternatives Considered

- **Rely on external zizmor or actionlint.** Rejected. Neither tool understands AW frontmatter, compiler-generated patterns, or gh-aw's taint and expression APIs, and both add an extra tool to users' toolchains.
- **Combine all checks into one rule.** Rejected. Users could not select, suppress, or grade the checks separately, and each check would lose its own severity and contract tests.
- **Write rules manually only.** Rejected as the only path. The minter adds steady, reviewed coverage, and manual authoring remains available.

## Consequences

- Existing workflows can now produce new warnings or errors. Examples include the `on.bots` pattern the compiler emits for squad workflows, `secrets: inherit` in windows-grower, and `curl | sh` in `shared/sq.md`. Owners must fix these findings or accept them.
- Maintainers must review and maintain five more rules, plus a daily stream of draft PRs.
- The minter can run without supervision only because every write is a draft PR within a narrow file allowlist and requires human review.
