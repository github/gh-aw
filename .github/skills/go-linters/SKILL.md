---
name: go-linters
description: Add and validate custom Go analysis linters in gh-aw.
---

# Go Linters

Use this guide when adding a new custom Go analysis linter in this repository.

For PR-driven linter generation (derive a rule from a specific pull request pattern), use `.github/skills/pr-to-go-linter/SKILL.md`.

## Where to add a new linter

1. Create a new package under `pkg/linters/<linter-name>/`.
2. Define an analyzer in that package (exported as `Analyzer`).
3. Add tests in the same package using `analysistest` with fixtures under `testdata/src/...`.
4. Register the analyzer in `cmd/linters/main.go` so it runs via the multichecker binary.

## Build and test linters

- Test only your linter package:
  - `go test ./pkg/linters/<linter-name>/...`
- Build the custom linter runner:
  - `go build ./cmd/linters`
- Run all custom linters across the repo:
  - `make golint-custom`

`make golint-custom` builds `cmd/linters` and runs it against `./cmd/...` and `./pkg/...`.

## Common AST pitfalls

### Unwrap parenthesized expressions

Expressions wrapped in parentheses appear as `*ast.ParenExpr`. Unwrap expressions before
asserting their AST type or comparing identifiers, so forms such as `(nil)` and `(err)` are
handled like `nil` and `err`. Use the shared helper:

```go
if ident, ok := astutil.UnwrapParenExpr(expr).(*ast.Ident); ok && ident.Name == "nil" {
	// Handle nil.
}
```

### Match all relevant statement shapes

Semantically similar calls can appear in different statement nodes. A call that discards a
result may be a bare `*ast.ExprStmt` or part of an `*ast.AssignStmt`; filtering only for one
shape misses the other. Include and handle each relevant node shape:

```go
nodeFilter := []ast.Node{(*ast.AssignStmt)(nil), (*ast.ExprStmt)(nil)}
return analyzerutil.Preorder(pass, nodeFilter, func(n ast.Node) {
	switch stmt := n.(type) {
	case *ast.AssignStmt:
		analyzeAssign(stmt)
	case *ast.ExprStmt:
		analyzeExpr(stmt)
	}
})
```

Before submitting a linter, check expression-shape assertions for parenthesis unwrapping and
verify its node filter covers relevant equivalent syntax forms.

## Coverage-aware perf gating

For linters that flag micro-optimizations (allocation/perf rules), only apply them on lines that
tests actually exercise — "hot paths" — rather than on dead or rarely-executed code where the
optimization brings no measurable benefit. Use the shared `pkg/linters/internal/coverage` package:

1. In your analyzer file, register a `-hot-threshold` flag in `init()` (not as a var initializer,
   to avoid an `Analyzer`/`run`/flag initialization cycle):

   ```go
   var hotThreshold *int

   func init() {
       hotThreshold = coverage.RegisterHotThresholdFlag(Analyzer)
   }
   ```

2. Immediately before reporting a diagnostic, gate it with `coverage.ShouldApply`:

   ```go
   if !coverage.ShouldApply(pass, node.Pos(), *hotThreshold) {
       return
   }
   ```

`coverage.ShouldApply` is permissive by default: when no coverage profile is loaded via the
`GH_AW_LINT_COVERAGE_PROFILE` environment variable, or when `hot-threshold` is `0`, it always
returns `true`, preserving pre-coverage-aware behavior. Only wire this into linters whose fix has
a genuine performance rationale (extra allocations, O(n²) behavior, etc.) — purely
readability/style linters should not be coverage-gated.

### Generating the coverage profile

```bash
go test -covermode=count -coverprofile=/tmp/coverage.out ./...
export GH_AW_LINT_COVERAGE_PROFILE=/tmp/coverage.out
make golint-custom
```

This profile is read once per linter-runner process. To lint only a specific subtree, scope
the `go test` and `golint-custom` commands to the same package path.
