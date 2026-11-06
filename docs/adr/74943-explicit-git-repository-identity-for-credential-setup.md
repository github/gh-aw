# ADR-74943: Explicit Git Repository Identity for Credential Setup

**Date**: 2025-07-14
**Status**: Draft
**Deciders**: pelikhan, Copilot coding agent

---

### Context

`configure_git_credentials.sh` rewrites the `origin` remote of the current checkout with an authenticated URL. It derived the target repository from `GITHUB_REPOSITORY`, which compiled steps tried to override for cross-repository (`current: true`) checkouts. `GITHUB_REPOSITORY` is runner-reserved, so the runner may re-export the workflow repository, which can make an external checkout's `origin` point at the workflow repo while HEAD still holds the target commit (issue #73023). This is a plausible, not confirmed, explanation of the intermittent reports. Since the script writes credentials into remotes, a wrong identity is both a correctness and a credential-scoping risk.

### Decision

We will pass the intended repository to credential setup through a new non-reserved variable, `GH_AW_GIT_REPOSITORY`. The compiler will emit the actual checkout identity for workspace-root, subdirectory, trial, and safe-output sub-checkouts, including the `.wiki` suffix where applicable. When the variable is absent, the script falls back to `GITHUB_REPOSITORY` so older callers keep working; an explicitly empty identity is rejected. The script will also fail closed: it checks that the effective fetch and push URLs match the expected server and repository both before and after it writes credentials, rejecting plaintext HTTP. On a mismatch it exits non-zero without printing URLs or tokens, restoring the original remote configuration if post-authentication verification fails. Workspaces without a checkout or `origin` retain their no-op behavior. Safe-output sub-checkouts use the same helper, so every path goes through one validation point.

### Alternatives Considered

#### Alternative 1: Keep overriding `GITHUB_REPOSITORY`

This is the smallest change and needs no new variable. We rejected it because the runner reserves `GITHUB_REPOSITORY` and can replace the override, which is the failure path under investigation.

#### Alternative 2: Derive identity from the existing `origin` remote only

The script would read the repository identity from the checkout's own remote and skip the env variable. We rejected it because a remote that already points at the wrong repository would be trusted without question. With no independent expected value, the script cannot fail closed.

#### Alternative 3: Explicit identity without remote validation

This would pass `GH_AW_GIT_REPOSITORY` but skip the before/after URL checks. It is simpler, but a mismatch would go unnoticed and credentials could be attached to an unexpected remote.

### Consequences

#### Positive
- Credential setup no longer depends on a runner-reserved variable for cross-repository checkouts.
- Fail-closed checks stop tokens from being attached to the wrong repository or server, and error output does not expose URLs or tokens.
- Safe-output sub-checkouts and the main checkout share one helper and one set of identity rules.

#### Negative
- Existing workflows have to be recompiled to emit `GH_AW_GIT_REPOSITORY`. Until then they use the legacy fallback.
- Strict URL matching can reject setups that worked before, such as unusual remote forms or mirrors. It recognizes HTTPS, SSH, scp-style, and case-insensitive equivalents only.
- The shell script now does more parsing and comparison, which adds maintenance work.

#### Neutral
- Ten golden test fixtures change because of the new env line in generated steps.
- The documentation adds a supported pattern for central workflows that sets `GH_AW_GIT_REPOSITORY` and `GITHUB_SERVER_URL`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
