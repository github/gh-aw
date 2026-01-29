#!/usr/bin/env bash
# Tests for sanitize_path.sh
# Run: bash sanitize_path_test.sh

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
SANITIZE_SCRIPT="${SCRIPT_DIR}/sanitize_path.sh"

# Test counter
TESTS_PASSED=0
TESTS_FAILED=0

# Test helper function
test_sanitize() {
  local name="$1"
  local input="$2"
  local expected="$3"
  
  # Run the sanitize script in a subshell to capture the result
  local result
  result=$(bash -c "source '$SANITIZE_SCRIPT' '$input' && echo \"\$PATH\"" 2>&1) || true
  
  if [ "$result" = "$expected" ]; then
    echo "✓ $name"
    TESTS_PASSED=$((TESTS_PASSED + 1))
  else
    echo "✗ $name"
    echo "  Input:    '$input'"
    echo "  Expected: '$expected'"
    echo "  Got:      '$result'"
    TESTS_FAILED=$((TESTS_FAILED + 1))
  fi
}

echo "Running sanitize_path.sh tests..."
echo

# Test cases
test_sanitize "already clean PATH" "/usr/bin:/usr/local/bin" "/usr/bin:/usr/local/bin"
test_sanitize "leading colon" ":/usr/bin:/usr/local/bin" "/usr/bin:/usr/local/bin"
test_sanitize "trailing colon" "/usr/bin:/usr/local/bin:" "/usr/bin:/usr/local/bin"
test_sanitize "multiple leading colons" ":::/usr/bin:/usr/local/bin" "/usr/bin:/usr/local/bin"
test_sanitize "multiple trailing colons" "/usr/bin:/usr/local/bin:::" "/usr/bin:/usr/local/bin"
test_sanitize "internal empty elements" "/usr/bin::/usr/local/bin" "/usr/bin:/usr/local/bin"
test_sanitize "multiple internal empty elements" "/usr/bin:::/usr/local/bin" "/usr/bin:/usr/local/bin"
test_sanitize "combined leading trailing and internal" ":/usr/bin:::/usr/local/bin:" "/usr/bin:/usr/local/bin"
test_sanitize "all colons" ":::" ""
test_sanitize "empty string" "" ""
test_sanitize "single path no colons" "/usr/bin" "/usr/bin"

echo
echo "Tests passed: $TESTS_PASSED"
echo "Tests failed: $TESTS_FAILED"

if [ "$TESTS_FAILED" -gt 0 ]; then
  exit 1
fi

echo "✓ All tests passed!"
