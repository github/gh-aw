#!/bin/bash
set +o histexpand

# Safe Outputs Specification Conformance Checker
# This script implements automated checks for the Safe Outputs specification
# Specification: docs/src/content/docs/specs/safe-outputs-specification.md
# Spec Version: 1.26.0 (2026-07-16)
# Script Version: 1.26.0 (2026-07-20)

set -euo pipefail

# Color codes for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
GREEN='\033[0;32m'
BLUE='\033[0;34m'
NC='\033[0m' # No Color

# Counters
CRITICAL_FAILURES=0
HIGH_FAILURES=0
MEDIUM_FAILURES=0
LOW_FAILURES=0

# Logging functions
log_critical() {
    echo -e "${RED}[CRITICAL]${NC} $1"
    ((CRITICAL_FAILURES += 1))
}

log_high() {
    echo -e "${RED}[HIGH]${NC} $1"
    ((HIGH_FAILURES += 1))
}

log_medium() {
    echo -e "${YELLOW}[MEDIUM]${NC} $1"
    ((MEDIUM_FAILURES += 1))
}

log_low() {
    echo -e "${BLUE}[LOW]${NC} $1"
    ((LOW_FAILURES += 1))
}

log_pass() {
    echo -e "${GREEN}[PASS]${NC} $1"
}

log_info() {
    echo -e "${BLUE}[INFO]${NC} $1"
}

# Change to repo root
cd "$(dirname "$0")/.."

echo "=================================================="
echo "Safe Outputs Specification Conformance Checker"
echo "=================================================="
echo ""

# SEC-001: Privilege Separation Enforcement
echo "Running SEC-001: Privilege Separation Enforcement..."
check_privilege_separation() {
    local failed=0
    
    # Find all compiled workflow files
    find .github/workflows -name "*.lock.yml" | while read -r workflow; do
        # Check if agent job has write permissions
        jobs_section=$(grep -A 50 "^jobs:" "$workflow" 2>/dev/null || true)
        agent_section=$(grep -A 20 "^\s*agent:" <<< "$jobs_section" 2>/dev/null || true)
        if grep -qE "issues:\s*write|pull-requests:\s*write|contents:\s*write" <<< "$agent_section"; then
            log_critical "SEC-001: Agent job in $workflow has write permissions"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "SEC-001: All agent jobs properly lack write permissions"
    fi
}
check_privilege_separation

