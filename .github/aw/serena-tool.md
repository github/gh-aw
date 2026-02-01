# Serena Language Server Tool

Serena is a **language service protocol (LSP) MCP server** that provides advanced code intelligence through semantic analysis, symbol navigation, and code understanding. It's designed for **deep code analysis tasks** that require understanding code structure, relationships, and semantics beyond simple text manipulation.

## When to Use Serena

Serena is powerful for **semantic code analysis** but comes with setup overhead. Use it when tasks genuinely require language-aware understanding:

### ✅ Excellent Use Cases

**Symbol and dependency analysis:**
- Finding all usages of a function/type across files
- Analyzing call graphs and dependency relationships
- Identifying duplicate or similar code patterns semantically
- Discovering outlier functions in wrong files

**Code quality and refactoring:**
- Detecting code that should be extracted or consolidated
- Analyzing function organization and clustering
- Identifying refactoring opportunities based on code structure
- Validating type usage and interface implementations

**Deep code understanding:**
- Understanding complex codebases quickly
- Analyzing module usage patterns
- Identifying architectural improvements
- Cross-file semantic analysis

### ❌ Poor Use Cases (Use simpler tools instead)

**Simple text operations:**
- Basic file editing or text manipulation → Use `edit` tool
- Search/replace by pattern → Use `grep` or `bash`
- File creation/deletion → Use `create` or `bash`

**When grep/bash suffice:**
- Finding text patterns → Use `grep`
- Listing files → Use `find` or `glob`
- Line counting → Use `wc`
- Simple code searches → Use `grep` with regex

**Configuration or data files:**
- YAML/JSON/config file edits → Use `edit` tool
- Markdown documentation → Use `edit` tool
- Non-code analysis → Serena won't help

## Configuration

Enable Serena by specifying target language(s) in workflow frontmatter:

```yaml
tools:
  serena: ["<language>"]  # One or more languages
```

### Supported Languages

Primary supported languages (with full LSP features):
- `go` - Go language (gopls)
- `typescript` - TypeScript/JavaScript
- `python` - Python (jedi or pyright)
- `ruby` - Ruby (solargraph)
- `rust` - Rust (rust-analyzer)
- `java` - Java
- `cpp` - C/C++
- `csharp` - C#

See `.serena/project.yml` for the complete list (25+ languages).

### Multi-language Support

For repositories with multiple languages:

```yaml
tools:
  serena: ["go", "typescript"]  # Multiple languages
```

The first language is the default fallback.

## Detecting Repository Language

**Method 1: Check language-specific files**
```bash
# Go
[ -f go.mod ] && echo "go"

# TypeScript/JavaScript  
[ -f package.json ] && echo "typescript"

# Python
[ -f requirements.txt ] || [ -f pyproject.toml ] && echo "python"

# Rust
[ -f Cargo.toml ] && echo "rust"

# Java
[ -f pom.xml ] || [ -f build.gradle ] && echo "java"
```

**Method 2: Examine file extensions**
```bash
find . -type f -name "*.go" | head -1  # Go files
find . -type f -name "*.ts" | head -1  # TypeScript
find . -type f -name "*.py" | head -1  # Python
```

## Available Serena Tools

Once Serena is enabled, you have access to powerful MCP tools:

### Core Navigation Tools

- `read_file` - Read file contents with semantic understanding
- `list_dir` - List directory contents
- `get_symbols_overview` - Get all symbols (functions, types) in a file
- `find_symbol` - Search for symbols by name (global or local)
- `find_referencing_symbols` - Find where a symbol is used
- `find_referencing_code_snippets` - Find code snippets referencing a symbol

### Code Editing Tools

- `create_text_file` - Create or overwrite files
- `insert_at_line` - Insert content at specific line
- `insert_before_symbol` / `insert_after_symbol` - Insert near symbols
- `replace_lines` - Replace line range with new content
- `replace_symbol_body` - Replace entire symbol definition
- `delete_lines` - Delete line range

### Analysis Tools

- `search_for_pattern` - Search for code patterns
- `onboarding` - Analyze project structure and essential tasks
- `activate_project` - Activate Serena for a workspace
- `restart_language_server` - Restart LSP if needed

### Memory and State

- `write_memory` / `read_memory` / `list_memories` - Store project insights
- `get_current_config` - View Serena configuration

## Practical Usage Patterns

### Pattern 1: Find All Usages of a Function

**Workflow**: Analyzing how a function is used across the codebase

```yaml
tools:
  serena: ["go"]
```

**Agent tasks:**
1. Use `find_symbol` to locate the function definition
2. Use `find_referencing_code_snippets` to find all call sites
3. Analyze patterns and suggest improvements

**Example**: See `go-fan.md` workflow (analyzes Go module usage)

### Pattern 2: Code Quality Analysis

**Workflow**: Identify refactoring opportunities by clustering functions

```yaml
tools:
  serena: ["go"]
```

**Agent tasks:**
1. Use `get_symbols_overview` on multiple files
2. Use `find_symbol` to locate similar function names
3. Use `search_for_pattern` to find duplicate patterns
4. Identify outliers and suggest consolidation

**Example**: See `semantic-function-refactor.md` workflow (clusters functions semantically)

### Pattern 3: Daily Code Deep Dives

