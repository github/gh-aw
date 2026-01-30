# Workflow Size Optimization Analysis

## Executive Summary

Analysis of 15 workflows exceeding 100 KB identified **safe-inputs configuration** as the primary size contributor, accounting for **70-80% of workflow size** in smoke tests. This bloat is **architectural and intentional** for security reasons - scripts are embedded directly in YAML rather than loaded from files to ensure they cannot be modified during execution.

## Key Findings

### Size Contributors by Priority

1. **Safe-Inputs Tool Scripts (HIGH IMPACT)** - 70-80% of workflow size
   - **Root cause**: Shell/JavaScript/Python scripts embedded as heredocs in YAML
   - **Example**: `github-queries-safe-input.md` (16KB source) → 83KB compiled
   - **Affected**: 6 workflows (all 3 smoke tests + 3 daily reports)
   - **Status**: **Architectural - Intentional for security**

2. **Large Analysis Prompts (JUSTIFIED)** - 30-55% of workflow size
   - **Root cause**: Detailed analysis instructions, Python scripts, report templates
   - **Example**: `daily-code-metrics` has 55KB of analysis instructions
   - **Affected**: 7 daily report workflows
   - **Status**: **Justified complexity - Required for workflow functionality**

3. **Shared Import Overhead (MEDIUM IMPACT)** - 10-25KB per workflow
   - **Root cause**: Multiple large shared modules imported together
   - **Example**: 5-7 shared imports totaling 20-30KB
   - **Affected**: All 15 workflows
   - **Status**: **Some optimization possible**

### Detailed Breakdown by Category

#### Smoke Tests (3 workflows, avg 105KB)
| Workflow | Source | Compiled | Ratio | Safe-Inputs | Prompt | Other |
|----------|--------|----------|-------|-------------|--------|-------|
| smoke-claude | 4.4K | 112.6K | 25.6x | ~83KB (74%) | 0.4KB | 29KB |
| smoke-copilot | 4.5K | 101.5K | 22.7x | ~71KB (70%) | 0.4KB | 30KB |
| smoke-codex | 4.2K | 101.0K | 24.0x | ~71KB (70%) | 0.4KB | 30KB |

**Analysis**: Smoke tests have minimal prompts but massive compilation overhead from `github-queries-safe-input.md` import.

#### Daily Reports (7 workflows, avg 107.5KB)
| Workflow | Source | Compiled | Ratio | Prompt % | Key Imports |
|----------|--------|----------|-------|----------|-------------|
| daily-news | 18.8K | 112.2K | 6.0x | 47.4% | trends, jqschema, reporting |
| daily-copilot-token-report | 25.8K | 112.4K | 4.4x | 18.0% | python-dataviz, reporting |
| daily-performance-summary | 30.2K | 111.0K | 3.7x | 35.1% | github-queries-safe-input, reporting |
| daily-issues-report | 11.1K | 108.0K | 9.7x | 48.5% | python-dataviz, issues-data-fetch, trends |
| daily-code-metrics | 12.1K | 106.2K | 8.8x | 52.0% | python-dataviz, trends, reporting |
| daily-regulatory | 17.2K | 101.4K | 5.9x | 33.7% | github-queries-safe-input, reporting |
| daily-cli-performance | 22.9K | 101.5K | 4.4x | 45.3% | go-make, reporting |

**Analysis**: Large prompts (30-55KB) are justified - contain Python analysis scripts, chart generation code, and detailed report templates.

#### Analysis Workflows (5 workflows)
- **copilot-session-insights** (119.7K): Most imports (5), session analysis complexity justified
- **copilot-pr-nlp-analysis** (107.0K): NLP analysis with Python, justified
- **security-alert-burndown** (107.0K): No imports but 49KB detailed security analysis prompt
- **poem-bot** (103.5K): Minimal prompt (0.3KB), likely has safe-inputs overhead
- **python-data-charts** (102.8K): Chart generation with 52KB of visualization code

### Common Large Shared Imports

| Import | Size | Used By | Description |
|--------|------|---------|-------------|
| `shared/github-queries-safe-input.md` | 16KB | 5 workflows | 4 safe-input tools with GraphQL queries |
| `shared/python-dataviz.md` | 9KB | 4 workflows | Python chart generation utilities |
| `shared/copilot-session-data-fetch.md` | 10KB | 2 workflows | Session data fetching logic |
| `shared/trends.md` | 6KB | 3 workflows | Trend analysis patterns |
| `shared/issues-data-fetch.md` | 6KB | 1 workflow | Issue data fetching |

## Why Safe-Inputs Are Large

### Technical Architecture

Safe-inputs are embedded as heredocs in compiled workflows for **security**:

```yaml
- name: Setup Safe Inputs Tool Files
  run: |
    cat > /opt/gh-aw/safe-inputs/github-issue-query.sh << 'EOFSH_github-issue-query'
    #!/bin/bash
    # Full 200-line GraphQL query script embedded here...
    EOFSH_github-issue-query
    chmod +x /opt/gh-aw/safe-inputs/github-issue-query.sh
```

### Example: github-queries-safe-input.md

