---
description: Detects inconsistencies between JSON schema, implementation code, and documentation
on:
  schedule: daily
  workflow_dispatch:
permissions:
  contents: read
  discussions: read
  issues: read
  pull-requests: read
engine: claude
tools:
  edit:
  bash: ["*"]
  github:
    mode: remote
    toolsets: [default, discussions]
  cache-memory:
    key: schema-consistency-cache-${{ github.workflow }}
safe-outputs:
  create-discussion:
    category: "audits"
    title-prefix: "[Schema Consistency] "
    max: 1
    close-older-discussions: true
timeout-minutes: 30
imports:
  - shared/mood.md
  - shared/reporting.md
---

# Schema Consistency Checker

You are an expert system that detects inconsistencies between:
- The main JSON schema of the frontmatter (`pkg/parser/schemas/main_workflow_schema.json`)
- The parser and compiler implementation (`pkg/parser/*.go` and `pkg/workflow/*.go`)
- The documentation (`docs/src/content/docs/**/*.md`)
- The workflows in the project (`.github/workflows/*.md`)

## Mission

Analyze the repository to find inconsistencies across these four key areas and create a discussion report with actionable findings.

## Cache Memory Strategy Storage

Use the cache memory folder at `/tmp/gh-aw/cache-memory/` to store and reuse successful analysis strategies:

1. **Read Previous Strategies**: Check `/tmp/gh-aw/cache-memory/strategies.json` for previously successful detection methods
2. **Strategy Selection**: 
   - 70% of the time: Use a proven strategy from the cache
   - 30% of the time: Try a radically different approach to discover new inconsistencies
   - Implementation: Use the day of year (e.g., `date +%j`) modulo 10 to determine selection: values 0-6 use proven strategies, 7-9 try new approaches
3. **Update Strategy Database**: After analysis, save successful strategies to `/tmp/gh-aw/cache-memory/strategies.json`

Strategy database structure:
```json
{
  "strategies": [
    {
      "id": "strategy-1",
      "name": "Schema field enumeration check",
      "description": "Compare schema enum values with parser constants",
      "success_count": 5,
      "last_used": "2024-01-15",
      "findings": 3
    }
  ],
  "last_updated": "2024-01-15"
}
```

## Analysis Areas

### 1. Schema vs Parser Implementation

**Check for:**
- Fields defined in schema but not handled in parser/compiler
- Fields handled in parser/compiler but missing from schema
- Type mismatches (schema says `string`, parser expects `object`)
- Enum values in schema not validated in parser/compiler
- Required fields not enforced
- Default values inconsistent between schema and parser/compiler

**Key files to analyze:**
- `pkg/parser/schemas/main_workflow_schema.json`
- `pkg/parser/schemas/mcp_config_schema.json`
- `pkg/parser/frontmatter.go` and `pkg/parser/*.go`
- `pkg/workflow/compiler.go` - main workflow compiler
- `pkg/workflow/tools.go` - tools configuration processing
- `pkg/workflow/safe_outputs.go` - safe-outputs configuration
- `pkg/workflow/cache.go` - cache and cache-memory configuration
- `pkg/workflow/permissions.go` - permissions processing
- `pkg/workflow/engine.go` - engine config and network permissions types
- `pkg/workflow/domains.go` - network domain allowlist functions
- `pkg/workflow/engine_network_hooks.go` - network hook generation
- `pkg/workflow/engine_firewall_support.go` - firewall support checking
- `pkg/workflow/strict_mode.go` - strict mode validation
- `pkg/workflow/stop_after.go` - stop-after processing
- `pkg/workflow/safe_jobs.go` - safe-jobs configuration (internal - accessed via safe-outputs.jobs)
- `pkg/workflow/runtime_setup.go` - runtime overrides
- `pkg/workflow/github_token.go` - github-token configuration
- `pkg/workflow/*.go` (all workflow processing files that use frontmatter)

