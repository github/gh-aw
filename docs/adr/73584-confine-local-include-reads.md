---
title: Confine Local Include Reads to Trusted Roots
description: Preserve resolver-selected include boundaries through native filesystem reads.
---

# ADR-73584: Confine Local Include Reads to Trusted Roots

**Date**: 2026-11-01
**Status**: Proposed
**Deciders**: Not recorded; human architecture approval is pending

---

### Context

Compile-time local includes in `pkg/parser` previously validated lexical paths but could follow repository-controlled symlinks to readable files outside the allowed directory. Validating a resolved target alone still leaves a gap if a path component changes before a later parser or compiler read. [PR #73584](https://github.com/github/gh-aw/pull/73584) addresses this confinement problem while preserving relative and absolute symlinks with in-base targets and lexical paths used by manifests and nested imports. The resolver selects the security base, normally `.github` or `.agents`; inferring a new base from a nested directory with either name during a reread must not weaken that original boundary. Native disk reads must also retain the cache-first handling introduced by PR #73582, while embedded and WASM virtual files keep their separate read paths.

### Decision

The implementation retains lexical containment checks, validates absolute candidate and base paths with `fileutil.ValidatePathWithinBase`, and requires successful `filepath.EvalSymlinks` resolution before returning the original lexical include path; a missing base or target is a file-not-found error, and symlinked bases are rejected. A typed, mutex-protected registry keyed by absolute lexical file path preserves every distinct resolver-selected absolute security base as immutable snapshots, so repository-relative rereads cannot discard an earlier boundary.

Native reads check import-cache paths first, then use `fileutil.ReadFileWithinBase` for every recorded base and fail if any read fails; unregistered paths beneath `.github` or `.agents` use the ancestor-root fallback. The helper opens an `os.Root`, verifies its identity against `os.Lstat(base)` with `os.SameFile`, rejects a symlinked or changed base, and reads the resolved in-base relative target through that pinned root to constrain path replacements. Ordered and legacy inlined compiler imports use `parser.ReadFile` rather than bypassing this read policy with `os.ReadFile`, preserving confinement across the existing string-only resolver/read API without changing manifest path representation.

### Alternatives Considered

#### Alternative 1: Keep lexical path containment only

The parser could continue checking only normalized lexical paths before reading with the existing file APIs. This is the smallest change and preserves all current path forms, but it does not protect against repository-controlled symlinks that point outside the allowed directory and does not address replacements between validation and read.

#### Alternative 2: Reject all symlinks in local include paths

The compiler could disallow symlinks entirely under `.github` and `.agents`. This would simplify the accepted layouts, but checking for symlinks before an ordinary read would still require race-resistant enforcement. It would also break supported relative and absolute symlinks to targets that remain within the allowed directory.

#### Alternative 3: Resolve targets and then use ordinary file reads

The parser could resolve a candidate with `filepath.EvalSymlinks`, validate the resolved path, and then call `os.ReadFile` on that path. This would improve on lexical-only validation, but it would leave a time-of-check/time-of-use window in which a parent or base could be replaced. Pinning the security root with `os.Root` provides stronger confinement at the cost of a more involved native read path.

#### Alternative 4: Infer the security base again at each read

Selecting the nearest `.github` or `.agents` ancestor avoids retaining resolver state and remains useful for unregistered native reads. For resolved imports, however, a nested directory with one of those names could be replaced with a symlink and mistaken for a new authority. Remembering all resolver-selected bases prevents a later resolution from replacing an earlier constraint without requiring a wider API redesign.

### Consequences

#### Positive
- Escaping targets, dangling links, symlink loops, and missing or symlinked bases fail closed for local includes.
- Pinned rooted reads confine file access during path-component replacement; recorded bases retain the original boundary for compiler rereads, including nested `.github` and `.agents` paths.
- Relative and absolute symlinks to targets within the allowed directory remain supported, and lexical paths remain available for manifests and nested imports.

#### Negative
- Local include resolution and reading now perform additional filesystem inspection and use a more complex security-sensitive path than a direct `os.ReadFile`, increasing maintenance and platform-specific behavior.
- Repositories that rely on symlinked `.github` or `.agents` roots, dangling links, or links to files outside the allowed directory will fail closed and may need to change their layout.
- Registry entries persist for the process lifetime, and multiple recorded bases require multiple rooted reads. This can conservatively reject a path used in different contexts and adds memory and I/O costs.

#### Neutral
- Import-cache paths keep their cache-aware reader ahead of local-root handling; embedded and WASM virtual files retain their separate behavior. Unregistered native paths outside `.github` and `.agents` still use the ordinary disk-read fallback.
- `ResolveIncludePath` preserves lexical paths even when symlink resolution is used for validation, so manifests and nested import bookkeeping do not change their path representation.
- The change adds a reusable `fileutil.ReadFileWithinBase` helper for other callers that need symlink-aware, race-resistant reads within a directory.
- Confinement is not a filesystem snapshot or a guarantee of unchanged file contents. This record documents the implementation decision, not human approval or a measured security-scan result.

---

*Completed from the generated draft against the PR implementation; proposed for human architecture review.*
