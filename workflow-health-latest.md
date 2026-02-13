# Workflow Health Dashboard - 2026-02-13

## Overview
- **Total workflows**: 149 (149 executable, 0 missing locks)
- **Healthy**: 127 (85%)
- **Warning**: 15 (10%) - Outdated locks
- **Critical**: 7 (5%) - Compilation failures
- **Inactive**: N/A
- **Compilation coverage**: 142/149 (95.3% ⚠️)
- **Overall health score**: 54/100 (↓ -41 from 95/100)

## 🔴 STATUS: DEGRADED - Active Compilation Failures

### Health Assessment Summary

**Status: DEGRADED** 

The ecosystem has **7 workflows failing compilation** due to strict mode firewall changes:
- ❌ **7 workflows failing compilation** (strict mode + custom domains)
- ⚠️ **15 workflows with outdated locks** (source modified after lock)
- ❌ **5 workflows with recent failures** (past 24h)
- ↓ **Health score dropped by 41 points** (95 → 54)
- 🚨 **Systemic issue detected**: Strict mode firewall validation breaking workflows

**Key Changes Since Last Check (2026-02-12):**
- ↓ Health score decreased by -41 points (95 → 54) - CRITICAL
- ❌ 7 workflows now failing compilation (strict mode + custom domains)
- ⚠️ 15 workflows have outdated lock files
- ❌ Issue #15374 created - strict mode firewall validation breaking workflows
- ❌ Agentic Maintenance workflow failing (compilation errors)

## Critical Issues 🚨

### 1. Strict Mode Firewall Breaking Workflows (Priority: P0 - BLOCKING)

**Status:** 7 workflows failing compilation due to strict mode firewall validation

**Root Cause:** Recent change in strict mode validation (commit `ec99734`) now enforces that `copilot`/`claude` engines with strict mode cannot use custom network domains - only known ecosystems (`defaults`, `python`, `node`, etc.) are allowed.

**Affected Workflows:**
1. **blog-auditor.md** - `engine: claude`, `strict: true`, uses `githubnext.com` domain
2. **cli-consistency-checker.md** - `engine: copilot`, uses `api.github.com` domain
3. **cli-version-checker.md** - `engine: claude`, `strict: true`, uses `api.github.com`, `ghcr.io` domains
4. **+4 more workflows** (identified in agentic-maintenance logs)

**Error Message:**
```
strict mode: engine 'copilot' does not support LLM gateway and requires 
network domains to be from known ecosystems (e.g., 'defaults', 'python', 'node'). 
Custom domains are not allowed for security.
```

