# ADR-74937: Use GitHub App Authentication for Repo-Memory

**Date**: 2026-11-04
**Status**: Draft
**Deciders**: gh-aw maintainers (review pending)

---

### Context

Repo-memory currently uses the workflow's built-in `GITHUB_TOKEN` for pushes and, where applicable, clones. That token cannot be configured as a bypass actor for repository rulesets, so rulesets restricting `memory/**` would also block the workflow's own memory updates. It also cannot provide the required access for private repositories outside the current repository. The implementation must preserve existing behavior for configurations that do not opt into app authentication while allowing each memory entry to use credentials scoped to its repository and operation.

### Decision

We will support optional per-entry GitHub App credentials in `tools.repo-memory`, in both object and array forms. For configured entries, the compiler will mint pinned installation tokens requesting only `contents: write` for pushes and `contents: read` for explicit-target clones, and will use those tokens for the corresponding Git and API operations; entries without an app continue to use `github.token`. Missing credentials may explicitly fall back to `github.token` with `ignore-if-missing: true`, and the push job will require only `contents: read` when every entry uses a non-fallback app.

### Alternatives Considered

#### Alternative 1: Continue using the built-in `GITHUB_TOKEN`

This is the existing behavior and requires no configuration or token-minting steps. It was not chosen because the built-in token cannot be added as the GitHub App bypass actor for a ruleset, leaving protected memory branches unusable by repo-memory; it also cannot authenticate private cross-repository access.

#### Alternative 2: Use a personal access token or deploy key

A stored personal access token or deploy key could authenticate pushes independently of `GITHUB_TOKEN`. It was not chosen because the requested integration already has a GitHub App token path, and app installation tokens are revocable, can be scoped to the target repository, and can request only the Contents permission needed by repo-memory.

#### Alternative 3: Configure one workflow-wide GitHub App credential

A single credential could simplify configuration and token generation. It was not chosen because repo-memory supports multiple entries with different target repositories; per-entry configuration keeps each token's owner and repository scope tied to the memory it serves and preserves mixed app/default configurations.

### Consequences

#### Positive
- Memory branches can be protected by rulesets that allow the installed memory app to bypass update and creation restrictions.
- Push and explicit-target clone operations use least-privilege, revocable installation tokens, enabling private cross-repository memory access.
- Existing workflows without `github-app` retain their current `github.token` behavior, while app-only push jobs can reduce their workflow-level Contents permission to read.

#### Negative
- Workflows that opt in must provision and protect GitHub App client credentials, install the app on each target repository, and configure the required permissions.
- The compiler and generated workflows become more complex because each entry may mint separate read or write tokens, validate credentials, and handle fallback behavior.
- Misconfigured app installations or ruleset bypass settings can prevent memory updates even though the workflow compiles.

#### Neutral
- `github-app` accepts the existing `client-id`/legacy `app-id`, `private-key`, `owner`, `repositories`, and `ignore-if-missing` configuration shape, but repo-memory rejects additional app permissions and requests Contents only.
- Tokens are minted in the relevant restore or push job and are revoked by the token action's cleanup step.
- Documentation must describe the `memory/**` ruleset configuration and the security boundary: a ruleset controls writers, not the content accepted by an authorized workflow.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
