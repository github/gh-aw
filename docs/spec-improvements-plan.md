# Safe Outputs Specification Improvements

This document outlines specific changes to be made to the Safe Outputs specification based on the security, usability, and requirements review.

## Priority 1: Critical Security Clarifications

### 1.1 Add Validation Pipeline Ordering (Section 3.3)

**Location**: After Property SP2 in Section 3.3

**Add new subsection**:

```markdown
#### Validation Pipeline Requirements

Implementations MUST execute validation steps in this exact sequence for all safe output operations:

**Stage 1: Schema Validation (REQUIRED)**
- Input: Raw MCP tool arguments
- Check: JSON schema validation against type-specific schema
- On failure: Reject immediately with E001 (INVALID_SCHEMA) error
- Output: Schema-validated operation data

**Stage 2: Limit Enforcement (REQUIRED)**
- Input: Count of operations of each type in current batch
- Check: Compare count against configured `max` for each type
- On failure: Reject entire batch with E002 (LIMIT_EXCEEDED) error
- Output: Limit-validated operation set

**Stage 3: Content Sanitization (REQUIRED)**
- Input: All text fields (title, body, description, etc.)
- Transform: Apply sanitization pipeline (see Section 9.2)
- On failure: Reject with E008 (SANITIZATION_FAILED) if unsafe content cannot be sanitized
- Output: Sanitized operation data

**Stage 4: Domain Filtering (CONDITIONAL)**
- Input: All URLs in markdown links and images
- Check: Validate against `allowed-domains` if configured
- Transform: Redact unauthorized URLs
- Output: Domain-filtered operation data

**Stage 5: Cross-Repository Validation (CONDITIONAL)**
- Input: `target-repo` parameter if present
- Check: Validate against `allowed-repos` or `allowed-github-references`
- On failure: Reject with E004 (INVALID_TARGET_REPO)
- Output: Authorized target repository

**Stage 6: Dependency Resolution (CONDITIONAL)**
- Input: Temporary IDs, parent references
- Check: Resolve references to actual GitHub resource numbers
- On failure: Reject with E005 (MISSING_PARENT)
- Output: Fully-resolved operation data

**Stage 7: GitHub API Invocation (EXECUTION)**
- Input: Validated, sanitized, authorized operation data
- Action: Execute GitHub API calls
- On failure: Return E007 (API_ERROR) with details

**Requirement VL1: Sequential Execution**

Stages MUST execute in the order specified above. A failure at any stage (1-6) MUST prevent Stage 7 from executing for that operation.

**Requirement VL2: Atomic Validation**

For single-operation types (max=1), validation failure MUST prevent any API calls. For batch operations, validation failure of one operation MUST NOT cause rejection of the entire batch unless it's a limit enforcement failure.

**Requirement VL3: Error Propagation**

Validation errors MUST include:
- Error code (E001-E008)
- Human-readable message
- Operation index (for batch operations)
- Field name (for schema validation errors)
```

### 1.2 Complete Cross-Repository Security Model (Section 3.2)

**Location**: After Threat T5 in Section 3.2

**Add new subsection**:

```markdown
### 3.2.6 Cross-Repository Security Model

**Repository Reference Format**

Target repositories MUST be specified in `owner/repo` format. Implementations MUST validate:
- Format matches regex: `^[a-zA-Z0-9_.-]+/[a-zA-Z0-9_.-]+$`
- Owner and repo components are non-empty
- No protocol prefix (https://, git://, etc.)

**Allowlist Resolution Order**

When evaluating cross-repository operations, implementations MUST apply these rules in order:

1. **Extract target-repo**: Parse from operation arguments or configuration
2. **Check type-specific allowlist**: If safe output type defines `allowed-repos`:
   - MUST match against this list
   - Type-specific allowlist OVERRIDES global allowlist
   - If match fails, REJECT with E004
3. **Check global allowlist**: If no type-specific allowlist and `allowed-github-references` is defined:
   - MUST match against this list
   - If match fails, REJECT with E004
4. **Default deny**: If no allowlists are defined:
   - MUST reject cross-repository operations
   - Same-repository operations are permitted

**Matching Rules**

- Matching is EXACT (case-sensitive)
- Wildcards (*, ?) are NOT supported
- Pattern matching is NOT supported
- Each repository MUST be explicitly listed

**Security Properties**

**Property SP6: Cross-Repository Containment**

For all cross-repository operations:
```
∀ op ∈ operations:
  op.target_repo ≠ null ⇒ 
    (op.target_repo ∈ type_allowlist ∨ 
     (type_allowlist = null ∧ op.target_repo ∈ global_allowlist))
```

**Property SP7: Deny-by-Default**

Without explicit allowlist configuration:
```
allowed_repos = null ∧ allowed_github_references = null ⇒
  ∀ op ∈ operations: op.target_repo = workflow.repository
