#!/bin/bash
# Dependabot PR Approval and Merge Script
# This script should be run by a user or bot with appropriate GitHub permissions
#
# Prerequisites:
# - GH_TOKEN environment variable set with repo access
# - gh CLI installed and authenticated
#
# Usage:
#   export GH_TOKEN="your_token_here"
#   bash scripts/merge_dependabot_prs.sh

set -e

echo "🔍 Dependabot PR Review and Merge Script"
echo "=========================================="
echo ""

# Check prerequisites
if ! command -v gh &> /dev/null; then
    echo "❌ Error: gh CLI is not installed"
    echo "   Install from: https://cli.github.com/"
    exit 1
fi

if [ -z "$GH_TOKEN" ]; then
    echo "⚠️  Warning: GH_TOKEN not set. Attempting to use gh auth status..."
    if ! gh auth status &> /dev/null; then
        echo "❌ Error: Not authenticated with GitHub"
        echo "   Run: gh auth login"
        exit 1
    fi
fi

# Change to repo root
cd "$(git rev-path --show-toplevel)"

echo "📋 Review Summary:"
echo "  - PR #13784: fast-xml-parser 5.3.3 → 5.3.4 (patch)"
echo "  - PR #13453: astro 5.16.12 → 5.17.1 (minor)"
echo ""

# Approve and merge PR #13784 (fast-xml-parser)
echo "📦 Processing PR #13784: fast-xml-parser"
echo "   Status: Approving..."

gh pr review 13784 --approve --body "## ✅ Approved - Safe to Merge

### Review Summary
- **Update Type**: Patch version (5.3.3 → 5.3.4)
- **CI Status**: ✅ All checks passed
- **Breaking Changes**: None
- **Changes**: Bug fix for HTML numeric and hex entities when out of range
- **Testing**: Doc build workflow passed successfully

### Analysis
This is a straightforward patch release that fixes handling of HTML entities. No breaking changes, and the fix improves robustness.

See \`DEPENDABOT_REVIEW_2026_02_06.md\` for detailed analysis."

echo "   Status: Enabling auto-merge (squash)..."
gh pr merge 13784 --squash --auto

echo "   ✅ PR #13784 approved and queued for merge"
echo ""

# Approve and merge PR #13453 (astro)
echo "📦 Processing PR #13453: astro"
echo "   Status: Approving..."

gh pr review 13453 --approve --body "## ✅ Approved - Safe to Merge

### Review Summary
- **Update Type**: Minor version (5.16.12 → 5.17.1)
- **CI Status**: ✅ All checks passed
- **Breaking Changes**: None affecting this project
  - Only breaking change is to experimental Fonts API which we don't use
- **New Features**: 
  - Async parser support for Content Layer API
  - Kernel configuration option for Sharp image service
- **Testing**: Doc build workflow passed successfully

### Analysis
Safe minor update with useful new features. The breaking change only affects experimental APIs not used in this project.

See \`DEPENDABOT_REVIEW_2026_02_06.md\` for detailed analysis."

echo "   Status: Enabling auto-merge (squash)..."
gh pr merge 13453 --squash --auto

echo "   ✅ PR #13453 approved and queued for merge"
echo ""

echo "✅ All Dependabot PRs processed!"
echo ""
echo "📝 Next Steps:"
echo "   - PRs will auto-merge once all checks pass"
echo "   - Monitor merges at: https://github.com/github/gh-aw/pulls"
echo "   - Update tracking issue after successful merge"
