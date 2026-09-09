# semverutil Package

The `semverutil` package provides shared semantic versioning primitives used across `pkg/workflow` and `pkg/cli`. Centralizing these helpers ensures that semver parsing, comparison, and compatibility logic is defined in one place.

## Overview

This package wraps `golang.org/x/mod/semver` with additional helpers for:
- Normalizing version strings (adding the required `v` prefix).
- Validating GitHub Actions version tags (`vmajor`, `vmajor.minor`, `vmajor.minor.patch`).
- Parsing versions into a structured `SemanticVersion` type.
- Comparing and checking compatibility between version strings.

## Public API

### Types

### `SemanticVersion`

A parsed semantic version with individual numeric components.

```go
type SemanticVersion struct {
    Major int
    Minor int
    Patch int
    Pre   string // Prerelease identifier without leading hyphen (e.g. "beta.1")
    Raw   string // Original version string without leading "v"
}
```

#### Methods

| Method | Description |
|--------|-------------|
| `IsPreciseVersion() bool` | Returns `true` if the version has at least two dots (e.g. `v6.0.0` is precise, `v6` is not) |
| `IsNewer(other *SemanticVersion) bool` | Returns `true` if this version is newer than `other` |

### Functions

### `EnsureVPrefix(v string) string`

Adds a leading `"v"` if `v` does not already have one. Required because `golang.org/x/mod/semver` demands the prefix.

```go
semverutil.EnsureVPrefix("1.2.3")  // → "v1.2.3"
semverutil.EnsureVPrefix("v1.2.3") // → "v1.2.3"
```

### `IsActionVersionTag(s string) bool`

Reports whether `s` is a valid GitHub Actions version tag. Accepted forms: `vmajor`, `vmajor.minor`, `vmajor.minor.patch`. Prerelease and build-metadata suffixes are **not** accepted.

```go
semverutil.IsActionVersionTag("v4")       // true
semverutil.IsActionVersionTag("v4.1")     // true
semverutil.IsActionVersionTag("v4.1.0")   // true
semverutil.IsActionVersionTag("v4.1.0-rc") // false
```

### `IsValid(ref string) bool`

Reports whether `ref` is a valid semantic version string (accepts any valid semver including prerelease/build-metadata, and bare versions without `"v"`).

```go
semverutil.IsValid("1.2.3")       // true
semverutil.IsValid("v1.2.3-beta") // true
semverutil.IsValid("not-a-ver")   // false
```

### `ParseVersion(v string) *SemanticVersion`

Parses `v` into a `SemanticVersion`. Returns `nil` if `v` is not a valid semver string.

```go
ver := semverutil.ParseVersion("v1.2.3")
if ver != nil {
    fmt.Println(ver.Major, ver.Minor, ver.Patch) // 1 2 3
}
```

### `Compare(v1, v2 string) int`

Compares two semantic versions using `golang.org/x/mod/semver`. Returns `1` if `v1 > v2`, `-1` if `v1 < v2`, or `0` if equal. Bare versions (without `"v"`) are accepted.

```go
semverutil.Compare("v2.0.0", "v1.9.9") // 1  (v2 is newer)
semverutil.Compare("v1.0.0", "v1.0.0") // 0  (equal)
semverutil.Compare("v0.9.0", "v1.0.0") // -1 (v0.9 is older)
```

### `IsMorePreciseVersion(v1, v2 string) bool`

Reports whether `v1` should sort ahead of `v2` in the action-version specificity ordering. Versions with more dot-separated components sort first (e.g. `"v4.3.0"` ahead of `"v4"`), and ties use lexicographic ordering. This is an ordering predicate, not a strict "more precise" check. No validation is performed; callers MUST ensure both inputs are well-formed version tags.

```go
semverutil.IsMorePreciseVersion("v4.3.0", "v4")     // true  (v4.3.0 is more specific)
semverutil.IsMorePreciseVersion("v4", "v4.3.0")     // false
semverutil.IsMorePreciseVersion("v4.3.0", "v4.3.0") // false (equal precision)
```

### `NormalizeGitDescribeSemver(v string) string`

Normalizes a version string produced by `git describe` into a canonical semver. Strips `git describe` prerelease suffixes of the form `N-gHEX` or `N-gHEX-dirty` (which indicate commits-since-tag metadata rather than real prerelease labels), leaving only the base semver. Non-`git describe` prerelease suffixes (e.g. `-beta.1`) are preserved. Returns the input unchanged if it is not a valid semver.

```go
semverutil.NormalizeGitDescribeSemver("v1.2.3-14-gabcdef")       // → "v1.2.3"
semverutil.NormalizeGitDescribeSemver("v1.2.3-14-gabcdef-dirty") // → "v1.2.3"
semverutil.NormalizeGitDescribeSemver("v1.2.3-beta.1")           // → "v1.2.3-beta.1"
semverutil.NormalizeGitDescribeSemver("v1.2.3")                  // → "v1.2.3"
```

### `IsCompatible(pinVersion, requestedVersion string) bool`

Reports whether `pinVersion` is semver-compatible with `requestedVersion`. Compatibility is defined as sharing the same major version.

```go
semverutil.IsCompatible("v5.0.0", "v5")     // true
semverutil.IsCompatible("v5.1.0", "v5.0.0") // true
semverutil.IsCompatible("v6.0.0", "v5")     // false
```

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/semverutil"

// Normalize a version string
semverutil.EnsureVPrefix("1.2.3") // → "v1.2.3"

// Parse into structured type
ver := semverutil.ParseVersion("v1.2.3")
if ver != nil {
    fmt.Println(ver.Major, ver.Minor, ver.Patch) // 1 2 3
}

// Compare versions
semverutil.Compare("v2.0.0", "v1.9.9") // 1 (v2 is newer)

// Check major-version compatibility
semverutil.IsCompatible("v5.1.0", "v5") // true
semverutil.IsCompatible("v6.0.0", "v5") // false
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging

**External**:
- `golang.org/x/mod/semver` — canonical semver parsing and comparison

## Design Notes

- All debug output uses `logger.New("semverutil:semverutil")` and is only emitted when `DEBUG=semverutil:*`.
- The package intentionally delegates to `golang.org/x/mod/semver` for canonical semver logic rather than implementing its own parsing.
- `ParseVersion` uses `semver.Canonical` before splitting into components, ensuring correct handling of short forms like `v1` (canonicalized to `v1.0.0`).
- `IsCompatible` returns `false` for invalid versions on either side before comparing majors, preventing two malformed version strings from being treated as compatible.

## Source Synchronization

Reviewed against recent source updates on 2026-07-24; `NormalizeGitDescribeSemver` added to Public API section. Re-verified on 2026-08-14; no further public-contract deltas identified. Re-verified on 2026-08-29; no public-contract deltas identified. Re-verified on 2026-09-03; no public-contract deltas identified. Re-verified on 2026-09-08; no public-contract deltas identified.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