```

**Example Configurations**

```yaml
# Example 1: Type-specific allowlist (overrides global)
safe-outputs:
  allowed-github-references: [owner/repo-a, owner/repo-b]
  
  create-issue:
    allowed-repos: [owner/repo-c]  # Only repo-c permitted for issues
    
  add-comment:
    # No type-specific list, uses global: repo-a, repo-b

# Example 2: Explicit same-repository only
safe-outputs:
  create-issue:
    # No allowlist = same repository only
    max: 5
```
```

### 1.3 Define Content Sanitization Pipeline (Section 9)

**Location**: Add new Section 9.2

**Add**:

```markdown
## 9.2 Content Sanitization Pipeline

**Applicability**

Content sanitization MUST be applied to all user-provided text fields in safe output operations. Text fields include:
- `title` (issues, PRs, discussions, projects)
- `body` (issues, PRs, discussions, comments)
- `description` (projects, status updates)
- `comment` (review comments)

**Sanitization Stages**

Implementations MUST apply these transformations in order:

**S1: Null Byte Removal**
- Remove all null bytes (`\0`, `\x00`) from strings
- Rationale: Prevents string truncation attacks

**S2: Markdown Link Validation**
- Pattern: `[text](url)` and `<url>`
- For each URL:
  - Extract domain
  - If `allowed-domains` is configured:
    - Check domain against allowlist
    - If not allowed: Replace with `[text]([URL redacted: unauthorized domain])`
  - Log redacted URLs to `/tmp/gh-aw/safeoutputs/redacted-domains.log`

**S3: Markdown Image Validation**
- Pattern: `![alt](url)`
- For each image URL:
  - Extract domain
  - If `allowed-domains` is configured:
    - Check domain against allowlist
    - If not allowed: Replace with `![alt]([Image URL redacted: unauthorized domain])`

**S4: HTML Tag Filtering** (Optional, depends on field type)
- Remove potentially dangerous tags:
  - `<script>`, `</script>`
  - `<iframe>`, `</iframe>`
  - `<object>`, `</object>`
  - `<embed>`, `</embed>`
- Remove event handlers:
  - `on*` attributes in HTML tags (onclick, onerror, etc.)
- Preserve safe GitHub Flavored Markdown tags:
  - `<details>`, `<summary>`, `<sub>`, `<sup>`, `<kbd>`

**S5: Command Injection Prevention**
- Do NOT execute or interpret code blocks
- Do NOT evaluate template expressions
- Preserve code blocks verbatim (no escaping needed in markdown)

**Excluded Content**

The following content MUST NOT be sanitized:
- Code blocks (` ``` `)
- Inline code (`` `code` ``)
- System-generated footers
- System-generated metadata

**Sanitization Reversibility**

Sanitization transformations are LOSSY and NOT reversible. Original content is not preserved after sanitization. This is intentional to prevent attempts to bypass sanitization.

**Conformance Requirement CR1: Pre-API Sanitization**

All content MUST be sanitized BEFORE GitHub API invocation. Unsanitized content MUST NEVER be passed to GitHub APIs.

**Verification**: Inspect handler code to confirm sanitization occurs before `octokit.*` calls.
```

## Priority 2: Requirements and Testability

### 2.1 Add Terminology Section (After Abstract)

**Location**: Add new section before Table of Contents

**Add**:

```markdown
## Terminology

This specification uses the following terms with precise definitions:

**Agent**: The AI-powered process executing in an untrusted context with read-only GitHub permissions. Synonyms: AI Agent, Agent Process.

**Safe Output Type**: A category of GitHub operation (e.g., `create_issue`, `add_comment`) with a corresponding MCP tool definition and handler implementation. Synonyms: Operation Type, Handler Type.

**MCP Gateway**: The HTTP server accepting MCP tool invocation requests and recording operations to NDJSON format. Runs in the same context as the agent.

**Safe Output Processor**: The privileged execution context that reads NDJSON artifacts, validates operations, and executes GitHub API calls. Synonyms: Handler, Processor.

**Handler**: JavaScript implementation processing operations of a specific safe output type.

**Validation**: Pre-execution verification of operation structure, limits, and authorization. Includes schema validation, limit enforcement, and allowlist checking.

**Sanitization**: Content transformation pipeline removing potentially malicious patterns while preserving legitimate content.

**Verification**: Post-compilation checking of configuration integrity through hash validation.

**Staged Mode**: Preview execution mode where operations are simulated without creating permanent GitHub resources.

**Temporary ID**: A placeholder identifier (format: `aw_<id>`) used to reference not-yet-created resources. Resolved to actual resource numbers during processing.

