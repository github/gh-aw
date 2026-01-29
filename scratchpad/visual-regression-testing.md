# Visual Regression Testing

## Overview

Visual regression testing ensures that console output formatting remains consistent across code changes. This prevents unintentional changes to table layouts, box rendering, tree structures, and error formatting that could negatively impact the user experience.

## Implementation

gh-aw uses the `github.com/charmbracelet/x/exp/golden` package for visual regression testing. Golden files capture the expected output for various console rendering scenarios, and tests automatically compare actual output against these golden files.

## Running Tests

### Normal Test Execution

Run golden tests like any other tests:

```bash
# Run all golden tests
go test -v ./pkg/console -run='^TestGolden_'

# Run all console tests including golden tests
make test-unit
```

### Updating Golden Files

When you intentionally change console output formatting, update the golden files:

```bash
# Update all golden files in console package
make update-golden

# Or use go test directly
go test -v ./pkg/console -run='^TestGolden_' -update
```

**Important**: Only update golden files when you have intentionally changed the output format. Review the diffs carefully before committing updated golden files.

## Test Coverage

The golden tests cover the following console rendering components:

### Table Rendering (`TestGolden_TableRendering`)

Tests table output with various configurations:
- Simple tables with headers and data rows
- Tables with titles
- Tables with totals
- Wide tables with many columns
- Empty tables

### Box Rendering (`TestGolden_BoxRendering`, `TestGolden_LayoutBoxRendering`)

Tests box formatting with different widths and content:
- Narrow boxes (20-30 characters)
- Medium boxes (60 characters)
- Wide boxes (100+ characters)
- Boxes with emoji and special characters
- Both `RenderTitleBox` (returns `[]string`) and `LayoutTitleBox` (returns `string`)

### Tree Rendering (`TestGolden_TreeRendering`)

Tests hierarchical tree structures:
- Single nodes
- Flat trees (one level)
- Nested trees (multiple levels)
- Deep hierarchies (5+ levels)
- Real-world examples (MCP server configurations)

### Error Formatting (`TestGolden_ErrorFormatting`)

Tests error message formatting:
- Basic errors with file position
- Warnings with hints
- Errors with source code context
- Multi-line context display
- Info messages

### Error with Suggestions (`TestGolden_ErrorWithSuggestions`)

Tests actionable error messages:
- Errors with multiple suggestions
- Errors without suggestions
- Single suggestion errors
- Compilation errors with fix recommendations

### Message Formatting (`TestGolden_MessageFormatting`)

Tests various message types:
- Success messages (✓)
- Info messages (ℹ)
- Warning messages (⚠)
- Error messages (✗)
- Location messages (📁)
- Command messages
- Progress messages

### Layout Composition (`TestGolden_LayoutComposition`)

Tests composing multiple layout elements:
- Title boxes with info sections
- Complete compositions with warnings
- Multiple emphasis boxes

### Emphasis Boxes (`TestGolden_LayoutEmphasisBox`)

Tests colored emphasis boxes:
- Error boxes (red)
- Warning boxes (yellow)
- Success boxes (green)
- Info boxes (blue)

## Adding New Golden Tests

When adding new console rendering functionality:

1. **Add test case** to appropriate test function in `pkg/console/golden_test.go`:

```go
func TestGolden_NewFeature(t *testing.T) {
    tests := []struct {
        name   string
        config YourConfig
    }{
        {
            name: "basic_case",
            config: YourConfig{
                // ... test data
            },
        },
    }

    for _, tt := range tests {
        t.Run(tt.name, func(t *testing.T) {
            output := YourRenderFunction(tt.config)
            golden.RequireEqual(t, []byte(output))
        })
    }
}
```

2. **Generate golden file**:

```bash
go test -v ./pkg/console -run='^TestGolden_NewFeature' -update
```

3. **Verify output** by reviewing the generated golden file:

```bash
cat pkg/console/testdata/TestGolden_NewFeature/basic_case.golden
```

