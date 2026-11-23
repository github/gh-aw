# ADR-80482: Add Advisory `hasprefixdotdotpathescape` Analyzer for Parent-Path Prefix Checks

**Date**: 2026-11-23
**Status**: Draft

## Context

Path-containment checks in gh-aw often use `strings.HasPrefix(value, "..")` to decide whether a relative path escapes its base directory. That prefix also matches legitimate names such as `..foo`, so the check can reject safe paths or accept the wrong shape of path. The linked issue #80478 reports unsafe checks of this form in `pkg/parser/include_expander.go`, `pkg/parser/remote_download_file.go`, `pkg/gitutil/gitutil.go`, and `pkg/cli/poutine.go`. Those findings come from the issue body and were not re-verified in this gate.

The repository already ships a custom `go/analysis` linter suite under `pkg/linters/`, with an `analyzerutil` helper, a shared `nolint` directive index, and a documentation sync test (`pkg/linters/doc_sync_test.go`) that tracks analyzers that are documented but not yet enforced in CI. Existing call sites have not yet been audited, so turning a new check on in CI would immediately fail on code that has not been reviewed.

## Decision

Add the `hasprefixdotdotpathescape` analyzer under `pkg/linters/hasprefixdotdotpathescape/` and register it in `pkg/linters/registry.go`. The analyzer:

- Matches `strings.HasPrefix` calls by resolved type (`types.Func` in package `strings`), so aliased imports, dot imports, and constant prefixes such as `"\x2e\x2e"` or a `const` built from `"." + "."` are detected.
- Reports only when the prefix value is exactly `..`. Checks such as `path == ".."` or `HasPrefix(path, "../")` and `HasPrefix(path, ".." + string(filepath.Separator))` are left alone.
- Skips generated files and honors the existing `nolint:hasprefixdotdotpathescape` directive.
- Recommends checking the exact `..` component plus a separator, or using `filepath.IsLocal`.

CI enforcement is deferred. The analyzer is listed in `notYetEnforced` in `pkg/linters/doc_sync_test.go` with the reason "existing parent-path checks need review and remediation before CI enforcement". It is documented in `pkg/linters/README.md` and `pkg/linters/doc.go` (analyzer count updated to 102), and it is listed in the `spec_test.go` documented-analyzers table.

## Alternatives Considered

- **Enforce the analyzer in CI immediately.** Rejected. Existing call sites named in #80478 have not been reviewed, so enforcement would block unrelated PRs before remediation. The `notYetEnforced` mechanism exists for this case.
- **Use a text or regex check in CI instead of a `go/analysis` analyzer.** Rejected. A textual check cannot resolve aliased or dot-imported `strings.HasPrefix`, constant-folded prefixes, or shadowed names. The analyzer resolves these through `pass.TypesInfo`, and the testdata fixtures cover them.
- **Fix the flagged call sites by hand and add no analyzer.** Rejected as the only action. It fixes today's instances but gives no guard against new ones. Remediation of existing call sites remains a follow-up before enforcement, as the PR states.

## Consequences

**Positive**

- Path-containment checks written with `strings.HasPrefix(value, "..")` get a diagnostic that points to the exact-component fix or `filepath.IsLocal`.
- The analyzer reuses existing linter infrastructure (`analyzerutil`, `nolint`, generated-file filtering) and has positive and negative `analysistest` fixtures.
- The documentation sync test keeps the README, `doc.go`, and the spec table aligned with the registry.

**Negative**

- Until remediation is complete, the analyzer is advisory only. It can flag existing call sites in the four files named in #80478, and CI will not catch them.
- Enforcement will require a remediation pass and then removing the entry from `notYetEnforced`. Maintainers must track that follow-up.
- Each new analyzer adds maintenance cost to the linter suite (now 102 analyzers).
