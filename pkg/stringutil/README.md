# stringutil Package

> Utility functions for string manipulation, sanitization, identifier normalization, ANSI stripping, URL parsing, and GitHub PAT validation.

## Overview

The `stringutil` package provides utility functions for working with strings. It is organized into focused sub-files covering ANSI stripping, identifier normalization, sanitization, URL utilities, and PAT (Personal Access Token) validation.

## Public API

The `stringutil` package is organized into focused sub-files:

| Sub-file | Functions |
|----------|-----------|
| `stringutil.go` | General string helpers (`Truncate`, `FormatList`, `IsPositiveInteger`) |
| `whitespace.go` | Whitespace normalization |
| `version.go` | Version value coercion |
| `ansi.go` | ANSI escape-code stripping |
| `identifiers.go` | Workflow name and path normalization |
| `sanitize.go` | Security-sensitive string sanitization |
| `urls.go` | URL normalization and domain extraction |
| `pat_validation.go` | GitHub PAT classification and validation |
| `fuzzy_match.go` | Fuzzy string matching for "Did you mean?" suggestions |

### Exported Types

| Type | Kind | Description |
|------|------|-------------|
| `SanitizeOptions` | struct | Options for `SanitizeName` (preserved characters, hyphen trimming, and default value) |
| `PATType` | string alias | Type of GitHub Personal Access Token (fine-grained, classic, oauth, unknown) with methods: `String()`, `IsFineGrained()`, `IsValid()` |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| `PATTypeFineGrained` | `PATType` | `"fine-grained"` | Fine-grained PAT (prefix `github_pat_`) |
| `PATTypeClassic` | `PATType` | `"classic"` | Classic PAT (prefix `ghp_`) |
| `PATTypeOAuth` | `PATType` | `"oauth"` | OAuth token (prefix `gho_`) |
| `PATTypeUnknown` | `PATType` | `"unknown"` | Unrecognized token format |

## General Utilities (`stringutil.go`)

### `Truncate(s string, maxLen int) string`

Truncates `s` to at most `maxLen` characters, appending `"..."` when truncation occurs. For `maxLen ≤ 3` the string is truncated without ellipsis.

```go
stringutil.Truncate("hello world", 8) // "hello..."
stringutil.Truncate("hi", 8)          // "hi"
```

### `FormatList(items []string) string`

Formats a slice of strings as a natural-language list with an Oxford comma.

```go
stringutil.FormatList([]string{"a", "b", "c"}) // "a, b, and c"
```

### `IsPositiveInteger(s string) bool`

Returns `true` if and only if `s` is a decimal integer that is strictly greater than zero, has no leading zeros, and contains no non-digit characters. Returns `false` for `""`, `"0"`, negative strings (e.g. `"-5"`), strings with leading zeros (e.g. `"007"`), and non-numeric strings.

## Whitespace Normalization (`whitespace.go`)

### `NormalizeWhitespace(content string) string`

Normalizes trailing whitespace in multi-line content. Trims trailing spaces, tabs, and carriage returns from every line, then ensures the content ends with exactly one newline (or is empty). This reduces spurious diffs caused by trailing-whitespace differences.

### `NormalizeLeadingWhitespace(content string) string`

Removes shared leading indentation from non-empty lines in a multi-line string. This is useful for normalizing heredoc-like blocks while preserving relative indentation.

## Version Value Coercion (`version.go`)

### `ParseVersionValue(version any) string`

Converts a `any`-typed version value (typically from YAML parsing, which may produce `int`, `float64`, or `string`) into a string. Returns an empty string for nil.

```go
stringutil.ParseVersionValue("20")    // "20"
stringutil.ParseVersionValue(20)      // "20"
stringutil.ParseVersionValue(20.0)    // "20"
```

## ANSI Escape Code Stripping (`ansi.go`)

### `StripANSI(s string) string`

Removes all ANSI/VT100 escape sequences from `s`. Handles CSI sequences (e.g. `\x1b[31m` for colors) and other ESC-prefixed sequences. This function is used before writing text into YAML files to prevent invisible characters from corrupting workflow output.

```go
colored := "\x1b[32mSuccess\x1b[0m"
plain := stringutil.StripANSI(colored) // "Success"
```

## Identifier Normalization (`identifiers.go`)

### `NormalizeWorkflowName(name string) string`

Removes `.md` and `.lock.yml` extensions from workflow names, returning the bare workflow identifier.

```go
stringutil.NormalizeWorkflowName("weekly-research.md")       // "weekly-research"
stringutil.NormalizeWorkflowName("weekly-research.lock.yml") // "weekly-research"
stringutil.NormalizeWorkflowName("weekly-research")          // "weekly-research"
```

### `NormalizeSafeOutputIdentifier(identifier string) string`

Converts dashes **and periods** to underscores in safe-output identifiers, normalizing user-facing `dash-separated` and dot-separated formats to the internal `underscore_separated` format required by MCP tool names (which must match `^[a-zA-Z0-9_-]+$`).

