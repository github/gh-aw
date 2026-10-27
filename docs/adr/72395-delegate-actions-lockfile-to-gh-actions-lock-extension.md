# ADR-72395: Delegate Actions Lockfile Maintenance to the gh-actions-lock Extension via Opt-in Compile Flag

**Date**: 2026-10-27
**Status**: Draft
**Deciders**: pelikhan (PR #72395) [TODO: verify]

---

### Context

`gh aw compile` already resolves and caches action pins in gh-aw's own `.github/aw/actions-lock.json`. Separately, the ecosystem has a standard Actions dependency lockfile (`.github/workflows/actions.lock`) maintained by the `github/gh-actions-lock` GitHub CLI extension. Users who adopt that standard lockfile currently have to remember to run the extension themselves after every compile, so the lockfile can drift from the compiled `.lock.yml` workflows. Any integration must not make ordinary compilation depend on an extra extension, must not corrupt `--json` output on stdout, and must work on GitHub Enterprise hosts using the same credentials/host detection the rest of gh-aw uses.

### Decision

We will add an opt-in `gh aw compile --actions-lock` flag that, after successful compilation and companion workflow generation, shells out to `gh actions-lock --no-narrow` from the repository root via the shared `workflow.ExecGHContext` helper. gh-aw will not reimplement the lockfile format, will never auto-install the extension, and will keep its existing `.github/aw/actions-lock.json` pin cache and pin resolution unchanged. Extension failures fail the command (with installation guidance) without rolling back compiled files; extension stdout/stderr are routed to stderr; and the flag is rejected in combination with `--no-emit`, `--read-only`, and `--watch`.

### Alternatives Considered

#### Alternative 1: Natively generate `actions.lock` inside the compiler

gh-aw already knows every action reference and has a pin resolver, so it could write the standard lockfile itself. This was not chosen because it duplicates the extension's format and semantics, creates a second implementation that must track upstream changes, and would blur the boundary with gh-aw's own pin cache.

#### Alternative 2: Replace `.github/aw/actions-lock.json` with the standard lockfile

Migrating gh-aw pin resolution onto the standard lockfile would remove the duplicated state. It was rejected for this change because it is a breaking change to existing pin resolution and caching behaviour, and would make ordinary compilation depend on the extension.

#### Alternative 3: Document a manual post-compile step only

Users could run `gh actions-lock` themselves after compiling. This requires no code but leaves the drift problem unsolved and offers no consistent handling of enterprise host/credential forwarding or JSON-safe output.

### Consequences

#### Positive
- Users can keep the standard Actions lockfile in sync with compiled workflows in a single command.
- No new runtime dependency for ordinary compilation; the extension is only required when the flag is set.
- Reusing `ExecGHContext` preserves `GH_HOST`, enterprise tokens, token precedence, config dir, and certificate environment, including origin-detected hosts.
- `--json` output stays valid because extension output goes to stderr.

#### Negative
- Two lockfiles (`.github/aw/actions-lock.json` and `.github/workflows/actions.lock`) now coexist, which can confuse users about which is authoritative.
- Correctness and host support depend on an externally versioned extension that gh-aw does not control.
- Failures after successful compilation leave compiled files on disk while returning an error, so partial state is possible.
- The extension scans its own default workflow directory, so `--dir` or compiling specific files does not narrow what it updates.

#### Neutral
- `--no-narrow` is always passed so existing SHA-pinned references are preserved.
- New mutual-exclusion validation rules are added for `--no-emit`, `--read-only`, and `--watch`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
