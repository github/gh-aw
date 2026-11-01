# ADR-73584: Confine Local Include Reads to Trusted Roots

**Date**: 2026-11-01
**Status**: Draft
**Deciders**: Unknown (generated from PR #73584)

---

### Context

Compile-time local includes in `pkg/parser` previously validated lexical paths but could follow repository-controlled symlinks to arbitrary readable files. Native parser reads also used `os.ReadFile`, so a path or parent directory could be replaced after validation and escape the intended `.github` or `.agents` boundary. PR #73584 addresses code scanning alert 1043 while preserving legitimate relative and absolute symlinks whose targets remain inside the allowed directory, as well as lexical paths used by manifests and nested imports. The repository already provides path-containment validation, and native Go `os.Root` can pin an allowed directory during the subsequent read.

### Decision

We will treat `.github` and `.agents` as trusted roots for local compile-time imports and native parser reads. We will resolve and validate actual targets with `filepath.EvalSymlinks` and `fileutil.ValidatePathWithinBase`, reject dangling or escaping links and symlinked roots, and read through a pinned `os.Root` so path-component replacements cannot escape the root. `ResolveIncludePath` will continue returning lexical paths for manifests and nested import resolution, while the read operation enforces the resolved security boundary.

### Alternatives Considered

#### Alternative 1: Keep lexical path containment only

The parser could continue checking only normalized lexical paths before reading with the existing file APIs. This is the smallest change and preserves all current path forms, but it does not protect against repository-controlled symlinks that point outside the allowed directory and does not address replacements between validation and read.

#### Alternative 2: Reject all symlinks in local include paths

The compiler could disallow symlinks entirely under `.github` and `.agents`. This would simplify the security model and avoid resolution races, but it would break the supported use of relative and absolute symlinks to targets that remain within the allowed directory, which this PR explicitly tests and documents.

#### Alternative 3: Resolve targets and then use ordinary file reads

The parser could resolve a candidate with `filepath.EvalSymlinks`, validate the resolved path, and then call `os.ReadFile` on that path. This would improve on lexical-only validation, but it would leave a time-of-check/time-of-use window in which a parent or base could be replaced. Pinning the security root with `os.Root` provides stronger confinement at the cost of a more involved native read path.

### Consequences

#### Positive
- Repository-controlled symlinks that resolve outside `.github` or `.agents`, including dangling links and escaping parent directories, are rejected instead of allowing arbitrary files into compilation output.
- Pinned rooted reads protect against post-validation path swaps and concurrent replacement of parent directories or the allowed base itself.
- Relative and absolute symlinks to targets within the allowed directory remain supported, and lexical paths remain available for manifests and nested imports.
- Regression tests cover escaping files and parents, dangling links, loops, symlinked roots, and concurrent replacements; documentation records the boundary for users.

#### Negative
- Local include resolution and reading now perform additional filesystem inspection and use a more complex security-sensitive path than a direct `os.ReadFile`, increasing maintenance and platform-specific behavior.
- Repositories that rely on symlinked `.github` or `.agents` roots, dangling links, or links to files outside the allowed directory will fail closed and may need to change their layout.
- The native rooted-read implementation depends on operating-system support for `os.Root`; virtual filesystem and non-native build paths must retain their separate behavior.

#### Neutral
- The security roots are limited to `.github` and `.agents`; ordinary native reads outside those directories continue through the existing disk-read fallback.
- `ResolveIncludePath` preserves lexical paths even when symlink resolution is used for validation, so manifests and nested import bookkeeping do not change their path representation.
- The change adds a reusable `fileutil.ReadFileWithinBase` helper for other callers that need symlink-aware, race-resistant reads within a directory.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
