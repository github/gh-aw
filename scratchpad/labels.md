---
description: Label usage guidelines for GitHub Agentic Workflows issue tracking
---

# Label Guidelines

## Purpose of Labels

Labels help organize and triage issues for project management. Use labels to:
- Categorize issue type (bug, enhancement, documentation)
- Indicate priority level
- Mark workflow automation status
- Identify component areas

## Label Categories

### Type Labels (choose one)
- **bug** - Something isn't working correctly
- **enhancement** - New feature or improvement request
- **documentation** - Documentation improvements or additions
- **question** - Questions about usage or behavior
- **testing** - Test-related issues

### Priority Labels (optional)
- **priority-high** - Critical issues requiring immediate attention
- **priority-medium** - Important but not urgent
- **priority-low** - Lower-priority improvements

### Component Labels (optional, choose multiple if needed)
- **cli** - Command-line interface
- **workflow** - Workflow compilation and processing
- **mcp** - MCP server integration
- **actions** - GitHub Actions integration
- **engine** - AI engine configuration

### Workflow Automation Labels (managed by automation)
- **ai-generated** - Issue created by AI workflow (Plan Command, etc.)
- **plan** - Planning issue with sub-tasks
- **ai-inspected** - Issue reviewed by AI workflow
- **smoke-copilot** - Smoke test results

### Domain Labels (used by scheduled workflows)
- **constraint-solving** - Constraint solving problems and algorithms (used by Constraint Solving POTD workflow)
- **problem-of-the-day** - Problem of the day (used by Constraint Solving POTD workflow)

### Status Labels
- **good first issue** - Suitable for new contributors
- **dependencies** - Dependency updates

## Label Usage Best Practices

### When to Add Labels

**During Issue Creation:**
- Add a type label (bug, enhancement, documentation, etc.)
- Add priority if urgent
- Add relevant component labels

**During Triage:**
- Review and update labels based on discussion
- Add `good first issue` for newcomer-friendly tasks
- Set priority based on impact

**Automation Labels:**
- `ai-generated` and `plan` are automatically added by workflows
- These should not be manually added or removed
- They help track AI-assisted issue creation and planning

### When to Remove Labels

**For Workflow Labels:**
- `plan` labels may be removed after all sub-tasks are completed
- Keep `ai-generated` for historical tracking
- Don't remove automation labels unless the issue was incorrectly tagged

**For Other Labels:**
- Update priority labels as urgency changes
- Remove incorrect type or component labels during triage

### Label Lifecycle

**AI-Generated Planning Issues:**
1. Created with `plan` + `ai-generated` labels
2. Add type and component labels for better categorization
3. Monitor sub-task completion
4. Consider removing `plan` label when all sub-tasks are complete
5. Close issue when work is done, keeping labels for historical reference

**Manual Issues:**
1. Created with type label (bug, enhancement, etc.)
2. Add component and priority labels during triage
3. Update labels as issue evolves
4. Close when resolved

## Label Hygiene

**Regular Maintenance:**
- Review unlabeled issues weekly and add appropriate labels
- Update priority labels as project needs change
- Ensure all open issues have at least a type label

**Avoiding Label Overload:**
- Use 2-4 labels per issue for effective filtering
- Don't duplicate information (e.g., title already says "bug")
- Prefer specific component labels over generic ones

## Label Taxonomy

**Current Label Structure:**
```text
Type: bug, enhancement, documentation, question, testing
Priority: priority-high, priority-medium, priority-low
Component: cli, workflow, mcp, actions, engine, automation
Workflow: ai-generated, plan, ai-inspected, smoke-copilot
Domain: constraint-solving, problem-of-the-day
Status: good first issue, dependencies
```

This taxonomy separates labels into six categories, which can be combined in GitHub's issue search to filter issues:
- `is:issue is:open label:bug label:priority-high` - Critical bugs
- `is:issue is:open label:enhancement label:good first issue` - Beginner-friendly enhancements
- `is:issue is:open label:plan` - Active planning issues

## Label Distribution Analysis

### Current State

Analysis of the repository (as of December 2024) shows:
- **Total open issues**: 35
- **Issues with `plan`**: 16 (45.7%)
- **Issues with `ai-generated`**: 16 (45.7%)
- **100% overlap**: All `plan` issues also have `ai-generated`
- **Unlabeled issues**: 0 (100% labeled)

### Key Findings

The label distribution matches the patterns described in the Key Findings section below. The high percentage of workflow labels reflects active AI-assisted planning, not a labeling problem.

**Why this is not a concern:**
1. Labels reflect actual project activity (active AI planning)
2. Automated issues carry the `ai-generated` label; manual issues do not
3. Label combinations can be used to filter issues via GitHub issue search
4. AI-generated issues are marked with the `ai-generated` label
5. Additional labels (type, component, priority) provide needed categorization

### Recommendations

✅ **Keep current structure** - No changes needed to `plan`/`ai-generated` labels
- Matches the label lifecycle described in this document
- Serves the purpose of tracking AI-generated planning issues
- Enables filtering with label combinations

❌ **Do not create `plan-*` subcategories** - would add label categories not currently needed for filtering
- Current system handles this with `is:open` / `is:closed` filters
- Would fragment label space

🔄 **Optional** (low priority): Remove `plan` label after sub-tasks complete
- Would make it an "active planning" indicator
- Keep `ai-generated` for historical tracking
- Not required, current approach is also valid

✅ **Monitor** for true label skew
- Watch type/priority labels (not workflow labels)
- Quarterly review recommended
- Warning signs: A type label exceeding 60% of open issues

## Success Metrics

✅ Zero unlabeled open issues  
✅ Automated issues carry `ai-generated`; manual issues do not  
✅ Label combinations support filtering via GitHub issue search  
✅ `ai-generated` label remains on AI-created issues

---

**Last Updated**: December 2024
