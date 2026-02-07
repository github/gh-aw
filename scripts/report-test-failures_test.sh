#!/bin/bash
# Test script for report-test-failures.sh

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
REPORT_SCRIPT="$SCRIPT_DIR/report-test-failures.sh"
TEST_DIR=$(mktemp -d)

cleanup() {
  rm -rf "$TEST_DIR"
}
trap cleanup EXIT

echo "Testing report-test-failures.sh"
echo "================================"
echo ""

# Test 1: No failures (should exit 0)
echo "Test 1: No failures"
cat > "$TEST_DIR/no-failures.json" << 'EOF'
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"pass","Package":"github.com/github/gh-aw/pkg/workflow","Test":"TestSomething","Elapsed":0.01}
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"pass","Package":"github.com/github/gh-aw/pkg/workflow","Elapsed":0.5}
EOF

if "$REPORT_SCRIPT" "$TEST_DIR/no-failures.json" > /dev/null 2>&1; then
  echo "✅ PASS: Correctly reported no failures"
else
  echo "❌ FAIL: Should exit 0 when no failures"
  exit 1
fi
echo ""

# Test 2: Individual test failure
echo "Test 2: Individual test failure"
cat > "$TEST_DIR/individual-failure.json" << 'EOF'
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"run","Package":"github.com/github/gh-aw/pkg/workflow","Test":"TestSomething"}
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"output","Package":"github.com/github/gh-aw/pkg/workflow","Test":"TestSomething","Output":"=== RUN   TestSomething\n"}
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"output","Package":"github.com/github/gh-aw/pkg/workflow","Test":"TestSomething","Output":"    test.go:123: expected 5, got 3\n"}
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"fail","Package":"github.com/github/gh-aw/pkg/workflow","Test":"TestSomething","Elapsed":0.01}
{"Time":"2026-02-07T04:43:04.672198249Z","Action":"fail","Package":"github.com/github/gh-aw/pkg/workflow","Elapsed":10.837}
EOF

if "$REPORT_SCRIPT" "$TEST_DIR/individual-failure.json" > /tmp/test2-output.txt 2>&1; then
  echo "❌ FAIL: Should exit 1 when failures found"
  exit 1
else
  if grep -q "TestSomething" /tmp/test2-output.txt && grep -q "test.go:123" /tmp/test2-output.txt; then
    echo "✅ PASS: Correctly detected and reported individual test failure"
  else
    echo "❌ FAIL: Missing expected failure details"
    cat /tmp/test2-output.txt
    exit 1
  fi
fi
echo ""

# Test 3: Package-level failure (no individual test)
echo "Test 3: Package-level failure only"
cat > "$TEST_DIR/package-failure.json" << 'EOF'
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"pass","Package":"github.com/github/gh-aw/pkg/workflow","Test":"TestA","Elapsed":0.01}
{"Time":"2026-02-07T04:43:04.669870648Z","Action":"output","Package":"github.com/github/gh-aw/pkg/workflow","Output":"FAIL\n"}
{"Time":"2026-02-07T04:43:04.672171709Z","Action":"output","Package":"github.com/github/gh-aw/pkg/workflow","Output":"FAIL\tgithub.com/github/gh-aw/pkg/workflow\t10.837s\n"}
{"Time":"2026-02-07T04:43:04.672198249Z","Action":"fail","Package":"github.com/github/gh-aw/pkg/workflow","Elapsed":10.837}
EOF

if "$REPORT_SCRIPT" "$TEST_DIR/package-failure.json" > /tmp/test3-output.txt 2>&1; then
  echo "❌ FAIL: Should exit 1 when failures found"
  exit 1
else
  if grep -q "Package-level Failure" /tmp/test3-output.txt && grep -q "No individual test marked as failed" /tmp/test3-output.txt; then
    echo "✅ PASS: Correctly detected and reported package-level failure"
  else
    echo "❌ FAIL: Missing expected package-level failure details"
    cat /tmp/test3-output.txt
    exit 1
  fi
fi
echo ""

# Test 4: Multiple files
echo "Test 4: Multiple test result files"
cat > "$TEST_DIR/file1.json" << 'EOF'
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"pass","Package":"github.com/github/gh-aw/pkg/workflow","Test":"TestA","Elapsed":0.01}
EOF

cat > "$TEST_DIR/file2.json" << 'EOF'
{"Time":"2026-02-07T04:43:04.632424046Z","Action":"fail","Package":"github.com/github/gh-aw/pkg/cli","Test":"TestB","Elapsed":0.02}
EOF

if "$REPORT_SCRIPT" "$TEST_DIR/file1.json" "$TEST_DIR/file2.json" > /tmp/test4-output.txt 2>&1; then
  echo "❌ FAIL: Should exit 1 when failures found"
  exit 1
else
  if grep -q "TestB" /tmp/test4-output.txt; then
    echo "✅ PASS: Correctly processed multiple files"
  else
    echo "❌ FAIL: Missing failure from second file"
    cat /tmp/test4-output.txt
    exit 1
  fi
fi
echo ""

# Test 5: Non-existent file
echo "Test 5: Non-existent file handling"
if "$REPORT_SCRIPT" "$TEST_DIR/nonexistent.json" > /tmp/test5-output.txt 2>&1; then
  echo "❌ FAIL: Should exit 1 when no valid files"
  exit 1
else
  if grep -q "ERROR: No valid test result files found" /tmp/test5-output.txt; then
    echo "✅ PASS: Correctly handled non-existent file"
  else
    echo "❌ FAIL: Wrong error message for non-existent file"
    cat /tmp/test5-output.txt
    exit 1
  fi
fi
echo ""

echo "================================"
echo "All tests passed! ✅"
echo ""
