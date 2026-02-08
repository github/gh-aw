# Analysis: Can `.github/aw/*.md` Files Be Imported Like `shared/` Files?

## Executive Summary

**YES** - Workflows CAN technically import files from `.github/aw/` directory, but **SHOULD NOT** due to semantic and architectural differences.

## Technical Feasibility

### ✅ What Works

1. **Import Syntax**: Both relative and absolute paths work:
   ```yaml
   imports:
     - ../aw/orchestration.md          # Relative from .github/workflows/
     - .github/aw/orchestration.md     # Absolute path
   ```

2. **Compilation**: Successfully compiles to `.lock.yml` files
3. **Content Merging**: Frontmatter from `.github/aw/` files merges same as `shared/` files
4. **Runtime Loading**: Content loads via `{{#runtime-import}}` macro

### 🔍 Path Resolution Logic

From `pkg/parser/remote_fetch.go`:

```go
// isWorkflowSpec checks if a path looks like a workflowspec
func isWorkflowSpec(path string) bool {
    // ...
    
    // Reject paths that start with "." (local paths like .github/workflows/...)
    if strings.HasPrefix(cleanPath, ".") {
        return false
    }
    
    // Reject paths that start with "shared/" (local shared files)
    if strings.HasPrefix(cleanPath, "shared/") {
        return false
    }
    
    // ...
}
```

**Key Point**: The code explicitly rejects `shared/` prefix as local, but does NOT reject `.github/` prefix. This means `.github/aw/` imports are treated as local file paths and work correctly.

## Directory Purpose & Architecture

### `.github/workflows/shared/` - Workflow Components

**Purpose**: Reusable workflow building blocks

**Contents**:
- Tool configurations (bash, github, web-fetch)
- MCP server setups (tavily, serena, ast-grep)
- Report formatting guidelines
- Data visualization templates
- Network and permission configurations

**Design Pattern**: Component library for composing workflows

**Import Stats**:
- 35+ shared components
- 65% of workflows (84/130) use imports
- Most imported: `reporting.md` (46 imports)

**Example Frontmatter**:
```yaml
---
# Tool configurations
tools:
  bash:
    allowed: [read, write]
  github:
    toolsets: [default]

# Report formatting
---
```

### `.github/aw/` - Agent Configuration Files

**Purpose**: Agent behavior and orchestration prompts

**Contents**:
- Agent configuration (applyTo patterns)
- Meta-instructions for creating/updating workflows
- Orchestration patterns
- Documentation references

**Design Pattern**: Agent instruction files, not workflow components

**Import Stats**:
- 15+ agent files
- **0 workflows** currently import from this directory
- Not designed for reuse across workflows

**Example Frontmatter**:
```yaml
---
name: create-agentic-workflow
description: Create new workflows
applyTo: ".github/workflows/*.md"
infer: false
---
```

## Key Differences

| Aspect | `.github/workflows/shared/` | `.github/aw/` |
|--------|----------------------------|---------------|
| **Primary Purpose** | Reusable workflow components | Agent instructions |
| **Frontmatter Type** | Tool configs, MCP servers | Agent metadata (name, applyTo) |
| **Content Type** | Technical configurations + guidance | Pure agent instructions |
| **Import Pattern** | `shared/file.md` (clean, conventional) | `../aw/file.md` or `.github/aw/file.md` (awkward) |
| **Current Usage** | Heavily imported (46 workflows) | Not imported |
| **Semantic Meaning** | "This is meant to be shared" | "This configures agent behavior" |
| **Directory Convention** | Standard for reusable components | Standard for agent configuration |

## Why `.github/aw/` Should NOT Be Used for Imports

### 1. **Semantic Confusion**

The `.github/aw/` directory has a specific, documented purpose - agent configuration:
- Files like `create-agentic-workflow.md` are prompts for meta-workflows
- Files like `github-agentic-workflows.md` provide documentation
- Using these for imports blurs the distinction

### 2. **Path Clarity**

```yaml
# Clear and intentional
imports:
  - shared/reporting.md

# Awkward and unclear
imports:
  - ../aw/orchestration.md
  - .github/aw/orchestration.md
```

The `shared/` prefix immediately signals "this is meant to be imported."

### 3. **Convention Over Configuration**

The codebase explicitly recognizes `shared/` as the conventional path:
- Path resolution explicitly handles `shared/` prefix
- Documentation consistently uses `shared/` examples
- 65% of workflows follow this pattern

### 4. **Future-Proofing**

If `.github/aw/` becomes a special directory with different semantics (e.g., agent marketplace, compiled agent files), using it for imports could break workflows.

## Recommendations

### ✅ DO: Use `shared/` for Reusable Components

```yaml
---
description: My workflow
imports:
  - shared/reporting.md
  - shared/mcp/tavily.md
  - shared/jqschema.md
---
```

### ❌ DON'T: Import from `.github/aw/`

```yaml
# Avoid this pattern
imports:
  - ../aw/orchestration.md
  - .github/aw/create-agentic-workflow.md
```

### 🔄 Migration: If Content Needs Sharing

If a file in `.github/aw/` contains useful reusable content:

1. Extract the reusable parts
2. Create a new file in `shared/`
3. Keep agent-specific content in `.github/aw/`

**Example**:

```bash
# Move orchestration patterns to shared
cp .github/aw/orchestration.md .github/workflows/shared/orchestration-patterns.md

# Update workflows to use shared version
imports:
  - shared/orchestration-patterns.md
```

## Test Results

### ✅ Compilation Test

```yaml
# Test workflow: .github/workflows/test-aw-import.md
---
description: Test importing from .github/aw
on:
  workflow_dispatch:
engine: copilot
imports:
  - ../aw/orchestration.md
---
```

**Result**: ✓ Compiles successfully to `.lock.yml`

**Generated Content**:
```yaml
# Resolved workflow manifest:
#   Imports:
#     - ../aw/orchestration.md

# Runtime loading:
{{#runtime-import .github/aw/orchestration.md}}
```

**Conclusion**: Technical feasibility confirmed, but architectural guidance remains - use `shared/` for clarity.

## Related Files

- Path resolution: `pkg/parser/remote_fetch.go`
- Import processing: `pkg/parser/import_processor.go`
- Import documentation: `docs/src/content/docs/reference/imports.md`
- Shared components blog: `docs/src/content/docs/blog/2026-01-30-imports-and-sharing.md`

## Action Items

1. ✅ Document that `.github/aw/` CAN be imported (technically)
2. ✅ Recommend NOT importing from `.github/aw/` (architecturally)
3. ✅ Update guidance to use `shared/` for all reusable components
4. 📝 Consider adding linter rule to warn about `.github/aw/` imports
5. 📝 Consider documentation update to clarify directory purposes

## Conclusion

While workflows CAN import from `.github/aw/`, they SHOULD NOT due to semantic differences, path awkwardness, and established conventions. The `shared/` directory is the proper location for reusable workflow components.

Use `.github/aw/` exclusively for agent configuration files and meta-workflow instructions.
