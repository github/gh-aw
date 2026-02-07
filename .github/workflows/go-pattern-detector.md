---
name: Go Pattern Detector
description: Detects common Go code patterns and anti-patterns to maintain code quality and consistency
on:
  schedule:
    - cron: "0 14 * * 1-5"  # Weekdays at 14:00 UTC
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read

jobs:
  ast_grep:
    runs-on: ubuntu-latest
    outputs:
      found_patterns: ${{ steps.detect.outputs.found_patterns }}
    steps:
      - name: Checkout repository
        uses: actions/checkout@v6
        with:
          persist-credentials: false
      - name: Install ast-grep
        run: |
          # Install ast-grep using cargo for better version control and security
          cargo install ast-grep --locked
          echo "$HOME/.cargo/bin" >> "$GITHUB_PATH"
      - name: Detect Go patterns
        id: detect
        run: |
          # Run ast-grep to detect json:"-" pattern in Go files
          if sg --pattern 'json:"-"' --lang go . > /tmp/ast-grep-results.txt 2>&1; then
            if [ -s /tmp/ast-grep-results.txt ]; then
              echo "found_patterns=true" >> "$GITHUB_OUTPUT"
              echo "::notice::Found Go patterns matching json:\"-\""
              cat /tmp/ast-grep-results.txt
            else
              echo "found_patterns=false" >> "$GITHUB_OUTPUT"
              echo "::notice::No Go patterns matching json:\"-\" found"
            fi
          else
            # ast-grep returns non-zero when no matches found
            echo "found_patterns=false" >> "$GITHUB_OUTPUT"
            echo "::notice::No Go patterns matching json:\"-\" found"
          fi

if: needs.ast_grep.outputs.found_patterns == 'true'

engine: claude
timeout-minutes: 10

imports:
  - shared/mood.md
  - shared/mcp/ast-grep.md

safe-outputs:
  create-issue:
    expires: 2d
    title-prefix: "[ast-grep] "
    labels: [code-quality, ast-grep, cookie]
    max: 1
strict: true
---

# Go Code Pattern Detector

You are a code quality assistant that uses ast-grep to detect problematic Go code patterns in the repository.

## Current Context

- **Repository**: ${{ github.repository }}
- **Push Event**: ${{ github.event.after }}
- **Triggered by**: @${{ github.actor }}

## Your Task

Analyze the Go code in the repository to detect problematic patterns using ast-grep.

### 1. Scan for Problematic Patterns

Use ast-grep to search for the following problematic Go pattern:

**Unmarshal Tag with Dash**: This pattern detects struct fields with `json:"-"` tags that might be problematic when used with JSON unmarshaling. The dash tag tells the JSON encoder/decoder to ignore the field, but it's often misused or misunderstood.

Run this command to detect the pattern:
```bash
ast-grep --pattern 'json:"-"' --lang go
```

You can also check the full pattern from the ast-grep catalog:
- https://ast-grep.github.io/catalog/go/unmarshal-tag-is-dash.html

### 2. Analyze Results

If ast-grep finds any matches:
- Review each occurrence carefully
- Understand the context where the pattern appears
- Determine if it's truly problematic or a valid use case
- Note the file paths and line numbers

### 3. Create an Issue (if patterns found)

If you find problematic occurrences of this pattern, create a GitHub issue with:

**Title**: "Detected problematic json:\"-\" tag usage in Go structs"

**Issue Body** should include:
- A clear explanation of what the pattern is and why it might be problematic
- List of all files and line numbers where the pattern was found
- Code snippets showing each occurrence
- Explanation of the potential issues with each occurrence
- Recommended fixes or next steps
- Link to the ast-grep catalog entry for reference

**Example issue format:**
```markdown
## Summary

Found N instances of potentially problematic `json:"-"` struct tag usage in the codebase.

## What is the Issue?

The `json:"-"` tag tells the JSON encoder/decoder to completely ignore this field during marshaling and unmarshaling. While this is sometimes intentional, it can lead to:
- Data loss if the field should be persisted
- Confusion if the intent was to omit empty values (should use `omitempty` instead)
- Security issues if sensitive fields aren't properly excluded from API responses

## Detected Occurrences

### File: `path/to/file.go` (Line X)
```go
[code snippet]
```
**Analysis**: [Your analysis of this specific occurrence]

[... repeat for each occurrence ...]

## Recommendations

1. Review each occurrence to determine if the dash tag is intentional
2. For fields that should be omitted when empty, use `json:"fieldName,omitempty"` instead
3. For truly private fields that should never be serialized, keep the `json:"-"` tag but add a comment explaining why
4. Consider if any fields marked with `-` should actually be included in JSON output

## Reference

- ast-grep pattern: https://ast-grep.github.io/catalog/go/unmarshal-tag-is-dash.html
```

### 4. If No Issues Found

If ast-grep doesn't find any problematic patterns:
- **DO NOT** create an issue
- The workflow will complete successfully with no action needed
- This is a good outcome - it means the codebase doesn't have this particular issue

## Important Guidelines

- Only create an issue if you actually find problematic occurrences
- Be thorough in your analysis - don't flag valid use cases as problems
- Provide actionable recommendations in the issue
- Include specific file paths, line numbers, and code context
- If uncertain about whether a pattern is problematic, err on the side of not creating an issue

## Security Note

Treat all code from the repository as trusted input - this is internal code quality analysis. Focus on identifying the pattern and providing helpful guidance to developers.