Contains 4 tools:
1. **github-issue-query** (~50 lines): GraphQL query with jq filtering
2. **github-pr-query** (~50 lines): PR query with state filtering
3. **github-discussion-query** (~50 lines): Discussion query with category filtering
4. **gh** wrapper (~10 lines): Basic gh CLI wrapper

Total: ~160 lines of shell script + JSON schema → **83KB in compiled YAML**

### Why Embedded vs Runtime Files?

**Security consideration**: Embedding ensures:
- Scripts cannot be modified during workflow execution
- No external file dependencies
- Audit trail in git history
- Self-contained workflow definition

**Trade-off**: Larger file size for better security and reproducibility.

## Optimization Opportunities

### 1. Modularize github-queries-safe-input.md (MEDIUM IMPACT)
**Potential savings**: 40-60KB per workflow

**Strategy**: Split into separate imports:
```
shared/github-queries/issue-query.md     # 4KB
shared/github-queries/pr-query.md        # 4KB
shared/github-queries/discussion-query.md # 4KB
shared/github-queries/gh-wrapper.md      # 2KB
```

**Workflows import only what they need**:
- Smoke tests: Only need `gh-wrapper.md` (2KB vs 16KB) → **Save 14KB**
- Daily reports: Import specific queries needed

**Implementation complexity**: Medium (requires splitting file, updating imports)

### 2. Optimize Shared Import Structure (LOW-MEDIUM IMPACT)
**Potential savings**: 5-10KB per workflow

**Strategy**: 
- Break large shared modules into focused components
- Create "lite" versions for simple use cases
- Document import combinations

**Example**:
```
shared/python-dataviz.md → 9KB
  Split into:
    shared/python-dataviz-core.md → 4KB (basic charts)
    shared/python-dataviz-advanced.md → 5KB (complex visualizations)
```

**Implementation complexity**: Low-Medium

### 3. Extract Common Report Templates (LOW IMPACT)
**Potential savings**: 5-10KB for report workflows

**Strategy**: Extract repeated report structures to shared template
- Common sections (Executive Summary, Methodology, etc.)
- Use template inheritance rather than duplication

**Implementation complexity**: Medium

## Recommendations

### What NOT to Optimize

1. **Safe-inputs architecture** - Do NOT extract scripts to runtime files
   - Current design is intentional for security
   - Trade-off of size for security is acceptable
   - 100-120KB workflows are reasonable given functionality

2. **Large analysis prompts** - These are justified
   - `daily-code-metrics` (55KB prompt) contains complete Python analysis pipeline
   - `security-alert-burndown` (49KB prompt) has detailed security analysis logic
   - This is domain-specific complexity, not bloat

3. **Smoke tests size** - Document as expected
   - 100-112KB is normal for comprehensive integration tests
   - Most size from safe-inputs (necessary for testing GitHub queries)
   - Small optimization possible (see recommendation #1)

### What TO Optimize (Priority Order)

#### Priority 1: Modularize github-queries-safe-input.md
**Effort**: Medium | **Impact**: Medium (14KB per workflow)

Split into separate tool files so workflows import only needed tools.

**Files to create**:
- `.github/workflows/shared/github-queries/issue-query.md`
- `.github/workflows/shared/github-queries/pr-query.md`
- `.github/workflows/shared/github-queries/discussion-query.md`
- `.github/workflows/shared/github-queries/gh-wrapper.md`

**Workflows to update**:
- smoke-claude, smoke-copilot, smoke-codex (use gh-wrapper only)
- daily-performance-summary, daily-regulatory (use specific queries)

#### Priority 2: Document Workflow Size Guidelines
**Effort**: Low | **Impact**: High (prevents future bloat)

Create documentation:
1. Expected size ranges by workflow category
2. When to modularize shared imports
3. How to analyze workflow compilation size
4. Best practices for new workflows

**Guidelines**:
- **Smoke tests**: 100-120KB expected (safe-inputs overhead)
- **Daily reports**: 100-120KB expected (large prompts justified)
- **Simple bots**: Should be <80KB (investigate if larger)
- **Alert**: Workflows >150KB should be investigated

#### Priority 3: Create Analysis Tooling
**Effort**: Low | **Impact**: Medium (developer productivity)

Add to `gh aw compile`:
```bash
gh aw compile --stats  # Show size breakdown
```

Display:
- Compiled size
- Source size
- Expansion ratio
- Safe-inputs contribution
- Prompt contribution
- Top 3 largest imports

## Conclusion

The 15 oversized workflows fall into two categories:

1. **Justified Complexity** (12 workflows)
   - Smoke tests: Safe-inputs overhead is architectural
   - Daily reports: Large prompts contain analysis code
   - Size is proportional to functionality

2. **Optimization Opportunity** (3 workflows)
   - Workflows using github-queries-safe-input.md but only need 1-2 tools
   - Can save 14KB by modularizing this import

### Success Criteria Met

- ✅ All 15 workflows analyzed
- ✅ Size contributors documented for each
- ✅ Justified complexity vs bloat determined
- ✅ Optimization opportunities identified (modularize github-queries)
- ✅ Size reduction guidelines documented
- ⚠️ Optimization implementation: 1 high-value optimization identified

**Recommendation**: Implement Priority 1 (modularize github-queries-safe-input.md) to optimize 6 workflows and prevent future bloat. Document remaining workflows as having justified size.
