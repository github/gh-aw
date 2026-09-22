# ADR-62416: Add explicit opt-out for CI trigger token emission

**Date**: 2026-09-21
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how gh-aw compiles workflows that use `create-pull-request` or `push-to-pull-request-branch` safe outputs. The PR description explains that compiled lock files always referenced `GH_AW_CI_TRIGGER_TOKEN` even in repositories that intentionally do not configure that secret, which made the generated workflow and `gh-aw-manifest` imply a requirement that did not actually exist at runtime. The code diff shows the compiler currently treats the extra-empty-commit token as always present unless set to `app`, and this PR adds a distinct disabled state plus documentation and regression tests. The design question is how workflow authors should suppress CI-trigger token emission without changing the existing default behavior for repositories that rely on the extra empty commit to start CI.

### Decision

We will support `github-token-for-extra-empty-commit: none` as an explicit opt-out for `create-pull-request` and `push-to-pull-request-branch`. During compilation, gh-aw will treat `none` and `app` as case-insensitive sentinel values, omit the `GH_AW_CI_TRIGGER_TOKEN` environment variable entirely when `none` is selected, and keep the current default behavior otherwise. We chose an explicit opt-out instead of inferring behavior from `permissions.copilot-requests: write` so repositories that still depend on the extra empty commit do not silently stop triggering CI.

### Alternatives Considered

#### Alternative 1: Automatically omit the token when `permissions.copilot-requests: write` is present

This was the behavior requested in the underlying issue and was considered because it would reduce lock-file references automatically for some repositories. It was not chosen because the PR description and code comments make clear that Copilot inference permissions and the CI-trigger token solve different problems; omitting the token automatically would break workflows that still need the extra empty commit to trigger CI.

#### Alternative 2: Keep always emitting `GH_AW_CI_TRIGGER_TOKEN` and rely on runtime no-op behavior

The previous behavior effectively kept the secret reference in compiled workflows even when the secret was unset, allowing runtime handling to skip the empty commit. This was considered because it preserves a single compilation path and does not add new configuration semantics. It was not chosen because the PR evidence shows that the lock file and manifest then misrepresent repository requirements and force users who intentionally do not use the secret to carry an unnecessary secret reference in generated workflows.

### Consequences

#### Positive
- Repositories that do not use `GH_AW_CI_TRIGGER_TOKEN` can remove that secret reference from both compiled workflow steps and the `gh-aw-manifest`.
- Existing workflows keep their current behavior unless authors explicitly opt out, avoiding silent CI-trigger regressions.
- Regression tests now cover the disabled state and mixed-case sentinel handling for both PR-related safe outputs.

#### Negative
- The compiler now carries another sentinel configuration value that must remain documented and consistently handled across schema, docs, and code.
- Using `none` disables the extra empty commit, so authors can unintentionally stop CI from triggering on created or updated PR branches if they misunderstand the setting.
- Token-selection precedence remains asymmetric when both PR safe outputs specify conflicting values, leaving that edge case intentionally unresolved for now.

#### Neutral
- The change refactors token emission into a dedicated helper without changing unrelated safe-output token handling.
- Custom token expressions still preserve their original casing and semantics because only sentinel matching is normalized.
- The user-facing configuration surface grows by one documented option rather than introducing a new field.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
