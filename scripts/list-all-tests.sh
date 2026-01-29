#!/bin/bash
# List all Go test function names from source files
# This script extracts test function names from *_test.go files

set -euo pipefail

# Find all test files and extract test function names
# Exclude TestMain as it's a special setup function, not an actual test
find . -name "*_test.go" -type f -exec grep -h "^func Test" {} \; | \
  sed 's/func \(Test[^(]*\).*/\1/' | \
  grep -v "^TestMain$" | \
  sort -u
