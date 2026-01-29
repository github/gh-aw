# Security Review: Template Injection Findings

## Executive Summary

Reviewed 2 zizmor template injection findings in GitHub Actions workflows. Both findings are **FALSE POSITIVES** with no actual security risk. However, Finding 2 reveals a potential bug with undefined environment variables.

**Status**: ✅ No security vulnerabilities found  
**Actions Required**: Fix undefined environment variables in Finding 2

---

## Finding 1: copilot-session-insights.lock.yml:204

### Location
- **File**: `.github/workflows/copilot-session-insights.lock.yml`
- **Line**: 204
- **Severity**: Informational (zizmor)

### Template Expansion
```yaml
- continue-on-error: true
  env:
    GH_TOKEN: ${{ secrets.COPILOT_GITHUB_TOKEN || secrets.GH_AW_GITHUB_TOKEN }}
  id: download-sessions
  name: List and download Copilot agent sessions
  run: |
    ...
    echo "::warning::Extension installation status from previous step: ${{ steps.install-extension.outputs.EXTENSION_INSTALLED }}"
    ...
```text

### Data Flow Analysis

```mermaid
graph LR
    A[Install gh Extension Step] --> B[Shell Conditional]
    B --> C{Extension Check}
    C -->|Success| D["echo 'EXTENSION_INSTALLED=true'"]
    C -->|Failure| E["echo 'EXTENSION_INSTALLED=false'"]
    D --> F[GITHUB_OUTPUT]
    E --> F
    F --> G[steps.install-extension.outputs.EXTENSION_INSTALLED]
    G --> H[Template Expansion in Warning]

    style D fill:#90EE90
    style E fill:#FFB6C1
    style F fill:#87CEEB
    style H fill:#FFD700
```text

1. **Source**: `${{ steps.install-extension.outputs.EXTENSION_INSTALLED }}`
2. **Origin**: Set by the "Install gh agent-task extension" step (lines 198-203)
3. **Value Assignment**:
   ```bash
   echo "EXTENSION_INSTALLED=true" >> "$GITHUB_OUTPUT"
   # OR
   echo "EXTENSION_INSTALLED=false" >> "$GITHUB_OUTPUT"
   ```
4. **Control**: Hardcoded boolean values within workflow-controlled script
5. **User Input**: None - values are deterministically set by shell conditionals

### Security Assessment

**Verdict**: ✅ **FALSE POSITIVE - NO SECURITY RISK**

**Rationale**:
- The template expansion references a step output that is set by the workflow itself
- The value is always one of two hardcoded strings: "true" or "false"
- No user-controlled data flows into this template expansion
- The step runs in a controlled environment with no external input
- Even if an attacker could somehow influence the gh CLI output, the value is constrained to boolean strings and only used in an echo statement

**Risk Level**: None

---

## Finding 2: mcp-inspector.lock.yml:1129

### Location
- **File**: `.github/workflows/mcp-inspector.lock.yml`
- **Line**: 1129
- **Severity**: Low (zizmor)

### Template Expansions
```yaml
- name: Setup MCPs
  env:
    GITHUB_MCP_SERVER_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}
    GH_AW_SAFE_OUTPUTS: ${{ env.GH_AW_SAFE_OUTPUTS }}
    GH_AW_ASSETS_BRANCH: ${{ env.GH_AW_ASSETS_BRANCH }}
    GH_AW_ASSETS_MAX_SIZE_KB: ${{ env.GH_AW_ASSETS_MAX_SIZE_KB }}
    GH_AW_ASSETS_ALLOWED_EXTS: ${{ env.GH_AW_ASSETS_ALLOWED_EXTS }}
```text

### Data Flow Analysis

```mermaid
graph TD
    A[Job-Level Environment] --> B[GH_AW_SAFE_OUTPUTS]
    B --> C["/opt/gh-aw/safeoutputs/outputs.jsonl"]
    C --> D[Template Expansion]

    E[Workflow Config] --> F{upload-assets configured?}
    F -->|No| G[GH_AW_ASSETS_* NOT SET]
    F -->|Yes| H[GH_AW_ASSETS_* SET]
    G --> I[Empty String]
    H --> J[Defined Values]
    I --> K[Template Expansion - Undefined Vars]
    J --> K

    D --> L[MCP Server Environment]
    K --> L

    style B fill:#87CEEB
    style C fill:#90EE90
    style G fill:#FFB6C1
    style I fill:#FFA500
    style L fill:#DDA0DD
```text

#### GH_AW_SAFE_OUTPUTS
1. **Source**: `${{ env.GH_AW_SAFE_OUTPUTS }}`
2. **Origin**: Set at job level (line 184)
   ```yaml
   env:
     GH_AW_SAFE_OUTPUTS: /opt/gh-aw/safeoutputs/outputs.jsonl
   ```
3. **Control**: Hardcoded path in workflow definition
4. **User Input**: None - fixed string literal

