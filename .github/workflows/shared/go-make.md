---
network:
  allowed:
    - go
mcp-scripts:
  go:
    description: "Execute any Go command. This tool is accessible as 'mcpscripts-go'. Provide whitespace-separated arguments after 'go' (e.g., args: 'test ./...'). Arguments are passed as data without glob expansion or shell evaluation."
    inputs:
      args:
        type: string
        description: "Arguments to pass to go CLI (without the 'go' prefix). Examples: 'test ./...', 'build ./cmd/gh-aw', 'mod tidy', 'fmt ./...', 'vet ./...'"
        required: true
    run: |
      echo "go $INPUT_ARGS"
      read -r -a args <<< "${INPUT_ARGS//$'\n'/ }"
      go "${args[@]}"

  make:
    description: "Execute any Make target. This tool is accessible as 'mcpscripts-make'. Provide whitespace-separated target names and options (e.g., args: 'build'). Arguments are passed as data without glob expansion or shell evaluation."
    inputs:
      args:
        type: string
        description: "Arguments to pass to make (target names and options). Examples: 'build', 'test-unit', 'lint', 'recompile', 'agent-finish', 'fmt build test-unit'"
        required: true
    run: |
      echo "make $INPUT_ARGS"
      read -r -a args <<< "${INPUT_ARGS//$'\n'/ }"
      make "${args[@]}"
---

**IMPORTANT — bash vs. MCP for validation:**

- **Use `mcpscripts-go` / `mcpscripts-make` early in a session** (within the first few minutes) for consistent, logged execution of Go and Make commands.
- **Use direct bash commands for end-of-session validation** (i.e., after any file-exploration or analysis phase that may have taken more than ~5 minutes). MCP connections time out after ~5 minutes of inactivity; calling `mcpscripts-*` at the end of a long session fails with `MCP error -32003: context canceled`.

**Validation via bash (preferred at end of session or after long exploration):**
```bash
make build
make test-unit
make recompile
```

**MCP tools (use early in session, before long exploration phases):**
```
Use the mcpscripts-go tool with args: "test ./..."
Use the mcpscripts-make tool with args: "build"
Use the mcpscripts-make tool with args: "lint"
Use the mcpscripts-make tool with args: "test-unit"
```

**Incorrect**:
```
Use the go mcp-script tool with args: "test ./..."  ❌ (Wrong tool name - use mcpscripts-go)
Use the mcpscripts-make tool with args: "build"     ❌ (after a long exploration phase — use bash instead)
```

<!--
## mcpscripts-go and mcpscripts-make Tools

Safe-input tools that wrap Go and Make commands for consistent execution in agentic workflows.

### Usage

```yaml
imports:
  - shared/go-make.md
```

### Invocation

#### mcpscripts-go

The tool is accessible as `mcpscripts-go` (or `mcpscripts_go` after normalization). Provide go CLI arguments via the `args` parameter:

```
mcpscripts-go with args: "test ./..."
mcpscripts-go with args: "build ./cmd/gh-aw"
mcpscripts-go with args: "mod tidy"
mcpscripts-go with args: "fmt ./..."
mcpscripts-go with args: "vet ./..."
mcpscripts-go with args: "test -v -run TestCompile ./pkg/cli"
```

The tool executes: `go <args>`

#### mcpscripts-make

The tool is accessible as `mcpscripts-make` (or `mcpscripts_make` after normalization). Provide make target(s) via the `args` parameter:

```
mcpscripts-make with args: "build"
mcpscripts-make with args: "test-unit"
mcpscripts-make with args: "lint"
mcpscripts-make with args: "recompile"
mcpscripts-make with args: "fmt lint build"
mcpscripts-make with args: "agent-finish"
```

The tool executes: `make <args>`

### Common Make Targets

From the gh-aw Makefile:

- **build** - Build the gh-aw binary
- **test** - Run all tests (unit + integration)
- **test-unit** - Run unit tests only (~25s, fast feedback)
- **test-integration-*** - Run specific integration test groups
- **lint** - Run linters
- **fmt** - Format code (Go, JavaScript, JSON)
- **recompile** - Recompile all workflow lock files
- **agent-finish** - Complete validation (fmt, lint, build, test, recompile)
- **fix** - Run gh-aw fix on all workflows
- **clean** - Remove build artifacts
- **deps** - Install Go dependencies
- **deps-dev** - Install development dependencies (linters)

### Common Go Commands

- **go test ./...** - Run all tests
- **go test ./pkg/...** - Run tests in pkg directory
- **go build ./cmd/gh-aw** - Build the main binary
- **go mod tidy** - Clean up go.mod and go.sum
- **go fmt ./...** - Format all Go code
- **go vet ./...** - Run Go vet static analyzer
- **go test -v -run TestName ./pkg/cli** - Run specific test
-->
