#!/bin/bash
# Report test failures from JSON test result files
# Parses the JSON output from 'go test -json' format and prints failure details

set -euo pipefail

if [ $# -eq 0 ]; then
  echo "Usage: $0 <test-result-file> [test-result-file...]"
  echo "Reports test failures from JSON test result files"
  echo ""
  echo "This script extracts and displays all test failures including:"
  echo "  - Individual test failures (Action:\"fail\" with Test field)"
  echo "  - Package-level failures (Action:\"fail\" without Test field)"
  echo "  - Test output leading up to failures"
  exit 1
fi

# Track if any failures found
FAILURES_FOUND=0
TOTAL_FILES=0

for file in "$@"; do
  if [ ! -f "$file" ]; then
    echo "⚠️  Warning: File $file does not exist, skipping..."
    continue
  fi
  
  TOTAL_FILES=$((TOTAL_FILES + 1))
  
  # Extract all failure entries from the JSON log
  # Look for lines with "Action":"fail"
  FAIL_ENTRIES=$(grep '"Action":"fail"' "$file" 2>/dev/null || true)
  
  if [ -z "$FAIL_ENTRIES" ]; then
    continue
  fi
  
  FAILURES_FOUND=1
  
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "❌ FAILURES FOUND in: $file"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
  
  # Process each failure entry
  echo "$FAIL_ENTRIES" | while IFS= read -r fail_line; do
    # Extract package name
    PACKAGE=$(echo "$fail_line" | grep -o '"Package":"[^"]*"' | sed 's/"Package":"\([^"]*\)"/\1/' || echo "unknown")
    
    # Extract test name (if present)
    TEST_NAME=$(echo "$fail_line" | grep -o '"Test":"[^"]*"' | sed 's/"Test":"\([^"]*\)"/\1/' || echo "")
    
    # Extract elapsed time (if present)
    ELAPSED=$(echo "$fail_line" | grep -o '"Elapsed":[0-9.]*' | sed 's/"Elapsed"://' || echo "")
    
    if [ -n "$TEST_NAME" ]; then
      echo "📍 Test Failure:"
      echo "   Package: $PACKAGE"
      echo "   Test:    $TEST_NAME"
      if [ -n "$ELAPSED" ]; then
        echo "   Elapsed: ${ELAPSED}s"
      fi
      echo ""
      
      # Try to extract the last few output lines before this failure
      # This helps show the actual error message
      echo "   Recent test output:"
      grep "\"Test\":\"$TEST_NAME\"" "$file" | grep '"Action":"output"' | tail -10 | while IFS= read -r output_line; do
        OUTPUT=$(echo "$output_line" | sed 's/.*"Output":"\(.*\)".*/\1/' | sed 's/\\n/\n/g' | sed 's/\\t/\t/g')
        echo "   $OUTPUT"
      done
      echo ""
    else
      echo "📦 Package-level Failure:"
      echo "   Package: $PACKAGE"
      if [ -n "$ELAPSED" ]; then
        echo "   Elapsed: ${ELAPSED}s"
      fi
      echo ""
      echo "   ⚠️  No individual test marked as failed!"
      echo "   This could indicate:"
      echo "   - A test panicked during initialization"
      echo "   - A race condition detected by -race flag"
      echo "   - A build/compilation issue in test code"
      echo "   - A test timeout"
      echo ""
      echo "   Recent package output (last 20 lines):"
      grep "\"Package\":\"$PACKAGE\"" "$file" | grep '"Action":"output"' | tail -20 | while IFS= read -r output_line; do
        OUTPUT=$(echo "$output_line" | sed 's/.*"Output":"\(.*\)".*/\1/' | sed 's/\\n/\n/g' | sed 's/\\t/\t/g')
        echo "   $OUTPUT"
      done
      echo ""
    fi
    
    echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
    echo ""
  done
done

if [ $FAILURES_FOUND -eq 0 ]; then
  if [ $TOTAL_FILES -eq 0 ]; then
    echo "❌ ERROR: No valid test result files found"
    exit 1
  else
    echo "✅ No test failures found in $TOTAL_FILES file(s)"
    exit 0
  fi
else
  echo ""
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo "Summary: Test failures detected"
  echo "━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━"
  echo ""
  echo "💡 Debugging tips:"
  echo "   1. Review the test output above for error messages"
  echo "   2. If no individual test failed, check for:"
  echo "      - Race conditions (run locally with -race flag)"
  echo "      - Test initialization panics"
  echo "      - Build errors in test files"
  echo "   3. Run the test locally with: go test -v -tags integration <package>"
  echo "   4. Add -run <TestName> to run a specific failing test"
  echo ""
  exit 1
fi