#### GH_AW_ASSETS_* Variables
1. **Source**: `${{ env.GH_AW_ASSETS_BRANCH }}`, `${{ env.GH_AW_ASSETS_MAX_SIZE_KB }}`, `${{ env.GH_AW_ASSETS_ALLOWED_EXTS }}`
2. **Origin**: **UNDEFINED** - These variables are never set in the workflow
3. **Expected Behavior**: Should only be set when `safe-outputs.upload-assets` is configured (per `pkg/workflow/safe_outputs.go:271-274`)
4. **Actual Behavior**: mcp-inspector.md does not configure `upload-assets`, only `create-discussion`
5. **Runtime Value**: Empty string (undefined env var expands to "")

### Security Assessment

**Verdict**: ✅ **FALSE POSITIVE - NO SECURITY RISK (but has a bug)**

**Rationale**:
- `GH_AW_SAFE_OUTPUTS`: Set to a hardcoded path with no user input
- `GH_AW_ASSETS_*`: Undefined variables that expand to empty strings
- No user-controlled data flows into any of these template expansions
- The undefined variables are passed to the MCP server environment but never used (since upload-assets is not configured)
- Template injections require attacker-controlled input; these are workflow-controlled or empty

**Risk Level**: None

**Bug Found**: The compiler unconditionally references `GH_AW_ASSETS_*` environment variables in the "Setup MCPs" step even when `upload-assets` is not configured. This is harmless but indicates inconsistent code generation.

---

## Mitigation Recommendations

### For Finding 1: copilot-session-insights.lock.yml:204
**No action required** - This is a false positive with no security risk.

### For Finding 2: mcp-inspector.lock.yml:1129
**Bug Fix Recommended** (non-security):

The compiler should conditionally include the `GH_AW_ASSETS_*` environment variables only when `upload-assets` is configured in `safe-outputs`.

**Current behavior** (`pkg/workflow/mcp-config.go` or similar):
```go
// Always includes ASSETS env vars
env["GH_AW_ASSETS_BRANCH"] = "${{ env.GH_AW_ASSETS_BRANCH }}"
```text

**Recommended behavior**:
```go
// Only include ASSETS env vars if upload-assets is configured
if workflowData.SafeOutputs.UploadAssets != nil {
    env["GH_AW_ASSETS_BRANCH"] = "${{ env.GH_AW_ASSETS_BRANCH }}"
    env["GH_AW_ASSETS_MAX_SIZE_KB"] = "${{ env.GH_AW_ASSETS_MAX_SIZE_KB }}"
    env["GH_AW_ASSETS_ALLOWED_EXTS"] = "${{ env.GH_AW_ASSETS_ALLOWED_EXTS }}"
}
```text

This would eliminate unnecessary undefined variable references and make the generated workflow cleaner.

---

## Validation Methodology

### Tools Used
- **zizmor**: GitHub Actions security scanner (identified the findings)
- **gh-aw**: Compiled workflows with `--zizmor` flag
- **Manual code review**: Traced data flow from source to template expansion

### Analysis Steps
1. ✅ Located exact template expansions in .lock.yml files
2. ✅ Traced back to source .md workflow files
3. ✅ Identified all input sources to template expansions
4. ✅ Verified no user-controlled data reaches expansions
5. ✅ Reviewed compiler code (`pkg/workflow/safe_outputs.go`)
6. ✅ Confirmed workflow trigger restrictions (schedule/workflow_dispatch only)

### Workflow Triggers
Both workflows use secure trigger patterns with no external input:

**copilot-session-insights.md**:
```yaml
on:
  schedule:
    - cron: "0 16 * * *"  # Daily at 16:00 UTC
  workflow_dispatch:
```text

**mcp-inspector.md**:
```yaml
on:
  schedule:
    - cron: "0 18 * * 1"  # Weekly on Mondays at 18:00 UTC
  workflow_dispatch:
```text

Neither workflow accepts external input from issues, pull requests, or comments that could be attacker-controlled.

---

## Conclusion

### Security Status: ✅ SAFE

Both zizmor template injection findings are **false positives**:
1. Finding 1 uses workflow-controlled step outputs with hardcoded boolean values
2. Finding 2 uses workflow-controlled environment variables (some undefined, but harmless)

### Recommended Actions

1. **No security fixes required** - No vulnerabilities found
2. **Optional code quality improvement**: Fix undefined env var references in Finding 2 (see recommendations above)
3. **Documentation**: Add this security review to the repository for future reference

### Reviewer
- **Date**: 2025-11-11
- **Tool Version**: gh-aw (commit 774fe5b), zizmor (via --zizmor flag)
- **Methodology**: Data flow analysis + manual code review

---

## References

- GitHub Actions Template Injection: https://docs.zizmor.sh/audits/#template-injection
- Compiler Code: `pkg/workflow/safe_outputs.go`, `pkg/workflow/mcp-config.go`
