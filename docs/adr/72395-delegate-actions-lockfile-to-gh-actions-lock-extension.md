# ADR-72395: Delegate Actions Lockfile Maintenance to the gh-actions-lock Extension via Opt-in Compile Flag

**Date**: 2026-10-27
**Status**: Proposed (under review in PR #72395)
**Deciders**: pelikhan ([PR #72395](https://github.com/github/gh-aw/pull/72395))

---

### Context

`gh aw compile` already resolves action and container pins using `.github/aw/actions-lock.json`, which also carries compiler metadata. Separately, `.github/workflows/actions.lock` records the Actions dependency graph and provenance using the format owned by [`github/gh-actions-lock`](https://github.com/github/gh-actions-lock). Users who adopt that lockfile currently have to run the extension after compilation or risk drift from the generated `.lock.yml` workflows. The integration must leave ordinary compilation independent of an extra extension, preserve compiler-owned SHA references, keep `--json` stdout valid, and reuse gh-aw's host and credential handling without claiming additional upstream host support.

### Decision

The proposed integration adds opt-in `gh aw compile --actions-lock`, invoking `gh actions-lock --no-narrow` once from the repository root after all compilation pipelines and companion workflow generators succeed. Delegating to the native extension keeps schema, graph resolution, and provenance behavior under upstream ownership; `--no-narrow` preserves compiler-owned SHA references rather than letting the extension rewrite them. The integration will not auto-install the extension or replace `.github/aw/actions-lock.json`: existing action/container pin resolution and metadata responsibilities remain with gh-aw.

The child runs through the shared `workflow.ExecGHContext` helper, with stdout and stderr routed to stderr so compile JSON remains valid. Missing-extension errors provide installation guidance; any extension failure fails the command without rolling back compiled files. The flag is incompatible with `--dry-run`, `--no-emit`, `--read-only`, and `--watch`, and does not implicitly request `--relock`, `--accept-moved`, or local-action migration.

### Alternatives Considered

#### Alternative 1: Natively generate `actions.lock` inside the compiler

gh-aw could resolve dependencies and write the standard lockfile itself. The official `github.com/github/actions-lockfile/go/pkg/lockfile` package can parse and validate the format, but it does not resolve references, identities, or the transitive dependency graph. The extension's resolver/writer is internal and cannot be imported as a supported Go package. Native generation would therefore require a second resolver and writer, not merely adopting the official parser; delegating avoids maintaining duplicate schema, graph, and provenance semantics.

#### Alternative 2: Replace `.github/aw/actions-lock.json` with the standard lockfile

Migrating gh-aw pin resolution onto the standard lockfile could consolidate state. This was investigated, then explicitly deferred by the user for this change: replacement requires deciding how to retain action/container pin-store behavior and compiler metadata, not just changing filenames. The opt-in hook leaves that migration open without coupling ordinary compilation to it.

#### Alternative 3: Document a manual post-compile step only

Users could continue running `gh actions-lock` themselves after compiling. This remains a valid workflow with no integration code, but requires users to coordinate successful compilation, SHA-preserving options, and a second invocation. The opt-in hook offers that coordination without changing the default compile path.

### Consequences

#### Positive
- Users can request an Actions lockfile refresh after successful compilation in a single command.
- No new runtime dependency for ordinary compilation; the extension is only required when the flag is set.
- Reusing `ExecGHContext` preserves existing host/credential precedence and enterprise configuration forwarding instead of introducing a separate authentication path.
- `--json` output stays valid because extension output goes to stderr.

#### Negative
- Adopters maintain two files with distinct responsibilities: gh-aw's action/container pin store and metadata, and the extension's dependency graph/provenance lockfile.
- Correctness and host support depend on an externally versioned extension that gh-aw does not control; forwarding enterprise settings does not add upstream GHES support.
- Failures after successful compilation leave compiled files on disk while returning an error, so partial state is possible.
- The extension scans its default `.github/workflows` directory, even for targeted compilation or `--dir`; compile selection does not narrow the extension's update scope.

#### Neutral
- No hook runs after failed compilation or generation, and incompatible nonstandard emission modes are rejected before invoking the extension.
- [Issue #70264](https://github.com/github/gh-aw/issues/70264) remains only partially addressed. Add/upgrade synchronization, compile-scope preservation, real-extension deterministic preservation tests, and upstream remote reusable-workflow/container coverage are outside this PR.

---

*This proposed decision is under review in PR #72395; it does not record merged or shipped acceptance.*