# SEC-002: Validation Before API Calls
echo "Running SEC-002: Validation Before API Calls..."
check_validation_ordering() {
    local failed=0
    
    for handler in actions/setup/js/*.cjs; do
        # Skip test files
        [[ "$handler" =~ test ]] && continue
        [[ "$handler" =~ parse ]] && continue
        [[ "$handler" =~ buffer ]] && continue
        
        # Check if handler has API calls
        if grep -q "octokit\." "$handler"; then
            # Check if validation appears before API calls
            if ! awk '/octokit\./{api_line=NR} /validate|sanitize|enforceLimit/{if(NR<api_line || api_line==0) valid=1} END{exit !valid}' "$handler" 2>/dev/null; then
                log_critical "SEC-002: $handler may have API calls before validation"
                failed=1
            fi
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "SEC-002: All handlers validate before API calls"
    fi
}
check_validation_ordering

# SEC-003: Max Limit Enforcement
echo "Running SEC-003: Max Limit Enforcement..."
check_max_limits() {
    local failed=0
    
    for handler in actions/setup/js/*.cjs; do
        # Skip test and utility files
        [[ "$handler" =~ (test|parse|buffer|factory) ]] && continue
        
        # Only check files that perform GitHub API operations
        if ! grep -q "octokit\." "$handler"; then
            continue
        fi
        
        # Check if handler enforces max limits using any recognized pattern
        if ! grep -qE "\.length.*>.*\.max|enforceMaxLimit|checkLimit|max.*exceeded|enforceArrayLimit|tryEnforceArrayLimit|limit_enforcement_helpers" "$handler"; then
            log_medium "SEC-003: $handler may not enforce max limits"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "SEC-003: All handlers enforce max limits"
    fi
}
check_max_limits

# SEC-004: Content Sanitization Required
echo "Running SEC-004: Content Sanitization Required..."
check_sanitization() {
    local failed=0

    for handler in actions/setup/js/{add_comment,create_issue,create_discussion,create_pull_request,update_issue,close_issue,close_discussion,close_pull_request,add_labels,remove_labels,create_check_run,approve_workflow_run,create_pr_review_comment,comment_memory,create_agent_session,assign_milestone,create_project,create_forecast_issue}.cjs; do
        # Skip test and utility files
        [[ "$handler" =~ (test|parse|buffer) ]] && continue
        [ ! -f "$handler" ] && continue

        # Skip files with a documented SEC-004 exemption annotation
        if grep -q "@safe-outputs-exempt[[:space:]]\\+SEC-004" "$handler"; then
            continue
        fi
        
        # Only inspect handlers that process body/title/content fields for safe-output operations.
        # Files with other operational roles (for example completion helpers) are exempt unless
        # they explicitly process safe-output content.
        if grep -qE '"body"|body:|"title"|title:|"content"|content:' "$handler"; then
            # Check for sanitization
            if ! grep -q "sanitize\|stripHTML\|escapeMarkdown\|cleanContent" "$handler"; then
                log_medium "SEC-004: $handler has body field but no sanitization"
                failed=1
            fi
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "SEC-004: All handlers properly sanitize content"
    fi
}
check_sanitization

# SEC-005: Cross-Repository Validation
echo "Running SEC-005: Cross-Repository Validation..."
check_cross_repo() {
    local failed=0
    
    for handler in actions/setup/js/*.cjs; do
        # Skip test files
        [[ "$handler" =~ test ]] && continue
        
        # Skip files with a documented SEC-005 exemption annotation
        if grep -q "@safe-outputs-exempt.*SEC-005" "$handler"; then
            continue
        fi
        
        # Check if handler supports target-repo (match explicit config-surface identifiers only)
        if grep -qE "\btarget-repo\b|\btargetRepo\b|\btarget_repo\b" "$handler"; then
            # Check for allowlist validation
            if ! grep -q "allowed.*[Rr]epos\|validateTargetRepo\|checkAllowedRepo" "$handler"; then
                log_high "SEC-005: $handler supports target-repo but lacks allowlist check"
                failed=1
            fi
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "SEC-005: All cross-repo handlers validate allowlists"
    fi
}
check_cross_repo

# USE-001: Error Code Standardization
echo "Running USE-001: Error Code Standardization..."
check_error_codes() {
    local failed=0
    
    for handler in actions/setup/js/*.cjs; do
        # Skip test files and non-safe-output modules
        [[ "$handler" =~ test ]] && continue
        [[ "$handler" =~ (apm_unpack|run_apm_unpack|observability|generate_observability) ]] && continue
        
        # Only check handlers that interact with GitHub via octokit or record safe output operations
        if ! grep -qE "octokit\.|safe_output|safeOutput|NDJSON" "$handler"; then
            continue
        fi
        
        # Check if handler throws errors
        if grep -q "throw.*Error\|core\.setFailed" "$handler"; then
            # Check for standardized error codes
            if ! grep -qE "E[0-9]{3}|ERROR_|ERR_" "$handler"; then
                log_low "USE-001: $handler may not use standardized error codes"
                failed=1
            fi
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "USE-001: All handlers use standardized error codes"
    fi
}
check_error_codes

# USE-002: Footer Attribution Required
echo "Running USE-002: Footer Attribution Required..."
check_footers() {
    local failed=0
    
    # Check handlers that create issues/PRs/discussions
    for handler in actions/setup/js/{create_issue,create_pull_request,create_discussion,add_comment}.cjs; do
        [ ! -f "$handler" ] && continue
        
        # Check if handler adds footers
        if ! grep -q "footer\|addFooter\|attribution\|AI generated" "$handler"; then
            log_low "USE-002: $handler may not add footer attribution"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "USE-002: All handlers add footer attribution when configured"
    fi
}
check_footers

# USE-003: Staged Mode Preview Format
echo "Running USE-003: Staged Mode Preview Format..."
check_staged_mode() {
    local failed=0
    
    for handler in actions/setup/js/*.cjs; do
        # Skip test files and non-safe-output modules
        [[ "$handler" =~ test ]] && continue
        [[ "$handler" =~ (apm_unpack|run_apm_unpack|observability|generate_observability) ]] && continue
        
        # Only check handlers that explicitly reference the safe outputs staged mode env var
        if grep -q "GH_AW_SAFE_OUTPUTS_STAGED\|logStagedPreviewInfo\|generateStagedPreview" "$handler"; then
            # Check for emoji in preview
            if ! grep -q "🎭\|Staged Mode.*Preview\|logStagedPreviewInfo\|generateStagedPreview" "$handler"; then
                log_low "USE-003: $handler has staged mode but missing 🎭 emoji"
                failed=1
            fi
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "USE-003: All handlers use correct staged mode format"
    fi
}
check_staged_mode

# REQ-001: RFC 2119 Keyword Usage
echo "Running REQ-001: RFC 2119 Keyword Usage..."
check_rfc2119() {
    local spec_file="docs/src/content/docs/specs/safe-outputs-specification.md"
    local failed=0
    
    # Check key sections have RFC 2119 keywords
    for section in "Security Architecture" "Configuration Semantics" "Execution Guarantees"; do
        section_text=$(grep -A 200 "## .*$section" "$spec_file" 2>/dev/null || true)
        if ! grep -q "MUST\|SHALL\|SHOULD\|MAY" <<< "$section_text"; then
            log_medium "REQ-001: Section '$section' may lack RFC 2119 keywords"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "REQ-001: Normative sections use RFC 2119 keywords"
    fi
}
check_rfc2119

# REQ-002: Safe Output Type Completeness
echo "Running REQ-002: Safe Output Type Completeness..."
check_type_completeness() {
    local spec_file="docs/src/content/docs/specs/safe-outputs-specification.md"
    local failed=0
    
    # Extract type names
    grep "^#### Type:" "$spec_file" 2>/dev/null | sed 's/^#### Type: //' | head -10 | while read -r type_name; do
        sections_found=0
        
        # Check for required sections
        for section in "MCP Tool Schema" "Operational Semantics" "Configuration Parameters" "Security Requirements" "Required Permissions"; do
            type_section=$(grep -A 200 "^#### Type: $type_name" "$spec_file" 2>/dev/null || true)
            if grep -q "**$section**" <<< "$type_section"; then
                ((sections_found += 1))
            fi
        done
        
        if [ $sections_found -lt 5 ]; then
            log_medium "REQ-002: Type '$type_name' has only $sections_found/5 required sections"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "REQ-002: All safe output types have complete documentation"
    fi
}
check_type_completeness

# REQ-003: Verification Method Specification
echo "Running REQ-003: Verification Method Specification..."
check_verification_methods() {
    local spec_file="docs/src/content/docs/specs/safe-outputs-specification.md"
    local failed=0
    
    # Check key requirements have verification methods
    # Accept both bold (**Verification:**) and italic (*Verification*:) formats
    for req in "AR1" "AR2" "AR3" "SP1" "SP2" "SP3"; do
        req_section=$(grep -A 30 "\*\*Requirement $req:\|\*\*Property $req:" "$spec_file" 2>/dev/null || true)
        if ! grep -qE "\*\*Verification\*\*:|\*Verification\*:|\*\*Formal Definition\*\*:|\*Formal Definition\*:" <<< "$req_section"; then
            log_low "REQ-003: Requirement $req may lack verification method"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "REQ-003: All requirements have verification methods"
    fi
}
check_verification_methods

# IMP-001: Handler Registration Completeness
echo "Running IMP-001: Handler Registration Completeness..."
check_handler_registration() {
    local failed=0
    
    # Check if standard handlers exist
    for type in create_issue add_comment close_issue update_issue add_labels remove_labels; do
        handler_file="actions/setup/js/${type}.cjs"
        if [ ! -f "$handler_file" ]; then
            log_high "IMP-001: Missing handler file $handler_file"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "IMP-001: All standard handlers are registered"
    fi
}
check_handler_registration

# IMP-002: Permission Computation Accuracy
echo "Running IMP-002: Permission Computation Accuracy..."
check_permission_computation() {
    # Check if permission computation file exists and is well-formed
    if [ -f "pkg/workflow/safe_outputs_permissions.go" ]; then
        # Basic check that it defines ComputePermissionsForSafeOutputs
        if grep -q "ComputePermissionsForSafeOutputs" "pkg/workflow/safe_outputs_permissions.go"; then
            log_pass "IMP-002: Permission computation function exists"
        else
            log_high "IMP-002: Permission computation function not found"
        fi
    else
        log_high "IMP-002: Permission computation file missing"
    fi
}
check_permission_computation

# IMP-003: Schema Validation Consistency
echo "Running IMP-003: Schema Validation Consistency..."
check_schema_consistency() {
    local failed=0
    
    # Check if safe outputs config generation file exists with schema functions
    if [ -f "pkg/workflow/safe_outputs_config_generation.go" ]; then
        # Check for schema generation functions (custom job tool definition generation)
        if ! grep -q "generateCustomJobToolDefinition" "pkg/workflow/safe_outputs_config_generation.go"; then
            log_medium "IMP-003: Dynamic schema generation function missing"
            failed=1
        fi
    else
        log_medium "IMP-003: Safe outputs config generation file missing"
        failed=1
    fi
    
    # Check if static schemas file exists (embedded JSON)
    if [ -f "pkg/workflow/js/safe_outputs_tools.json" ]; then
        # Verify it contains MCP tool definitions with inputSchema
        if ! grep -q '"inputSchema"' "pkg/workflow/js/safe_outputs_tools.json"; then
            log_medium "IMP-003: Static schema definitions missing inputSchema"
            failed=1
        fi
    else
        log_medium "IMP-003: Static safe outputs tools schema missing"
        failed=1
    fi
    
    # Check if safe_outputs_config.go has documentation about schema architecture
    if [ -f "pkg/workflow/safe_outputs_config.go" ]; then
        if ! grep -q "Schema Generation Architecture" "pkg/workflow/safe_outputs_config.go"; then
            log_medium "IMP-003: Schema architecture documentation missing"
            failed=1
        fi
    fi
    
    if [ $failed -eq 0 ]; then
        log_pass "IMP-003: Schema generation is implemented"
    fi
}
check_schema_consistency

# IMP-004: Safe Output Config Schema Coverage
check_safe_output_config_schema_coverage() {
    local missing_properties

    echo "Running IMP-004: Safe Output Config Schema Coverage..."

    missing_properties=$(python3 - <<'PY'
import json
import re
from pathlib import Path

schema = json.loads(Path("pkg/parser/schemas/main_workflow_schema.json").read_text())
structs = {}
handler_fields = {}

for path in Path("pkg/workflow").glob("*.go"):
    if path.name.endswith("_test.go"):
        continue
    content = path.read_text()
    for match in re.finditer(r"(?ms)^type\s+(\w+)\s+struct\s*\{(.*?)^\}", content):
        structs[match.group(1)] = match.group(2)

handlers = Path("pkg/workflow/safe_output_handlers.go").read_text()
handler_key = None
for line in handlers.splitlines():
    key_match = re.search(r'Key:\s*"([^"]+)"', line)
    if key_match:
        handler_key = key_match.group(1)
    field_match = re.search(r'StructField:\s*"([^"]+)"', line)
    if field_match and handler_key:
        handler_fields[field_match.group(1)] = handler_key

compiler_populated_fields = {
    "safe-outputs.call-workflow.workflow_files",
    "safe-outputs.dispatch-workflow.workflow_files",
    "safe-outputs.dispatch-workflow.aw_context_workflows",
}

tool_configured_outputs = {"comment-memory"}


def yaml_fields(struct_name):
    for line in structs.get(struct_name, "").splitlines():
        match = re.match(r'\s*(.*?)\s+`yaml:"([^"]+)"', line)
        if not match:
            continue
        tag = match.group(2).split(",", 1)[0]
        if tag == "-":
            continue
        yield tag, ",inline" in match.group(2)


def properties(node):
    result = {}
    ref = node.get("$ref", "")
    defs_prefix = "#/$defs/"
    if ref.startswith(defs_prefix):
        result.update(properties(schema["$defs"][ref[len(defs_prefix):]]))
    for name, definition in node.get("properties", {}).items():
        if name in result and result[name] != definition:
            raise ValueError(f"conflicting schema definitions for property: {name}")
        result[name] = definition
    for alternative in ("allOf", "anyOf", "oneOf"):
        for child in node.get(alternative, []):
            for name, definition in properties(child).items():
                if name in result and result[name] != definition:
                    raise ValueError(f"conflicting schema definitions for property: {name}")
                result[name] = definition
    return result


missing = []


safe_outputs = properties(schema["properties"]["safe-outputs"])
for line in structs["SafeOutputsConfig"].splitlines():
    match = re.match(r'\s*(\w+)\s+\*?(\w+)\s+`yaml:"([^"]+)"', line)
    if not match:
        continue
    struct_field, config_type, output_name = match.groups()
    if struct_field not in handler_fields:
        continue
    output_name = output_name.split(",", 1)[0]
    if output_name in tool_configured_outputs:
        continue
    output_schema = safe_outputs.get(output_name)
    if output_schema is None:
        missing.append(f"safe-outputs.{output_name}")
        continue

    output_properties = properties(output_schema)
    for tag, inline in yaml_fields(config_type):
        property_path = f"safe-outputs.{output_name}.{tag}"
        if not inline and property_path not in compiler_populated_fields and tag not in output_properties:
            missing.append(property_path)

print("\n".join(sorted(set(missing))))
PY
)

    if [ -n "$missing_properties" ]; then
        while IFS= read -r property; do
            log_high "IMP-004: Safe output config property is missing from schema: $property"
        done <<< "$missing_properties"
    else
        log_pass "IMP-004: All safe output config properties are declared in the schema"
    fi
}
check_safe_output_config_schema_coverage

# MCE-001: Tool Description Constraint Disclosure (Section 8.3 MCE2)
echo "Running MCE-001: Tool Description Constraint Disclosure..."
check_mce_constraint_disclosure() {
    local tools_json="pkg/workflow/js/safe_outputs_tools.json"
    local failed=0
    
    if [ ! -f "$tools_json" ]; then
        log_high "MCE-001: Tool definitions file missing: $tools_json"
        return
    fi
    
    # Per spec Section 8.3 MCE2: add_comment MUST surface its constraint limits in description
    # Required: 65536 char limit, 10 mentions, 50 links (checks the combined tool JSON file)
    if ! grep -iE "65536" "$tools_json" > /dev/null 2>&1; then
        log_medium "MCE-001: add_comment tool description may be missing 65536 character limit"
        failed=1
    fi
    if ! grep -iE "10 mention" "$tools_json" > /dev/null 2>&1; then
        log_medium "MCE-001: add_comment tool description may be missing 10 mention limit"
        failed=1
    fi
    if ! grep -iE "50 link" "$tools_json" > /dev/null 2>&1; then
        log_medium "MCE-001: add_comment tool description may be missing 50 link limit"
        failed=1
    fi
    
    # Verify add_comment description contains CONSTRAINTS or IMPORTANT keyword
    add_comment_section=$(grep -A 5 '"add_comment"' "$tools_json" 2>/dev/null || true)
    if ! grep -qE "CONSTRAINTS|IMPORTANT.*constraint|validation constraint" <<< "$add_comment_section"; then
        log_medium "MCE-001: add_comment tool description missing required CONSTRAINTS/IMPORTANT disclosure"
        failed=1
    fi
    
    if [ $failed -eq 0 ]; then
        log_pass "MCE-001: Tool descriptions properly disclose enforcement constraints"
    fi
}
check_mce_constraint_disclosure

# MCE-002: Dual Enforcement Pattern (Section 8.3 MCE4)
echo "Running MCE-002: Dual Enforcement Pattern..."
check_mce_dual_enforcement() {
    local failed=0
    local helpers_file="actions/setup/js/comment_limit_helpers.cjs"
    
    # Per spec Section 8.3 MCE4: constraints must be enforced at both MCP invocation
    # time and safe output processing time
    
    # Check constraint helper module exists
    if [ ! -f "$helpers_file" ]; then
        log_high "MCE-002: Constraint helper module missing: $helpers_file"
        failed=1
        return
    fi
    
    # Check that both the MCP gateway handler (records operations) and the safe output 
    # processor (add_comment.cjs - executes API calls) import/use the constraint helpers.
    # Per spec MCE4: dual enforcement must exist at both invocation and processing time.
    local gateway_handler="actions/setup/js/safe_outputs_handlers.cjs"
    local add_comment_handler="actions/setup/js/add_comment.cjs"
    
    for handler in "$gateway_handler" "$add_comment_handler"; do
        if [ ! -f "$handler" ]; then
            log_medium "MCE-002: Expected handler file missing: $handler"
            failed=1
            continue
        fi
        if ! grep -q "comment_limit_helpers\|enforceCommentLimits" "$handler"; then
            log_medium "MCE-002: $handler does not enforce comment constraints (dual enforcement pattern)"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "MCE-002: Dual enforcement pattern implemented in both gateway and processor"
    fi
}
check_mce_dual_enforcement

# CI-001: Cache Memory Integrity Scripts Exist (Section 11 CI6, CI10)
echo "Running CI-001: Cache Memory Integrity Scripts Exist..."
check_cache_memory_scripts() {
    local failed=0
    
    # Per spec Section 11 CI6 and CI10: setup and commit scripts must exist
    local setup_script="actions/setup/sh/setup_cache_memory_git.sh"
    local commit_script="actions/setup/sh/commit_cache_memory_git.sh"
    
    for script in "$setup_script" "$commit_script"; do
        if [ ! -f "$script" ]; then
            log_high "CI-001: Required cache memory integrity script missing: $script"
            failed=1
        fi
    done
    
    if [ $failed -eq 0 ]; then
        log_pass "CI-001: Cache memory integrity scripts exist"
    fi
}
check_cache_memory_scripts

# CI-002: Cache Memory Integrity Branch Support (Section 11.2, CI7, CI8)
echo "Running CI-002: Cache Memory Integrity Branch Support..."
check_cache_integrity_branches() {
    local setup_script="actions/setup/sh/setup_cache_memory_git.sh"
    local commit_script="actions/setup/sh/commit_cache_memory_git.sh"
    local failed=0
    
    if [ ! -f "$setup_script" ]; then
        log_medium "CI-002: Setup script missing — skipping integrity branch check"
        return
    fi
    
    # Per spec Section 11.2: all four integrity levels must be supported (merged > approved > unapproved > none)
    for level in merged approved unapproved none; do
        if ! grep -q "\"$level\"\|'$level'" "$setup_script"; then
            log_high "CI-002: Integrity level '$level' not found in setup script"
            failed=1
        fi
    done
    
    # Per spec CI8: merge-down from higher-integrity branches must be implemented
    if ! grep -q "merge\|git merge" "$setup_script"; then
        log_high "CI-002: Setup script missing merge-down implementation (CI8)"
        failed=1
    fi
    
    # Per spec CI9: merge failure must abort and exit with non-zero status
    if ! grep -qE "merge.*abort|abort.*merge|exit.*\$|exit [0-9]" "$setup_script"; then
        log_high "CI-002: Setup script missing merge failure abort/exit handling (CI9)"
        failed=1
    fi
    
    # Per spec CI11: commit script must invoke git gc --auto for compaction
    if [ -f "$commit_script" ]; then
        if ! grep -q "git gc" "$commit_script"; then
            log_medium "CI-002: Commit script missing 'git gc --auto' (CI11 repository compaction)"
            failed=1
        fi
    fi
    
    # Per spec CI12: commit script must handle missing .git gracefully
    if [ -f "$commit_script" ]; then
        if ! grep -q '\.git\|no.*git\|skip.*git' "$commit_script"; then
            log_medium "CI-002: Commit script may not handle missing .git directory (CI12)"
            failed=1
        fi
    fi
    
    if [ $failed -eq 0 ]; then
        log_pass "CI-002: Cache memory integrity branching properly implemented"
    fi
}
check_cache_integrity_branches

# MCE-003: Constraint Limit Consistency (Section 8.3 MCE5)
echo "Running MCE-003: Constraint Limit Consistency..."
check_mce_constraint_consistency() {
    local tools_json="pkg/workflow/js/safe_outputs_tools.json"
    local helpers_file="actions/setup/js/comment_limit_helpers.cjs"
    local failed=0

    if [ ! -f "$helpers_file" ]; then
        log_high "MCE-003: Constraint helper module missing: $helpers_file"
        return
    fi
    if [ ! -f "$tools_json" ]; then
        log_high "MCE-003: Tool definitions file missing: $tools_json"
        return
    fi

    # Per spec Section 8.3 MCE5: limits in tool descriptions MUST match enforcement code.
    # Extract the declared limits from comment_limit_helpers.cjs and verify they appear
    # verbatim in the tools JSON, ensuring both layers agree on the constraint values.

    # Check body length limit (65536) appears in both files
    if ! grep -q "65536" "$helpers_file"; then
        log_high "MCE-003: MAX_COMMENT_LENGTH (65536) not found in $helpers_file"
        failed=1
    fi
    if ! grep -q "65536" "$tools_json"; then
        log_high "MCE-003: 65536 character limit not found in $tools_json"
        failed=1
    fi

    # Check mention limit (10) appears in both files
    if ! grep -qE "MAX_MENTIONS\s*=\s*10|max.*mention.*10|10.*mention" "$helpers_file"; then
        log_high "MCE-003: MAX_MENTIONS (10) not declared in $helpers_file"
        failed=1
    fi
    if ! grep -qiE "10 mention|mention.*10" "$tools_json"; then
        log_high "MCE-003: 10-mention limit not found in $tools_json"
        failed=1
    fi

    # Check link limit (50) appears in both files
    if ! grep -qE "MAX_LINKS\s*=\s*50|max.*link.*50|50.*link" "$helpers_file"; then
        log_high "MCE-003: MAX_LINKS (50) not declared in $helpers_file"
        failed=1
    fi
    if ! grep -qiE "50 link|link.*50" "$tools_json"; then
        log_high "MCE-003: 50-link limit not found in $tools_json"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "MCE-003: Constraint limits are consistent between tool descriptions and enforcement code"
    fi
}
check_mce_constraint_consistency

# CI-003: Policy Hash and Nopolicy Sentinel (Section 11.4 CI3, CI4)
echo "Running CI-003: Policy Hash and Nopolicy Sentinel..."
check_policy_hash_implementation() {
    local cache_integrity_file="pkg/workflow/cache_integrity.go"
    local failed=0

    # Per spec Section 11.4 CI3: policy hash MUST be SHA-256, first 8 chars of lowercase hex
    # Per spec Section 11.4 CI4: workflows without policy MUST use "nopolicy" sentinel
    if [ ! -f "$cache_integrity_file" ]; then
        log_high "CI-003: Cache integrity implementation missing: $cache_integrity_file"
        return
    fi

    # Check SHA-256 is used for policy hash computation (CI3)
    if ! grep -q "sha256\|crypto/sha256" "$cache_integrity_file"; then
        log_high "CI-003: SHA-256 not used for policy hash computation (CI3)"
        failed=1
    fi

    # Check nopolicy sentinel constant exists (CI4)
    if ! grep -q "nopolicy\|noPolicySentinel" "$cache_integrity_file"; then
        log_high "CI-003: 'nopolicy' sentinel not found in cache integrity implementation (CI4)"
        failed=1
    fi

    # Check that the 8-character prefix is taken (CI3: first 8 chars of lowercase hex)
    if ! grep -qE "\[:8\]|first.*8|8.*char|hex\[:8\]" "$cache_integrity_file"; then
        log_medium "CI-003: 8-character hash prefix truncation not evident in $cache_integrity_file (CI3)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "CI-003: Policy hash uses SHA-256 with nopolicy sentinel as required"
    fi
}
check_policy_hash_implementation

# CI-004: .git Directory Exclusion from Cache Memory Validation (Section 11.5 CI5)
echo "Running CI-004: .git Directory Exclusion from Validation..."
check_git_dir_exclusion() {
    local setup_script="actions/setup/sh/setup_cache_memory_git.sh"
    local failed=0

    # Per spec Section 11.5 CI5: file validation steps MUST skip the .git directory.
    # The .git directory contains binary/extension-less files not managed by the agent.

    if [ ! -f "$setup_script" ]; then
        log_medium "CI-004: Setup script missing — skipping .git exclusion check"
        return
    fi

    # Check that the setup script explicitly excludes .git from validation walks.
    if ! grep -qE "\.git|not -path .*\.git|prune.*\.git|exclude.*git|skip.*\.git" "$setup_script"; then
        log_medium "CI-004: Setup script does not reference .git exclusion (CI5)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "CI-004: .git directory exclusion from validation is present"
    fi
}
check_git_dir_exclusion

# CI-005: Integrity-Scoped Cache Keys (Section 11.3 CI1)
echo "Running CI-005: Integrity-Scoped Cache Keys..."
check_integrity_scoped_keys() {
    local failed=0

    # Per spec Section 11.3 CI1: All cache-memory keys MUST include the integrity level
    # and policy hash as prefixes in the format: memory-{integrityLevel}-{policyHash}-...

    local cache_lock_files
    cache_lock_files=$(find .github/workflows -name "*.lock.yml" -exec grep -l "GH_AW_CACHE_MEMORY\|cache-memory" {} \; 2>/dev/null)

    if [ -z "$cache_lock_files" ]; then
        log_pass "CI-005: No cache-memory workflows found — nothing to check"
        return
    fi

    while IFS= read -r workflow; do
        # Extract cache keys from the workflow
        # Valid format: memory-{integrityLevel}-{policyHash}-...
        # integrityLevel must be one of: merged, approved, unapproved, none
        # policyHash must be 8-char hex or the sentinel "nopolicy"
        while IFS= read -r key_line; do
            key=$(echo "$key_line" | sed 's/.*key:\s*//')
            if [[ "$key" =~ ^memory- ]]; then
                if ! echo "$key" | grep -qE "^memory-(merged|approved|unapproved|none)-(nopolicy|[0-9a-f]{8})-"; then
                    log_high "CI-005: Cache key in $workflow does not follow integrity-scoped format (CI1): $key"
                    failed=1
                fi
            fi
        done < <(grep "key: memory-" "$workflow" 2>/dev/null)
    done <<< "$cache_lock_files"

    if [ $failed -eq 0 ]; then
        log_pass "CI-005: All cache-memory keys use the integrity-scoped format (CI1)"
    fi
}
check_integrity_scoped_keys

