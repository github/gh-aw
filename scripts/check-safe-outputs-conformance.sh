#!/bin/bash
# Safe Outputs Specification Conformance Checker
# This script implements automated checks for the Safe Outputs specification
# Based on findings from docs/spec-review-findings.md

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
    ((CRITICAL_FAILURES++))
}

log_high() {
    echo -e "${RED}[HIGH]${NC} $1"
    ((HIGH_FAILURES++))
}

log_medium() {
    echo -e "${YELLOW}[MEDIUM]${NC} $1"
    ((MEDIUM_FAILURES++))
}

log_low() {
    echo -e "${BLUE}[LOW]${NC} $1"
    ((LOW_FAILURES++))
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
        if grep -A 50 "^jobs:" "$workflow" | grep -A 20 "^\s*agent:" | grep -qE "issues:\s*write|pull-requests:\s*write|contents:\s*write"; then
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
        
        # Check if handler enforces max limits
        if ! grep -q "\.length.*>.*\.max\|enforceMaxLimit\|checkLimit\|max.*exceeded" "$handler"; then
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
    
    for handler in actions/setup/js/*.cjs; do
        # Skip test and utility files
        [[ "$handler" =~ (test|parse|buffer) ]] && continue
        
        # Check if handler has body/content fields
        if grep -q "\"body\"\|body:" "$handler"; then
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
        
        # Check if handler supports target-repo
        if grep -q "target.*[Rr]epo\|targetRepo" "$handler"; then
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
        # Skip test files
        [[ "$handler" =~ test ]] && continue
        
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
        # Skip test files
        [[ "$handler" =~ test ]] && continue
        
        # Check if handler has staged mode
        if grep -q "staged.*true\|isStaged\|GH_AW_SAFE_OUTPUTS_STAGED" "$handler"; then
            # Check for emoji in preview
            if ! grep -q "🎭\|Staged Mode.*Preview" "$handler"; then
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
    local spec_file="docs/src/content/docs/reference/safe-outputs-specification.md"
    local failed=0
    
    # Check key sections have RFC 2119 keywords
    for section in "Security Architecture" "Configuration Semantics" "Execution Guarantees"; do
        if ! grep -A 200 "## .*$section" "$spec_file" 2>/dev/null | grep -q "MUST\|SHALL\|SHOULD\|MAY"; then
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
    local spec_file="docs/src/content/docs/reference/safe-outputs-specification.md"
    local failed=0
    
    # Extract type names
    grep "^#### Type:" "$spec_file" 2>/dev/null | sed 's/^#### Type: //' | head -10 | while read -r type_name; do
        sections_found=0
        
        # Check for required sections
        for section in "MCP Tool Schema" "Operational Semantics" "Configuration Parameters" "Security Requirements" "Required Permissions"; do
            if grep -A 200 "^#### Type: $type_name" "$spec_file" 2>/dev/null | grep -q "**$section**"; then
                ((sections_found++))
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
    local spec_file="docs/src/content/docs/reference/safe-outputs-specification.md"
    local failed=0
    
    # Check key requirements have verification methods
    for req in "AR1" "AR2" "AR3" "SP1" "SP2" "SP3"; do
        if ! grep -A 30 "**Requirement $req:\|**Property $req:" "$spec_file" 2>/dev/null | grep -q "Verification:\|Formal Definition:"; then
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
        # Basic check that it defines computePermissionsForSafeOutputs
        if grep -q "computePermissionsForSafeOutputs" "pkg/workflow/safe_outputs_permissions.go"; then
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
    
    # Check if safe outputs config Go file exists
    if [ -f "pkg/workflow/safe_outputs_config.go" ]; then
        # Check for schema generation functions
        if ! grep -q "generateSchema\|buildSchema\|toolSchema" "pkg/workflow/safe_outputs_config.go"; then
            log_medium "IMP-003: Schema generation functions may be missing"
            failed=1
        fi
    else
        log_medium "IMP-003: Safe outputs config file missing"
        failed=1
    fi
    
    if [ $failed -eq 0 ]; then
        log_pass "IMP-003: Schema generation is implemented"
    fi
}
check_schema_consistency

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
