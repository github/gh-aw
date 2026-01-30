#!/bin/bash
# Extract executed test names from JSON test result files
# Parses the JSON output from 'go test -json' format

set -euo pipefail

if [ $# -eq 0 ]; then
  echo "Usage: $0 <test-result-dir>"
  echo "Extracts executed test names from JSON test result files in the specified directory"
  exit 1
fi

TEST_RESULT_DIR="$1"

if [ ! -d "$TEST_RESULT_DIR" ]; then
  echo "Error: Directory $TEST_RESULT_DIR does not exist"
  exit 1
fi

# Find all JSON test result files and extract test names
# Look for lines with "Action":"run" and extract the "Test" field
# Strip subtest names (everything after the first '/') to get only top-level test names
# Process each file separately to handle cases where files might be empty or have no matches
temp_file=$(mktemp)
find "$TEST_RESULT_DIR" -name "*.json" -type f | while read -r file; do
  if [ -s "$file" ]; then
    # File exists and is not empty
    # Extract test names and strip subtest suffixes
    grep '"Action":"run"' "$file" 2>/dev/null | \
      grep -o '"Test":"[^"]*"' | \
      sed 's/"Test":"\([^"]*\)"/\1/' | \
      sed 's/\/.*//' >> "$temp_file" || true
  fi
done

# Sort and deduplicate the results
if [ -s "$temp_file" ]; then
  sort -u "$temp_file"
  rm -f "$temp_file"
else
  # No tests found - this is an error condition
  rm -f "$temp_file"
  echo "Error: No test execution records found in $TEST_RESULT_DIR" >&2
  exit 1
fi
