# File/URL Inlining Feature Implementation Summary

## Feature Overview

This implementation adds runtime import support for including file and URL content directly within workflow prompts at runtime:

- **`{{#runtime-import filepath}}`** - Include entire file content (from `.github` folder)
- **`{{#runtime-import filepath:10-20}}`** - Include lines 10-20 from a file (1-indexed, from `.github` folder)
- **`{{#runtime-import https://example.com/file.txt}}`** - Fetch and include URL content (with caching)

**Security Note:** File imports are **restricted to the `.github` folder** to prevent access to arbitrary repository files. URLs are not restricted.

## Implementation Details

### Architecture

The feature reuses and extends the existing `runtime_import.cjs` infrastructure:

1. **File Processing** (`processRuntimeImport`)
   - Reads files from `.github` folder relative to `GITHUB_WORKSPACE`
   - Supports `.github/` prefix trimming (both `file.md` and `.github/file.md` work)
   - Supports line range extraction (1-indexed, inclusive)
   - Applies content sanitization (front matter removal, XML comment stripping, macro detection)
   - Smart email address filtering to avoid processing `user@example.com`
   - **Security:** Validates all paths stay within `.github` folder

2. **URL Processing** (`processUrlImport`)
   - Fetches HTTP/HTTPS URLs (not restricted to `.github`)
   - Caches content for 1 hour in `/tmp/gh-aw/url-cache/`
   - Uses SHA256 hash of URL for cache filenames
   - Applies same sanitization as file content

3. **Integration** (`interpolate_prompt.cjs`)
   - Step 1: Process `{{#runtime-import}}` macros
   - Step 2: Interpolate variables (`${GH_AW_EXPR_*}`)
   - Step 3: Render template conditionals (`{{#if}}`)

### Processing Order

```
Workflow Source (.md)
        ↓
    Compilation
        ↓
    Lock File (.lock.yml)
        ↓
[Runtime Execution in GitHub Actions]
        ↓
    {{#runtime-import}} → Includes external markdown files
        ↓
    ${GH_AW_EXPR_*} → Variable interpolation
        ↓
    {{#if}} → Template conditionals
        ↓
    Final Prompt
```

## Example Usage

### Example 1: Code Review with Guidelines

```markdown
---
description: Automated code review with standards
on: pull_request
engine: copilot
---

# Code Review Agent

Please review this pull request following our coding guidelines.

## Coding Standards

{{#runtime-import docs/coding-standards.md}}

## Security Checklist

{{#runtime-import https://raw.githubusercontent.com/org/security/main/checklist.md}}

## Review Process

1. Check code quality
2. Verify security practices
3. Ensure documentation is updated
```

### Example 2: Bug Analysis with Context

```markdown
---
description: Analyze bug with surrounding code
on: issues
engine: copilot
---

# Bug Analysis

## Reported Issue

${{ github.event.issue.body }}

## Relevant Code Section

The issue appears to be in the authentication module:

{{#runtime-import src/auth.go:45-75}}

## Related Test Cases

{{#runtime-import tests/auth_test.go:100-150}}

Please analyze the bug and suggest a fix.
```

### Example 3: Documentation Update

```markdown
---
description: Update documentation with latest examples
on: workflow_dispatch
engine: copilot
---

# Documentation Updater

Update our README with the latest version information.

## Current README Header

{{#runtime-import README.md:1-10}}

## License Information

{{#runtime-import LICENSE:1-5}}

Ensure all documentation is consistent and up-to-date.
```

## Testing Coverage

### Unit Tests in `runtime_import.test.cjs`

**File Processing Tests:**
- ✅ Full file content reading
- ✅ Line range extraction (single line, multiple lines, ranges)
- ✅ Invalid line range detection (out of bounds, start > end)
- ✅ Front matter removal and warnings
- ✅ XML comment removal
- ✅ GitHub Actions macro detection and rejection
- ✅ Subdirectory file handling
- ✅ Empty files
- ✅ Files with only front matter

**Macro Processing Tests:**
- ✅ Single `{{#runtime-import}}` reference
- ✅ Multiple `{{#runtime-import}}` references
- ✅ `{{#runtime-import filepath:line-line}}` syntax
- ✅ Multiple line ranges in same content
- ✅ Subdirectory paths
- ✅ Unicode content handling
- ✅ Special characters in content