### 2. Schema vs Documentation

**Check for:**
- Schema fields not documented
- Documented fields not in schema
- Type descriptions mismatch
- Example values that violate schema
- Missing or outdated examples
- Enum values documented but not in schema

**Key files to analyze:**
- `docs/src/content/docs/reference/frontmatter.md`
- `docs/src/content/docs/reference/frontmatter-full.md`
- `docs/src/content/docs/reference/*.md` (all reference docs)

### 3. Schema vs Actual Workflows

**Check for:**
- Workflows using fields not in schema
- Workflows using deprecated fields
- Invalid field values according to schema
- Missing required fields
- Type violations in actual usage
- Undocumented field combinations

**Key files to analyze:**
- `.github/workflows/*.md` (all workflow files)
- `.github/workflows/shared/**/*.md` (shared components)

### 4. Parser vs Documentation

**Check for:**
- Parser/compiler features not documented
- Documented features not implemented in parser/compiler
- Error messages that don't match docs
- Validation rules not documented

**Focus on:**
- `pkg/parser/*.go` - frontmatter parsing
- `pkg/workflow/*.go` - workflow compilation and feature processing

## Detection Strategies

Here are proven strategies you can use or build upon:

### Strategy 1: Field Enumeration Diff
1. Extract all field names from schema
2. Extract all field names from parser code (look for YAML tags, map keys)
3. Extract all field names from documentation
4. Compare and find missing/extra fields

### Strategy 2: Type Analysis
1. For each field in schema, note its type
2. Search parser for how that field is processed
3. Check if types match
4. Report type mismatches

### Strategy 3: Enum Validation
1. Extract enum values from schema
2. Search for those enums in parser validation
3. Check if all enum values are handled
4. Find undocumented enum values

### Strategy 4: Example Validation
1. Extract code examples from documentation
2. Validate each example against the schema
3. Report examples that don't validate
4. Suggest corrections

### Strategy 5: Real-World Usage Analysis
1. Parse all workflow files in the repo
2. Extract frontmatter configurations
3. Check each against schema
4. Find patterns that work but aren't in schema (potential missing features)

### Strategy 6: Grep-Based Pattern Detection
1. Use bash/grep to find specific patterns
2. Example: `grep -r "type.*string" pkg/parser/schemas/ | grep engine`
3. Cross-reference with parser implementation

## Implementation Steps

### Step 1: Load Previous Strategies
```bash
# Check if strategies file exists
if [ -f /tmp/gh-aw/cache-memory/strategies.json ]; then
  cat /tmp/gh-aw/cache-memory/strategies.json
fi
```

### Step 2: Choose Strategy
- If cache exists and has strategies, use proven strategy 70% of time
- Otherwise or 30% of time, try new/different approach

### Step 3: Execute Analysis
Use chosen strategy to find inconsistencies. Examples:

**Example: Field enumeration**
```bash
# Extract schema fields using jq for robust JSON parsing
jq -r '.properties | keys[]' pkg/parser/schemas/main_workflow_schema.json 2>/dev/null | sort -u

# Extract parser fields from pkg/parser (look for yaml tags)
grep -r "yaml:\"" pkg/parser/*.go | grep -o 'yaml:"[^"]*"' | sort -u

# Extract workflow compiler fields from pkg/workflow (look for yaml tags and frontmatter access)
grep -r "yaml:\"" pkg/workflow/*.go | grep -o 'yaml:"[^"]*"' | sort -u
grep -r 'frontmatter\["[^"]*"\]' pkg/workflow/*.go | grep -o '\["[^"]*"\]' | sort -u

# Extract documented fields
grep -r "^###\? " docs/src/content/docs/reference/frontmatter.md
```

**Example: Type checking**
```bash
# Find schema field types (handles different JSON Schema patterns)
jq -r '
  (.properties // {}) | to_entries[] | 
  "\(.key): \(.value.type // .value.oneOf // .value.anyOf // .value.allOf // "complex")"
' pkg/parser/schemas/main_workflow_schema.json 2>/dev/null || echo "Failed to parse schema"
```

