# ADR-67425: Prefer a Compatible Local actionlint Binary Before Docker

**Date**: 2026-10-10
**Status**: Draft
**Deciders**: pelikhan (PR author); gh-aw maintainers (review pending)

---

### Context

`gh aw compile` runs actionlint over generated `.lock.yml` files as a quality gate. Unlike the other
two scanners in `pkg/cli` (zizmor and poutine), which already resolve a compatible binary from `PATH`
via `localScannerPath` before falling back to a pinned Docker image, actionlint was Docker-only. That
made compilation fail or stall on machines without a usable Docker daemon (sandboxes, CI images with
no Docker-in-Docker, developer laptops with Docker stopped), even when a current `actionlint` was
already installed. It also forced Docker image preparation work and blocked the MCP read-only scanning
path when Docker-only tools were unavailable. The constraint is that native and Docker runs must stay
behaviourally identical: the same integration flags (shellcheck/pyflakes), ignore patterns, JSON
output format, strict-mode handling, and context cancellation semantics.

### Decision

We will make actionlint local-first, matching the existing zizmor/poutine pattern: `buildActionlintCommand`
resolves `actionlint` from `PATH` through `localScannerPath` and uses it when its reported version is at
least the pinned `minActionlintVersion` (1.7.12, the version of the pinned Docker image); otherwise it
falls back to the Docker invocation. Missing, outdated, prerelease-below-minimum, and unparseable
versions all fall back to Docker, so an unrecognized binary is never assumed compatible. Lint arguments
are factored into a shared `buildActionlintArgs` used by both the native and Docker command builders so
the two paths cannot drift, with native execution rooted at the repository root to preserve relative
paths in findings. Docker image preparation is skipped when a compatible local actionlint exists, and
native scanning remains available through MCP when other Docker-only tools are not.

### Alternatives Considered

#### Alternative 1: Keep actionlint Docker-only

Docker pinning guarantees one exact actionlint build plus bundled shellcheck/pyflakes, so output is
byte-reproducible across machines. It was rejected because it makes a routine compile step hard-fail in
Docker-less environments, which is the concrete problem this PR addresses, and because the repository
had already accepted the local-first trade-off for zizmor and poutine — leaving actionlint inconsistent
was the larger cost.

#### Alternative 2: Prefer local actionlint unconditionally (no version gate)

Simply using any `actionlint` found on `PATH` would be the smallest change and would avoid the
`--version` subprocess on every invocation. It was rejected because older actionlint releases differ in
flags and JSON findings shape, so an old or unidentifiable binary would produce confusing diagnostics or
silent gate weakening. Gating on `>= 1.7.12` with fallback-on-doubt keeps the failure mode conservative.

*(A third option — an explicit opt-in flag/env var to select the runner — was considered implicitly but
not adopted, since it adds user-facing configuration surface for behaviour that can be decided
automatically.)*

### Consequences

#### Positive
- `gh aw compile` and MCP-based scanning now work on Docker-less machines when actionlint is installed,
  removing a hard dependency on a container runtime for a common path.
- Actionlint behaves consistently with zizmor and poutine, so there is one mental model (and one helper,
  `localScannerPath`) for scanner resolution.
- Native runs skip image pull/prepare work, which is typically much faster than the Docker path.

#### Negative
- Results are no longer guaranteed to come from one pinned image: a local 1.7.x build may differ in
  minor diagnostics from the Docker image, so findings can vary between developer machines and CI.
- Native execution does not bring bundled shellcheck/pyflakes; those integrations silently depend on
  whatever is on `PATH` and are not installed automatically, so integration coverage can be weaker locally.
- Every actionlint invocation now pays a `--version` subprocess (5s-timeout) probe for binary resolution.

#### Neutral
- `minActionlintVersion` becomes a maintenance point that must be bumped in lockstep with the pinned
  Docker image tag.
- Shared argument construction (`buildActionlintArgs`) is now load-bearing for parity between the two
  execution paths and is covered by the new regression tests in `pkg/cli/actionlint_local_test.go` and
  `pkg/cli/scanner_local_test.go`.
- Scanner documentation under `docs/src/content/docs/reference/` was updated to describe the local-first
  behaviour and the `PATH`-dependency of shellcheck/pyflakes.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
