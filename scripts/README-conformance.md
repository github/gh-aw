# Safe Outputs Specification Conformance Checker

This directory contains automated tools for verifying conformance with the Safe Outputs MCP Gateway Specification.

## Overview

The conformance checker validates implementations against normative requirements in the [Safe Outputs Specification](/docs/src/content/docs/reference/safe-outputs-specification.md). It runs automated checks across four categories:

- **SEC**: Security requirements (privilege separation, validation ordering, sanitization)
- **USE**: Usability requirements (error codes, footers, staged mode formatting)
- **REQ**: Requirements compliance (RFC 2119 keywords, testability, completeness)
- **IMP**: Implementation requirements (handler registration, permission computation, schema validation)

## Usage

### Run All Checks

```bash
./scripts/check-safe-outputs-conformance.sh
```

### Exit Codes

- `0`: All checks passed or only low/medium severity warnings
- `1`: High severity failures detected
- `2`: Critical severity failures detected

### Output Format

The checker uses color-coded output:

- 🔴 **[CRITICAL]**: Must be fixed immediately (security violations)
- 🔴 **[HIGH]**: Should be fixed soon (significant issues)
- 🟡 **[MEDIUM]**: Should be addressed (quality issues)
- 🔵 **[LOW]**: Nice to have (minor improvements)
- 🟢 **[PASS]**: Check passed

## Checks Implemented

### Security Checks

| ID | Name | Severity | Description |
|----|------|----------|-------------|
| SEC-001 | Privilege Separation Enforcement | CRITICAL | Verifies agent jobs lack write permissions |
| SEC-002 | Validation Before API Calls | CRITICAL | Ensures validation occurs before GitHub API calls |
| SEC-003 | Max Limit Enforcement | MEDIUM | Verifies handlers enforce max operation limits |
| SEC-004 | Content Sanitization Required | MEDIUM | Checks that content sanitization is applied |
| SEC-005 | Cross-Repository Validation | HIGH | Validates cross-repo operations check allowlists |

### Usability Checks

| ID | Name | Severity | Description |
|----|------|----------|-------------|
| USE-001 | Error Code Standardization | LOW | Verifies handlers use standardized error codes |
| USE-002 | Footer Attribution Required | LOW | Checks footers are added when configured |
| USE-003 | Staged Mode Preview Format | LOW | Validates staged mode uses 🎭 emoji format |

### Requirements Checks

| ID | Name | Severity | Description |
|----|------|----------|-------------|
| REQ-001 | RFC 2119 Keyword Usage | MEDIUM | Verifies normative sections use MUST/SHOULD/MAY |
| REQ-002 | Safe Output Type Completeness | MEDIUM | Checks each type has all required documentation |
| REQ-003 | Verification Method Specification | LOW | Ensures requirements include verification methods |

### Implementation Checks

| ID | Name | Severity | Description |
|----|------|----------|-------------|
| IMP-001 | Handler Registration Completeness | HIGH | Verifies all standard handlers exist |
| IMP-002 | Permission Computation Accuracy | HIGH | Checks permission computation function exists |
| IMP-003 | Schema Validation Consistency | MEDIUM | Validates schema generation is implemented |

## Adding New Checks

To add a new conformance check:

1. Add a check function following the naming pattern `check_<category>_<name>`
2. Use the logging functions: `log_critical`, `log_high`, `log_medium`, `log_low`, `log_pass`
3. Increment the appropriate failure counter when a check fails
4. Add a call to your check function in the main script flow
5. Document the check in this README

Example:

```bash
check_new_requirement() {
    local failed=0
    
    # Your check logic here
    if [ condition_not_met ]; then
        log_high "CHECK-ID: Description of failure"
        failed=1
    fi
    
    if [ $failed -eq 0 ]; then
        log_pass "CHECK-ID: Check passed successfully"
    fi
}
```

## Related Documentation

- [Safe Outputs Specification](/docs/src/content/docs/reference/safe-outputs-specification.md) - Complete normative specification
- [Specification Review Findings](/docs/spec-review-findings.md) - Detailed security, usability, and requirements review
- [Specification Improvements Plan](/docs/spec-improvements-plan.md) - Roadmap for addressing findings

## CI Integration

The conformance checker can be integrated into CI/CD pipelines:

```yaml
# .github/workflows/conformance.yml
name: Conformance Check
on: [push, pull_request]

jobs:
  conformance:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - name: Run conformance checks
        run: ./scripts/check-safe-outputs-conformance.sh
```

## Maintenance

The conformance checker should be updated when:

- New safe output types are added
- Specification requirements change
- New security properties are added
- Implementation patterns evolve

Regular maintenance ensures the checker stays aligned with the specification and implementation.