**URL Processing Tests:**
- ✅ No URL references handling
- ✅ Regular URLs without @ prefix ignored
- ✅ URL pattern matching

**Error Handling Tests:**
- ✅ Missing file errors
- ✅ Invalid line range errors
- ✅ GitHub Actions macro errors

**Integration Tests:**
- ✅ Works with existing runtime-import feature
- ✅ All JavaScript tests pass
- ✅ All Go unit tests pass

## Real-World Use Cases

### 1. Consistent Code Review Standards

Instead of duplicating review guidelines in every workflow:

```markdown
{{#runtime-import .github/workflows/shared/review-standards.md}}
```

### 2. Security Audit Checklists

Include security checklists from a central source:

```markdown
{{#runtime-import https://company.com/security/api-security-checklist.md}}
```

### 3. Code Context for AI Analysis

Provide specific code sections for targeted analysis:

```markdown
Review this function:

{{#runtime-import src/payment/processor.go:234-267}}

Compare with the test:

{{#runtime-import tests/payment/processor_test.go:145-178}}
```

### 4. License and Attribution

Include license information in generated content:

```markdown
## License

{{#runtime-import LICENSE:1-5}}
```

### 5. Configuration Templates

Reference standard configurations:

```markdown
Use this Terraform template:

{{#runtime-import templates/vpc-config.tf:10-50}}
```

## Performance Considerations

### File Processing
- ✅ Fast - reads local files from `GITHUB_WORKSPACE`
- ✅ No network overhead
- ✅ Line range extraction is O(n) where n = file lines

### URL Processing
- ✅ 1-hour cache reduces network requests
- ✅ SHA256 hash for safe cache filenames
- ⚠️ First fetch adds latency (~500ms-2s depending on URL)
- ⚠️ Cache stored in `/tmp/gh-aw/url-cache/` (ephemeral, per workflow run)

### Content Sanitization
- ✅ Minimal overhead for front matter and XML comment removal
- ✅ Regex-based GitHub Actions macro detection is fast

## Security Considerations

### GitHub Actions Macro Prevention
All inlined content is checked for `${{ ... }}` expressions to prevent:
- Template injection attacks
- Unintended variable expansion
- Security vulnerabilities

### Front Matter Stripping
YAML front matter is automatically removed to prevent:
- Configuration leakage
- Metadata exposure
- Parsing conflicts

### URL Caching Security
- Cache uses SHA256 hash of URL for filenames
- Cache stored in ephemeral `/tmp/` directory
- Cache automatically cleaned up after workflow run
- No persistent storage across workflow runs

## Limitations

1. **Line ranges use raw file content** - Line numbers refer to the original file before front matter removal
2. **URL cache is per-run** - Cache doesn't persist across workflow runs
3. **No recursive processing** - Inlined content cannot contain additional inline references
4. **Network errors fail workflow** - Failed URL fetches cause workflow failure (by design)
5. **No authentication for URLs** - URL fetching doesn't support authenticated requests

## Future Enhancements

Potential improvements for future versions:

1. **Persistent URL cache** - Cache URLs across workflow runs
2. **Authenticated URL fetching** - Support for private URLs with tokens
3. **Recursive inlining** - Support nested inline references
4. **Custom cache TTL** - Allow per-URL cache expiration configuration
5. **Binary file support** - Handle base64-encoded binary content
6. **Git ref support** - `@repo@ref:path/to/file.md` syntax for cross-repo files

## Conclusion

The runtime import feature provides a flexible way to include external content in workflow prompts. It uses the `{{#runtime-import}}` macro syntax for consistent and predictable behavior.

### Key Benefits
- ✅ **Clear syntax** with `{{#runtime-import}}`
- ✅ **Line range support** for targeted content
- ✅ **URL fetching** with automatic caching
- ✅ **Security built-in** with macro detection
- ✅ **Comprehensive testing** with unit tests

### Implementation Quality
- ✅ All tests passing
- ✅ Comprehensive documentation
- ✅ Example workflows provided
- ✅ No breaking changes to existing features
- ✅ Clean integration with existing codebase