# CI-006: Restore Key Cascade (Section 11.3 CI2)
echo "Running CI-006: Restore Key Cascade..."
check_restore_key_cascade() {
    local failed=0

    # Per spec Section 11.3 CI2: Restore keys MUST use the same integrity-scoped prefix
    # so that a partial key match never crosses integrity level boundaries.
    # The restore-keys pattern must not contain a run_id (to allow matching prior runs).

    local cache_lock_files
    cache_lock_files=$(find .github/workflows -name "*.lock.yml" -exec grep -l "GH_AW_CACHE_MEMORY\|cache-memory" {} \; 2>/dev/null)

    if [ -z "$cache_lock_files" ]; then
        log_pass "CI-006: No cache-memory workflows found — nothing to check"
        return
    fi

    while IFS= read -r workflow; do
        # Check each restore-key entry that starts with "memory-"
        while IFS= read -r restore_key; do
            key=$(echo "$restore_key" | sed 's/^\s*//')
            if [[ "$key" =~ ^memory- ]]; then
                # restore-keys should include the integrity level and policy hash prefix
                # but must NOT include the run_id (to match prior runs of same workflow)
                if ! echo "$key" | grep -qE "^memory-(merged|approved|unapproved|none)-(nopolicy|[0-9a-f]{8})-"; then
                    # Allow the legacy fallback "memory-" entry documented in spec
                    if [ "$key" != "memory-" ]; then
                        log_high "CI-006: Restore key in $workflow does not use integrity-scoped prefix (CI2): $key"
                        failed=1
                    fi
                fi
                # Restore keys must NOT include github.run_id (they are prefix-only)
                if echo "$key" | grep -q "run_id"; then
                    log_medium "CI-006: Restore key in $workflow includes run_id — should be prefix-only for cascade (CI2): $key"
                    failed=1
                fi
            fi
        done < <(awk '/restore-keys:/,/^[^|]/' "$workflow" 2>/dev/null | grep "memory-")
    done <<< "$cache_lock_files"

    if [ $failed -eq 0 ]; then
        log_pass "CI-006: All cache-memory restore keys use the integrity-scoped prefix (CI2)"
    fi
}
check_restore_key_cascade