4. **Run test** without update flag to ensure it passes:

```bash
go test -v ./pkg/console -run='^TestGolden_NewFeature'
```

## Golden File Organization

Golden files are organized by test name in the `pkg/console/testdata/` directory:

```
pkg/console/testdata/
├── TestGolden_BoxRendering/
│   ├── narrow_box.golden
│   ├── medium_box.golden
│   └── wide_box.golden
├── TestGolden_TableRendering/
│   ├── simple_table.golden
│   ├── table_with_title.golden
│   └── wide_table.golden
└── TestGolden_TreeRendering/
    ├── single_node.golden
    ├── flat_tree.golden
    └── nested_tree.golden
```

The `golden.RequireEqual()` function automatically creates subdirectories based on the test name and uses the subtest name for the file.

## TTY vs Non-TTY Output

The golden tests capture non-TTY output (plain text without ANSI color codes). This ensures:
- Consistent test results regardless of terminal environment
- Readable golden files that can be reviewed in version control
- Compatibility with CI/CD environments

For TTY-specific testing, use the existing `console_test.go` tests which validate that styled output contains expected content.

## Failure Diagnosis

When a golden test fails, the test output shows a diff:

```
golden_test.go:123: output does not match, expected:

    ┌──────┬───────┐
    │Name  │Status │
    └──────┴───────┘

got:

    ┌──────┬───────┬────────┐
    │Name  │Status │Duration│
    └──────┴───────┴────────┘

diff:

    --- golden
    +++ run
    @@ -1,3 +1,3 @@
    -┌──────┬───────┐
    -│Name  │Status │
    -└──────┴───────┘
    +┌──────┬───────┬────────┐
    +│Name  │Status │Duration│
    +└──────┴───────┴────────┘
```

**Steps to resolve**:

1. **Review the diff** to understand what changed
2. **Determine if the change was intentional**:
   - If yes: Update golden file with `make update-golden`
   - If no: Fix the code to restore original formatting
3. **Commit golden file changes** with clear explanation of why formatting changed

## Best Practices

### When to Update Golden Files

✅ **Update when**:
- Intentionally improving console output formatting
- Fixing visual bugs in table/box rendering
- Adding new columns or fields to tables
- Changing error message format for better clarity

❌ **Don't update when**:
- Tests fail unexpectedly during development
- Making unrelated code changes
- Unsure about the impact of formatting changes

### Reviewing Golden File Changes

Before committing updated golden files:

1. **Review all diffs** carefully in your PR
2. **Verify intentional changes** match expectations
3. **Test in terminal** to see actual styled output
4. **Document the change** in your commit message

### Golden File Maintenance

- Keep golden files in version control
- Review golden file changes in code reviews
- Don't ignore failing golden tests
- Update documentation when adding new test categories

## CI/CD Integration

Golden tests run as part of the standard test suite:

```yaml
# .github/workflows/test.yml
- name: Run tests
  run: make test-unit
```

CI will fail if golden tests don't match, preventing accidental formatting regressions from being merged.

## Troubleshooting

### Tests Pass Locally But Fail in CI

**Cause**: Line ending differences between Windows/Unix or TTY detection issues

**Solution**: Ensure consistent line endings and verify tests run in non-TTY mode

### Golden Files Show ANSI Escape Codes

**Cause**: Tests are detecting TTY when they shouldn't

**Solution**: Golden tests should force non-TTY mode. Check that terminal detection is disabled in test setup.

### Can't Update Golden Files

**Cause**: Permission issues or incorrect path

**Solution**: Ensure `testdata/` directory exists and has write permissions

## Related Documentation

- [Console Rendering](console-rendering.md) - Overview of console rendering system
- [Testing Guidelines](testing.md) - General testing practices
- [Error Messages](error-messages.md) - Error message style guide

## References

- [charmbracelet/x/exp/golden](https://github.com/charmbracelet/x/tree/main/exp/golden) - Golden test library
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal rendering library
