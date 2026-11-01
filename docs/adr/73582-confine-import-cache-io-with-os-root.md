# ADR-73582: Confine Import Cache I/O with `os.Root` Handles and Atomic Publication

**Date**: 2026-05-21
**Status**: Draft
**Deciders**: pelikhan, [TODO: verify other reviewers]

---

### Context

The parser caches remote imports on disk under `.github/aw/imports/<owner>/<repo>/<sha>/<file>` inside the repository being compiled. Because this directory is repository-controlled, a malicious or careless repository can place symlinks (including dangling links or symlinked parent directories) in the cache tree. The previous implementation used path-based `os.Stat`, `os.MkdirAll`, `os.WriteFile`, and `os.ReadFile`, so the compiler could follow such links and read or overwrite files outside the cache (code-scanning alert 1044, CWE-59). Path-based checks alone also leave a time-of-check/time-of-use window between looking up a cache path and reading it.

### Decision

We will perform all native import-cache filesystem operations through pinned directory handles (`os.Root`), descending one validated component at a time and rejecting symlinks, non-regular files, and components that are not a single local filename. Cache files will be published atomically by writing a temporary file and renaming (or hard-linking, for `.gitattributes`, to preserve existing files) it into place, so existing link targets are never truncated. Native parser reads of cache paths are routed through a confined `ImportCache.ReadFile` via the default `readFileFunc`, closing the lookup-to-read race. On platforms without the required confinement guarantees (WebAssembly, Plan 9), on-disk caching fails explicitly instead of falling back to unconfined I/O; browser virtual-file reads are unchanged.

### Alternatives Considered

#### Alternative 1: Path-based validation (`Lstat` / `EvalSymlinks`) before standard I/O

Check each path component with `os.Lstat` or resolve it with `filepath.EvalSymlinks` and verify the result stays within the cache, then continue to use `os.ReadFile`/`os.WriteFile`. This is simpler and portable, but it is inherently racy: a component can be swapped for a symlink between the check and the use. It was rejected because the PR explicitly targets the lookup-to-read race and concurrent replacement.

#### Alternative 2: Remove the cache from the repository tree or disable on-disk caching

Store the cache outside the repository (e.g., a user cache directory) or always re-fetch imports. This would eliminate repository-controlled symlinks, but it changes the existing, committed cache layout and `.gitattributes` behavior that users rely on, and adds network cost. It was not chosen because the PR aims to preserve existing valid cache behavior.

#### Alternative 3: Platform-specific `openat`/`O_NOFOLLOW` syscalls

Implement confinement directly with low-level syscalls per OS. This offers fine-grained control but requires per-platform code and duplicates what Go's standard `os.Root` already provides portably. [TODO: verify whether this was considered]

### Consequences

#### Positive
- Symlink entries, symlinked parent directories, dangling links, and non-regular files in the cache can no longer redirect reads or writes outside the cache.
- Atomic publication means partially written cache files are never observed and existing symlink/hardlink targets are never truncated.
- Routing the parser's default reader through confinement covers consumers that receive a cache path without an `ImportCache` instance.

#### Negative
- More complex I/O code (handle traversal, `SameFile` re-checks, temporary-file cleanup) that must be maintained, plus a new public `ImportCache.ReadFile` API that callers must use instead of `os.ReadFile`.
- On-disk import caching is now unavailable on WebAssembly and Plan 9 instead of degrading silently.
- Any path containing the import-cache directory marker is read through the confined path, which may reject previously tolerated (e.g., symlinked) cache layouts.

#### Neutral
- Relies on Go's `os.Root` API (and `rand.Text`, `strings.SplitSeq`), tying the implementation to a recent Go toolchain.
- The include-confinement fix being developed separately must preserve the native default-reader change in `virtual_fs.go` when the two are composed.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