# MCE-004: Early Validation at MCP Invocation (Section 8.3 MCE1)
echo "Running MCE-004: Early Validation at MCP Invocation..."
check_mce_early_validation() {
    local gateway_handler="actions/setup/js/safe_outputs_handlers.cjs"
    local failed=0

    # Per spec Section 8.3 MCE1: MCP servers MUST enforce operational constraints during
    # tool invocation (Phase 4) rather than deferring all validation to safe output
    # processing (Phase 6). This provides immediate feedback to the LLM.

    if [ ! -f "$gateway_handler" ]; then
        log_high "MCE-004: MCP gateway handler missing: $gateway_handler"
        return
    fi

    # Check that constraint validation (enforceCommentLimits or equivalent) is called
    # before the operation is appended to safe outputs (appendSafeOutput)
    if ! grep -q "enforceCommentLimits\|enforceConstraints\|validateEarly" "$gateway_handler"; then
        log_high "MCE-004: MCP gateway handler does not call early constraint validation (MCE1)"
        failed=1
    fi

    # Verify the handler references MCE1 requirement in documentation
    if ! grep -q "MCE1\|Early Validation\|tool invocation" "$gateway_handler"; then
        log_medium "MCE-004: MCE1 early validation pattern not documented in $gateway_handler"
        failed=1
    fi

    # Ensure validation occurs before recording (appendSafeOutput / recordOperation)
    # by checking that enforceCommentLimits appears before appendSafeOutput in the file
    enforce_line=$(grep -n "enforceCommentLimits" "$gateway_handler" | head -1 | cut -d: -f1)
    append_line=$(grep -n "appendSafeOutput" "$gateway_handler" | head -1 | cut -d: -f1)
    if [ -n "$enforce_line" ] && [ -n "$append_line" ]; then
        if [ "$enforce_line" -gt "$append_line" ]; then
            log_critical "MCE-004: enforceCommentLimits appears AFTER appendSafeOutput — validation is not early (MCE1)"
            failed=1
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "MCE-004: MCP gateway enforces constraints early at tool invocation (MCE1)"
    fi
}
check_mce_early_validation

# MCE-005: Actionable Error Responses (Section 8.3 MCE3)
echo "Running MCE-005: Actionable Error Responses..."
check_mce_actionable_errors() {
    local helpers_file="actions/setup/js/comment_limit_helpers.cjs"
    local gateway_handler="actions/setup/js/safe_outputs_handlers.cjs"
    local failed=0

    # Per spec Section 8.3 MCE3: When constraints are violated, MCP servers MUST return
    # error responses that:
    #   1. Identify the violated constraint with specific name and limit
    #   2. Report the actual value that triggered the violation
    #   3. Provide remediation guidance on how to correct the issue
    #   4. Use standard error codes (E006-E008 for add_comment limits in §8.3 MCE table)
    #
    # Note: §8.3 assigns E006=body-length, E007=mentions, E008=links for add_comment MCP
    # constraint violations. §9.5 defines a separate global error catalog where
    # E006=INVALID_LABEL, E007=API_ERROR, E008=SANITIZATION_FAILED. These two uses are
    # distinct contexts: §8.3 codes apply to MCP-layer constraint violations; §9.5 codes
    # apply to processor-layer validation and API errors.

    if [ ! -f "$helpers_file" ]; then
        log_high "MCE-005: Constraint helper module missing: $helpers_file"
        return
    fi

    # Check that error messages identify the constraint name and limit (requirement 1)
    if ! grep -qE "E006.*length|E007.*mention|E008.*link" "$helpers_file"; then
        log_medium "MCE-005: Error messages do not clearly identify constraint name in $helpers_file (MCE3 req 1)"
        failed=1
    fi

    # Check that error messages report the actual violating value (requirement 2)
    # Look for patterns like "got ${body.length}" or "contains ${mentions}" 
    if ! grep -qE "got \\\${|contains \\\${|\\.length\}" "$helpers_file"; then
        log_medium "MCE-005: Error messages do not report the actual violating value in $helpers_file (MCE3 req 2)"
        failed=1
    fi

    # Check that error responses include remediation guidance (requirement 3)
    # Either via a structured data.guidance field or guidance-oriented language in errors
    if ! grep -qE "guidance|reduce|Reduce|consider|Consider|fewer|lower" "$helpers_file"; then
        log_low "MCE-005: Error responses in $helpers_file may lack remediation guidance (MCE3 req 3)"
        failed=1
    fi

    # Check that standard error codes are used (requirement 4)
    if ! grep -qE "E006|E007|E008" "$helpers_file"; then
        log_medium "MCE-005: Standard error codes E006-E008 not used in $helpers_file (MCE3 req 4)"
        failed=1
    fi

    # Check gateway handler re-throws with standard -32602 JSON-RPC error code
    if [ -f "$gateway_handler" ]; then
        if ! grep -q "\-32602" "$gateway_handler"; then
            log_medium "MCE-005: MCP gateway handler does not return -32602 JSON-RPC error code for constraint violations (MCE3)"
            failed=1
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "MCE-005: Error responses include constraint identification, actual values, and standard codes (MCE3)"
    fi
}
check_mce_actionable_errors

# MCE-006: JSON-RPC Core Server Error Code Validity and Plain Object Error Handling (Section 8.2)
echo "Running MCE-006: JSON-RPC Core Server Error Code Validity..."
check_mce_core_error_handling() {
    local core_file="actions/setup/js/mcp_server_core.cjs"
    local failed=0

    # Per spec Section 8.2 (JSON-RPC 2.0 compliance): The MCP server MUST return valid
    # JSON-RPC error codes (negative integers) in all error responses. Positive integers
    # such as process exit codes (1, 2, etc.) are NOT valid JSON-RPC error codes and MUST
    # NOT be used, as they produce non-conformant responses (e.g. "code=1").
    # The handleMessage function MUST also properly serialize error messages from thrown
    # plain objects (non-Error instances) to avoid '[object Object]' in error responses.

    if [ ! -f "$core_file" ]; then
        log_high "MCE-006: MCP server core file missing: $core_file"
        return
    fi

    # Check that the error code is validated as a negative integer before use.
    # The guard pattern (e.code < 0) prevents positive subprocess exit codes from
    # being forwarded as JSON-RPC error codes.
    if ! grep -qE "e\.code < 0|err\.code < 0|code.*<.*0" "$core_file"; then
        log_critical "MCE-006: MCP server core does not guard against non-negative JSON-RPC error codes — non-conformant responses possible (Section 8.2)"
        failed=1
    fi

    # Check that error messages from thrown plain objects are serialized to a string
    # to prevent '[object Object]' appearing as the error message when non-Error
    # instances are thrown by handler code. This can be done directly with String()
    # or via the shared getErrorMessage() helper from error_helpers.cjs, as long as
    # that helper itself guards against non-string `.message` values.
    local helper_file="actions/setup/js/error_helpers.cjs"
    local uses_string_directly=0
    local uses_safe_helper=0

    if grep -qE 'String\(e\.message\)|String\(err\.message\)' "$core_file"; then
        uses_string_directly=1
    fi

    if grep -qE 'getErrorMessage' "$core_file" && [ -f "$helper_file" ]; then
        # The helper must not fall back to stringifying the entire error object when
        # the error has a non-string `.message` property — it must coerce the message
        # value itself instead.
        if grep -qzE 'message = typeof error\.message === "string" \? error\.message : String\(error\.message\)' "$helper_file"; then
            uses_safe_helper=1
        fi
    fi

    if [ $uses_string_directly -eq 0 ] && [ $uses_safe_helper -eq 0 ]; then
        log_high "MCE-006: MCP server core does not use String() for error message serialization — risk of '[object Object]' in responses (Section 8.2)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "MCE-006: MCP server core enforces valid JSON-RPC error codes and proper error message serialization (Section 8.2)"
    fi
}
check_mce_core_error_handling

# SEC-006: Error Code Catalog Completeness (Section 9.5, v1.26.0)
# Spec v1.26.0 expanded the error catalog to E001-E010, adding E009 (CONFIG_HASH_MISMATCH)
# and E010 (RATE_LIMIT_EXCEEDED). Implementations MUST use standardized error codes for
# all validation and execution failures (Section 9.5 normative requirement).
echo "Running SEC-006: Error Code Catalog Completeness (Section 9.5)..."
check_error_catalog_completeness() {
    local spec_file="docs/src/content/docs/specs/safe-outputs-specification.md"
    local processor_dir="actions/setup/js"
    local failed=0

    # Verify spec defines the full E001-E010 catalog (introduced in v1.26.0)
    for code in E009 E010; do
        if ! grep -q "$code" "$spec_file"; then
            log_high "SEC-006: Spec does not define error code $code — catalog may be out of date"
            failed=1
        fi
    done

    # Check that E009 (CONFIG_HASH_MISMATCH) and E010 (RATE_LIMIT_EXCEEDED) are referenced
    # in implementation handler files (processor layer must recognise all catalog codes)
    if [ -d "$processor_dir" ]; then
        if ! grep -rqE "E009|CONFIG_HASH_MISMATCH" "$processor_dir"/ 2>/dev/null; then
            log_medium "SEC-006: No handler references E009 (CONFIG_HASH_MISMATCH) — rate-limit and hash-mismatch error codes may be absent from processor"
            failed=1
        fi
        if ! grep -rqE "E010|RATE_LIMIT_EXCEEDED" "$processor_dir"/ 2>/dev/null; then
            log_medium "SEC-006: No handler references E010 (RATE_LIMIT_EXCEEDED) — rate-limit retry error code may be absent from processor"
            failed=1
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "SEC-006: Error code catalog E001-E010 is complete and referenced in spec and implementation"
    fi
}
check_error_catalog_completeness

# SEC-007: EH1 Early Failure Detection - Validation Before API (Section 9.5, v1.26.0)
# Requirement EH1: Validation errors (E001-E006) MUST be detected before any GitHub API calls.
# Scope: applies to safe-output processor handlers (files that record NDJSON operations).
echo "Running SEC-007: EH1 Early Failure Detection (Section 9.5)..."
check_early_failure_detection() {
    local failed=0

    # Only check files that are safe-output processors (handle recorded NDJSON operations).
    # These files directly process NDJSON operations (not just import a helper for staged mode).
    while IFS= read -r handler; do
        # Must reference NDJSON/safeOutput in processing context (not just import helpers)
        if ! grep -qE "output\.ndjson|processOperation|safe_outputs_processor|recordOperation" "$handler" 2>/dev/null; then
            continue
        fi
        if ! grep -qE "octokit\.|github\.rest\." "$handler" 2>/dev/null; then
            continue
        fi
        # The handler must contain validation/sanitization before any octokit call.
        if ! grep -qE "validate|sanitize|enforce|E00[1-6]|INVALID_SCHEMA|LIMIT_EXCEEDED|UNAUTHORIZED_DOMAIN|INVALID_TARGET_REPO|MISSING_PARENT|INVALID_LABEL" "$handler" 2>/dev/null; then
            log_medium "SEC-007: $handler is a safe-output handler but may lack pre-API validation (EH1 requirement)"
            failed=1
        fi
    done < <(find actions/setup/js -name "*.cjs" ! -name "*test*" 2>/dev/null)

    if [ $failed -eq 0 ]; then
        log_pass "SEC-007: Safe-output handlers include pre-API validation checks (EH1)"
    fi
}
check_early_failure_detection

# AR2-001: Artifact-Based Communication (Section 3.1, AR2, v1.26.0)
# AR2: Agent-to-processor communication MUST use GitHub Actions artifacts.
# Environment variables, network channels, and shared filesystems MUST NOT be used.
echo "Running AR2-001: Artifact-Based Communication (Section 3.1 AR2)..."
check_artifact_communication() {
    local failed=0

    # Check that compiled workflow files use artifact upload/download for safe outputs
    local upload_count
    upload_count=$(grep -rl "upload-artifact\|actions/upload-artifact" .github/workflows/ 2>/dev/null | wc -l)
    if [ "$upload_count" -eq 0 ]; then
        log_medium "AR2-001: No workflow found using artifact upload — AR2 artifact-based communication may not be implemented"
        failed=1
    fi

    local download_count
    download_count=$(grep -rl "download-artifact\|actions/download-artifact" .github/workflows/ 2>/dev/null | wc -l)
    if [ "$download_count" -eq 0 ]; then
        log_medium "AR2-001: No workflow found using artifact download — AR2 artifact-based communication may not be implemented"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "AR2-001: Workflows use GitHub Actions artifacts for agent-to-processor communication (AR2)"
    fi
}
check_artifact_communication

