#!/bin/bash
# List all Go test function names that are actually runnable
# This script uses 'go test -list' to discover tests, which respects build tags

set -euo pipefail

# Create temporary files for test lists
unit_tests=$(mktemp)
integration_tests=$(mktemp)

# Clean up temporary files on exit
trap "rm -f $unit_tests $integration_tests" EXIT

# List unit tests (excludes integration tests via build tag)
# Filter for lines starting with "Test" followed by an uppercase letter (actual test functions)
go test -list='^Test' -tags '!integration' ./... 2>/dev/null | grep '^Test[A-Z]' | grep -v '^TestMain$' > "$unit_tests" || true

# List integration tests (only integration tests via build tag)
# Filter for lines starting with "Test" followed by an uppercase letter (actual test functions)
go test -list='^Test' -tags 'integration' ./... 2>/dev/null | grep '^Test[A-Z]' | grep -v '^TestMain$' > "$integration_tests" || true

# Combine both lists, sort, and deduplicate
cat "$unit_tests" "$integration_tests" | sort -u
