# Serena Language Server Tool

Serena is a **language service protocol (LSP) MCP server** for semantic code analysis. Use it ONLY when you need deep code understanding beyond text manipulation.

## Quick Decision: Should I Use Serena?

**✅ YES - Use Serena when you need:**
- Symbol navigation (find all usages of a function/type)
- Call graph analysis across files
- Semantic duplicate detection (not just text matching)
- Refactoring analysis (functions in wrong files, extraction opportunities)
- Type relationships and interface implementations

**❌ NO - Use simpler tools when:**
- Searching text patterns → Use `grep`
- Editing files → Use `edit` tool
- Running commands → Use `bash`
- Working with YAML/JSON/Markdown → Use `edit` tool
- Simple file operations → Use `bash` or `create`

**Rule of thumb**: If `grep` or `bash` can solve it in 1-2 commands, don't use Serena.

## Configuration

Add to workflow frontmatter:

```yaml
tools:
  serena: ["go"]  # Specify language(s): go, typescript, python, ruby, rust, java, cpp, csharp
```

Multi-language repositories:
```yaml
tools:
  serena: ["go", "typescript"]  # First language is default fallback
```

## Available Serena Tools

### Navigation & Analysis
- `find_symbol` - Search for symbols by name
- `get_symbols_overview` - List all symbols in a file
- `find_referencing_symbols` - Find where a symbol is used
- `find_referencing_code_snippets` - Find code snippets using a symbol
- `search_for_pattern` - Search for code patterns (regex)

### Code Editing
- `read_file` - Read file with semantic context
- `create_text_file` - Create/overwrite files
- `insert_at_line` - Insert content at line number
- `insert_before_symbol` / `insert_after_symbol` - Insert near symbols
- `replace_lines` - Replace line range
- `replace_symbol_body` - Replace symbol definition
- `delete_lines` - Delete line range

### Project Management
- `activate_project` - **REQUIRED** - Activate Serena for workspace
- `onboarding` - Analyze project structure
- `restart_language_server` - Restart LSP if needed
- `get_current_config` - View Serena configuration
- `list_dir` - List directory contents

## Usage Workflow

### 1. Activate Serena First

**Always call activate_project before other Serena tools:**

```javascript
// activate_project tool
{
  "path": "/home/runner/work/gh-aw/gh-aw"
}
```

### 2. Combine with Other Tools

**Best practice**: Use bash for discovery, Serena for analysis

```yaml
tools:
  serena: ["go"]
  bash:
    - "find pkg -name '*.go' ! -name '*_test.go'"
    - "cat go.mod"
  github:
    toolsets: [default]
```

**Pattern**: 
1. Use `bash` to list files
2. Use Serena to analyze semantic structure
3. Use `edit` to make changes

### 3. Use Cache for Recurring Analysis

Track analysis state across runs:

```yaml
tools:
  serena: ["go"]
  cache-memory: true  # Store analysis history
```

Load cache → Analyze new/changed files → Save results → Avoid redundant work

## Common Patterns

### Pattern 1: Find All Function Usages

```
1. Use find_symbol to locate function definition
2. Use find_referencing_code_snippets to find call sites
3. Analyze patterns
```

### Pattern 2: Code Quality Analysis

```
1. Use get_symbols_overview on multiple files
2. Use find_symbol for similar function names
3. Use search_for_pattern for duplicate logic
4. Identify consolidation opportunities
```

### Pattern 3: Daily Code Analysis

```
1. Load previous state from cache-memory
2. Select files using round-robin or priority
3. Use Serena for semantic analysis
4. Save findings to cache
5. Generate improvement tasks
```

## Production Examples

Workflows successfully using Serena:

- **go-fan** (97.6% success) - Go module usage analysis with round-robin
- **sergo** (94.4% success) - Daily code quality with 50/50 cached/new strategies
- **semantic-function-refactor** - Function clustering and outlier detection
- **daily-compiler-quality** - Rotating file analysis with cache tracking

## Common Pitfalls

❌ **Using Serena for non-code files** - Use `edit` for YAML/JSON/Markdown
❌ **Forgetting activate_project** - Always call first
❌ **Not combining with bash** - Use bash for file discovery
❌ **Missing language configuration** - Must specify language(s)

## Supported Languages

Primary languages with full LSP features:
- `go` (gopls)
- `typescript` (TypeScript/JavaScript)
- `python` (jedi/pyright)
- `ruby` (solargraph)
- `rust` (rust-analyzer)
- `java`, `cpp`, `csharp`

See `.serena/project.yml` for complete list (25+ languages).

## Decision Tree

```
Task requires code semantics/structure?
├─ NO → Use bash/edit/view
└─ YES
    ├─ Simple text search/replace? → Use grep/bash
    ├─ Config/data files? → Use edit
    └─ Symbol/structure/semantic patterns? → Use Serena ✅
```