```go
stringutil.NormalizeSafeOutputIdentifier("create-issue")           // "create_issue"
stringutil.NormalizeSafeOutputIdentifier("executor-workflow.agent") // "executor_workflow_agent"
```

### `NormalizeIdentifierToHyphens(identifier string) string`

Converts underscores **and periods** to hyphens, normalizing user-facing `underscore_separated` and dot-separated formats to the hyphen-separated format conventionally used for GitHub Actions job names. This is the hyphen-canonical counterpart to `NormalizeSafeOutputIdentifier`.

```go
stringutil.NormalizeIdentifierToHyphens("create_issue")            // "create-issue"
stringutil.NormalizeIdentifierToHyphens("executor_workflow.agent") // "executor-workflow-agent"
```

### `MarkdownToLockFile(mdPath string) string`

Converts a workflow markdown path (`.md`) to its compiled lock file path (`.lock.yml`). Returns the path unchanged if it already ends with `.lock.yml`.

```go
stringutil.MarkdownToLockFile(".github/workflows/test.md")
// → ".github/workflows/test.lock.yml"
```

### `LockFileToMarkdown(lockPath string) string`

Converts a compiled lock file path (`.lock.yml`) back to its markdown source path (`.md`). Returns the path unchanged if it already ends with `.md`.

```go
stringutil.LockFileToMarkdown(".github/workflows/test.lock.yml")
// → ".github/workflows/test.md"
```

## Sanitization (`sanitize.go`)

These functions remove sensitive information to prevent accidental leakage in logs or error messages.

### `SanitizeName(name string, opts *SanitizeOptions) string`

Sanitizes a name for identifiers and filenames using configurable behavior (preserved special characters, optional hyphen trimming, and fallback default value).

### `SanitizeErrorMessage(message string) string`

Redacts potential secret key names from error messages. Matches uppercase `SNAKE_CASE` identifiers (e.g. `MY_SECRET_KEY`, `API_TOKEN`) and PascalCase identifiers ending with security-related suffixes (e.g. `GitHubToken`, `ApiKey`). Common GitHub Actions workflow keywords (`GITHUB`, `RUNNER`, `WORKFLOW`, etc.) are excluded from redaction.

```go
stringutil.SanitizeErrorMessage("Error: MY_SECRET_TOKEN is invalid")
// → "Error: [REDACTED] is invalid"
```

### `SanitizeIdentifierName(name string, extraAllowed func(rune) bool) string`

Sanitizes a string for use as a programming-language identifier by replacing invalid characters with underscores and prefixing `_` when the identifier starts with a digit. `extraAllowed` can be used to permit additional runes beyond the normal identifier rules; if `extraAllowed` is `nil`, no extra characters are allowed.

### `SanitizeParameterName(name string) string`

Sanitizes a parameter name for use as a JavaScript/GitHub Actions identifier. Preserves letters, digits, `$`, and `_`, prepends `_` when the name starts with a digit, and replaces all other characters with underscores.

```go
stringutil.SanitizeParameterName("my-param")  // "my_param"
stringutil.SanitizeParameterName("$special")  // "$special"
stringutil.SanitizeParameterName("123param")  // "_123param"
```

### `SanitizePythonVariableName(name string) string`

Sanitizes a string for use as a Python variable name. Preserves letters, digits, and `_` (no `$`), prepends `_` when the name starts with a digit, and replaces all other characters with underscores.

```go
stringutil.SanitizePythonVariableName("my-param") // "my_param"
stringutil.SanitizePythonVariableName("123param")  // "_123param"
```

### `SanitizeToolID(toolID string) string`

Removes common MCP prefixes (`mcp-`) and suffixes (`-mcp`) from tool identifiers. Returns the original ID if the cleaned result would be empty.

```go
stringutil.SanitizeToolID("notion-mcp")      // "notion"
stringutil.SanitizeToolID("mcp-notion")      // "notion"
stringutil.SanitizeToolID("some-mcp-server") // "some-mcp-server" (middle occurrence unchanged)
stringutil.SanitizeToolID("mcp")             // "mcp" (prevents empty result)
```

### `SanitizeForFilename(slug string) string`

Converts a repository slug (e.g. `owner/repo`) to a filesystem-safe string. Replaces `/` with `-` and any remaining non-alphanumeric characters (except `-`, `_`, `.`) with `-`. Returns `"clone-mode"` if the slug is empty. Does **not** change the letter case.

```go
stringutil.SanitizeForFilename("owner/repo")     // "owner-repo"
stringutil.SanitizeForFilename("my.org/my_repo") // "my.org-my_repo"
stringutil.SanitizeForFilename("")               // "clone-mode"
```

## URL Utilities (`urls.go`)

### `NormalizeGitHubHostURL(rawHostURL string) string`

Normalizes a GitHub host URL by ensuring it has an `https://` scheme and no trailing slash. Accepts bare hostnames, URLs with or without a scheme, and URLs with trailing slashes.

```go
stringutil.NormalizeGitHubHostURL("github.example.com")        // "https://github.example.com"
stringutil.NormalizeGitHubHostURL("https://github.com/")       // "https://github.com"
```

