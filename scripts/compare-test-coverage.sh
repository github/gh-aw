#!/bin/bash
# Compare all tests vs executed tests to find any missing test coverage
# This ensures all tests are being run in CI unless explicitly skipped

set -euo pipefail

if [ $# -ne 2 ]; then
  echo "Usage: $0 <all-tests-file> <executed-tests-file>"
  echo "Compares the two lists and reports any missing tests"
  exit 1
fi

ALL_TESTS_FILE="$1"
EXECUTED_TESTS_FILE="$2"

if [ ! -f "$ALL_TESTS_FILE" ]; then
  echo "Error: All tests file $ALL_TESTS_FILE does not exist"
  exit 1
fi

if [ ! -f "$EXECUTED_TESTS_FILE" ]; then
  echo "Error: Executed tests file $EXECUTED_TESTS_FILE does not exist"
  exit 1
fi

echo "📊 Test Coverage Analysis"
echo "========================="
echo ""

ALL_COUNT=$(wc -l < "$ALL_TESTS_FILE")
EXECUTED_COUNT=$(wc -l < "$EXECUTED_TESTS_FILE")

echo "Total tests defined in codebase: $ALL_COUNT"
echo "Tests executed in CI: $EXECUTED_COUNT"
echo ""

# Find tests that are defined but not executed
MISSING_TESTS=$(comm -23 "$ALL_TESTS_FILE" "$EXECUTED_TESTS_FILE")

if [ -z "$MISSING_TESTS" ]; then
  echo "✅ SUCCESS: All tests are being executed in CI!"
  echo ""
  echo "Test coverage: 100% ($EXECUTED_COUNT/$ALL_COUNT tests executed)"
  exit 0
else
  MISSING_COUNT=$(echo "$MISSING_TESTS" | wc -l)
  COVERAGE_PERCENT=$(awk "BEGIN {printf \"%.1f\", ($EXECUTED_COUNT / $ALL_COUNT) * 100}")
  
  echo "❌ FAILURE: Found $MISSING_COUNT tests that are NOT being executed in CI"
  echo ""
  echo "Test coverage: $COVERAGE_PERCENT% ($EXECUTED_COUNT/$ALL_COUNT tests executed)"
  echo ""
  echo "Missing tests:"
  echo "=============="
  echo "$MISSING_TESTS" | head -20
  
  if [ "$MISSING_COUNT" -gt 20 ]; then
    echo "... and $((MISSING_COUNT - 20)) more"
  fi
  
  echo ""
  echo "These tests are defined in *_test.go files but were not executed"
  echo "in any of the test jobs. Please either:"
  echo "  1. Add them to the appropriate test job pattern, or"
  echo "  2. Remove them if they are obsolete"
  
  exit 1
fi
