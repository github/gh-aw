# Quick Reference: ProjectOps + Orchestration Improvements

## Pain Points → Solutions Matrix

| Pain Point | Root Cause | Current Workaround | Recommended Solution | Priority | LOC Impact |
|------------|------------|-------------------|---------------------|----------|------------|
| **Tool-Call Ordering** | LLM non-determinism | Strict prompts (STEP 1/2) | `depends_on` field | Medium | ~150 LOC |
| **Missing island_id** | Run-based islands only | Read→Remove→Rewrite | Named islands (`island_id`) | **HIGH** | ~50 LOC |
| **Timing Dependencies** | Parallel worker/aggregator | Polling with 90s delays | Project Status Polling | Medium | Docs only |
| **Event Re-entrancy** | Issue events retrigger | Manual workflow `if` conditions | `prevent-retrigger` flag | **HIGH** | ~100 LOC |

---

## Implementation Roadmap

```
Phase 1: High Priority (Week 1-2)
┌─────────────────────────────────────────────────┐
│ 1. Named Islands (island_id)                    │
│    - Update schema + update_pr_description_helpers │
│    - Backward compatible (falls back to runId)  │
│    - ~50 LOC                                    │
│                                                 │
│ 2. Re-entrancy Protection (prevent-retrigger)   │
│    - Add HTML markers to created issues         │
│    - Auto-generate workflow conditionals        │
│    - ~100 LOC                                   │
└─────────────────────────────────────────────────┘

Phase 2: Documentation (Week 3)
┌─────────────────────────────────────────────────┐
│ 3. Project Status Polling Pattern               │
│    - Document coordination using update-project  │
│    - Provide orchestrator/aggregator examples   │
│    - No code changes required                   │
└─────────────────────────────────────────────────┘

Phase 3: Medium Priority (Week 4-6)
┌─────────────────────────────────────────────────┐
│ 4. Dependency Chains (depends_on)               │
│    - Formalize deferred execution               │
│    - Extend handler_manager.cjs                 │
│    - ~150 LOC                                   │
└─────────────────────────────────────────────────┘

Phase 4: Future Enhancements (TBD)
┌─────────────────────────────────────────────────┐
│ 5. Wait-for-Workflows (Optional)                │
│    - New safe output for workflow sync          │
│    - Only if Project polling insufficient       │
│    - ~300 LOC                                   │
└─────────────────────────────────────────────────┘
```

---

## Code Examples

### 1. Named Islands (High Priority)

**Before** (Aggregator duplicates sections):
```javascript
update_issue({
  issue_number: 123,
  operation: "replace-island",  // Uses runId - creates new island each run
  body: "## Compliance\n- Status: Pending"
});
```

**After** (Aggregator updates same section):
```javascript
update_issue({
  issue_number: 123,
  operation: "replace-island",
  island_id: "compliance-summary",  // Named island - updates same section
  body: "## Compliance\n- Status: Complete"
});
```

---

### 2. Re-entrancy Protection (High Priority)

**Before** (Manual filtering required):
```yaml
# Workflow must manually check labels
on:
  issues:
    types: [opened, labeled]

jobs:
  agent:
    if: |
      !contains(github.event.issue.labels.*.name, 'orchestrator-created')
```

**After** (Automatic protection):
```yaml
# Frontmatter enables auto-protection
safe-outputs:
  create-issue:
    max: 10
    prevent-retrigger: true  # Automatic marker + conditional
```

---

### 3. Project Status Polling (Documentation)

**Pattern for coordination**:
```javascript
// Worker: Mark complete when done
update_project({
  project: "...",
  content_type: "draft_issue",
  draft_title: "Worker 1 Status",
  fields: {
    "Status": "Complete",
    "Worker ID": "worker-1"
  }
});

// Aggregator: Poll until all workers complete
const allComplete = await checkProjectStatus({
  project: "...",
  expectedWorkers: 5,
  status: "Complete"
});

if (!allComplete) {
  // Workers still running, defer aggregation
  return;
}

// Safe to aggregate now
```

---

### 4. Dependency Chains (Medium Priority)

**Before** (Flaky ordering):
```javascript
// Agent may execute these out of order
create_issue({ title: "Task" });  // Returns temp ID: aw_temp_001
link_sub_issue({ parent: 100, sub: "aw_temp_001" });  // May fail if create not done
```

**After** (Explicit dependencies):
```javascript
create_issue({ title: "Task" });  // Returns temp ID: aw_temp_001
link_sub_issue({
  parent: 100,
  sub: "aw_temp_001",
  depends_on: ["aw_temp_001"]  // Defers until temp ID resolves
});
```

---

## Benefits Summary

| Solution | Reliability Gain | Complexity | User Effort |
|----------|-----------------|------------|-------------|
| Named Islands | ✅ **Eliminates duplication** | Low | None (opt-in) |
| Re-entrancy Protection | ✅ **No cascading runs** | Low | None (auto) |
| Project Status Polling | ✅ **90%+ sync accuracy** | None | Pattern docs |
| Dependency Chains | ⬆️ **95%+ ordering** | Medium | None (auto) |

---

## Key Takeaways

1. **Highest ROI**: Named Islands + Re-entrancy Protection (~150 LOC, solves 2 major pain points)
2. **No New Features Needed**: Project Status Polling uses existing `update-project`
3. **Backward Compatible**: All changes are opt-in or transparent
4. **Natural Fit**: Extends existing patterns rather than adding new ones

See `PROJECTOPS_ORCHESTRATION_ANALYSIS.md` for complete technical details and implementation guidance.
