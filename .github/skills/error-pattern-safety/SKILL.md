---
name: error-pattern-safety
description: Apply safe error-pattern matching rules for agentic engines.
---


# Error Pattern Safety Guidelines

Use these regex safety rules in agentic engines to prevent JavaScript infinite loops.

## The Problem

With the JavaScript global flag (`/pattern/g`), zero-width matches can cause infinite loops because:

1. JavaScript's `regex.exec()` with the `g` flag uses `lastIndex` to track position
2. When a pattern matches zero-width, `lastIndex` doesn't advance
3. The same position is matched repeatedly, causing an infinite loop

## Dangerous Pattern Examples

**❌ NEVER USE THESE PATTERNS:**

```javascript
// Pure .* - matches everything including empty string at end
/.*/g

// Single character with * - matches zero or more (including zero)
/a*/g

// Patterns that can match empty string
/(x|y)*/g
```

## Safe Pattern Examples

**✅ ALWAYS USE PATTERNS LIKE THESE:**

```javascript
// Required prefix before .*
/error.*/gi
/error.*permission.*denied/gi

// Specific structure with required content
/\[(\d{4}-\d{2}-\d{2})\]\s+(ERROR):\s+(.+)/g

// Required characters throughout
/access denied.*user.*not authorized/gi
```

## Pattern Safety Rules

1. **Always require at least one character match**
   - Use `.+` instead of `.*` when you need "something"
   - Ensure pattern has required prefix/suffix

2. **Never use bare `.*` as the entire pattern**
   - Always combine with required text: `error.*`
   - Never just `.*` or `.*?`

3. **Test patterns against empty string**
   ```javascript
   const regex = /your-pattern/g;
   if (regex.test("")) {
     throw new Error("Pattern matches empty string - DANGEROUS!");
   }
   ```

4. **Use specific anchors when possible**
   - Start: `^error.*`
   - End: `.*error$`
   - Word boundaries: `\berror\b`

## Validation Tests

All error patterns must pass the same safety checks used by the repo’s unit suite:

### Go tests

```go
// Test that pattern doesn't match empty string
func TestPatternSafety(t *testing.T) {
    pattern := "your-pattern"
    regex := regexp.MustCompile(pattern)

    if regex.MatchString("") {
        t.Error("Pattern matches empty string!")
    }
}
```

Run the relevant package tests with `make test-unit`.

### JavaScript tests

```javascript
test("should not match empty string", () => {
  const regex = new RegExp("your-pattern", "g");
  expect(regex.test("")).toBe(false);
});
```

Use the relevant `*.test.cjs` suite under `actions/setup/js/` or `pkg/workflow/js/` for the area you changed, or run the repo’s JavaScript checks via `make test-js`.

## Safety Mechanisms in the validation layer

The repo’s validation helpers include built-in protections for dangerous regex patterns:

1. **Zero-width detection**: Checks whether a regex stops advancing across iterations
2. **Iteration warning**: Warns when repeated runs approach a hang threshold
3. **Hard limit**: Stops execution before runaway loops can lock the process

```javascript
if (regex.lastIndex === lastIndex) {
  core.error(`Infinite loop detected! Pattern: ${pattern.pattern}`);
  break;
}
```

## Adding New Error Patterns

When adding new error patterns to engines:

1. **Write the pattern with required content**
   ```go
   {
       Pattern:      `(?i)error.*permission.*denied`,
       LevelGroup:   0,
       MessageGroup: 0,
       Description:  "Permission denied error",
   }
   ```

2. **Test against empty string**
   - Run: `make test-unit`
   - Checks: `TestAllEnginePatternsSafe`

3. **Test with actual log samples**
   - Ensure it matches real errors
   - Ensure it doesn't match informational text

4. **Document the pattern**
   - Add clear description
   - Note what it's designed to catch

## Pattern Conversion: Go to JavaScript

Patterns are converted from Go to JavaScript:

```go
// Go pattern (case-insensitive flag)
Pattern: `(?i)error.*permission.*denied`

// Converted to JavaScript
new RegExp("error.*permission.*denied", "gi")
```

The `(?i)` prefix is removed because JavaScript uses the `i` flag instead.

## Examples from Current Codebase

### ✅ Safe Patterns

```go
// Requires "error" prefix
Pattern: `(?i)error.*permission.*denied`

// Requires specific timestamp format
Pattern: `(\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z)\s+\[(ERROR)\]\s+(.+)`

// Requires "access denied" prefix
Pattern: `(?i)access denied.*user.*not authorized`
```

### How to Fix Unsafe Patterns

If you find a pattern that matches empty string:

**Before (unsafe):**
```go
Pattern: `.*error.*`  // Can match empty at start/end
```

**After (safe):**
```go
Pattern: `error.*`     // Requires "error" at start
// OR
Pattern: `.*error.+`   // Requires "error" and at least one char after
// OR
Pattern: `\berror\b.*` // Requires word "error"
```

## Testing Checklist

Before committing pattern changes:

- [ ] Run `make test-unit`
- [ ] Verify the relevant engine error-pattern tests still pass
- [ ] Run the JavaScript checks for the changed area with `make test-js` or the targeted Vitest suite
- [ ] Verify the pattern matches intended error messages
- [ ] Verify the pattern does not match informational text or empty-string edge cases

## References

- Go regex syntax: https://pkg.go.dev/regexp/syntax
- JavaScript regex: https://developer.mozilla.org/en-US/docs/Web/JavaScript/Guide/Regular_Expressions
- Current repo validation: `make test-unit` and `make test-js`