**Provenance**: Metadata identifying the workflow and run that created a GitHub resource. Included in footers or API metadata fields.
```

### 2.2 Add Error Code Catalog (After Section 9)

**Location**: Add new Section 9.3

**Add**:

```markdown
## 9.3 Error Code Catalog

Implementations MUST use standardized error codes for validation and execution failures.

### Error Code Table

| Code | Name | Description | When to Use | HTTP Status Equivalent |
|------|------|-------------|-------------|------------------------|
| E001 | INVALID_SCHEMA | Operation failed JSON schema validation | Input does not match type-specific schema | 400 Bad Request |
| E002 | LIMIT_EXCEEDED | Operation count exceeds configured max | Batch contains more operations than allowed | 429 Too Many Requests |
| E003 | UNAUTHORIZED_DOMAIN | URL contains non-allowlisted domain | Domain filtering rejected URL | 403 Forbidden |
| E004 | INVALID_TARGET_REPO | target-repo not in allowed-repos | Cross-repository validation failed | 403 Forbidden |
| E005 | MISSING_PARENT | Referenced parent issue/PR not found | Temporary ID or parent reference cannot be resolved | 404 Not Found |
| E006 | INVALID_LABEL | Label does not exist in repository | Label validation failed | 404 Not Found |
| E007 | API_ERROR | GitHub API returned error | GitHub API call failed | 502 Bad Gateway |
| E008 | SANITIZATION_FAILED | Content contains unsanitizable unsafe patterns | Sanitization pipeline detected unremovable threats | 422 Unprocessable Entity |
| E009 | CONFIG_HASH_MISMATCH | Configuration hash verification failed | Workflow YAML was modified after compilation | 403 Forbidden |
| E010 | RATE_LIMIT_EXCEEDED | GitHub API rate limit exceeded | Too many API calls | 429 Too Many Requests |

### Error Message Format

All errors MUST conform to this JSON structure:

```json
{
  "error": {
    "code": "E002",
    "name": "LIMIT_EXCEEDED",
    "message": "Operation count exceeds configured limit",
    "details": {
      "type": "create_issue",
      "attempted": 5,
      "max": 3,
      "operation_index": 3
    },
    "timestamp": "2026-02-14T16:39:20.948Z",
    "workflow_run": "https://github.com/owner/repo/actions/runs/12345"
  }
}
```

**Required Fields**:
- `code`: Error code from table above (E001-E010)
- `name`: Error name from table above
- `message`: Human-readable description
- `timestamp`: ISO 8601 timestamp

**Optional Fields**:
- `details`: Type-specific error context (operation_index, field names, etc.)
- `workflow_run`: URL to workflow run for provenance

### Error Handling Requirements

**Requirement EH1: Early Failure Detection**

Validation errors (E001-E006) MUST be detected before any GitHub API calls are made.

**Requirement EH2: Clear Error Messages**

Error messages MUST:
- Clearly state what went wrong
- Include enough context to debug (field names, values)
- Suggest remediation when possible

**Requirement EH3: Error Logging**

All errors MUST be logged to:
- GitHub Actions step output (visible in workflow run)
- Job summary (visible in workflow run summary)
- STDERR (for local development)
```

### 2.3 Add Edge Case Behavior (After Section 10)

**Location**: Add new Section 10.1

**Add**:

```markdown
## 10.1 Edge Case Behavior

This section defines required behavior for unusual or boundary conditions.

### Empty Operations

**Scenario**: NDJSON artifact contains zero operations

**Behavior**:
- Safe output job MUST succeed (exit code 0)
- Job summary SHOULD display: "✅ No operations to process"
- No GitHub API calls are made
- No errors are raised

**Rationale**: Empty operations are valid (agent may determine no action is needed).

### Zero Max Limit

**Scenario**: Configuration specifies `max: 0` for a safe output type

**Behavior**:
- Type is DISABLED (MCP tool is not registered)
- Attempts to invoke disabled type MUST return MCP error:
  ```json
  {"error": {"code": -32601, "message": "Method not found"}}
  ```
- No configuration is generated for disabled types

**Rationale**: `max: 0` is an explicit disable signal.

### API Rate Limiting

**Scenario**: GitHub API returns 429 (rate limit exceeded) or 403 with X-RateLimit-Remaining: 0

**Behavior**:
- Processor MUST retry with exponential backoff:
  - 1st retry: After 60 seconds
  - 2nd retry: After 120 seconds  
  - 3rd retry: After 240 seconds
- After 3 retries, MUST fail with E010 error
- Error details MUST include rate limit reset time from `X-RateLimit-Reset` header

**Rationale**: Transient rate limits should not fail workflows unnecessarily.

### Workflow Cancellation

**Scenario**: Workflow is manually cancelled during agent execution

**Behavior**:
- Safe output job MUST NOT execute if artifact upload was interrupted
- Partial NDJSON artifacts MUST NOT be processed
- GitHub Actions automatically handles cleanup
- No additional logic required in handlers

