# ADR-64334: Use GitHub App token for gh-proxy

**Date**: 2026-09-29
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request changes how `tools.github.mode: gh-proxy` authenticates `gh` commands when `tools.github.github-app` is configured. Before this change, the CLI proxy resolved `GH_TOKEN` from the explicit GitHub token setting or the default secret fallback, which meant private repositories available only through the job-minted App installation token were not readable through the proxy. The diff also adds tests and documentation that clarify credential boundaries between proxy authentication and safe outputs authentication. The architectural decision is how `gh-proxy` should choose its credential when both proxy mode and a GitHub App configuration are present.

### Decision

We will make `gh-proxy` use the same-job GitHub App installation token whenever `tools.github.mode` is `gh-proxy` or `cli` and `tools.github.github-app` is configured. In that mode, the proxy will fail closed by forcing the App token minting step to require credentials instead of silently honoring `ignore-if-missing` fallback behavior. Safe outputs authentication remains independent: the proxy App token is used only for the agent job's `gh` access, while safe outputs continue to use their separately configured token or workflow fallback. We chose this because the PR evidence shows the default token path could not access private repositories granted to the App installation, breaking proxy-backed GitHub reads.

### Alternatives Considered

#### Alternative 1: Keep the existing explicit-token and default-secret resolution for gh-proxy

This was the prior behavior and remains a viable path when no GitHub App is configured. It was not chosen for the App-configured case because the PR description and tests show that the default proxy token could not read private repositories that were available only to the minted installation token.

#### Alternative 2: Reuse the proxy App token for safe outputs as well

This was a realistic simplification because both features perform GitHub-authenticated operations inside the same workflow. It was not chosen because the PR explicitly preserves credential boundaries, and the added tests assert that safe outputs must continue using their own configured credential rather than inheriting the source App token.

### Consequences

#### Positive
- `gh-proxy` can read repositories that are available to the configured GitHub App installation but not to the default workflow token.
- Proxy authentication behavior becomes deterministic when a GitHub App is configured, with test coverage for token selection, ordering, and failure behavior.
- Documentation now explains that proxy `allowed-repos` and integrity policies remain separate from the App installation scope.

#### Negative
- Workflows using `gh-proxy` with `ignore-if-missing` on the App key now fail the mint step instead of falling back, which is a stricter operational requirement.
- The compiler now carries another mode-specific authentication branch that must stay aligned across proxy startup, token minting, and validation tests.

#### Neutral
- Existing explicit `github-token` and default secret fallback behavior is preserved when no GitHub App configuration is present.
- The change affects workflow compilation and generated authentication wiring rather than runtime business logic outside GitHub access.
- The repository gains additional unit tests documenting credential isolation between the proxy and safe outputs jobs.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