### `ExtractDomainFromURL(urlStr string) string`

Extracts the hostname (without port) from a URL string. Falls back to simple string parsing when `url.Parse` cannot handle the input.

```go
stringutil.ExtractDomainFromURL("https://api.github.com/repos") // "api.github.com"
```

## PAT Validation (`pat_validation.go`)

### `PATType`

A string type representing the category of a GitHub Personal Access Token.

| Constant | Value | Prefix |
|----------|-------|--------|
| `PATTypeFineGrained` | `"fine-grained"` | `github_pat_` |
| `PATTypeClassic` | `"classic"` | `ghp_` |
| `PATTypeOAuth` | `"oauth"` | `gho_` |
| `PATTypeUnknown` | `"unknown"` | (other) |

Methods: `String() string`, `IsFineGrained() bool`, `IsValid() bool`

### `ClassifyPAT(token string) PATType`

Determines the token type from its prefix.

### `ValidateCopilotPAT(token string) error`

Returns `nil` if the token is a fine-grained PAT; returns an actionable error message with a link to create the correct token type otherwise.

```go
if err := stringutil.ValidateCopilotPAT(token); err != nil {
    fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
}
```

### `GetPATTypeDescription(token string) string`

Returns a human-readable description of the token type (e.g. `"fine-grained personal access token"`).

## Fuzzy Matching (`fuzzy_match.go`)

### `FindClosestMatches(target string, candidates []string, maxResults int) []string`

Finds the closest matching strings using Levenshtein distance. Returns up to `maxResults` matches that have a distance of 3 or less. Results are sorted by distance (closest first), then alphabetically for ties. Case-insensitive matching. Exact matches are excluded.

This function is useful for "Did you mean?" suggestions when a user provides an unrecognized value (e.g., a typo in an engine name or event type).

```go
engines := []string{"copilot", "claude", "codex", "custom"}
matches := stringutil.FindClosestMatches("copiliot", engines, 3)
// → ["copilot"]
```

### `LevenshteinDistance(a, b string) int`

Computes the Levenshtein distance between two strings — the minimum number of single-character edits (insertions, deletions, or substitutions) required to change one string into the other. Uses dynamic programming with space optimization (only the previous row is stored).

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/stringutil"

// Truncate a long string for display
stringutil.Truncate("hello world", 8) // "hello..."

// Strip ANSI color codes from terminal output
plain := stringutil.StripANSI("\x1b[32mSuccess\x1b[0m") // "Success"

// Normalize workflow names
stringutil.NormalizeWorkflowName("weekly-research.md")       // "weekly-research"
stringutil.NormalizeWorkflowName("weekly-research.lock.yml") // "weekly-research"

// Convert markdown path to lock file and back
stringutil.MarkdownToLockFile(".github/workflows/test.md")       // ".github/workflows/test.lock.yml"
stringutil.LockFileToMarkdown(".github/workflows/test.lock.yml") // ".github/workflows/test.md"

// Redact secrets from error messages
stringutil.SanitizeErrorMessage("Error: MY_SECRET_TOKEN is invalid")
// → "Error: [REDACTED] is invalid"

// Normalize a GitHub host URL
stringutil.NormalizeGitHubHostURL("github.example.com") // "https://github.example.com"

// Validate a Copilot PAT
if err := stringutil.ValidateCopilotPAT(token); err != nil {
    fmt.Fprintln(os.Stderr, console.FormatErrorMessage(err.Error()))
}

// Find closest matches for "Did you mean?" suggestions
engines := []string{"copilot", "claude", "codex", "custom"}
matches := stringutil.FindClosestMatches("copiliot", engines, 3)
// → ["copilot"]

// Compute Levenshtein distance
distance := stringutil.LevenshteinDistance("copiliot", "copilot")
// → 1
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging

## Design Decisions

- All debug output uses namespace-prefixed loggers (`stringutil:identifiers`, `stringutil:sanitize`, `stringutil:urls`, `stringutil:pat_validation`, `stringutil:whitespace`, `stringutil:version`) and is only emitted when `DEBUG=stringutil:*`.
- `SanitizeErrorMessage` is intentionally conservative: it excludes common GitHub Actions keywords to avoid over-redacting legitimate error messages.
- `StripANSI` handles both CSI sequences (`ESC[`) and other ESC-prefixed sequences to cover the full range of ANSI escape codes found in terminal output.

## Thread Safety

All functions in this package are stateless pure functions operating on immutable string inputs. They are safe to call concurrently from multiple goroutines without synchronization.

<!-- BEGIN SOURCE-VERIFIED EXPORT COVERAGE -->
## Source-verified export coverage

This appendix is generated from the current non-test Go source files in this package and records any exported top-level symbols that are not already described above.

| Category | Count |
|----------|------:|
| Types | 2 |
| Constants | 4 |
| Variables | 0 |
| Functions and methods | 29 |
| Additional symbols documented in this appendix | 0 |

The sections above already mention every exported top-level symbol in the current source tree.
<!-- END SOURCE-VERIFIED EXPORT COVERAGE -->

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
