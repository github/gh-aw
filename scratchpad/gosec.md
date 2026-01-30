---
description: Documentation for gosec security exclusions and rationale
applyTo: "**/*"
---

# Gosec Security Exclusions

This document provides documentation for all gosec security rule exclusions configured in `.golangci.yml`. These exclusions have been reviewed for security impact and provide an audit trail for compliance requirements.

## Configuration Source

### Primary Source of Truth: `.golangci.yml`
The `.golangci.yml` file (section `linters-settings.gosec.exclude`) serves as the authoritative source for gosec exclusion configuration. This ensures consistency across development and CI/CD environments.

### Usage Locations
The exclusions defined in `.golangci.yml` are applied in:
1. **Makefile** (`make security-gosec` target) - Uses command-line `-exclude` flag
2. **GitHub Actions** (`.github/workflows/security-scan.yml`) - Uses command-line `-exclude` flag

**Important**: When updating gosec exclusions in `.golangci.yml`, also update the `-exclude` flags in both Makefile and GitHub Actions workflows to maintain consistency.

## Overview

This project uses [gosec](https://github.com/securego/gosec) for security scanning with specific exclusions documented below. Each exclusion includes:
- CWE mapping for compliance tracking
- Detailed rationale explaining why it's excluded
- Context about specific use cases
- Mitigation strategies for security risks
- Review dates for audit trail

### Gosec Rules Reference

For complete documentation of all available gosec rules, see:
- **Gosec Official Documentation**: https://github.com/securego/gosec
- **Available Rules**: https://github.com/securego/gosec#available-rules
- **Rule Details**: Each rule below links to its specific documentation section

## Global Exclusions

The following gosec rules are globally excluded across the entire codebase:

### G101: Hardcoded Credentials
- **CWE**: CWE-798 (Use of Hard-coded Credentials)
- **Documentation**: https://github.com/securego/gosec#available-rules (G101 - Look for hardcoded credentials)
- **Rationale**: High false positive rate on variable names containing terms like `token`, `secret`, `password`, `key`, etc. The rule triggers on identifiers, not actual values.
- **Examples of False Positives**:
  - Variable names: `tokenURL`, `secretName`, `apiKeyHeader`
  - Constants: `DefaultTokenEnv`, `SecretPrefix`
  - Function parameters: `func getToken(tokenName string)`
- **Mitigation**: 
  - Actual secrets are stored in GitHub Secrets or environment variables
  - No credentials are committed to source code
  - Pre-commit hooks and code review catch actual credential leaks
- **Review Date**: 2025-12-25

### G115: Integer Overflow Conversion
- **CWE**: CWE-190 (Integer Overflow or Wraparound)
- **Documentation**: https://github.com/securego/gosec#available-rules (G115 - Integer overflow conversion)
- **Rationale**: Integer conversions are acceptable in most cases in this codebase. The code primarily deals with configuration values, file sizes, and counts that are within safe ranges.
- **Context**: 
  - Configuration values are bounded and validated
  - File operations use appropriate size types
  - Counter values are within safe integer ranges
- **Mitigation**:
  - Input validation on all external data
  - Unit tests cover edge cases including boundary values
  - Runtime panics are acceptable for truly invalid conversions
- **Review Date**: 2025-12-25

### G602: Slice Bounds Check
- **CWE**: CWE-118 (Improper Access of Indexable Resource)
- **Documentation**: https://github.com/securego/gosec#available-rules (G602 - Slice access out of bounds)
- **Rationale**: Go runtime provides automatic bounds checking. Out-of-bounds access causes a panic rather than undefined behavior or memory corruption.
- **Context**:
  - Go's built-in bounds checking is a language safety feature
  - Panics are recovered at appropriate boundaries
  - Known false positives with switch statement bounds checks
- **Mitigation**:
  - Comprehensive unit tests cover slice operations
  - Integration tests validate real-world usage patterns
  - Panics are handled gracefully at API boundaries
- **Review Date**: 2025-12-25

## File-Specific Exclusions

The following files have specific gosec rule exclusions with documented rationale:

### G204: Subprocess Execution with Variable Arguments
- **CWE**: CWE-78 (OS Command Injection)
- **Documentation**: https://github.com/securego/gosec#available-rules (G204 - Audit use of command execution)
- **Files**: 
  - `pkg/cli/actionlint.go` - Docker commands for actionlint
  - `pkg/parser/remote_fetch.go` - Git commands for remote workflow fetching
  - `pkg/cli/download_workflow.go` - Git operations for workflow downloads
  - `pkg/cli/mcp_inspect.go` - Exec commands for MCP inspector
  - `pkg/cli/mcp_inspect_mcp.go` - MCP server execution
  - `pkg/cli/poutine.go` - Docker commands for Poutine scanner
  - `pkg/cli/zizmor.go` - Docker commands for Zizmor scanner
  - `pkg/workflow/js_comments_test.go` - Node command in tests
  - `pkg/workflow/playwright_mcp_integration_test.go` - npx commands in integration tests
  - `pkg/cli/status_command_test.go` - Binary execution in tests
- **Rationale**: Commands are constructed from:
  - Validated workflow configurations (parsed and type-checked)
  - Controlled Docker image references from known registries
  - Git operations on validated repository references
  - Tool invocations with allowlisted arguments
- **Mitigation**:
  - Input validation before command construction
  - Allowlist checks for command arguments
  - Docker images from trusted registries only
  - Git URLs validated against GitHub patterns
  - Test files use controlled test data
- **Review Date**: 2025-12-25

### G404: Insecure Random Number Generation
- **CWE**: CWE-338 (Use of Cryptographically Weak PRNG)
- **Documentation**: https://github.com/securego/gosec#available-rules (G404 - Insecure random number source)
- **Files**:
  - `pkg/cli/add_command.go` - Random IDs for temporary resources
  - `pkg/cli/update_git.go` - Non-cryptographic random operations
- **Rationale**: `math/rand` is used for non-cryptographic purposes such as:
  - Generating temporary file names
  - Creating random test data
  - Non-security-sensitive unique identifiers
- **Mitigation**:
  - Cryptographic operations use `crypto/rand`
  - Security tokens and secrets use cryptographically secure sources
  - Random values are not used for security decisions
- **Review Date**: 2025-12-25

### G306: Weak File Permissions
- **CWE**: CWE-276 (Incorrect Default Permissions)
- **Documentation**: https://github.com/securego/gosec#available-rules (G306 - Poor file permissions used when creating a file)
- **Files**:
  - `_test.go` (all test files) - Test file creation with 0644
  - `pkg/cli/mcp_inspect.go` - Executable script with 0755
  - `pkg/cli/actions_build_command.go` - Shell scripts with 0755
- **Configuration**: `G204: "0644"`, `G306: "0644"` allowed
- **Rationale**:
  - 0644 permissions are appropriate for regular files (owner read/write, group/others read)
  - 0755 permissions are correct for executable scripts (owner read/write/execute, group/others read/execute)
  - Test files are in temporary directories and cleaned up
  - Production files are in user-controlled directories
- **Mitigation**:
  - File permissions match Unix conventions
  - Sensitive data files use restrictive permissions (0600)
  - Executable scripts correctly marked as executable
- **Review Date**: 2025-12-25

### G305: File Traversal in Archive Extraction
- **CWE**: CWE-22 (Path Traversal)
- **Documentation**: https://github.com/securego/gosec#available-rules (G305 - File traversal when extracting zip/tar archive)
- **Files**:
  - `pkg/cli/logs_download.go` - Workflow log archive extraction
- **Rationale**: GitHub Actions workflow logs are downloaded from GitHub API (trusted source)
- **Mitigation**:
  - Logs downloaded only from authenticated GitHub API
  - Extraction performed in controlled temporary directories
  - Paths validated before file operations
  - Archives from trusted sources only
- **Review Date**: 2025-12-25

### G110: Potential Decompression Bomb
- **CWE**: CWE-400 (Uncontrolled Resource Consumption)
- **Documentation**: https://github.com/securego/gosec#available-rules (G110 - Potential DoS vulnerability via decompression bomb)
- **Files**:
  - `pkg/cli/logs_download.go` - Workflow log decompression
- **Rationale**: Workflow logs are from GitHub Actions (trusted source) with known size constraints
- **Mitigation**:
  - Logs have maximum size limits imposed by GitHub Actions
  - Decompression in temporary directories with disk space monitoring
  - User-initiated operation with visible progress
- **Review Date**: 2025-12-25

### G301: Directory Permissions
- **CWE**: CWE-276 (Incorrect Default Permissions)
- **Documentation**: https://github.com/securego/gosec#available-rules (G301 - Poor file permissions used when creating a directory)
- **Files**:
  - `pkg/parser/import_cache.go` - Cache directory creation
  - `pkg/parser/frontmatter_includes_test.go` - Test directory setup
  - `pkg/testutil/tempdir.go` - Temporary directory utilities
- **Rationale**: 0755 permissions are appropriate for directories (owner read/write/execute, group/others read/execute)
- **Mitigation**:
  - Directories created in user-controlled locations
  - Cache directories follow standard Unix permissions
  - Temporary directories cleaned up after use
  - Sensitive data uses restrictive permissions where needed
- **Review Date**: 2025-12-25

### G302: File Permissions for chmod Operations
- **CWE**: CWE-276 (Incorrect Default Permissions)
- **Documentation**: https://github.com/securego/gosec#available-rules (G302 - Poor file permissions used with chmod)
- **Rationale**: chmod operations with 0755 are acceptable for making files executable
- **Context**:
  - Used for shell scripts that need to be executed
  - Follows standard Unix executable file permissions
  - Only applied to generated scripts, not user data
- **Mitigation**:
  - chmod only applied to generated script files
  - Permissions appropriate for the file type (executable scripts)
  - Files created in controlled locations
- **Review Date**: 2025-12-25

### G304: File Inclusion via Variable
- **CWE**: CWE-22 (Path Traversal)
- **Documentation**: https://github.com/securego/gosec#available-rules (G304 - File path provided as taint input)
- **Files**:
  - `pkg/parser/frontmatter_content.go` - Frontmatter file reading
  - `pkg/parser/include_expander.go` - Workflow include processing
  - `pkg/parser/include_processor.go` - Include directive handling
- **Rationale**: File paths are validated before use in workflow parsing and include processing
- **Mitigation**:
  - All file paths validated against allowed patterns
  - Path traversal attempts detected and rejected
  - Files only read from known safe locations
  - Input validation on all user-provided paths
- **Review Date**: 2025-12-25

## Suppression Guidelines

When you need to suppress gosec warnings in code, use `#nosec` annotations with proper justification:

**Required Format**:
```go
// #nosec G<rule-id> -- <brief justification>
<code that triggers the warning>
```text

**Best Practices**:
1. **Always include the rule ID**: `G204`, `G404`, etc.
2. **Use `--` separator**: Clearly separates rule ID from justification
3. **Keep justifications brief**: Under 80 characters
4. **Be specific**: Explain why this particular instance is safe
5. **Consider alternatives**: Is there a way to avoid the suppression?

**Examples**:

```go
// #nosec G204 -- Command arguments validated via allowlist
cmd := exec.Command("docker", validatedArgs...)

// #nosec G404 -- Random ID for temporary file, not security-sensitive
tmpID := fmt.Sprintf("tmp-%d", rand.Intn(1000000))

// #nosec G306 -- Executable script requires 0755 permissions
os.WriteFile(scriptPath, content, 0755)
```text

**When to Use Suppressions**:
- ✅ After confirming the code is actually safe
- ✅ When the security risk is mitigated by other controls
- ✅ For false positives that cannot be avoided
- ✅ In test files for controlled test scenarios

**When NOT to Use Suppressions**:
- ❌ To bypass legitimate security issues
- ❌ Without understanding the security implication
- ❌ Without documenting the justification
- ❌ As a shortcut instead of fixing the code

**Review Process**:
1. All `#nosec` annotations are reviewed during code review
2. Reviewers must verify the justification is valid
3. Security-sensitive suppressions require additional review
4. Suppressions should be rare - prefer secure alternatives

## Compliance and Audit Trail

This documentation provides an audit trail for compliance requirements:

- **Security Audits**: Documents accepted risks and mitigation strategies
- **Compliance Standards**: Supports SOC2, ISO 27001, and similar frameworks
- **Change Management**: Review dates track when exclusions were last evaluated
- **Incident Response**: Provides context for security incident investigations

**Review Schedule**:
- Security exclusions reviewed quarterly or when:
  - New gosec rules are added
  - Major security incidents occur in the Go ecosystem
  - Significant codebase refactoring is performed
  - Compliance requirements change

---

**Last Updated**: 2025-12-25
