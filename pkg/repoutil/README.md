# repoutil Package

The `repoutil` package provides utility functions for working with GitHub repository slugs.

## Overview

This package offers focused helpers for parsing and normalizing repository identifiers, which are used throughout the codebase wherever GitHub repositories are referenced.

## Public API

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `SplitRepoSlug` | `func(slug string) (owner, repo string, err error)` | Splits a repository slug of the form `owner/repo` into its two components; returns an error when the slug does not contain exactly one `/` or when either component is empty |
| `NormalizeRepoForAPI` | `func(repo string) (ownerRepo string, host string)` | Splits a repository string of the form `[HOST/]owner/repo` into the `owner/repo` portion and an optional host name for GHES/Proxima API calls |

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/repoutil"

owner, repo, err := repoutil.SplitRepoSlug("github/gh-aw")
if err != nil {
    return fmt.Errorf("invalid repository: %w", err)
}
// owner = "github", repo = "gh-aw"
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging

## Thread Safety

Both functions are pure and stateless; they MAY be called concurrently without synchronization.

## Design Notes

- All debug output uses `logger.New("repoutil:repoutil")` and is only emitted when `DEBUG=repoutil:*`.
- For paths that include sub-folders (e.g. GitHub Actions `uses:` fields such as `github/codeql-action/upload-sarif`), use `gitutil.ExtractBaseRepo` first to strip the sub-path before calling `SplitRepoSlug`.
- `NormalizeRepoForAPI` only treats three-segment strings as `HOST/owner/repo`; plain `owner/repo` values are returned unchanged with an empty host.

## Source Synchronization

Reviewed against source on 2026-07-26; no public-contract deltas identified. Re-verified on 2026-08-14; still no public-contract deltas. Re-verified on 2026-08-29; still no public-contract deltas. Re-verified on 2026-09-03; still no public-contract deltas. Re-verified on 2026-09-08; still no public-contract deltas.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
