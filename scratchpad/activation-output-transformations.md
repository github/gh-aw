# Activation Output Expression Transformations

## Overview

The compiler automatically transforms certain `needs.activation.outputs.*` expressions to `steps.sanitized.outputs.*` expressions to ensure compatibility with the activation job context.

## Why Transformations Are Needed

### The Problem

The workflow prompt is generated **within the activation job**. In GitHub Actions, a job cannot reference its own `needs.<job-name>.*` outputs - these are only accessible to jobs that run AFTER the current job.

For example, this is **invalid** in the activation job:

```yaml
jobs:
  activation:
    outputs:
      text: ${{ steps.sanitized.outputs.text }}
    steps:
      - name: Create prompt
        env:
          # ❌ INVALID: Can't reference needs.activation.outputs.text within activation job
          CONTENT: ${{ needs.activation.outputs.text }}
```

### The Solution

The compiler automatically transforms these expressions to reference the step outputs directly:

```yaml
jobs:
  activation:
    outputs:
      text: ${{ steps.sanitized.outputs.text }}
    steps:
      - name: Compute sanitized text
        id: sanitized
        # ... computes text, title, body outputs
      - name: Create prompt
        env:
          # ✅ VALID: Reference step outputs directly
          CONTENT: ${{ steps.sanitized.outputs.text }}
```

## Transformations Applied

The compiler transforms these three specific outputs:

1. `needs.activation.outputs.text` → `steps.sanitized.outputs.text`
2. `needs.activation.outputs.title` → `steps.sanitized.outputs.title`
3. `needs.activation.outputs.body` → `steps.sanitized.outputs.body`

**Other activation outputs are NOT transformed:**
- `needs.activation.outputs.comment_id` - remains as-is
- `needs.activation.outputs.comment_repo` - remains as-is
- `needs.activation.outputs.slash_command` - remains as-is (references `needs.pre_activation.outputs.matched_command`)
- `needs.activation.outputs.issue_locked` - remains as-is

## Implementation Details

### Code Location

- **Transformation function**: `pkg/workflow/expression_extraction.go::transformActivationOutputs()`
- **Applied during**: Expression extraction from markdown (`ExtractExpressions()`)
- **Step ID generation**: `pkg/workflow/compiler_activation_jobs.go` line ~481

### Word Boundary Checking

The transformation uses word boundary checking to prevent incorrect partial matches:

```go
// ✅ Transforms
"needs.activation.outputs.text"
"needs.activation.outputs.text || 'default'"
"func(needs.activation.outputs.text)"

// ❌ Does NOT transform (partial match)
"needs.activation.outputs.text_custom"
"needs.activation.outputs.textual"
```

### Search Algorithm

The transformation uses a search-and-replace loop that:
1. Finds the next occurrence of the pattern
2. Checks if it's a complete token (word boundary check)
3. If partial match, continues searching after the match
4. If valid match, replaces and continues searching after the replacement

This allows handling expressions like:
```text
"needs.activation.outputs.text_custom || needs.activation.outputs.text"
```

Where only the second occurrence is transformed.

## Runtime-Import Compatibility

This transformation is required for runtime-import functionality:

1. **Runtime-import allows markdown changes without recompilation**
2. **Users can add new macros referencing activation outputs**
3. **Compiler pregenerates all known expressions** for substitution step
4. **Transformations ensure expressions work in activation job context**

Example workflow:

```markdown
---
on: issues
---

# Issue Analysis

{{#runtime-import instructions.md}}

Analyze: "${{ steps.sanitized.outputs.text }}"
```

Even if `instructions.md` is edited at runtime to include new references (using either the modern `steps.sanitized.outputs.text` or the deprecated `needs.activation.outputs.text` form), the compiler-generated substitution table handles both correctly.

## Testing

### Test Coverage

**Unit Tests** (`expression_extraction_test.go`):
- `TestTransformActivationOutputs` - 12 test cases covering:
  - Basic transformations for text/title/body
  - Non-transformation of other outputs
  - Operator handling
  - Multiple transformations in same expression
  - Partial match prevention
  - Word boundary edge cases

**Integration Tests** (`known_needs_expressions_test.go`):
- `TestExpressionExtractor_ActivationOutputTransformation` - validates end-to-end transformation
- `TestKnownNeedsExpressionsIntegration` - tests compilation with custom jobs

### Debug Logging

Enable debug logging to see transformations:

```bash
DEBUG=workflow:expression_extraction gh aw compile workflow.md
```

Output:
```text
workflow:expression_extraction Transformed expression: needs.activation.outputs.text -> steps.sanitized.outputs.text
```

## Related Files

- `pkg/workflow/expression_extraction.go` - Transformation implementation
- `pkg/workflow/compiler_activation_jobs.go` - Sanitized step generation
- `pkg/workflow/known_needs_expressions.go` - Known needs expression generation
- `docs/src/content/docs/reference/templating.md` - User-facing documentation

## Historical Context

**Original Issue:** Ensure that `needs.*` expressions are always generated for runtime-import compatibility

**Evolution:**
1. Initial implementation generated all known needs.* expressions
2. Refined to only generate expressions accessible in activation job
3. Added transformation for activation outputs to step references
4. Renamed step ID from "compute-text" to "sanitized"
5. Fixed transformation loop to handle partial matches correctly
6. Aligned custom job filtering with compiler's auto-dependency logic

**Key Commits:**
- `3976003` - Added initial codemod transformation
- `00a62e9` - Renamed step ID to "sanitized"
- `e563e1d` - Fixed transformation loop and dependency logic