### Step 4: Record Findings
Create a structured list of inconsistencies found:

```markdown
## Inconsistencies Found

### Schema ↔ Parser Mismatches
1. **Field `engine.version`**: 
   - Schema: defines as string
   - Parser: not validated in frontmatter.go
   - Impact: Invalid values could pass through

### Schema ↔ Documentation Mismatches  
1. **Field `cache-memory`**:
   - Schema: defines array of objects with `id` and `key`
   - Docs: only shows simple boolean example
   - Impact: Advanced usage not documented

### Parser ↔ Documentation Mismatches
1. **Error message for invalid `on` field**:
   - Parser: "trigger configuration is required"
   - Docs: doesn't mention this error
   - Impact: Users may not understand error
```

### Step 5: Update Cache
Save successful strategy and findings to cache:
```bash
# Update strategies.json with results
cat > /tmp/gh-aw/cache-memory/strategies.json << 'EOF'
{
  "strategies": [...],
  "last_updated": "2024-XX-XX"
}
EOF
```

### Step 6: Create Discussion
Generate a comprehensive report for discussion output.

## Discussion Report Format

Create a well-structured discussion report:

```markdown
# 🔍 Schema Consistency Check - [DATE]

## Summary

- **Inconsistencies Found**: [NUMBER]
- **Categories Analyzed**: Schema, Parser, Documentation, Workflows
- **Strategy Used**: [STRATEGY NAME]
- **New Strategy**: [YES/NO]

## Critical Issues

[List high-priority inconsistencies that could cause bugs]

## Documentation Gaps

[List areas where docs don't match reality]

## Schema Improvements Needed

[List schema enhancements needed]

## Parser Updates Required

[List parser code that needs updates]

## Workflow Violations

[List workflows using invalid/undocumented features]

## Recommendations

1. [Specific actionable recommendation]
2. [Specific actionable recommendation]
3. [...]

## Strategy Performance

- **Strategy Used**: [NAME]
- **Findings**: [COUNT]
- **Effectiveness**: [HIGH/MEDIUM/LOW]
- **Should Reuse**: [YES/NO]

## Next Steps

- [ ] Fix schema definitions
- [ ] Update parser validation
- [ ] Update documentation
- [ ] Fix workflow files
```

## Important Guidelines

### Security
- Never execute untrusted code from workflows
- Validate all file paths before reading
- Sanitize all grep/bash commands
- Read-only access to schema, parser, and documentation files for analysis
- Only modify files in `/tmp/gh-aw/cache-memory/` (never modify source files)

### Quality
- Be thorough but focused on actionable findings
- Prioritize issues by severity (critical bugs vs documentation gaps)
- Provide specific file:line references when possible
- Include code snippets to illustrate issues
- Suggest concrete fixes

### Efficiency  
- Use bash tools efficiently (grep, jq, etc.)
- Cache results when re-analyzing same data
- Don't re-check things found in previous runs (check cache first)
- Focus on high-impact areas

### Strategy Evolution
- Try genuinely different approaches when not using cached strategies
- Document why a strategy worked or failed
- Update success metrics in cache
- Consider combining successful strategies

## Tools Available

You have access to:
- **bash**: Any command (use grep, jq, find, cat, etc.)
- **edit**: Create/modify files in cache memory
- **github**: Read repository data, discussions

## Success Criteria

A successful run:
- ✅ Analyzes all 4 areas (schema, parser, docs, workflows)
- ✅ Uses or creates an effective detection strategy
- ✅ Updates cache with strategy results
- ✅ Finds at least one category of inconsistencies OR confirms consistency
- ✅ Creates a detailed discussion report
- ✅ Provides actionable recommendations

Begin your analysis now. Check the cache, choose a strategy, execute it, and report your findings in a discussion.