**Workflow**: Systematically analyze codebase over time

```yaml
tools:
  serena: ["go"]
  cache-memory: true  # Track analysis history
```

**Agent tasks:**
1. Load previous analysis state from cache
2. Select rotation strategy (round-robin, prioritize recent updates)
3. Use Serena for deep semantic analysis
4. Save findings to cache for next run
5. Generate actionable improvement tasks

**Examples**: 
- `sergo.md` - Daily Go analysis with 50/50 cached/new strategies
- `go-fan.md` - Daily Go module reviewer with round-robin

### Pattern 4: Compiler/Large File Analysis

**Workflow**: Analyze large complex files for quality

```yaml
tools:
  serena: ["go"]
  cache-memory: true
  bash:
    - "find pkg/workflow -name 'compiler*.go' ! -name '*_test.go'"
    - "wc -l pkg/workflow/compiler*.go"
```

**Agent tasks:**
1. Use bash to identify target files
2. Track file hashes to detect changes
3. Use Serena for semantic analysis only on changed files
4. Focus on human-written quality standards

**Example**: See `daily-compiler-quality.md` workflow

## Best Practices

### 1. Combine with Other Tools

Serena works best **in combination** with standard tools:

```yaml
tools:
  serena: ["go"]
  github:
    toolsets: [default]  # For repo info
  bash:
    - "find pkg -name '*.go'"  # File discovery
    - "cat go.mod"  # Read dependencies
  edit:  # For making changes
```

**Use bash for discovery, Serena for analysis, edit for changes.**

### 2. Use Cache Memory for State

For recurring workflows, track analysis history:

```yaml
tools:
  serena: ["go"]
  cache-memory: true
```

**Pattern**: Load cache → analyze new/changed files → save results → avoid redundant work

### 3. Start with Project Activation

Always activate the project first:

```javascript
// Use activate_project tool
{
  "path": "/home/runner/work/gh-aw/gh-aw"
}
```

### 4. Leverage Memory for Insights

Store long-term insights using Serena's memory:

```javascript
// write_memory tool
{
  "name": "authentication_pattern",
  "content": "Auth logic centralized in pkg/auth/handler.go"
}
```

### 5. Don't Over-Use Serena

**Good**: "Analyze function call graph for authentication flow" → Use Serena

**Bad**: "Read package.json to get version" → Just use bash `cat package.json`

**Rule of thumb**: If grep/bash can do it in 1-2 commands, don't use Serena.

## Common Pitfalls

### ❌ Using Serena for Non-Code Files

**Wrong**: Analyzing YAML/JSON/Markdown with Serena
**Right**: Use `view`, `edit`, or `bash` tools

### ❌ Forgetting to Activate Project

**Wrong**: Directly calling Serena tools without activation
**Right**: Call `activate_project` first

### ❌ Not Combining with Bash

**Wrong**: Using only Serena tools for everything
**Right**: Use bash for file discovery, Serena for semantic analysis

### ❌ Missing Language Configuration

**Wrong**: Adding `serena: true` without specifying language
**Right**: `serena: ["go"]` with explicit language(s)

## Real-World Examples

### Example Workflows Using Serena

1. **go-fan.md** - Go module usage reviewer (83 successful runs)
   - Uses Serena for semantic analysis of module usage
   - Combines with GitHub API for repo metadata
   - Round-robin selection with priority for recent updates

2. **sergo.md** - Daily Go code quality analyzer (17 of 18 runs successful)
   - Scans available Serena tools dynamically
   - 50% cached strategies, 50% new exploration
   - Tracks success metrics in cache

3. **semantic-function-refactor.md** - Function clustering analyzer
   - Uses Serena to find duplicate/similar functions
   - Identifies outliers (functions in wrong files)
   - Semantic pattern detection beyond text matching

4. **daily-compiler-quality.md** - Compiler code quality checker
   - Rotating file analysis with cache tracking
   - Uses Serena for semantic quality assessment
   - Avoids re-analyzing unchanged files

5. **cloclo.md** - Claude-powered general assistant
   - Serena available for code analysis requests
   - Combined with Playwright and other tools
   - Flexible for various task types

### Success Metrics

Based on recent workflow runs:
- **go-fan**: 81/83 successful runs (97.6% success rate)
- **sergo**: 17/18 successful runs (94.4% success rate)
- **Average execution time**: 6-8 minutes for Serena workflows
- **Most common language**: Go (100% of analyzed workflows)

## Decision Tree

```
Does the task require understanding code semantics/structure?
├─ NO  → Use bash, edit, view, grep instead
└─ YES → Continue
    │
    ├─ Is it simple text search/replace?
    │  └─ YES → Use grep/bash instead
    │
    ├─ Is it for non-code files (YAML, JSON, MD)?
    │  └─ YES → Use view/edit instead
    │
    └─ Does it involve:
       ├─ Finding symbol usages?
       ├─ Analyzing code structure?
       ├─ Detecting semantic patterns?
       ├─ Understanding dependencies?
       └─ YES to any → Use Serena! ✅
```

## Getting Help

- **Serena config**: Check `.serena/project.yml` in repository
- **Tool list**: Use `get_current_config` MCP tool to see available tools
- **Language servers**: Serena auto-detects based on project files
- **Debugging**: Use `restart_language_server` if LSP has issues