**Impact:** 
- Workflows cannot compile and deploy
- Agentic Maintenance workflow failing (run #21984242074)
- Blocks release readiness

**Tracking:** Issue #15374 (open)

**Recommended Actions:**
1. **Immediate fix (P0):** Update affected workflows to either:
   - Set `strict: false` to allow custom domains, OR
   - Remove custom domains and use ecosystem shortcuts (`defaults`, `node`, etc.)
2. **Test fix:** Run `gh aw compile --validate` to verify all 149 workflows compile
3. **Document:** Add migration guide for workflows using custom domains + strict mode

**Example Fix:**
```yaml
# BEFORE (fails):
engine: copilot
strict: true
network:
  allowed: [defaults, "api.github.com"]

# AFTER (option 1 - disable strict):
engine: copilot
strict: false
network:
  allowed: [defaults, "api.github.com"]

# AFTER (option 2 - use ecosystem):
engine: copilot
strict: true
network:
  allowed: [defaults, node]  # api.github.com covered by 'node' ecosystem
```

### 2. Daily Fact Workflow - Stale Action Pin (Priority: P2 - Maintenance)

**Status:** Workflow failing due to stale action pin (MODULE_NOT_FOUND: handle_noop_message.cjs)

**Analysis:**
- **Root Cause**: Workflow lock file uses stale action pin (`c4e091835c7a94dc7d3acb8ed3ae145afb4995f3`)
- **Missing File**: `handle_noop_message.cjs` doesn't exist in that pinned commit
- **File Added**: After commit `c4e091835c7a94dc7d3acb8ed3ae145afb4995f3` in commit `855fefb7`
- **Impact**: Workflow fails at conclusion step
- **Latest Failure**: [§21984740800](https://github.com/github/gh-aw/actions/runs/21984740800)
- **Issue Status**: #15380 (open)

**Resolution:**
```bash
gh aw compile .github/workflows/daily-fact.md
```

### 3. Daily Copilot Token Report - No Safe Output (Priority: P2 - Expected Behavior)

**Status:** Workflow "fails" but this is expected when no tool calls made

**Analysis:**
- **Run**: [§21984730054](https://github.com/github/gh-aw/actions/runs/21984730054)
- **Cause**: No safe output tool calls made during execution
- **Impact**: Low - expected behavior pattern
- **Action**: Monitor for actual failures vs. expected no-ops

## Warnings ⚠️

### Outdated Lock Files (15 workflows)

The following workflows have source `.md` files modified after their `.lock.yml` files were compiled:

1. safe-output-health.md
2. technical-doc-writer.md
3. lockfile-stats.md
4. daily-team-evolution-insights.md
5. daily-repo-chronicle.md
6. notion-issue-summary.md
7. chroma-issue-indexer.md
8. functional-pragmatist.md
9. stale-repo-identifier.md
10. developer-docs-consolidator.md
11. daily-copilot-token-report.md
12. prompt-clustering-analysis.md
13. claude-code-user-docs-review.md
14. daily-news.md
15. repo-audit-analyzer.md

**Recommendation:** Run `make recompile` to update all outdated lock files.

**Impact:** Medium - workflows may run with outdated configurations, causing unexpected behavior.

## Healthy Workflows ✅

**127 workflows (85%)** operating normally with up-to-date lock files and no compilation issues.

## Systemic Issues

### Issue: Strict Mode Firewall Validation Breaking Workflows

- **Affected workflows:** 7+ workflows
- **Pattern:** Workflows using `copilot`/`claude` engines with `strict: true` + custom network domains
- **Root cause:** Recent validation change (commit `ec99734`) enforces ecosystem-only domains in strict mode
- **Recommendation:** 
  1. Update affected workflows to use `strict: false` or ecosystem shortcuts
  2. Document breaking change and migration path
  3. Add validation tests for strict mode + custom domains
- **Action:** Issue #15374 created with detailed analysis and recommended fixes
- **Priority:** P0 (BLOCKING) - affects compilation and deployment

## Ecosystem Statistics (Past 7 Days)

### Run Statistics
- **Total workflow runs**: 30
- **Successful runs**: 9 (30%)
- **Failed runs**: 5 (17%)
- **Action required**: 12 (40%)
- **Skipped**: 2 (7%)
- **In progress**: 2 (7%)

### Success Rate Breakdown
- **Pure success rate** (success/total): 30%
- **Operational success rate** (success + action_required): 70%
- **Failure rate**: 17% (concerning increase)

**Note**: High "action_required" rate is expected - these are PR-triggered workflows awaiting human approval/review.

### Recent Failures (Past 48h)
1. **Daily Fact About gh-aw** - 1 failure (stale action pin)
2. **Daily Copilot Token Consumption Report** - 1 failure (no safe output)
3. **Agentic Maintenance** - 1 failure (compilation errors - strict mode)
4. **Running Copilot coding agent** - 1 failure
5. **CI** - 1 failure (strict mode tests)

## Trends

- **Overall health score**: 54/100 (↓ -41 from 95/100, CRITICAL DEGRADATION)
- **New failures this period**: 7 compilation failures
- **Ongoing failures**: 3 (daily-fact, agentic-maintenance, CI)
- **Fixed issues this period**: 0
- **Average workflow health**: 85% (127/149 healthy)
- **Compilation success rate**: 95.3% (142/149)

### Historical Comparison
| Date | Health Score | Critical Issues | Compilation Coverage | Workflow Count | Notable Issues |
|------|--------------|-----------------|---------------------|----------------|----------------|
| 2026-02-08 | 96/100 | 0 workflows | 100% | 147 | - |
| 2026-02-09 | 97/100 | 0 workflows | 100% | 148 | - |
| 2026-02-10 | 78/100 | 1 workflow | 100% | 148 | 11 outdated locks, daily-fact |
| 2026-02-11 | 82/100 | 1 workflow | 99.3% | 148 | daily-fact (ongoing), agentics-maintenance (transient) |
| 2026-02-12 | 95/100 | 0 workflows | 100% | 148 | daily-fact (stale action pin) |
| 2026-02-13 | 54/100 | 7 workflows | 95.3% | 149 | **Strict mode breaking changes** |

**Trend**: ↓ **CRITICAL DEGRADATION** - Health score dropped 41 points due to strict mode firewall validation changes

## Recommendations

### High Priority (P0 - BLOCKING)

1. **Fix strict mode firewall validation breaking 7+ workflows**
   - Update affected workflows to use `strict: false` or ecosystem shortcuts
   - Test with `gh aw compile --validate` to ensure all workflows compile
   - Document breaking change and provide migration guide
   - **Tracking:** Issue #15374
   - **Impact:** BLOCKING - prevents compilation and deployment

### Medium Priority (P1 - High)

1. **Recompile 15 outdated lock files**
   - Run `make recompile` to update all outdated locks
   - Verify workflows compile without errors
   - Commit and push updated lock files

2. **Fix daily-fact stale action pin**
   - Recompile workflow: `gh aw compile .github/workflows/daily-fact.md`
   - Verify action pin updated to include `handle_noop_message.cjs`
   - **Tracking:** Issue #15380

### Medium Priority (P2 - Maintenance)

1. **Document strict mode ecosystem requirements**
   - Add migration guide for workflows using custom domains
   - Document which domains are covered by ecosystem shortcuts
   - Update reference documentation with examples

2. **Add strict mode validation tests**
   - Add tests for strict mode + custom domains (should fail)
   - Add tests for strict mode + ecosystem shortcuts (should pass)
   - Ensure regression tests cover new validation rules

### Low Priority (P3 - Nice to Have)

1. **Monitor "action_required" workflows**
   - Track PR-triggered workflows awaiting approval
   - Ensure timely human review of automated PRs

## Actions Taken This Run

- ✅ Comprehensive health assessment completed
- ✅ Analyzed 30 workflow runs from past 7 days
- ✅ Identified 7 critical compilation failures (strict mode)
- ✅ Root cause analysis: strict mode firewall validation breaking changes
- ✅ Found existing issue #15374 tracking the strict mode problem
- ✅ Identified 15 workflows with outdated lock files
- ✅ Calculated health score: 54/100 (critical degradation)
- ✅ Updated shared memory with current health status
- ✅ Generating coordination notes for other meta-orchestrators

## Release Mode Assessment

**Release Mode Status**: ❌ **NOT PRODUCTION READY**

Given the **release mode** focus on quality, security, and documentation:
- ❌ **7 workflows failing compilation** (BLOCKING)
- ❌ **95.3% compilation coverage** (below 100% target)
- ⚠️ **85% workflows healthy** (below 95% target)
- ❌ **Systemic issue affecting multiple workflows** (strict mode)
- ⚠️ **15 workflows with outdated locks** (configuration drift)
- ❌ **Health score at 54/100** (critical threshold is 80/100)

**Recommendation**: System is **NOT production-ready**. The strict mode firewall validation changes introduced a breaking change that affects 7+ workflows and blocks compilation. This must be resolved before release.

**Blocking issues:**
1. Fix strict mode firewall validation (Issue #15374) - P0
2. Recompile outdated lock files - P1
3. Achieve 100% compilation coverage - P1

---
> **Last updated**: 2026-02-13T11:31:25Z  
> **Next check**: Automatic on next trigger or 2026-02-14  
> **Workflow run**: [§21985231505](https://github.com/github/gh-aw/actions/runs/21985231505)
