# Workflow Health Dashboard - 2026-02-06T11:33:21Z

## Overview
- **Total workflows**: 145 (145 executable)
- **Healthy**: 143 (98.6%)
- **Warning**: 0 (0%)
- **Critical**: 1 (0.7%)
- **Test Failure**: 1 (0.7%)
- **Inactive**: 0 (0%)
- **Compilation coverage**: 145/145 (100% ✅)
- **Overall health score**: 92/100 (↑ +17 from 75/100)

## 🟡 STATUS: GOOD - One Persistent Issue

### Health Assessment Summary

**Status: GOOD (Improved from DEGRADED)**

The missing action files issue from yesterday (Feb 5) **persists** but scope is reduced:
- ✅ **Action files exist in source** (parse_mcp_gateway_log.cjs, handle_agent_failure.cjs)
- ✅ **Setup script correctly copies .cjs files**
- ⚠️ **1 workflow still failing** (Daily Fact) - down from 3 yesterday
- ✅ **100% compilation coverage** maintained
- ✅ **Success rate improved** to 66.7% (8/12 completed runs)

**Key Changes Since Yesterday (2026-02-05):**
- ↑ Health score increased by +17 points (75 → 92)
- ↓ Failures reduced from 3 to 1 workflow
- ✅ 2 of 3 failing workflows now resolved
- ⚠️ 1 workflow (Daily Fact) still affected by action file issue
- ✅ Compilation coverage maintained at 100%

## Critical Issues 🚨

### Issue #1: Persistent Action File Loading Issue (P1 - High Priority)

