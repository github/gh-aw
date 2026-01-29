---
title: Using Serena
description: Configure the Serena MCP server for semantic code analysis and intelligent code editing in your agentic workflows.
sidebar:
  order: 5
---

This guide covers using [Serena](https://github.com/oraios/serena), a powerful coding agent toolkit that provides semantic code retrieval and editing capabilities to agentic workflows.

## What is Serena?

Serena is an MCP server that enhances AI agents with IDE-like tools for semantic code analysis and manipulation. It supports **30+ programming languages** through Language Server Protocol (LSP) integration, enabling agents to find symbols, navigate relationships, edit at symbol level, and analyze code structure - all without reading entire files or performing text-based searches.

> [!TIP]
> Serena excels at navigating and manipulating complex codebases, especially for large, well-structured projects where precise code navigation and editing are essential.

## Quick Start

### Basic Configuration

Add Serena to your workflow using the short syntax with a list of languages:

```yaml wrap
---
engine: copilot
permissions:
  contents: read
tools:
  serena: ["go", "typescript", "python"]
---
```

This enables Serena for Go, TypeScript, and Python code analysis.

### Example: Code Analysis

```yaml wrap
---
engine: copilot
permissions:
  contents: read
tools:
  serena: ["go"]
  github:
    toolsets: [default]
---

# Code Quality Analyzer

Analyze Go code for quality improvements:
1. Find all exported functions and check for missing documentation
2. Identify code patterns and suggest improvements
```

## Configuration Options

### Configuration Syntax

**Short syntax** (recommended for most cases):
```yaml wrap
tools:
  serena: ["go", "typescript"]
```

**Long syntax** for language-specific options:
```yaml wrap
tools:
  serena:
    version: latest
    args: ["--verbose"]
    languages:
      go:
        version: "1.21"
        go-mod-file: "go.mod"  # Use "backend/go.mod" if in subdirectory
        gopls-version: "v0.14.2"
      python:
        version: "3.12"
```

Key configuration fields: `version` (Serena version), `args` (CLI arguments), and `languages` (per-language settings).

## Language Support

Serena supports **30+ programming languages** through Language Server Protocol (LSP):

| Category | Languages |
|----------|-----------|
| **Systems** | C, C++, Rust, Go, Zig |
| **JVM** | Java, Kotlin, Scala, Groovy (partial) |
| **Web** | JavaScript, TypeScript, Dart, Elm |
| **Dynamic** | Python, Ruby, PHP, Perl, Lua |
| **Functional** | Haskell, Elixir, Erlang, Clojure, OCaml |
| **Scientific** | R, Julia, MATLAB, Fortran |
| **Shell** | Bash, PowerShell |
| **Other** | C#, Swift, Nix, Markdown, YAML, TOML |

> [!NOTE]
> Some language servers require additional dependencies. Most are automatically installed by Serena, but check the [Language Support](https://oraios.github.io/serena/01-about/020_programming-languages.html) documentation for specific requirements.

## Available Tools

Serena provides semantic code tools organized into three categories:

| Category | Tools |
|----------|-------|
| **Symbol Navigation** | `find_symbol`, `find_referencing_symbols`, `get_symbol_definition`, `list_symbols_in_file` |
| **Code Editing** | `replace_symbol_body`, `insert_after_symbol`, `insert_before_symbol`, `delete_symbol` |
| **Project Analysis** | `find_files`, `get_project_structure`, `analyze_imports` |

These tools enable agents to work at the **symbol level** rather than the file level, making code operations more precise and context-aware.

## Memory Configuration

Serena caches language server indexes for faster operations. Create the cache directory in your workflow:

```bash
mkdir -p /tmp/gh-aw/cache-memory/serena
```

Optionally configure cache-memory in frontmatter:
```yaml wrap
tools:
  serena: ["go"]
  cache-memory:
    key: serena-analysis
```

## Usage Examples

### Find Unused Functions

```yaml wrap
---
engine: copilot
tools:
  serena: ["go"]
  github:
    toolsets: [default]
---

# Find Unused Code

1. Configure memory: `mkdir -p /tmp/gh-aw/cache-memory/serena`
2. Use `find_symbol` and `find_referencing_symbols` to identify unused exports
3. Report findings
```

### Automated Refactoring

```yaml wrap
---
engine: claude
permissions:
  contents: write
tools:
  serena: ["python"]
  edit:
---

# Add Type Hints

1. Find functions without type hints
2. Add annotations using `replace_symbol_body`
3. Verify correctness
```

## Best Practices

Configure cache directory early (`mkdir -p /tmp/gh-aw/cache-memory/serena`) for faster operations. Prefer symbol-level operations (`replace_symbol_body`) over file-level edits. For Go projects, explicitly set `go-mod-file` location. Combine Serena with other tools like `github`, `edit`, and `bash` for complete workflows. For large codebases, start with targeted analysis of specific packages before expanding scope.


## Troubleshooting

**Language server not found:** Install required dependencies (e.g., `go install golang.org/x/tools/gopls@latest` for Go).

**Memory permission issues:** Ensure cache directory exists with proper permissions: `mkdir -p /tmp/gh-aw/cache-memory/serena && chmod 755 /tmp/gh-aw/cache-memory/serena`

**Go module path not found:** Explicitly configure `go-mod-file: "path/to/go.mod"` in language settings.

**Slow initial analysis:** Expected behavior as language servers build indexes. Subsequent runs use cached data. Enable cache-memory for persistence or run on schedule to maintain warm cache.


## Related Documentation

- [Using MCPs](/gh-aw/guides/mcps/) - General MCP server configuration
- [Tools Reference](/gh-aw/reference/tools/) - Complete tools configuration
- [Getting Started with MCPs](/gh-aw/guides/getting-started-mcp/) - MCP introduction
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Automated pull requests and issues
- [Frontmatter Reference](/gh-aw/reference/frontmatter/) - All configuration options

## External Resources

- [Serena GitHub Repository](https://github.com/oraios/serena) - Official repository
- [Serena Documentation](https://oraios.github.io/serena/) - Comprehensive user guide
- [Language Support](https://oraios.github.io/serena/01-about/020_programming-languages.html) - Supported languages and dependencies
- [Serena Tools Reference](https://oraios.github.io/serena/01-about/035_tools.html) - Complete tool documentation