**Rationale**: GitHub Actions cancellation is handled at platform level.

### Concurrent Workflow Runs

**Scenario**: Multiple workflow runs execute concurrently for the same workflow

**Behavior**:
- Each run operates independently
- Max limits are per-run (NOT global across runs)
- No coordination or locking between runs
- Operations in separate runs do NOT affect each other's limits

**Rationale**: Simplicity and avoiding distributed coordination complexity.

### Malformed NDJSON

**Scenario**: NDJSON artifact contains invalid JSON on one or more lines

**Behavior**:
- Parser MUST skip invalid lines with warning
- Valid lines MUST be processed
- Job summary MUST show: "⚠️ Skipped N malformed entries"
- Invalid lines MUST be logged to STDERR

**Rationale**: Partial failure should not prevent valid operations from executing.

### Missing Artifact

**Scenario**: Safe output job cannot download artifact (artifact not found)

**Behavior**:
- Job MUST fail with clear error message
- Error MUST suggest checking agent job completion
- Exit code MUST be non-zero

**Rationale**: Missing artifact indicates upstream failure that must be addressed.

### Duplicate Temporary IDs

**Scenario**: Multiple operations use the same `temporary_id`

**Behavior**:
- First operation using the ID succeeds and establishes mapping
- Subsequent operations using the same ID MUST reference the first operation's result
- If this creates ambiguity (e.g., two issues both want to be "aw_parent"), MUST reject with E005

**Rationale**: Deterministic behavior prevents confusion.
```

## Priority 3: Usability Improvements

### 3.1 Add Configuration Examples (New Appendix)

**Location**: Add new Appendix G after Appendix F

**Add**: *(See full appendix in main findings document)*

### 3.2 Clarify Staged Mode Interactions (Section 5.2)

**Location**: At end of Section 5.2 (GP2: staged)

**Add**:

```markdown
### Staged Mode Feature Interactions

When `staged: true` is configured, implementations MUST apply the following behavior:

**Max Limits**: RESPECTED
- Preview shows only operations up to configured max limit
- Exceeding max limit MUST be detected and reported even in staged mode
- Rationale: Preview should accurately show what would happen

**Footers**: INCLUDED
- Preview content MUST include footers as they would appear
- Rationale: Preview must show complete final content

**Sanitization**: APPLIED
- All content MUST undergo sanitization pipeline
- Preview shows sanitized content, not original
- Redacted URLs shown as `[URL redacted: unauthorized domain]`
- Rationale: Preview must show actual processed content

**Domain Filtering**: APPLIED
- `allowed-domains` restrictions enforced in preview
- Unauthorized domains redacted in preview output
- Rationale: Validation rules should be testable in staged mode

**Cross-Repository Validation**: APPLIED
- Invalid `target-repo` values MUST be rejected even in staged mode
- Allowlist checking occurs normally
- Rationale: Authorization errors should be caught in preview

**GitHub API Calls**: SKIPPED
- No resources created on GitHub
- No GitHub API calls made (neither read nor write)
- Rationale: Staged mode must have zero side effects

**Preview Output Format**: STANDARDIZED
- All previews MUST use 🎭 emoji prefix
- All previews MUST state "No resources were created"
- All previews MUST show operation count
```

## Implementation Checklist

### Phase 1: Specification Updates (Week 1)
- [ ] Add validation pipeline ordering section (1.1)
- [ ] Complete cross-repository security model (1.2)  
- [ ] Define content sanitization pipeline (1.3)
- [ ] Add terminology section (2.1)
- [ ] Add error code catalog (2.2)
- [ ] Add edge case behavior section (2.3)
- [ ] Clarify staged mode interactions (3.2)
- [ ] Add configuration examples appendix (3.1)

### Phase 2: Automated Checker Integration (Week 2)
- [ ] Test conformance checker script
- [ ] Add conformance check to CI pipeline
- [ ] Document how to run conformance checks locally
- [ ] Create conformance badge for README

### Phase 3: Implementation Updates (Week 3-4)
- [ ] Update handlers to match validation pipeline order
- [ ] Implement standardized error codes
- [ ] Add edge case handling
- [ ] Update tests to cover new requirements

### Phase 4: Documentation (Week 4)
- [ ] Update README with conformance information
- [ ] Create implementation guide
- [ ] Add migration guide for existing workflows
- [ ] Document conformance testing process

## Success Criteria

- [ ] All specification findings addressed in updated specification
- [ ] Automated conformance checker passes on current implementation
- [ ] All RFC 2119 keywords used correctly and consistently
- [ ] All safe output types have complete documentation
- [ ] All requirements have verification methods
- [ ] Edge cases are well-defined
- [ ] Error codes are standardized and documented
