#!/bin/bash
# check-file-sizes.sh - Monitor Go file sizes and function counts
#
# This script identifies Go files that exceed recommended thresholds:
# - 50+ functions: Consider splitting (warning)
# - 40-49 functions: Approaching threshold (info)
#
# Exit codes:
#   0 - No issues or only informational warnings
#   Non-zero is never returned (non-blocking check)

set -euo pipefail

# Colors for output
RED='\033[0;31m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
GREEN='\033[0;32m'
NC='\033[0m' # No Color

# Thresholds
WARN_THRESHOLD=50
INFO_THRESHOLD=40

# Counter for warnings
warn_count=0
info_count=0

echo "Checking Go file sizes in pkg/ directory..."
echo ""

# Find all .go files (excluding test files) and count functions
while IFS= read -r file; do
    # Count function declarations (lines starting with "func ")
    func_count=$(grep -c "^func " "$file" || true)
    line_count=$(wc -l < "$file")
    
    # Only report files with 40+ functions
    if [ "$func_count" -ge "$WARN_THRESHOLD" ]; then
        echo -e "${RED}⚠️  WARNING${NC}: $file"
        echo -e "   Functions: ${RED}$func_count${NC} | Lines: $line_count"
        echo -e "   Consider splitting this file (threshold: ${WARN_THRESHOLD} functions)"
        echo ""
        warn_count=$((warn_count + 1))
    elif [ "$func_count" -ge "$INFO_THRESHOLD" ]; then
        echo -e "${YELLOW}ℹ️  INFO${NC}: $file"
        echo -e "   Functions: ${YELLOW}$func_count${NC} | Lines: $line_count"
        echo -e "   Approaching threshold (${INFO_THRESHOLD}-${WARN_THRESHOLD} functions)"
        echo ""
        info_count=$((info_count + 1))
    fi
done < <(find pkg -name "*.go" ! -name "*_test.go" -type f | sort)

# Summary
echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo "Summary:"
echo ""

if [ "$warn_count" -eq 0 ] && [ "$info_count" -eq 0 ]; then
    echo -e "${GREEN}✓ All files are within recommended size guidelines${NC}"
    echo ""
    echo "No files exceed the 40-function threshold."
elif [ "$warn_count" -eq 0 ]; then
    echo -e "${BLUE}ℹ️  $info_count file(s) approaching threshold (${INFO_THRESHOLD}-${WARN_THRESHOLD} functions)${NC}"
    echo ""
    echo "These files are getting large but still within acceptable range."
    echo "Consider reviewing them if adding significant functionality."
else
    echo -e "${YELLOW}⚠️  $warn_count file(s) exceed recommended size (${WARN_THRESHOLD}+ functions)${NC}"
    if [ "$info_count" -gt 0 ]; then
        echo -e "${BLUE}ℹ️  $info_count file(s) approaching threshold (${INFO_THRESHOLD}-${WARN_THRESHOLD} functions)${NC}"
    fi
    echo ""
    echo "Files exceeding ${WARN_THRESHOLD} functions should be evaluated for splitting."
    echo "However, domain complexity may justify larger files."
    echo ""
    echo "See scratchpad/code-organization.md for file size guidelines and justified large files."
fi

echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
echo ""
echo "Note: This is an informational check only. Large files may be justified"
echo "by domain complexity. See scratchpad/code-organization.md for guidelines."

# Always exit 0 (non-blocking)
exit 0