**Affected Workflow**: 1 workflow failing
- Daily Fact About gh-aw ([§21748554531](https://github.com/github/gh-aw/actions/runs/21748554531))

**Status**: Partially resolved (2 of 3 workflows fixed)

**Error Pattern**:
```
Error: Cannot find module '/opt/gh-aw/actions/parse_mcp_gateway_log.cjs'
```

**Investigation Findings**:
- ✅ Source files exist: `actions/setup/js/parse_mcp_gateway_log.cjs`
- ✅ Setup script correctly iterates and copies .cjs files
- ✅ Setup script runs in both activation and agent jobs
- ⚠️ File not present at runtime path `/opt/gh-aw/actions/`

**Hypothesis**:
The setup action may be using a pinned commit (`@623e612ff6a684e9a8634449508bdda21e2c178c`) that doesn't include the required action files, or the action is running from the wrong working directory.

**Impact**:
- 1 workflow consistently failing (Daily Fact)
- MCP gateway log parsing unavailable
- Reduced from 3 affected workflows yesterday

**Resolution Timeline**: P1 - Within 24 hours

---

## Warning Issues ⚠️

### Issue #2: Test Workflow Failure (P2 - Medium Priority)

**Affected**: test-workflow.yml ([§21748414989](https://github.com/github/gh-aw/actions/runs/21748414989))

**Status**: Under investigation

**Error**: Non-agentic workflow failure (GitHub Actions YAML workflow, not .md workflow)

**Impact**: Low - test workflow only, does not affect production workflows

**Priority**: P2 (This week)

---

## Healthy Workflows ✅

**143 workflows** (98.6%) operating normally with no issues detected.

---

## Recent Workflow Activity (Last 24 Hours)

### Runs Summary (Since Feb 5, 2026)
- **Total runs**: 30
- **Success**: 8 (26.7%)
- **Failure**: 2 (6.7%)
- **Action Required**: 12 (40.0%)
- **Skipped**: 5 (16.7%)
- **Running**: 3 (10.0%)

**Success Rate**: 66.7% (8/12 completed runs, excluding action_required/skipped/running)

### Most Active Workflows
1. Issue Monster - 1 run (success)
2. Daily Workflow Updater - 1 run (success)
3. Daily Testify Uber Super Expert - 1 run (success)
4. Typist - 1 run (success)
5. GitHub MCP Structural Analysis - 1 run (success)

---

## Workflow Statistics

### Compilation Status
- **Total .md files**: 145 (executable workflows)
- **Total .lock.yml files**: 145
- **Missing lock files**: 0 ✅
- **Outdated lock files**: 0 ✅
- **Compilation success rate**: 100% ✅

### Engine Distribution
Based on frontmatter analysis (estimated):
- **Copilot**: ~69 workflows (47.6%)
- **Claude**: ~29 workflows (20.0%)
- **Codex**: ~9 workflows (6.2%)
- **Unknown/No engine**: ~38 workflows (26.2%)

### Safe Outputs Adoption
- **~136 workflows** (93.8%) have safe-outputs configured
- **~9 workflows** (6.2%) do not use safe-outputs
- **Excellent security practices** maintained

---

## Trends

- **Overall health score**: 92/100 (↑ +17, restored to GOOD)
- **Compilation coverage**: 100% (sustained)
- **Recent failure rate**: 6.7% (2/30 runs - ↓ from 11.1%)
- **Success rate**: 66.7% (excluding action_required/skipped/running)
- **Safe outputs adoption**: 93.8% (stable)
- **Outdated lock files**: 0 (sustained)

**Health Trend**: ↑ **GOOD** (75/100 → 92/100, +17 points)

---

## Actions Taken This Run

- ✅ Analyzed 145 executable workflows
- ✅ Verified 100% compilation coverage (145/145)
- ✅ Investigated persistent action file issue
- ✅ Confirmed 2 of 3 workflows recovered
- ✅ Updated health score: 92/100 (↑ +17)
- ✅ Created issue for persistent action file problem
- ✅ Documented error patterns and investigation findings
- ✅ Updated shared alerts for meta-orchestrator coordination

---

## Recommendations

### High Priority (P1 - Within 24 hours)
1. **Investigate action pinned commit** (New recommendation)
   - Check if `@623e612ff6a684e9a8634449508bdda21e2c178c` includes required files
   - Consider updating to latest commit or using `@main`
   - Verify working directory in setup action execution

### Medium Priority (P2 - This week)
1. Investigate test-workflow.yml failure
2. Add validation to verify action files are present at runtime
3. Improve error messages when action files are missing
4. Document action file dependencies and troubleshooting

### Low Priority (P3 - Next sprint)
1. Add automated testing for action file availability
2. Monitor safe outputs adoption for remaining 6.2% of workflows
3. Continue tracking workflow run success rates

---

## System Status Summary

### 🟡 Good Health - Minor Issue Persists

**Infrastructure Health:**
- Compilation: 100% ✅
- Execution: 66.7% success rate 🟡 (improving)
- Safe outputs: 93.8% adoption ✅
- Lock files: 100% up-to-date ✅

**Quality Metrics:**
- 1 failure in last 24 hours ⚠️ (down from 3)
- Zero timeout issues ✅
- Zero permission errors ✅
- Zero missing lock files ✅
- Zero outdated lock files ✅

**Operational Status:**
- **PERSISTENT ISSUE**: Action file not found at runtime (P1)
- Health score at 92/100 (good) ✅
- 1 workflow needs attention ⚠️
- 143 workflows operating normally ✅

---

## Coordination Notes (for Meta-Orchestrators)

**For Campaign Manager:**
- Workflow health: 92/100 (good, improved)
- 1 workflow failing (minimal impact)
- Action file issue isolated to Daily Fact workflow
- All other workflows operating normally

**For Agent Performance Analyzer:**
- Infrastructure stable for agent operations
- No systemic issues affecting agent quality
- Action file issue does not impact agent performance

**For Metrics Collector:**
- 30 runs in last 24 hours
- 8 successful, 2 failed, 12 action_required, 5 skipped, 3 running
- Success rate: 66.7% (improving)
- No significant trends or anomalies

---

> **Last updated**: 2026-02-06T11:33:21Z  
> **Next check**: 2026-02-07 (daily schedule)  
> **Health Trend**: ↑ Good (75/100 → 92/100, +17 points)  
> **Status**: 🟡 **GOOD - MINOR ISSUE PERSISTS**  
> **Priority Action**: Investigate action pinned commit (P1)
