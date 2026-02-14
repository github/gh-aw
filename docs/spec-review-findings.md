# Safe Outputs Specification Review Findings

**Date**: 2026-02-14  
**Specification**: [Safe Outputs MCP Gateway Specification v1.8.0](/docs/src/content/docs/reference/safe-outputs-specification.md)  
**Commit**: [a5b6606](https://github.com/github/gh-aw/commit/a5b6606aead2b2f2c3c53a46da1d1fe88f5ee583)  
**Reviewer**: Automated Security, Usability, and Requirements Review

## Executive Summary

This document presents findings from a comprehensive review of the Safe Outputs MCP Gateway Specification from three perspectives: **security**, **usability**, and **requirements compliance**. The specification establishes a W3C-style normative framework for secure AI-to-GitHub operation translation.

**Overall Assessment**: The specification is **well-structured** with strong security foundations and clear architectural separation. However, several areas require clarification and enhancement to enable effective automated rule encoding and conformance testing.

### Key Strengths
- ✅ Strong security architecture with privilege separation
- ✅ Clear RFC 2119 requirement levels
- ✅ Comprehensive threat model with 5 identified threats
- ✅ Detailed safe output type catalog (36 types documented)
- ✅ Complete permission documentation for GitHub Actions Token and GitHub App

### Areas Requiring Improvement
- ⚠️ Ambiguous enforcement requirements for some security properties
- ⚠️ Missing automated test specifications
- ⚠️ Inconsistent normative language in some sections
- ⚠️ Limited guidance on error handling edge cases
- ⚠️ Insufficient detail on cross-repository validation rules

---

## 1. Security Review Findings

### 1.1 Critical Security Issues

#### **Finding S1: Ambiguous Validation Ordering Requirements**

**Location**: Section 3.3, Property SP2 (Validation Precedence Invariant)

**Issue**: The specification states "validation logic MUST execute before any GitHub API invocation" but does not specify:
1. What constitutes "validation logic" (schema validation only, or also sanitization, limit checks, etc.)?
2. Whether partial validation results can trigger API calls
3. How to handle validation dependencies (e.g., validating parent issue exists before creating sub-issue)

**Security Impact**: **MEDIUM** - Unclear validation ordering could lead to race conditions or partial execution states where some validations are bypassed.

**Recommendation**:
```markdown
Add explicit validation pipeline specification:

**Validation Pipeline Ordering**:

Implementations MUST execute validation in this exact order:

1. **Schema Validation** (MUST reject invalid JSON structure before proceeding)
2. **Limit Enforcement** (MUST reject operations exceeding max limits)
3. **Content Sanitization** (MUST sanitize all text fields)
4. **Domain Filtering** (MUST apply allowed-domains if configured)
5. **Cross-Repository Validation** (MUST validate target-repo against allowed-repos)
6. **Dependency Resolution** (MUST resolve temporary IDs and parent references)
7. **GitHub API Invocation** (Only after steps 1-6 complete successfully)

Any failure in steps 1-6 MUST prevent step 7 from executing.
```

**Automated Rule**:
```yaml
# validation-ordering-check.yml
rules:
  - id: validation-before-api
    pattern: |
      Must verify that all safe output handler implementations:
      1. Perform schema validation before API calls
      2. Enforce limits before API calls
      3. Sanitize content before API calls
    check: |
      grep -A 50 "function.*Handler" actions/setup/js/*.cjs | \
      grep -B 5 "octokit\." | \
      grep -q "validateSchema\|enforceLimit\|sanitize"
    severity: CRITICAL
```

---

#### **Finding S2: Insufficient Cross-Repository Security Model**

**Location**: Section 3.2, Threat T5 (Cross-Repository Privilege Escalation)

**Issue**: The specification describes `allowed-github-references` and per-type `allowed-repos` but does not specify:
1. How these interact when both are configured
2. Whether allowlist matching is exact or supports patterns
3. How to handle organization-wide or wildcard allowlists securely
4. What happens when target-repo is specified but not in allowed-repos

**Security Impact**: **HIGH** - Ambiguous cross-repository rules could allow unauthorized repository targeting through configuration misunderstanding.

**Recommendation**:
```markdown
Add Cross-Repository Security Model section:

**Cross-Repository Validation Rules**:

When `target-repo` is specified in a safe output operation:

1. **Extract Repository Reference**: Parse `target-repo` as `owner/repo` format
2. **Validate Format**: MUST match regex `^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
3. **Check Global Allowlist**: If `allowed-github-references` is non-empty:
   - MUST check if `target-repo` matches any entry
   - Match is EXACT (no wildcards, no pattern matching)
4. **Check Type-Specific Allowlist**: If safe output type has `allowed-repos`:
   - MUST check if `target-repo` matches any entry
   - Type-specific allowlist OVERRIDES global allowlist (does not merge)
5. **Deny by Default**: If neither allowlist permits the repo, MUST reject operation

**Wildcard Rules**: Wildcards (*, ?) are NOT supported in allowlists. Each repository MUST be explicitly listed.

**Organization-Wide**: To allow all repos in an organization:
- List repositories explicitly, OR
- Use GitHub App permissions with organization-level access
```

**Automated Rule**:
```yaml
# cross-repo-validation-check.yml
rules:
  - id: cross-repo-allowlist-enforcement
    pattern: |
      All handlers with target-repo support must validate against allowlist
    check: |
      for handler in create_issue.cjs add_comment.cjs; do
        grep -q "validateTargetRepo\|checkAllowedRepos" "actions/setup/js/$handler" || exit 1
      done
    severity: HIGH
```

---

#### **Finding S3: Incomplete Content Sanitization Specification**

**Location**: Section 9 (Content Integrity Mechanisms)

**Issue**: While sanitization is mentioned throughout, the specification does not provide:
1. Exact definition of what "sanitization" entails
2. List of prohibited patterns or characters
3. Handling of markdown-specific exploits (e.g., image injection, HTML in markdown)
4. Whether sanitization is reversible or lossy

**Security Impact**: **MEDIUM** - Inconsistent sanitization implementations could lead to content injection vulnerabilities.

**Recommendation**:
```markdown
Add Sanitization Requirements section:

**Content Sanitization Pipeline**:

Implementations MUST apply these sanitization transforms to all user-provided text fields:

1. **Markdown Link Validation**:
   - Extract all markdown links: `[text](url)`
   - Validate URL against `allowed-domains` if configured
   - Redact unauthorized URLs: `[text]([URL redacted: unauthorized domain])`

2. **HTML Tag Stripping** (for fields that don't support HTML):
   - Remove `<script>`, `<iframe>`, `<object>`, `<embed>` tags
   - Remove `on*` event handlers in HTML attributes
   - Preserve safe markdown-supported HTML (e.g., `<details>`, `<summary>`)

3. **Command Injection Prevention**:
   - Escape shell metacharacters in code blocks
   - Do NOT execute or interpret code blocks

4. **Image URL Validation**:
   - Validate image URLs `![alt](url)` against allowed-domains
   - Redact unauthorized image URLs

5. **Null Byte Handling**:
   - Remove null bytes (\0) from all strings
   - Reject inputs containing null bytes

**Non-Sanitized Fields**: The following fields MUST NOT be sanitized (verbatim):
- Code blocks (```code```)
- URLs in footers added by the system
```

**Automated Rule**:
```yaml
# sanitization-check.yml
rules:
  - id: sanitization-completeness
    pattern: |
      All handlers must import and use sanitization functions
    check: |
      for handler in actions/setup/js/*.cjs; do
        [[ "$handler" =~ (test|parse|buffer) ]] && continue
        grep -q "sanitize\|stripHTML\|validateURL" "$handler" || exit 1
      done
    severity: MEDIUM
```

---

### 1.2 Moderate Security Issues

#### **Finding S4: Unclear Artifact Storage Security Model**

**Location**: Section 3.1, Requirement AR2 (Communication Channel Integrity)

**Issue**: The specification requires artifact storage for communication but does not specify:
1. Access controls on artifacts (who can download?)
2. Artifact retention policies (how long do they persist?)
3. Handling of sensitive data in artifacts
4. Encryption requirements (at rest? in transit?)

**Security Impact**: **LOW-MEDIUM** - Artifacts might contain sensitive operation details accessible to unauthorized parties.

**Recommendation**:
```markdown
Add Artifact Security Requirements:

**Artifact Access Control**:
- Artifacts MUST be scoped to the workflow run (not accessible across runs)
- Artifacts MUST require same permissions as workflow run to download
- Implementations SHOULD delete artifacts after safe output processing completes

**Sensitive Data Handling**:
- Implementations SHOULD NOT include secrets in artifact data
- GitHub Actions secret masking SHOULD be verified before artifact upload
- Implementations MAY encrypt artifact contents (not required but permitted)

**Retention Policy**:
- Artifacts SHOULD be retained for at least 1 day (for debugging)
- Artifacts MUST be deleted after 90 days (GitHub Actions limit)
- Implementations MAY implement shorter retention (e.g., 7 days)
```

---

#### **Finding S5: Missing Rate Limiting Specification**

**Location**: Section 3.2, Threat T4 (Resource Exhaustion)

**Issue**: While max limits are specified, there's no guidance on:
1. Rate limiting across multiple workflow runs
2. Preventing rapid workflow re-triggering
3. Global resource consumption limits (e.g., total issues per day)

**Security Impact**: **LOW** - Within single runs, max limits are effective. Across runs, potential for abuse remains.

**Recommendation**:
```markdown
Add Rate Limiting Guidance:

**Per-Run Limits**: Enforced via `max` configuration (existing)

**Cross-Run Considerations**:
Implementations MAY implement additional safeguards:
- Daily operation caps (e.g., max 100 issues per day per workflow)
- Workflow trigger rate limiting (e.g., max 10 runs per hour)
- Organization-wide quotas

**NOTE**: Cross-run limits are OPTIONAL and not required for conformance.
```

---

### 1.3 Security Best Practices

#### **Finding S6: Consider Adding Security Testing Requirements**

**Recommendation**: Add normative security test requirements to conformance section.

```markdown
Add to Section 2.3:

**Method M5: Adversarial Testing (RECOMMENDED)**

Implementations SHOULD verify security properties under adversarial conditions:

1. **Prompt Injection Scenarios**:
   - Test with malicious inputs attempting to exceed max limits
   - Test with inputs containing command injection attempts
   - Test with inputs attempting cross-repository escalation

2. **Configuration Tampering Attempts**:
   - Test with modified YAML (should fail hash check)
   - Test with invalid configuration (should reject)

3. **Boundary Condition Testing**:
   - Test with exactly `max` operations (should succeed)
   - Test with `max + 1` operations (should reject)
   - Test with empty operations (should handle gracefully)
```

---

## 2. Usability Review Findings

### 2.1 Documentation Clarity Issues

#### **Finding U1: Inconsistent Terminology**

**Issue**: Multiple terms used interchangeably:
- "Agent" vs "AI Agent" vs "Agent Process"
- "Safe Output Type" vs "Operation Type" vs "Handler Type"
- "Validation" vs "Verification" (sometimes overlapping meaning)

**Impact**: **LOW** - Could cause confusion but context usually makes meaning clear.

**Recommendation**: Add terminology section defining canonical terms.

```markdown
## Terminology

**Agent**: The AI-powered process executing in the untrusted context. Uses read-only GitHub token.

**Safe Output Type**: A category of GitHub operation (e.g., create_issue, add_comment). Each type has a corresponding MCP tool and handler.

**Handler**: JavaScript implementation processing operations of a specific safe output type.

**Validation**: Pre-execution checking of operation structure, limits, and content (schema validation, limit enforcement).

**Verification**: Post-execution checking of configuration integrity (hash checking, signature verification).
```

---

#### **Finding U2: Missing Error Code Catalog**

**Issue**: Specification mentions errors but doesn't provide:
1. Complete list of error codes
2. Error message format requirements
3. Guidance on when to use each error type

**Impact**: **MEDIUM** - Inconsistent error handling across implementations.

**Recommendation**:
```markdown
Add Error Code Catalog section:

## Error Codes

All safe output processors MUST use these error codes:

| Code | Name | Description | HTTP Status |
|------|------|-------------|-------------|
| E001 | INVALID_SCHEMA | Operation failed JSON schema validation | 400 |
| E002 | LIMIT_EXCEEDED | Operation count exceeds configured max | 429 |
| E003 | UNAUTHORIZED_DOMAIN | URL contains non-allowlisted domain | 403 |
| E004 | INVALID_TARGET_REPO | target-repo not in allowed-repos | 403 |
| E005 | MISSING_PARENT | parent issue reference not found | 404 |
| E006 | INVALID_LABEL | Label does not exist in repository | 404 |
| E007 | API_ERROR | GitHub API returned error | 502 |
| E008 | SANITIZATION_FAILED | Content sanitization detected unsafe patterns | 422 |

**Error Message Format**:
```json
{
  "error": {
    "code": "E002",
    "message": "Limit exceeded: attempted 5 create_issue operations but max is 3",
    "details": {
      "type": "create_issue",
      "attempted": 5,
      "max": 3
    }
  }
}
```
```

**Automated Rule**:
```yaml
# error-code-check.yml
rules:
  - id: error-code-usage
    pattern: |
      Handlers must use standardized error codes
    check: |
      grep -h "throw.*Error\|return.*error" actions/setup/js/*.cjs | \
      grep -q "E[0-9]{3}" || echo "WARNING: Non-standard error codes found"
    severity: LOW
```

---

#### **Finding U3: Limited Configuration Examples**

**Issue**: Section 5 provides syntax but few complete examples showing:
1. Complex multi-type configurations
2. Common configuration patterns
3. Migration from simple to complex configs

**Impact**: **LOW** - Users can figure it out but examples would help.

**Recommendation**: Add configuration patterns appendix.

```markdown
## Appendix G: Configuration Patterns

### Pattern 1: Simple Issue Tracking
```yaml
safe-outputs:
  create-issue:
    max: 1
    labels: [automated]
```

### Pattern 2: Multi-Type with Global Footer
```yaml
safe-outputs:
  footer: true  # Applied to all types
  
  create-issue:
    max: 3
    labels: [bug, automated]
  
  add-comment:
    max: 2
    hide-older-comments: true
```

### Pattern 3: Cross-Repository Operations
```yaml
safe-outputs:
  allowed-github-references:
    - owner/repo-a
    - owner/repo-b
  
  create-issue:
    max: 5
    target-repo: owner/repo-a
```

### Pattern 4: Staged Mode Development
```yaml
safe-outputs:
  staged: true  # Enable preview mode globally
  
  create-issue:
    max: 10  # Safe to set high in staged mode
  
  add-comment:
    max: 5
```
```

---

### 2.2 Operational Clarity Issues

#### **Finding U4: Unclear Staged Mode Interaction with Other Features**

**Issue**: Section 5.2 (GP2) describes staged mode but doesn't clarify:
1. Does staged mode respect max limits? (preview all operations or only up to max?)
2. Are footers added in staged mode previews?
3. Does sanitization occur in staged mode?

**Impact**: **LOW** - Likely obvious from implementation but could be explicit.

**Recommendation**:
```markdown
Add to Section 5.2 (staged mode):

**Staged Mode Feature Interactions**:

When `staged: true`:
- Max limits: RESPECTED (preview only shows operations up to max limit)
- Footers: INCLUDED in previews (shows what would be added)
- Sanitization: APPLIED (previews show sanitized content)
- Domain filtering: APPLIED (previews show redacted URLs)
- Cross-repo validation: APPLIED (invalid target-repo rejected even in staged mode)

**Rationale**: Staged mode should accurately preview what would happen, including all validation and transformation steps.
```

---

#### **Finding U5: Missing Guidance on Temporary ID Patterns**

**Issue**: Temporary IDs are mentioned (e.g., Section 7.1, create_issue) but specification lacks:
1. Complete list of where temporary IDs can be used
2. Resolution order when multiple operations reference each other
3. Error handling when temporary ID references are circular

**Impact**: **LOW-MEDIUM** - Could lead to implementation inconsistencies.

**Recommendation**:
```markdown
Add Temporary ID Resolution section:

## Temporary ID Resolution

**Pattern**: `^aw_[A-Za-z0-9]{3,8}$` (e.g., `aw_abc123`)

**Supported Operations**:
- create_issue (in `body` field, references resolved to issue numbers)
- add_comment (in `body` field)
- create_pull_request (in `body` field)

**Resolution Order**:
1. Operations are processed in NDJSON order
2. When temporary ID `aw_xxx` is created, map it to actual GitHub resource number
3. Before processing next operation, replace all `#aw_xxx` references with actual numbers
4. If reference to undefined temporary ID is encountered, REJECT operation with E005 error

**Circular References**: NOT SUPPORTED. If detected, reject with error.

**Example**:
```json
{"type": "create_issue", "title": "Parent", "temporary_id": "aw_p1"}
{"type": "create_issue", "title": "Child", "body": "Related to #aw_p1", "parent": "aw_p1"}
```

After processing:
- First issue created with number #123
- Second issue body updated to "Related to #123" and linked to parent #123
```

---

## 3. Requirements Review Findings

### 3.1 RFC 2119 Keyword Usage Issues

#### **Finding R1: Inconsistent MUST Usage**

**Issue**: Some normative requirements use descriptive language instead of RFC 2119 keywords:

Examples:
- Section 4.2: "Operations flow through the system following this precise sequence" (should be MUST?)
- Section 5.1: "Global parameters have unreserved names" (MUST?)
- Section 7.x: Many "When X occurs, Y happens" statements without MUST/SHOULD

**Impact**: **MEDIUM** - Reduces enforceability and automated rule encoding.

**Recommendation**: Audit all sections and convert implicit requirements to explicit MUST/SHOULD statements.

**Automated Rule**:
```yaml
# rfc2119-consistency-check.yml
rules:
  - id: normative-statement-keywords
    pattern: |
      Check that normative statements use RFC 2119 keywords
    check: |
      # Find sentences that describe requirements without keywords
      grep -E "(must|shall|will|is required to|needs to)" \
        docs/src/content/docs/reference/safe-outputs-specification.md | \
        grep -v "MUST\|SHALL\|REQUIRED" || true
    severity: MEDIUM
    action: |
      Review findings and convert to RFC 2119 keywords where appropriate
```

---

#### **Finding R2: Missing SHOULD Justifications**

**Issue**: RFC 2119 states that SHOULD recommendations require rationale, but many SHOULDs lack justification:

Examples:
- Section 2.3, Method M2: "Security test suite SHOULD include..." (why?)
- Section 5.2: "Staged mode is RECOMMENDED for..." (why recommended vs required?)

**Impact**: **LOW** - Makes it harder to understand when to deviate from recommendations.

**Recommendation**: Add rationale for all SHOULD statements.

```markdown
Example:
**BEFORE**:
Security test suite SHOULD include prompt injection scenarios.

**AFTER**:
Security test suite SHOULD include prompt injection scenarios.

*Rationale*: While not strictly required for conformance, prompt injection testing provides critical validation that security mitigations are effective. Implementations without adversarial testing may unknowingly contain vulnerabilities. However, basic conformance can be demonstrated through functional testing alone.
```

---

### 3.2 Testability Issues

#### **Finding R3: Untestable Requirements**

**Issue**: Some requirements are stated but lack clear test criteria:

Examples:
- Section 1.3, Principle P1: "Write permissions MUST reside in separate execution contexts" - How to verify "separate"?
- Section 3.3, Property SP4: "Content MUST undergo sanitization" - How to prove it happened?

**Impact**: **MEDIUM** - Reduces ability to verify conformance.

**Recommendation**: Add verification methods to all requirements.

```markdown
Example for P1:

**Principle P1: Security Through Architectural Separation**

Write permissions MUST reside in separate execution contexts from AI reasoning.

**Verification Methods**:
1. **Static Analysis**: Inspect workflow YAML. Agent job MUST NOT declare write permissions.
2. **Runtime Inspection**: In agent job, `echo $GITHUB_TOKEN` and verify it lacks write scopes using `gh api user --jq .permissions`.
3. **Process Isolation**: Verify agent and safe output jobs run in different containers/VMs (check `$RUNNER_NAME` differs).
```

---

#### **Finding R4: Missing Conformance Test Suite**

**Issue**: Section 2.3 mentions conformance testing but notes "A normative conformance test suite is RECOMMENDED for future specification versions but not currently provided."

**Impact**: **MEDIUM** - Without test suite, conformance is subjective.

**Recommendation**: Create conformance test suite as separate deliverable.

**Proposed Structure**:
```
tests/conformance/
├── security/
│   ├── test_privilege_separation.sh
│   ├── test_validation_ordering.sh
│   ├── test_limit_enforcement.sh
│   └── test_sanitization.sh
├── functionality/
│   ├── test_create_issue.sh
│   ├── test_add_comment.sh
│   ├── test_staged_mode.sh
│   └── test_cross_repo.sh
├── configuration/
│   ├── test_config_parsing.sh
│   ├── test_config_validation.sh
│   └── test_config_inheritance.sh
└── protocol/
    ├── test_mcp_http.sh
    ├── test_ndjson_format.sh
    └── test_error_responses.sh
```

Each test should:
1. State requirement being tested (with spec section reference)
2. Provide test setup instructions
3. Define pass/fail criteria
4. Include expected output examples

---

### 3.3 Completeness Issues

#### **Finding R5: Missing Safe Output Type Requirements**

**Issue**: Section 7 documents 36 safe output types, but not all have complete specifications. Some are missing:
- **Required Permissions** documentation
- **Operational Semantics** details
- **Configuration Parameters** lists
- **Security Requirements**

**Impact**: **MEDIUM** - Incomplete type definitions reduce implementation consistency.

**Recommendation**: Audit all 36 types and ensure each has:
1. ✅ MCP Tool Schema
2. ✅ Operational Semantics (5+ bullets)
3. ✅ Configuration Parameters (complete list)
4. ✅ Security Requirements (at least 2)
5. ✅ Required Permissions (both token and app)
6. ✅ Example usage

**Automated Rule**:
```yaml
# safe-output-type-completeness-check.yml
rules:
  - id: type-definition-completeness
    pattern: |
      Each safe output type must have complete documentation
    check: |
      # Extract all "#### Type: " headers
      grep "^#### Type:" docs/src/content/docs/reference/safe-outputs-specification.md | \
      while read -r line; do
        type_name=$(echo "$line" | sed 's/^#### Type: //')
        
        # Check for required sections
        has_schema=$(grep -A 100 "^#### Type: $type_name" spec.md | grep -q "MCP Tool Schema" && echo "yes" || echo "no")
        has_semantics=$(grep -A 100 "^#### Type: $type_name" spec.md | grep -q "Operational Semantics" && echo "yes" || echo "no")
        has_config=$(grep -A 100 "^#### Type: $type_name" spec.md | grep -q "Configuration Parameters" && echo "yes" || echo "no")
        has_security=$(grep -A 100 "^#### Type: $type_name" spec.md | grep -q "Security Requirements" && echo "yes" || echo "no")
        has_permissions=$(grep -A 100 "^#### Type: $type_name" spec.md | grep -q "Required Permissions" && echo "yes" || echo "no")
        
        if [[ "$has_schema" != "yes" ]] || [[ "$has_semantics" != "yes" ]] || \
           [[ "$has_config" != "yes" ]] || [[ "$has_security" != "yes" ]] || \
           [[ "$has_permissions" != "yes" ]]; then
          echo "INCOMPLETE: $type_name (schema=$has_schema, semantics=$has_semantics, config=$has_config, security=$has_security, permissions=$has_permissions)"
        fi
      done
    severity: MEDIUM
```

---

#### **Finding R6: Undefined Behavior for Edge Cases**

**Issue**: Specification doesn't address several edge cases:
1. What happens when max=0? (disable type entirely? or error?)
2. What happens when empty NDJSON artifact is uploaded?
3. What happens when GitHub API rate limit is exceeded?
4. What happens when workflow is cancelled mid-execution?

**Impact**: **LOW** - Edge cases are rare but should be specified.

**Recommendation**: Add edge case handling section.

```markdown
## Edge Case Behavior

### Empty Operations
When NDJSON artifact contains zero operations:
- Safe output job MUST succeed (not an error)
- Job summary SHOULD display "No operations to process"

### Zero Max Limit
When `max: 0` is configured for a type:
- Type is DISABLED (MCP tool not registered)
- Attempts to invoke disabled type MUST return error

### API Rate Limiting
When GitHub API rate limit is exceeded:
- Safe output processor MUST retry with exponential backoff
- After 3 retries, MUST fail with clear error message
- MUST log rate limit reset time

### Workflow Cancellation
When workflow is cancelled during agent execution:
- Safe output job MUST NOT execute
- Partial NDJSON artifact MUST NOT be processed
- No GitHub resources MUST be created

### Concurrent Workflow Runs
When multiple runs execute concurrently:
- Each run operates independently
- Max limits are per-run, not global
- No coordination between runs (no distributed locking)
```

---

## 4. Automated Checker Rules

Based on the findings above, here is a comprehensive set of automated rules that can be encoded:

### 4.1 Security Rules

```yaml
# security-checks.yml
rules:
  - id: SEC-001
    name: Privilege Separation Enforcement
    description: Verify agent jobs lack write permissions
    check: |
      grep -A 50 "jobs:" compiled-workflow.lock.yml | \
      grep -A 10 "agent:" | \
      grep -E "issues:.*write|pull-requests:.*write" && exit 1 || exit 0
    severity: CRITICAL
    remediation: Remove write permissions from agent job

  - id: SEC-002
    name: Validation Before API Calls
    description: Ensure validation happens before GitHub API calls
    check: |
      for handler in actions/setup/js/*.cjs; do
        [[ "$handler" =~ test ]] && continue
        # Check that validation functions appear before API calls
        awk '/octokit\./{line=NR} /validate|sanitize/{if(NR<line) valid=1} END{exit !valid}' "$handler" || {
          echo "FAIL: $handler has API calls before validation"
          exit 1
        }
      done
    severity: CRITICAL
    remediation: Reorder code to validate before API calls

  - id: SEC-003
    name: Max Limit Enforcement
    description: Verify max limits are checked before processing
    check: |
      for handler in actions/setup/js/*.cjs; do
        [[ "$handler" =~ test ]] && continue
        grep -q "\.length.*>.*\.max\|enforceMaxLimit\|checkLimit" "$handler" || {
          echo "WARN: $handler may not enforce max limits"
        }
      done
    severity: HIGH
    remediation: Add max limit check before processing operations

  - id: SEC-004
    name: Content Sanitization Required
    description: Verify content sanitization is applied
    check: |
      for handler in actions/setup/js/*.cjs; do
        [[ "$handler" =~ (test|parse|buffer) ]] && continue
        grep -q "sanitize\|stripHTML\|escapeMarkdown" "$handler" || {
          echo "WARN: $handler may not sanitize content"
        }
      done
    severity: MEDIUM
    remediation: Import and use sanitization functions

  - id: SEC-005
    name: Cross-Repository Validation
    description: Verify cross-repo operations check allowlist
    check: |
      for handler in actions/setup/js/*.cjs; do
        grep -q "target.*repo\|targetRepo" "$handler" && {
          grep -q "allowed.*repos\|validateTargetRepo" "$handler" || {
            echo "FAIL: $handler has target-repo but no allowlist check"
            exit 1
          }
        }
      done
    severity: HIGH
    remediation: Add allowlist validation for cross-repo operations
```

### 4.2 Usability Rules

```yaml
# usability-checks.yml
rules:
  - id: USE-001
    name: Error Code Standardization
    description: Verify handlers use standardized error codes
    check: |
      for handler in actions/setup/js/*.cjs; do
        [[ "$handler" =~ test ]] && continue
        # Check for error codes in E### format
        grep -E "throw.*Error|return.*error" "$handler" | \
        grep -qE "E[0-9]{3}" || {
          echo "WARN: $handler may not use standard error codes"
        }
      done
    severity: LOW
    remediation: Use standard E### error codes

  - id: USE-002
    name: Footer Attribution Required
    description: Verify footers are added when footer:true
    check: |
      for handler in actions/setup/js/*.cjs; do
        grep -q "footer.*true\|addFooter\|attribution" "$handler" || {
          echo "INFO: $handler may not add footers"
        }
      done
    severity: LOW
    remediation: Ensure footer is appended when configured

  - id: USE-003
    name: Staged Mode Preview Format
    description: Verify staged mode uses correct preview format
    check: |
      for handler in actions/setup/js/*.cjs; do
        grep -q "staged.*true\|isStaged" "$handler" && {
          grep -q "🎭\|Staged Mode" "$handler" || {
            echo "WARN: $handler has staged mode but no 🎭 emoji"
          }
        }
      done
    severity: LOW
    remediation: Use 🎭 emoji in staged mode previews
```

### 4.3 Requirements Rules

```yaml
# requirements-checks.yml
rules:
  - id: REQ-001
    name: RFC 2119 Keyword Usage
    description: Verify normative sections use MUST/SHOULD/MAY
    check: |
      # Check sections that should have normative statements
      for section in "Security Architecture" "Configuration Semantics" "Execution Guarantees"; do
        grep -A 200 "## .*$section" docs/src/content/docs/reference/safe-outputs-specification.md | \
        grep -q "MUST\|SHALL\|SHOULD\|MAY" || {
          echo "WARN: Section '$section' may lack RFC 2119 keywords"
        }
      done
    severity: MEDIUM
    remediation: Add MUST/SHOULD/MAY keywords to normative statements

  - id: REQ-002
    name: Safe Output Type Completeness
    description: Verify each type has all required documentation sections
    check: |
      # Extract type names and check for required sections
      grep "^#### Type:" docs/src/content/docs/reference/safe-outputs-specification.md | \
      sed 's/^#### Type: //' | while read -r type; do
        sections_found=0
        for section in "MCP Tool Schema" "Operational Semantics" "Configuration Parameters" "Security Requirements" "Required Permissions"; do
          grep -A 150 "^#### Type: $type" docs/src/content/docs/reference/safe-outputs-specification.md | \
          grep -q "**$section**" && ((sections_found++))
        done
        if [ $sections_found -lt 5 ]; then
          echo "INCOMPLETE: Type '$type' has only $sections_found/5 required sections"
        fi
      done
    severity: MEDIUM
    remediation: Complete documentation for all safe output types

  - id: REQ-003
    name: Verification Method Specification
    description: Verify requirements include verification methods
    check: |
      # Check that key requirements have verification guidance
      for req in "AR1" "AR2" "AR3" "SP1" "SP2" "SP3"; do
        grep -A 30 "**Requirement $req:\|**Property $req:" docs/src/content/docs/reference/safe-outputs-specification.md | \
        grep -q "Verification:\|Formal Definition:" || {
          echo "WARN: Requirement $req may lack verification method"
        }
      done
    severity: LOW
    remediation: Add verification methods to all key requirements
```

### 4.4 Implementation Rules

```yaml
# implementation-checks.yml
rules:
  - id: IMP-001
    name: Handler Registration Completeness
    description: Verify all configured types have handlers
    check: |
      # Extract safe output types from config
      configured_types=$(jq -r '.tools | keys[]' /opt/gh-aw/safeoutputs/config.json 2>/dev/null || echo "")
      
      # Check each has handler file
      for type in $configured_types; do
        handler_file="actions/setup/js/$(echo $type | tr '_' '-').cjs"
        [ ! -f "$handler_file" ] && {
          echo "MISSING: Handler file $handler_file for type $type"
          exit 1
        }
      done
    severity: HIGH
    remediation: Create handler file for configured type

  - id: IMP-002
    name: Permission Computation Accuracy
    description: Verify computed permissions match type requirements
    check: |
      # Compare permissions in workflow vs required permissions
      go test -run TestPermissionsComputation ./pkg/workflow/ || {
        echo "FAIL: Permission computation tests failed"
        exit 1
      }
    severity: HIGH
    remediation: Fix permission computation logic

  - id: IMP-003
    name: Schema Validation Consistency
    description: Verify schemas match specification
    check: |
      # For each type, compare generated schema to spec
      for type in create_issue add_comment; do
        # Extract schema from spec
        spec_required=$(grep -A 20 "\"$type\"" docs/src/content/docs/reference/safe-outputs-specification.md | \
                       grep "\"required\":" | head -1)
        
        # Compare to generated config
        config_required=$(jq -r ".tools.${type}.inputSchema.required" /opt/gh-aw/safeoutputs/config.json 2>/dev/null)
        
        # Simplified check (actual implementation would be more thorough)
        [ -z "$config_required" ] && echo "WARN: Type $type schema may not match spec"
      done
    severity: MEDIUM
    remediation: Update schema generation to match specification
```

---

## 5. Recommendations Summary

### 5.1 High Priority (Must Address)

1. **[S2] Clarify Cross-Repository Security Model** - Add explicit validation rules and precedence order for allowlists
2. **[R1] Audit and Fix RFC 2119 Keyword Usage** - Ensure all normative statements use MUST/SHOULD/MAY
3. **[R3] Add Verification Methods to Requirements** - Make all requirements testable
4. **[R4] Create Conformance Test Suite** - Provide normative tests for implementers

### 5.2 Medium Priority (Should Address)

1. **[S1] Specify Validation Pipeline Ordering** - Eliminate ambiguity in validation sequence
2. **[S3] Complete Content Sanitization Specification** - Define exact sanitization rules
3. **[U2] Add Error Code Catalog** - Standardize error codes and messages
4. **[R5] Complete Safe Output Type Documentation** - Ensure all 36 types have full specifications
5. **[R6] Define Edge Case Behavior** - Specify handling of unusual conditions

### 5.3 Low Priority (Nice to Have)

1. **[U1] Standardize Terminology** - Add glossary for consistency
2. **[U3] Add Configuration Examples** - Provide more real-world examples
3. **[U4] Clarify Staged Mode Interactions** - Document how staged mode affects other features
4. **[U5] Add Temporary ID Resolution Guidance** - Complete specification of temporary ID handling
5. **[S5] Add Rate Limiting Guidance** - Provide optional cross-run limit recommendations

---

## 6. Implementation Roadmap

### Phase 1: Critical Security Clarifications (Week 1-2)
- [ ] Add validation pipeline ordering specification (S1)
- [ ] Complete cross-repository security model (S2)
- [ ] Add content sanitization requirements (S3)
- [ ] Implement automated security checks (SEC-001 through SEC-005)

### Phase 2: Requirements and Testability (Week 3-4)
- [ ] Audit and fix RFC 2119 keyword usage (R1)
- [ ] Add verification methods to all requirements (R3)
- [ ] Begin conformance test suite development (R4)
- [ ] Implement requirements checks (REQ-001 through REQ-003)

### Phase 3: Usability Improvements (Week 5-6)
- [ ] Add error code catalog (U2)
- [ ] Create configuration examples appendix (U3)
- [ ] Add terminology section (U1)
- [ ] Document staged mode interactions (U4)
- [ ] Implement usability checks (USE-001 through USE-003)

### Phase 4: Completeness (Week 7-8)
- [ ] Complete all 36 safe output type specifications (R5)
- [ ] Add edge case behavior definitions (R6)
- [ ] Add temporary ID resolution section (U5)
- [ ] Complete conformance test suite (R4)
- [ ] Implement implementation checks (IMP-001 through IMP-003)

---

## 7. Conclusion

The Safe Outputs MCP Gateway Specification provides a strong foundation for secure AI-to-GitHub operations with clear architectural separation and comprehensive threat mitigation strategies. The specification follows W3C conventions and uses RFC 2119 keywords appropriately in most sections.

**Key achievements**:
- ✅ Well-defined security architecture
- ✅ Comprehensive threat model
- ✅ Detailed safe output type catalog
- ✅ Clear permission requirements

**Areas for improvement**:
- Clarify ambiguous security requirements (validation ordering, cross-repo rules)
- Enhance testability with verification methods
- Improve usability through standardized errors and examples
- Complete documentation for all safe output types

**Next steps**:
1. Prioritize and address high-priority findings
2. Develop conformance test suite
3. Implement automated checker rules
4. Iterate on specification based on implementation feedback

---

**Document Version**: 1.0  
**Last Updated**: 2026-02-14  
**Status**: Initial Review Complete