# TYPE-001: merge_pull_request Handler Existence and Default Branch Protection (Section 7.3, v1.17.0)
echo "Running TYPE-001: merge_pull_request Handler Existence and Default Branch Protection..."
check_merge_pull_request_handler() {
    local handler="actions/setup/js/merge_pull_request.cjs"
    local failed=0

    # Per spec Section 7.3: merge_pull_request handler must exist
    if [ ! -f "$handler" ]; then
        log_high "TYPE-001: merge_pull_request handler missing: $handler"
        return
    fi

    # Per spec Section 7.3: Merge to the repository default branch MUST be refused
    if ! grep -q "isDefault" "$handler"; then
        log_critical "TYPE-001: merge_pull_request handler does not check isDefault — default branch protection missing"
        failed=1
    fi

    # Per spec Section 7.3: Policy gates must be enforced (required checks, review decision)
    if ! grep -qE "requiredChecks|review_decision|required_labels|allowed_labels" "$handler"; then
        log_high "TYPE-001: merge_pull_request handler missing policy gate checks"
        failed=1
    fi

    # Per spec Section 7.3: mergeability check must be present
    if ! grep -q "mergeable" "$handler"; then
        log_high "TYPE-001: merge_pull_request handler does not verify mergeability"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-001: merge_pull_request handler exists with default branch protection and policy gates"
    fi
}
check_merge_pull_request_handler

# TYPE-002: comment_memory Memory ID Validation (Section 7.3, v1.18.0)
echo "Running TYPE-002: comment_memory Memory ID Validation..."
check_comment_memory_validation() {
    local handler="actions/setup/js/comment_memory.cjs"
    local helpers="actions/setup/js/comment_memory_helpers.cjs"
    local failed=0

    # Per spec Section 7.3: comment_memory handler must exist
    if [ ! -f "$handler" ]; then
        log_high "TYPE-002: comment_memory handler missing: $handler"
        return
    fi

    # Per spec Section 7.3: memory_id MUST be validated as [A-Za-z0-9_-]+
    if ! grep -qE "\[A-Za-z0-9_-\]" "$handler" "$helpers" 2>/dev/null; then
        log_critical "TYPE-002: comment_memory does not validate memory_id with [A-Za-z0-9_-]+ pattern — path traversal risk"
        failed=1
    fi

    # Per spec Section 7.3: Managed comment scan MUST be bounded by a maximum page limit
    if ! grep -qE "MAX_SCAN_PAGES|maxScanPages|scan.*limit|COMMENT_MEMORY_MAX_SCAN" "$handler"; then
        log_high "TYPE-002: comment_memory scan is not bounded by a maximum page limit"
        failed=1
    fi

    # Per spec Section 7.3: Body content MUST undergo sanitization before upsert
    if ! grep -qE "sanitize|validateBody|enforceComment" "$handler"; then
        log_high "TYPE-002: comment_memory body content does not appear to be sanitized before upsert"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-002: comment_memory handler validates memory_id, bounds scanning, and sanitizes body"
    fi
}
check_comment_memory_validation

# TYPE-003: comment_memory Not Exposed as MCP Tool (Section 7.3, v1.18.0)
echo "Running TYPE-003: comment_memory Not Exposed as Agent MCP Tool..."
check_comment_memory_not_mcp_tool() {
    local safe_outputs_tools="pkg/workflow/js/safe_outputs_tools.json"
    local failed=0

    # Per spec Section 7.3: comment_memory MUST NOT be exposed as an agent-editable MCP tool
    # when file-based comment-memory synchronization is active.
    if [ -f "$safe_outputs_tools" ]; then
        if grep -q '"comment_memory"' "$safe_outputs_tools"; then
            log_high "TYPE-003: comment_memory is registered as an agent MCP tool in $safe_outputs_tools — spec requires it NOT be exposed"
            failed=1
        fi
    fi

    # Also check the gateway handler tool registration
    local gateway="actions/setup/js/safe_outputs_handlers.cjs"
    if [ -f "$gateway" ]; then
        if grep -qE '"comment_memory"|comment_memory.*tool.*register' "$gateway"; then
            log_medium "TYPE-003: comment_memory may be registered as an MCP tool in $gateway — verify file-based sync is used instead"
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-003: comment_memory is not registered as an agent-editable MCP tool"
    fi
}
check_comment_memory_not_mcp_tool

# TYPE-004: create-issue Auto-Injection for Workflows Without safe-outputs (Section 4.3, v1.19.0)
echo "Running TYPE-004: create-issue Auto-Injection..."
check_create_issue_auto_injection() {
    local failed=0

    # Per spec Section 4.3 (v1.19.0): When no safe-outputs: section is present, the compiler
    # MUST automatically inject a default create-issue configuration (max: 1, labels: [workflowID],
    # title-prefix: "[workflowID]"). Auto-injection is suppressed when any non-builtin safe output
    # is explicitly configured.

    # Verify the Go compiler implements auto-injection by checking test or implementation files
    if ! grep -rqE "auto.inject.*create.issue|inject.*create.issue|autoInject|injectDefault.*create" pkg/workflow/ 2>/dev/null; then
        log_high "TYPE-004: No evidence of create-issue auto-injection logic in pkg/workflow/ (spec Section 4.3 v1.19.0)"
        failed=1
    fi

    # Verify auto-injection is suppressed when non-builtin outputs are configured
    if ! grep -rqE "suppress.*inject|non.builtin|noop.*missing.tool.*missing.data|system.type" pkg/workflow/ 2>/dev/null; then
        log_medium "TYPE-004: Cannot confirm auto-injection suppression for non-builtin safe outputs (spec Section 4.3 v1.19.0)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-004: create-issue auto-injection is implemented and suppression logic exists"
    fi
}
check_create_issue_auto_injection

