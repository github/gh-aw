# ADR-74943: Explicit Git Repository Identity for Credential Setup

**Date**: 2026-11-06
**Status**: Proposed
**Deciders**: pelikhan, Copilot coding agent

---

### Context

`configure_git_credentials.sh` rewrites the `origin` remote of the current checkout with an authenticated URL. It derived the target repository from `GITHUB_REPOSITORY`, which compiled steps tried to override for cross-repository (`current: true`) checkouts. `GITHUB_REPOSITORY` is runner-reserved, so the runner may re-export the workflow repository, which can make an external checkout's `origin` point at the workflow repo while HEAD still holds the target commit (issue #73023). This is a plausible, not confirmed, explanation of the intermittent reports. Since the script writes credentials into remotes, a wrong identity is both a correctness and a credential-scoping risk.

### Decision

Credential setup receives the intended repository through the non-reserved variable `GH_AW_GIT_REPOSITORY`. The compiler emits the actual checkout identity for workspace-root, subdirectory, trial, and safe-output sub-checkouts, including the `.wiki` suffix where applicable. State-persistence jobs use the workflow repository they actually clone, rather than inheriting the trial logical repository. When the variable is absent, the script falls back to `GITHUB_REPOSITORY` for legacy callers; an explicitly empty identity is rejected.

For expression-based paths, generated credential steps resolve the evaluated checkout directories at runtime in checkout order. The last checkout whose resolved directory equals the workspace root determines the expected root identity. This handles `.`, empty paths, equivalent normalized paths, and absolute root paths without deriving identity from `origin`. Superseded root checkouts are not re-authenticated with earlier identities. A current checkout that resolves to a subdirectory keeps its own identity; one that resolves to the root follows the same ordering rule. Expressions are passed as environment data, not interpolated into shell commands, and a literal or evaluated `-` is normalized to `./-` before `cd`.

The shared shell helper checks that every effective fetch and push URL matches the expected server and repository before and after writing credentials, rejecting plaintext HTTP. It uses the shell, Git, and standard utilities without requiring Node for identity verification. On failure it exits non-zero without printing URLs or tokens. Failed post-authentication verification restores the original raw origin URL; restoration failure triggers credential-URL removal with explicit diagnostics if cleanup also fails.

Workspaces without their own checkout or `origin` retain no-op authentication behavior without discovering a parent checkout. Untrusted checkouts, Git configuration-read errors, and runtime directory-resolution failures stop credential setup rather than being treated as missing remotes. This change does not grant new token permissions or change per-checkout token selection.

### Alternatives Considered

#### Alternative 1: Keep overriding `GITHUB_REPOSITORY`

This is the smallest change and needs no new variable. We rejected it because the runner reserves `GITHUB_REPOSITORY` and can replace the override, which is the failure path under investigation.

#### Alternative 2: Derive identity from the existing `origin` remote only

The script would read the repository identity from the checkout's own remote and skip the env variable. We rejected it because a remote that already points at the wrong repository would be trusted without question. With no independent expected value, the script cannot fail closed.

#### Alternative 3: Explicit identity without remote validation

This would pass `GH_AW_GIT_REPOSITORY` but skip the before/after URL checks. It is simpler, but a mismatch would go unnoticed and credentials could be attached to an unexpected remote.

#### Alternative 4: Resolve root identity only at compile time

A literal `path: .` can be resolved at compile time, but an expression may evaluate to the root and replace the default checkout. Checking only literal paths rejects that valid layout. Runtime directory comparison preserves checkout ordering while retaining an independently configured repository identity.

### Consequences

#### Positive
- Credential setup no longer depends on a runner-reserved variable for cross-repository checkouts.
- Fail-closed checks stop tokens from being attached to the wrong repository or server, and error output does not expose URLs or tokens.
- Safe-output sub-checkouts and the main checkout share one helper and one set of identity rules.
- Expression-resolved root paths work consistently in agent and safe-output jobs, without weakening remote verification.

#### Negative
- Existing workflows have to be recompiled to emit `GH_AW_GIT_REPOSITORY`. Until then they use the legacy fallback.
- Strict URL matching can reject setups that worked before, such as unusual remote forms or mirrors. It recognizes HTTPS, SSH, scp-style, and case-insensitive equivalents only.
- The shell script now does more parsing and comparison, which adds maintenance work.
- Workflows with expression-based paths add runtime directory checks. Missing checkout directories fail explicitly.

#### Neutral
- Compiled workflow locks and ten golden fixtures were regenerated for the non-reserved identity variable. Existing checked-in workflows have no additional drift from the runtime-path fix.
- The checkout reference documents explicit identities, runtime path resolution, and failure behavior.

### Validation and Limits

The credential-helper suite covers 42 cases, including runner-reserved identity precedence, URL rewrites, rollback, missing checkouts, untrusted ownership, and operation without Node. Forty generated-step execution cases exercise agent and safe-output runtime paths, checkout ordering, wiki/trial/current layouts, shell-data safety, and fail-closed boundaries. Focused Go and WASM checks and compilation/drift checks for all 356 repository workflows passed.

The aggregate local gate remains blocked by previously documented custom-linter findings on unchanged lines. An unrelated, intermittent environment-override Go test failed in CI but passed repeated checks on both the PR head and original base; it is deferred rather than hidden. These local results do not establish live GitHub authentication compatibility for every runner/server configuration or prove the cause of the original intermittent reports. Approval of this proposed decision remains a maintainer responsibility.