# INT-001: JSON Schema Draft 7 Validation (Section 9.1)
echo "Running INT-001: JSON Schema Draft 7 Validation..."
check_json_schema_draft7() {
    local tools_json="pkg/workflow/js/safe_outputs_tools.json"
    local gateway_handler="actions/setup/js/safe_outputs_handlers.cjs"
    local failed=0

    # Per spec Section 9.1: All tool invocations MUST validate against JSON Schema Draft 7.
    # Check that tool schemas declare draft-07 or that the gateway validates using Ajv/equivalent.

    if [ ! -f "$tools_json" ]; then
        log_high "INT-001: Tool definitions file missing: $tools_json"
        return
    fi

    # Per spec Section 9.1: Schema validation is provided by the MCP framework via inputSchema.
    # Check that the tool definitions include inputSchema on all tools, which enables
    # JSON Schema Draft 7 validation at the MCP server level.
    # Note: Explicit Ajv usage is one approach; relying on MCP framework schema enforcement
    # via inputSchema is the primary conformant pattern in this implementation.

    # Verify inputSchema is present on all tools (required by JSON Schema Draft 7 pattern)
    local tools_without_schema
    tools_without_schema=$(python3 -c "
import json, sys
try:
    data = json.load(open('$tools_json'))
    tools = data if isinstance(data, list) else data.get('tools', [])
    missing = [t.get('name','?') for t in tools if isinstance(t, dict) and 'inputSchema' not in t]
    if missing: print(','.join(missing))
except Exception as e:
    sys.exit(0)
" 2>/dev/null)
    if [ -n "$tools_without_schema" ]; then
        log_medium "INT-001: Tools missing inputSchema in $tools_json: $tools_without_schema"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "INT-001: Tool schemas include inputSchema for JSON Schema Draft 7 validation"
    fi
}
check_json_schema_draft7

# INT-002: Sanitization Pipeline Completeness (Section 9.4 S1, S4)
echo "Running INT-002: Sanitization Pipeline Completeness..."
check_sanitization_pipeline() {
    local core_sanitizer="actions/setup/js/sanitize_content_core.cjs"
    local fallback_sanitizer="actions/setup/js/sanitize_content.cjs"
    local failed=0

    # Per spec Section 9.4: Implementations MUST apply these stages in order:
    # S1: Null byte removal (remove \x00 and control chars)
    # S4: HTML tag filtering (remove <script>, <iframe>, on* event handlers)

    local sanitizer_file=""
    if [ -f "$core_sanitizer" ]; then
        sanitizer_file="$core_sanitizer"
    elif [ -f "$fallback_sanitizer" ]; then
        sanitizer_file="$fallback_sanitizer"
    else
        log_high "INT-002: Sanitization implementation file missing (expected $core_sanitizer)"
        return
    fi

    # Check S1: Null byte / control character removal (Section 9.4 S1)
    # Spec requires removal of all null bytes (\0, \x00).
    # Implementation may use a control-char range starting at \x00 (e.g., /[\x00-\x08...]/)
    if ! grep -qE 'x00|removeNull|null.*byte|byte.*null' "$sanitizer_file"; then
        log_high "INT-002: Sanitization pipeline missing null byte removal (Section 9.4 S1)"
        failed=1
    fi

    # Check S4: HTML tag filtering — <script>, <iframe>, on* event handlers (Section 9.4 S4)
    if ! grep -qE 'script|iframe|on\*|onerror|onclick|event.*handler|dangerous.*attr|strip.*attr' "$sanitizer_file"; then
        log_high "INT-002: Sanitization pipeline missing HTML tag/event handler filtering (Section 9.4 S4)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "INT-002: Sanitization pipeline implements null byte removal (S1) and HTML filtering (S4)"
    fi
}
check_sanitization_pipeline

# EXEC-001: System Types Processed Last (Section 10.2)
echo "Running EXEC-001: System Types Processed Last..."
check_system_types_ordering() {
    local manager_file="actions/setup/js/safe_output_handler_manager.cjs"
    local failed=0

    # Per spec Section 10.2: Operations execute in NDJSON order, with system types
    # (noop, missing_tool, missing_data, report_incomplete) processed LAST.

    if [ ! -f "$manager_file" ]; then
        log_high "EXEC-001: Safe output handler manager missing: $manager_file"
        return
    fi

    # Check that system types are collected separately (prerequisite for last processing)
    if ! grep -qE "missing_tool.*missing_data.*noop|collect.*missing|system.*type" "$manager_file"; then
        log_medium "EXEC-001: Handler manager does not appear to separate system types for ordering (Section 10.2)"
        failed=1
    fi

    # Verify that noop, missing_tool, missing_data, report_incomplete are recognized as a group
    if ! grep -q "report_incomplete" "$manager_file"; then
        log_medium "EXEC-001: report_incomplete system type not handled in $manager_file (Section 10.2)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "EXEC-001: System types (noop, missing_tool, missing_data, report_incomplete) are grouped for last processing"
    fi
}
check_system_types_ordering

# EXEC-002: Zero Max Limit Disables Type (Section 10.6)
echo "Running EXEC-002: Zero Max Limit Disables Type..."
check_zero_max_disables_type() {
    local failed=0

    # Per spec Section 10.6: When max: 0 is configured for a safe output type,
    # the type MUST be disabled (MCP tool not registered, no config generated).

    # Check Go compiler: types with max: 0 should not appear in generated config
    if grep -rqE "max.*==.*0|\.Max.*==.*0|maxIsZero|disabledType|skipZeroMax" pkg/workflow/safe_outputs*.go 2>/dev/null; then
        log_pass "EXEC-002: Compiler handles max: 0 type disabling"
        return
    fi

    # Alternative: check if there are tests validating zero-max disabling
    if grep -rqE "max.*0.*disabled|max.*:.*0|\"max\".*0" pkg/workflow/safe_outputs*test*.go 2>/dev/null; then
        log_pass "EXEC-002: Tests validate max: 0 type disabling behavior"
        return
    fi

    # Check if the gateway handler skips tools not present in config (indirectly validates zero-max)
    local gateway="actions/setup/js/safe_outputs_handlers.cjs"
    if [ -f "$gateway" ]; then
        if grep -qE "toolsConfig|registeredTools|register.*tool|tool.*register" "$gateway"; then
            log_pass "EXEC-002: Gateway registers tools from config (zero-max types absent from config will not be registered)"
            return
        fi
    fi

    log_medium "EXEC-002: No explicit evidence that max: 0 disables/unregisters the safe output type (Section 10.5)"
    failed=1

    if [ $failed -eq 0 ]; then
        log_pass "EXEC-002: max: 0 properly disables safe output type registration"
    fi
}
check_zero_max_disables_type

# WTD-001: Reviewable Annotation Requirements (Section 10.5 WTD1, T-WTD-001)
echo "Running WTD-001: Reviewable Annotation Requirements..."
check_wtd_reviewable_annotation() {
    local threat_warning_file="actions/setup/js/threat_detection_warning.cjs"
    local footer_file="actions/setup/js/generate_footer.cjs"
    local failed=0

    # Per spec Section 10.5 WTD1: Reviewable outputs MUST include all three of:
    # 1. A caution block with "agentic threat detected" text
    # 2. A visible threat label string: "agentic threat detected"
    # 3. An XML comment marker: <!-- gh-aw-threat-detected -->
    # The implementation uses generate_footer.cjs (for the caution block) and
    # threat_detection_warning.cjs (centralised marker/helper).

    if [ ! -f "$footer_file" ]; then
        log_high "WTD-001: Footer generator missing: $footer_file"
        return
    fi

    # Check caution block with WTD1-required text (requirement 1)
    if ! grep -q "\[!CAUTION\]" "$footer_file"; then
        log_critical "WTD-001: Footer generator missing [!CAUTION] block (WTD1 requirement 1)"
        failed=1
    fi

    # Check label string "agentic threat detected" (requirement 2)
    # The footer may delegate to a centralised template (threat_detection_caution.md) or
    # to threat_detection_warning.cjs, so accept those as equivalent evidence.
    local label_found=0
    if grep -q "agentic threat detected" "$footer_file" 2>/dev/null; then
        label_found=1
    elif grep -q "threat_detection_caution" "$footer_file" 2>/dev/null; then
        local caution_template="actions/setup/md/threat_detection_caution.md"
        if [ -f "$caution_template" ] && grep -q "agentic threat detected" "$caution_template" 2>/dev/null; then
            label_found=1
        elif [ -f "$threat_warning_file" ] && grep -q "agentic threat detected" "$threat_warning_file" 2>/dev/null; then
            label_found=1
        fi
    fi
    if [ $label_found -eq 0 ]; then
        log_critical "WTD-001: Footer generator missing 'agentic threat detected' label string (WTD1 requirement 2)"
        failed=1
    fi

    # Check XML comment marker (requirement 3) — may be defined in the centralised
    # threat_detection_warning.cjs helper and injected via getThreatDetectedMarker()
    local marker_found=0
    if grep -q "gh-aw-threat-detected" "$footer_file" 2>/dev/null; then
        marker_found=1
    elif [ -f "$threat_warning_file" ] && grep -q "gh-aw-threat-detected" "$threat_warning_file" 2>/dev/null; then
        # Footer delegates to threat_detection_warning.cjs via getThreatDetectedMarker
        if grep -q "getThreatDetectedMarker" "$footer_file"; then
            marker_found=1
        fi
    fi
    if [ $marker_found -eq 0 ]; then
        log_critical "WTD-001: XML marker '<!-- gh-aw-threat-detected -->' not found in footer or centralised helper (WTD1 requirement 3)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "WTD-001: Reviewable annotation includes caution block, threat label, and XML marker (WTD1)"
    fi
}
check_wtd_reviewable_annotation

# WTD-002: Convertible Fallback push_to_pull_request_branch → create_pull_request (Section 10.5 WTD2, T-WTD-002)
echo "Running WTD-002: Convertible Fallback for push_to_pull_request_branch..."
check_wtd_convertible_fallback() {
    local push_handler="actions/setup/js/push_to_pull_request_branch.cjs"
    local manager="actions/setup/js/safe_output_handler_manager.cjs"
    local failed=0

    # Per spec Section 10.5 WTD2: push_to_pull_request_branch MUST fall back to
    # create_pull_request with WTD1 caution, label, and XML marker when threat
    # detection executes in warn mode and reports a threat signal.

    if [ ! -f "$push_handler" ]; then
        log_high "WTD-002: push_to_pull_request_branch handler missing: $push_handler"
        failed=1
    else
        # Check the handler has detection conclusion handling
        if ! grep -q "GH_AW_DETECTION_CONCLUSION\|detectionConclusionEnv" "$push_handler"; then
            log_high "WTD-002: push_to_pull_request_branch handler does not check GH_AW_DETECTION_CONCLUSION (WTD2)"
            failed=1
        fi

        # Check it creates a review PR (fallback to create_pull_request semantics)
        # The handler may create a pull request directly via octokit rather than
        # delegating to the create_pull_request safe output type
        if ! grep -qE "create_pull_request|createPullRequest|review.*PR|review.*pr|pulls\.create|review_pr" "$push_handler"; then
            log_high "WTD-002: push_to_pull_request_branch handler missing create_pull_request fallback (WTD2)"
            failed=1
        fi

        # Check that the caution text is emitted in the fallback.
        # The handler may delegate to threat_detection_warning.cjs via
        # getThreatWarningPresentation(), so accept that as equivalent evidence.
        local threat_label_found=0
        if grep -q "agentic threat detected" "$push_handler" 2>/dev/null; then
            threat_label_found=1
        elif grep -q "getThreatWarningPresentation" "$push_handler" 2>/dev/null; then
            local threat_warning_file="actions/setup/js/threat_detection_warning.cjs"
            if [ -f "$threat_warning_file" ] && grep -q "agentic threat detected" "$threat_warning_file" 2>/dev/null; then
                threat_label_found=1
            fi
        fi
        if [ $threat_label_found -eq 0 ]; then
            log_high "WTD-002: push_to_pull_request_branch fallback missing 'agentic threat detected' text (WTD2 / WTD1)"
            failed=1
        fi
    fi

    # Also verify the handler manager registers push_to_pull_request_branch as Convertible
    if [ -f "$manager" ]; then
        if ! grep -qE "convertible|push_to_pull_request_branch.*create_pull_request|Convertible" "$manager"; then
            log_medium "WTD-002: Handler manager does not declare push_to_pull_request_branch as Convertible (WTD2)"
            failed=1
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "WTD-002: push_to_pull_request_branch has convertible fallback to create_pull_request (WTD2)"
    fi
}
check_wtd_convertible_fallback

# WTD-003: Abort-Class Outputs Produce Threat-Detected Error Outcomes (Section 10.5 WTD3, T-WTD-003)
echo "Running WTD-003: Abort-Class Output Handling..."
check_wtd_abort_outputs() {
    local manager="actions/setup/js/safe_output_handler_manager.cjs"
    local failed=0

    # Per spec Section 10.5 WTD3: Abort-classified outputs MUST NOT be applied.
    # Implementations MUST activate a threat-detected code path, emit an explicit
    # failure summary, and return a machine-readable threat-detected error outcome.

    if [ ! -f "$manager" ]; then
        log_high "WTD-003: Safe output handler manager missing: $manager"
        return
    fi

    # Check THREAT_WARNING_ABORT_TYPES set exists (defines abort-class types)
    if ! grep -q "THREAT_WARNING_ABORT_TYPES" "$manager"; then
        log_critical "WTD-003: THREAT_WARNING_ABORT_TYPES not defined in handler manager (WTD3)"
        failed=1
    fi

    # Check abort policy stops execution (MUST NOT apply the safe output)
    if ! grep -qE "policy.*abort|abort.*policy|abort.*threat|threat.*abort" "$manager"; then
        log_high "WTD-003: Abort policy branch not found in handler manager (WTD3)"
        failed=1
    fi

    # Check machine-readable threat-detected error outcome is returned
    if ! grep -qE "threat_detected_abort_policy|threatDetected.*true|errorCode.*threat" "$manager"; then
        log_high "WTD-003: No machine-readable threat-detected error outcome in handler manager (WTD3)"
        failed=1
    fi

    # Verify WTD3 requirement ID is referenced in code comments (for traceability)
    if ! grep -q "WTD3\|WTD-3\|Requirement.*WTD" "$manager"; then
        log_low "WTD-003: WTD3 requirement ID not referenced in handler manager for traceability"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "WTD-003: Abort-class outputs have threat-detected abort handling and machine-readable error outcomes (WTD3)"
    fi
}
check_wtd_abort_outputs

# TYPE-005: add_comment Status-Comment Reuse Extension (Section 7.1, v1.21.0)
echo "Running TYPE-005: add_comment Status-Comment Reuse Extension..."
check_add_comment_status_target() {
    local handler="actions/setup/js/add_comment.cjs"
    local failed=0

    # Per spec Section 7.1 (v1.21.0):
    # 1. When target:"status" is set and a reusable status comment ID is available,
    #    implementations MUST update the existing issue/PR comment instead of creating a new one.
    # 2. When target:"status" is set but no reusable status comment ID is available,
    #    implementations MUST create a new comment.
    # 3. target:"status" and comment_id MUST be rejected for discussion comments.

    if [ ! -f "$handler" ]; then
        log_high "TYPE-005: add_comment handler missing: $handler"
        return
    fi

    # Check that target=status handling exists
    if ! grep -qE 'target.*status|status.*target' "$handler"; then
        log_high "TYPE-005: add_comment handler has no target=status handling (Section 7.1 requirement 1/2)"
        failed=1
    fi

    # Check that existing comment update path exists (MUST update existing comment when ID available)
    if ! grep -qE 'updateComment|update.*comment|commentIdToReuse|comment_id.*reuse' "$handler"; then
        log_high "TYPE-005: add_comment handler lacks existing comment update path for status reuse (Section 7.1 requirement 1)"
        failed=1
    fi

    # Check that fallback to new comment creation exists (MUST create new when no ID available)
    if ! grep -qE 'no reusable status comment|creating a new comment|statusCommentId.*null|statusCommentId.*empty' "$handler"; then
        log_medium "TYPE-005: add_comment handler may lack fallback new-comment creation for target=status with no ID (Section 7.1 requirement 2)"
        failed=1
    fi

    # Check that discussion rejection is implemented (MUST reject target=status for discussions)
    if ! grep -qE 'discussion.*reject|only.*issue.*pull.request|issue.*pull.request.*only|not.*discussion' "$handler"; then
        log_high "TYPE-005: add_comment handler must reject target=status for discussion comments (Section 7.1 requirement 3)"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-005: add_comment handler correctly implements status-comment reuse extension (Section 7.1 v1.21.0)"
    fi
}
check_add_comment_status_target

# TYPE-006: push_to_pull_request_branch base-branch Parameter (Section 7.3, v1.22.0)
echo "Running TYPE-006: push_to_pull_request_branch base-branch Parameter..."
check_push_to_pr_branch_base_branch() {
    local handler="actions/setup/js/push_to_pull_request_branch.cjs"
    local go_config="pkg/workflow/push_to_pull_request_branch.go"
    local failed=0

    # Per spec Section 7.3 (v1.22.0): push_to_pull_request_branch MUST support a
    # base-branch configuration parameter. When omitted, the handler resolves the base
    # branch via: (1) explicit config, (2) runtime checkout manifest, (3) origin/HEAD,
    # (4) repository default branch via API.

    if [ ! -f "$handler" ]; then
        log_high "TYPE-006: push_to_pull_request_branch handler missing: $handler"
        return
    fi

    # Check that the handler reads a base_branch config value
    if ! grep -qE "base_branch|base-branch|baseBranch" "$handler"; then
        log_high "TYPE-006: push_to_pull_request_branch handler does not read base-branch config (Section 7.3 v1.22.0)"
        failed=1
    fi

    # Check that Go config struct has a BaseBranch field (spec requires explicit config support)
    if [ -f "$go_config" ]; then
        if ! grep -q "BaseBranch\|base-branch" "$go_config"; then
            log_high "TYPE-006: push_to_pull_request_branch Go config struct missing BaseBranch field (Section 7.3 v1.22.0)"
            failed=1
        fi
    else
        log_medium "TYPE-006: Go config file missing: $go_config — cannot verify BaseBranch field"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-006: push_to_pull_request_branch supports base-branch configuration parameter (Section 7.3 v1.22.0)"
    fi
}
check_push_to_pr_branch_base_branch

# TYPE-007: Hang-Safety for Handler Git Operations (Section 7.3, v1.22.0)
echo "Running TYPE-007: Hang-Safety for Handler Git Operations..."
check_git_hang_safety() {
    local git_helpers="actions/setup/js/git_helpers.cjs"
    local push_handler="actions/setup/js/push_to_pull_request_branch.cjs"
    local failed=0

    # Per spec Section 7.3 (v1.22.0): Base-branch resolution MUST NOT depend on
    # interactive credential prompts; git operations issued by the handler MUST run
    # with GIT_TERMINAL_PROMPT=0 and an enforced timeout so credential-less
    # environments fail fast rather than hanging.

    if [ ! -f "$git_helpers" ]; then
        log_high "TYPE-007: git_helpers.cjs missing — hang-safety cannot be verified: $git_helpers"
        return
    fi

    # Check GIT_TERMINAL_PROMPT=0 is set in the shared git helper
    if ! grep -qE 'GIT_TERMINAL_PROMPT.*"0"|GIT_TERMINAL_PROMPT.*=.*0' "$git_helpers"; then
        log_critical "TYPE-007: GIT_TERMINAL_PROMPT=0 not set in $git_helpers — interactive prompt hang risk (Section 7.3 v1.22.0)"
        failed=1
    fi

    # Check that an enforced timeout is present in the git helper (default 60 s per spec)
    if ! grep -qE "timeout|TIMEOUT" "$git_helpers"; then
        log_critical "TYPE-007: No enforced timeout found in $git_helpers — indefinite hang risk (Section 7.3 v1.22.0)"
        failed=1
    fi

    # Confirm push_to_pull_request_branch routes git operations through git_helpers
    if [ -f "$push_handler" ]; then
        if ! grep -q "git_helpers" "$push_handler"; then
            log_high "TYPE-007: push_to_pull_request_branch does not import git_helpers.cjs — hang-safety coverage uncertain (Section 7.3 v1.22.0)"
            failed=1
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-007: Git operations enforce GIT_TERMINAL_PROMPT=0 and timeout for hang-safety (Section 7.3 v1.22.0)"
    fi
}
check_git_hang_safety

# TYPE-008: create_check_run Handler Existence and Dual-Permission Profile (Section 7.3, v1.23.0)
echo "Running TYPE-008: create_check_run Handler and Dual-Permission Profile..."
check_create_check_run_handler() {
    local handler="actions/setup/js/create_check_run.cjs"
    local go_config="pkg/workflow/create_check_run.go"
    local handler_registry="pkg/workflow/safe_output_handlers.go"
    local failed=0

    # Per spec Section 7.3 (v1.23.0): create_check_run handler must exist
    if [ ! -f "$handler" ]; then
        log_high "TYPE-008: create_check_run handler missing: $handler"
        failed=1
    fi

    # Per spec Section 7.3: Go config struct must exist with Target field support
    if [ ! -f "$go_config" ]; then
        log_high "TYPE-008: create_check_run Go config file missing: $go_config"
        failed=1
    elif ! grep -q "Target" "$go_config"; then
        log_high "TYPE-008: create_check_run Go config missing Target field for PR targeting (Section 7.3 v1.23.0)"
        failed=1
    fi

    # Per spec Section 7.3 dual-permission profile: checks:write without target,
    # adds pull-requests:read when target is configured.
    if [ -f "$handler_registry" ]; then
        if ! grep -q "NewPermissionsChecksWrite()" "$handler_registry"; then
            log_critical "TYPE-008: create_check_run dual-permission profile missing checks:write base permission (Section 7.3 v1.23.0)"
            failed=1
        fi
        if ! grep -q "NewPermissionsChecksWritePRRead" "$handler_registry"; then
            log_high "TYPE-008: create_check_run dual-permission profile missing pull-requests:read when target configured (Section 7.3 v1.23.0)"
            failed=1
        fi
        # Verify the target-conditional branch exists in the registry
        create_check_run_section=$(grep -A 15 '"create-check-run"' "$handler_registry" 2>/dev/null || true)
        if ! grep -q "Target" <<< "$create_check_run_section"; then
            log_high "TYPE-008: create_check_run permission builder does not branch on Target field (Section 7.3 v1.23.0)"
            failed=1
        fi
    else
        log_medium "TYPE-008: Handler registry file missing: $handler_registry — cannot verify dual-permission profile"
        failed=1
    fi

    # Per spec Section 7.3: SHA resolution must fall back to GITHUB_SHA / context.sha
    if [ -f "$handler" ]; then
        if ! grep -qE "GITHUB_SHA|context\.sha" "$handler"; then
            log_high "TYPE-008: create_check_run handler missing GITHUB_SHA / context.sha fallback for SHA resolution (Section 7.3 v1.23.0)"
            failed=1
        fi
    fi

    # Per spec Section 7.3: staged mode must skip the Checks API call
    if [ -f "$handler" ]; then
        if ! grep -q "isStaged\|isStagedMode\|staged" "$handler"; then
            log_high "TYPE-008: create_check_run handler missing staged mode handling (Section 7.3 v1.23.0)"
            failed=1
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-008: create_check_run handler exists with dual-permission profile and staged mode support (Section 7.3 v1.23.0)"
    fi
}
check_create_check_run_handler

# TYPE-009: add_comment discussions Permission Opt-In Default (Section 7.1, v1.24.0)
echo "Running TYPE-009: add_comment discussions Permission Opt-In Default..."
check_add_comment_discussions_optin() {
    local go_config="pkg/workflow/add_comment.go"
    local tools_json="pkg/workflow/js/safe_outputs_tools.json"
    local handler_registry="pkg/workflow/safe_output_handlers.go"
    local failed=0

    # Per spec Section 7.1 (v1.24.0): discussions:write permission is opt-in for add_comment.
    # Default (nil or false) must EXCLUDE discussions:write.

    # Check Go struct comment documents "Default (nil or false) excludes discussions:write"
    if [ -f "$go_config" ]; then
        if ! grep -qE "nil.*false.*excludes.*discussions|false.*excludes.*discussions:write|Default.*nil.*false.*excludes" "$go_config"; then
            log_high "TYPE-009: add_comment Go config does not document that default excludes discussions:write (Section 7.1 v1.24.0)"
            failed=1
        fi
    else
        log_high "TYPE-009: add_comment Go config missing: $go_config"
        failed=1
    fi

    # Check tool description discloses the opt-in requirement to the agent
    if [ -f "$tools_json" ]; then
        if ! grep -iE "discussions.*opt.in|opt.in.*discussions|discussions.*false|not.*require.*discussions.*write|default.*not.*discussions" "$tools_json"; then
            log_medium "TYPE-009: Tool description in $tools_json may not disclose opt-in requirement for discussions:write (Section 7.1 v1.24.0)"
            failed=1
        fi
    else
        log_medium "TYPE-009: Tool definitions file missing: $tools_json"
        failed=1
    fi

    # Check permissions test confirms default excludes discussions (verifying behavioural test coverage)
    local perm_test="pkg/workflow/safe_outputs_permissions_test.go"
    if [ -f "$perm_test" ]; then
        if ! grep -qiE "default.*excludes.*discussions|excludes.*discussions.*default|add-comment.*default.*pull-requests.*discussions" "$perm_test"; then
            log_low "TYPE-009: No test case confirming default exclusion of discussions:write for add_comment (Section 7.1 v1.24.0)"
            failed=1
        fi
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-009: add_comment discussions:write is opt-in by default as required (Section 7.1 v1.24.0)"
    fi
}
check_add_comment_discussions_optin

# TYPE-010: Wildcard Target Requirements Validation (Section 7.0.1)
echo "Running TYPE-010: Wildcard Target Requirements Validation..."
check_wildcard_target_requirements() {
    local gateway_handler="actions/setup/js/safe_outputs_handlers.cjs"
    local tools_json="pkg/workflow/js/safe_outputs_tools.json"
    local failed=0

    # Per spec Section 7.0.1: When a safe-output type is configured with target: "*",
    # the MCP gateway MUST validate requests against x-safe-outputs-target-requirements["*"]
    # before recording or executing intent. If no anyOf field is present, the request
    # MUST be rejected with an MCP validation error.

    if [ ! -f "$gateway_handler" ]; then
        log_high "TYPE-010: MCP gateway handler missing: $gateway_handler"
        return
    fi

    # Check that validateWildcardTargetRequirement function is defined in the gateway
    if ! grep -q "validateWildcardTargetRequirement" "$gateway_handler"; then
        log_critical "TYPE-010: Gateway handler missing wildcard target requirements validation function (Section 7.0.1)"
        failed=1
    fi

    # Check that the validation error is returned early before recording the operation
    wildcard_validation_section=$(grep -A 3 "validateWildcardTargetRequirement" "$gateway_handler" 2>/dev/null || true)
    if ! grep -q "return wildcardTargetValidationError" <<< "$wildcard_validation_section"; then
        log_critical "TYPE-010: Wildcard target validation error is not returned early before recording (Section 7.0.1)"
        failed=1
    fi

    # Check that at least some tools declare x-safe-outputs-target-requirements in the tools JSON
    if [ -f "$tools_json" ]; then
        if ! grep -q '"x-safe-outputs-target-requirements"' "$tools_json"; then
            log_high "TYPE-010: No tools define x-safe-outputs-target-requirements in $tools_json (Section 7.0.1)"
            failed=1
        fi
    else
        log_high "TYPE-010: Tool definitions file missing: $tools_json"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-010: Wildcard target requirements validation is implemented and enforced before recording (Section 7.0.1)"
    fi
}
check_wildcard_target_requirements

# TYPE-011: close_entity allow-body Enforcement (Section 7.3)
echo "Running TYPE-011: close_entity allow-body Enforcement..."
check_close_entity_allow_body() {
    local helpers_file="actions/setup/js/close_entity_helpers.cjs"
    local go_tools_gen="pkg/workflow/safe_outputs_tools_generation.go"
    local failed=0

    # Per spec Section 7.3 (close_issue, close_discussion): When allow-body: false is configured:
    # 1. The handler MUST NOT post a closing comment; any body SHALL be discarded.
    # 2. The implementation MUST log a warning if a non-empty body value was discarded.
    # 3. The body field MUST be removed from the inputSchema required array at compile time.
    # 4. The tool description MUST be amended with the constraint "Closing comments are disabled".

    if [ ! -f "$helpers_file" ]; then
        log_high "TYPE-011: Close entity helpers file missing: $helpers_file"
        failed=1
    else
        # Check allow-body guard exists (requirement 1: handler MUST NOT post comment)
        if ! grep -qE "allow_body|allowBody" "$helpers_file"; then
            log_high "TYPE-011: Close entity helpers missing allow-body enforcement (Section 7.3 requirement 1)"
            failed=1
        fi

        # Check warning is logged when non-empty body is discarded (requirement 2: MUST log warning)
        if ! grep -qE "core\.warning.*allow-body|allow-body.*warning" "$helpers_file"; then
            log_high "TYPE-011: Close entity helpers missing warning log for discarded body (Section 7.3 requirement 2)"
            failed=1
        fi
    fi

    # Check compile-time removal of body from required array (requirement 3)
    if [ -f "$go_tools_gen" ]; then
        if ! grep -q "computeRequiredFieldRemovals" "$go_tools_gen"; then
            log_high "TYPE-011: Go tools generation missing computeRequiredFieldRemovals for allow-body: false (Section 7.3 requirement 3)"
            failed=1
        fi
    else
        log_medium "TYPE-011: Go tools generation file missing: $go_tools_gen"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-011: close_entity allow-body: false discards body with warning and removes field from schema (Section 7.3)"
    fi
}
check_close_entity_allow_body

# TYPE-012: Fork-Backed Pull Request Semantics (Section 7.1, v1.26.0)
echo "Running TYPE-012: Fork-Backed Pull Request Semantics..."
check_fork_backed_pr_semantics() {
    local pr_handler="actions/setup/js/create_pull_request.cjs"
    local push_handler="actions/setup/js/push_to_pull_request_branch.cjs"
    local compiler_job="pkg/workflow/compiler_safe_outputs_job.go"
    local failed=0

    # Per spec Section 7.1 v1.26.0:
    # 1. head-repo MUST be validated against the configured allowlist (same as target-repo).
    # 2. When head-repo differs from target-repo, the PR MUST use an owner-qualified head ref.
    # 3. Implicit reuse of arbitrary pre-existing fork branches MUST NOT occur.
    # 4. head-github-app takes precedence over head-github-token when both are configured.
    # 5. Successful executions MUST record head_repo in the safe-output summary and manifest.
    # 6. push_to_pull_request_branch follow-up is limited to PRs whose head repo matches head-repo.

    if [ ! -f "$pr_handler" ]; then
        log_high "TYPE-012: create_pull_request handler missing: $pr_handler"
        failed=1
    else
        # Check head-repo is resolved and validated against allowlist (requirement 1)
        if ! grep -qE "resolveAndValidateRepo.*head.repo|head.*repo.*allowedRepos|headRepo.*allowlist" "$pr_handler"; then
            log_high "TYPE-012: create_pull_request handler does not validate head-repo against allowlist (Section 7.1 v1.26.0 requirement 1)"
            failed=1
        fi

        # Check owner-qualified head ref is used when repos differ (requirement 2)
        # The pattern `owner:branch` is the owner-qualified form used by GitHub API
        if ! grep -qE "getPullRequestHeadRef|pushRepoParts\.owner.*branch|owner.*:.*branch" "$pr_handler"; then
            log_high "TYPE-012: create_pull_request handler missing owner-qualified head reference for fork-backed PRs (Section 7.1 v1.26.0 requirement 2)"
            failed=1
        fi

        # Check head_repo is recorded in summary/manifest (requirement 5)
        if ! grep -q "head_repo" "$pr_handler"; then
            log_high "TYPE-012: create_pull_request handler does not record head_repo in summary/manifest (Section 7.1 v1.26.0 requirement 5)"
            failed=1
        fi
    fi

    # Check push_to_pull_request_branch restricts to configured head-repo (requirement 6)
    if [ -f "$push_handler" ]; then
        if ! grep -qE "head.repo|headRepo|head_repo" "$push_handler"; then
            log_high "TYPE-012: push_to_pull_request_branch handler does not enforce head-repo restriction (Section 7.1 v1.26.0 requirement 6)"
            failed=1
        fi
    else
        log_medium "TYPE-012: push_to_pull_request_branch handler missing: $push_handler"
        failed=1
    fi

    # Check head-github-app precedence over head-github-token is implemented (requirement 4)
    if [ -f "$compiler_job" ]; then
        if ! grep -qE "HeadGitHubApp|head.github.app|headGithubApp" "$compiler_job"; then
            log_high "TYPE-012: Compiler missing head-github-app support for fork credential precedence (Section 7.1 v1.26.0 requirement 4)"
            failed=1
        fi
    else
        log_medium "TYPE-012: Compiler job file missing: $compiler_job"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-012: Fork-backed PR semantics implemented: allowlist, owner-qualified refs, head_repo provenance, and head-github-app precedence (Section 7.1 v1.26.0)"
    fi
}
check_fork_backed_pr_semantics

# TYPE-013: approve_workflow_run Handler Semantics (Section 7.3, v1.28.4)
echo "Running TYPE-013: approve_workflow_run Handler Semantics..."
check_approve_workflow_run_semantics() {
    local handler="actions/setup/js/approve_workflow_run.cjs"
    local manager="actions/setup/js/safe_output_handler_manager.cjs"
    local failed=0

    # Per spec Section 7.3 (approve_workflow_run, v1.28.4):
    # 1. run_id MUST be validated as a positive safe integer.
    # 2. Staged mode MUST preview without reading GitHub state or consuming max limit.
    # 3. Eligibility requires event=pull_request, non-empty pull_requests, status=waiting.
    # 4. Workflow filename MUST match an allowed-workflows pattern (yml/yaml equivalence).
    # 5. Every associated pull request MUST be the triggering PR or explicitly allowed.
    # 6. pull_request_target events and unapproved fork pull requests MUST be rejected.
    # 7. Protected files on associated pull requests MUST block approval.
    # 8. Live approvals MUST require an explicit external github-token or GitHub App token.
    # 9. Handler MUST be classified as an Abort type for warn-mode threat-detection failures.

    if [ ! -f "$handler" ]; then
        log_high "TYPE-013: approve_workflow_run handler missing: $handler"
        failed=1
    else
        if ! grep -qE "parsePositiveInt" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not validate run_id as a positive integer (Section 7.3 requirement 1)"
            failed=1
        fi

        if ! grep -qE "isStagedMode|isStaged" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler missing staged-mode preview support (Section 7.3 requirement 2)"
            failed=1
        fi

        if ! grep -qE "run\.event\s*!==\s*[\"']pull_request[\"']" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not verify event is pull_request (Section 7.3 requirement 3)"
            failed=1
        fi

        if ! grep -qE "isAwaitingApproval|APPROVABLE_RUN_STATUSES|run\.status\s*!==\s*[\"']waiting[\"']" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not verify run status is waiting (Section 7.3 requirement 3)"
            failed=1
        fi

        if ! grep -qE "isAllowedWorkflow|allowed_workflows" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not validate allowed-workflows (Section 7.3 requirement 4)"
            failed=1
        fi

        if ! grep -qE "\.replace\(/\\\\\.yaml\\\$/i" "$handler"; then
            log_medium "TYPE-013: approve_workflow_run handler may not normalize .yml/.yaml equivalence (Section 7.3 requirement 4)"
            failed=1
        fi

        if ! grep -qE "allowed_pull_requests|allowedPullRequests|currentPullRequest" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not authorize pull requests against triggering/allowed set (Section 7.3 requirement 5)"
            failed=1
        fi

        if ! grep -qE "pull_request_target" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not reject pull_request_target events (Section 7.3 requirement 6)"
            failed=1
        fi

        if ! grep -qE "isHeadRepoAllowed|allowed_repos|allowedRepos|isForkPullRequest|config\.fork" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not reject fork pull requests unless allowed via allowed-repos (Section 7.3 requirement 6)"
            failed=1
        fi

        if ! grep -qE "checkFileProtectionPostApply|getModifiedPullRequestFiles" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not check protected files before approval (Section 7.3 requirement 7)"
            failed=1
        fi

        if ! grep -qE "github-token.*requires an external|requires an external github-token" "$handler"; then
            log_high "TYPE-013: approve_workflow_run handler does not require an explicit external github-token/App token (Section 7.3 requirement 8)"
            failed=1
        fi
    fi

    if [ -f "$manager" ]; then
        if ! grep -qE "THREAT_WARNING_ABORT_TYPES" "$manager" || ! sed -n '/THREAT_WARNING_ABORT_TYPES = new Set(\[/,/\]);/p' "$manager" | grep -q "approve_workflow_run"; then
            log_high "TYPE-013: approve_workflow_run is not classified as an Abort type for warn-mode threat detection (Section 7.3 requirement 9)"
            failed=1
        fi
    else
        log_medium "TYPE-013: Safe output handler manager missing: $manager"
        failed=1
    fi

    if [ $failed -eq 0 ]; then
        log_pass "TYPE-013: approve_workflow_run implements run-id validation, staged preview, eligibility, workflow/PR authorization, fork rejection, protected-file checks, explicit credentials, and Abort classification (Section 7.3 v1.28.4)"
    fi
}
check_approve_workflow_run_semantics

# Summary
echo ""
echo "=================================================="
echo "Conformance Check Summary"
echo "=================================================="
echo -e "${RED}Critical Failures:${NC} $CRITICAL_FAILURES"
echo -e "${RED}High Failures:${NC} $HIGH_FAILURES"
echo -e "${YELLOW}Medium Failures:${NC} $MEDIUM_FAILURES"
echo -e "${BLUE}Low Failures:${NC} $LOW_FAILURES"
echo ""

# Exit code based on failures
if [ $CRITICAL_FAILURES -gt 0 ]; then
    echo -e "${RED}FAIL:${NC} Critical conformance issues found"
    exit 2
elif [ $HIGH_FAILURES -gt 0 ]; then
    echo -e "${RED}FAIL:${NC} High priority conformance issues found"
    exit 1
elif [ $MEDIUM_FAILURES -gt 0 ]; then
    echo -e "${YELLOW}WARN:${NC} Medium priority conformance issues found"
    exit 0
else
    echo -e "${GREEN}PASS:${NC} All checks passed"
    exit 0
fi
