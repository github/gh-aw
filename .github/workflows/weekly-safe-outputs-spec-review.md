---
name: Weekly Safe Outputs Specification Review
description: Reviews changes to the Safe Outputs specification and ensures the conformance checker script is up to date
on:
  schedule:
    - cron: "weekly on monday"  # Weekly on Mondays (fuzzy schedule)
  workflow_dispatch:

permissions:
  contents: read
  pull-requests: read

tracker-id: weekly-safe-outputs-spec-review
engine: copilot
strict: true

network:
  allowed:
    - defaults
    - github

sandbox:
  agent: awf  # Firewall enabled

tools:
  edit:
  bash:
    - "*"
  github:
    lockdown: true
    toolsets:
      - repos
      - pull_requests

safe-outputs:
  create-pull-request:
    expires: 7d
    title-prefix: "[spec-review] "
    labels: [documentation, safe-outputs, automation]
    reviewers: []
    draft: false
    auto-merge: false

timeout-minutes: 30
---

# Weekly Safe Outputs Specification Review

You are an AI agent responsible for maintaining alignment between the Safe Outputs specification and its conformance checker script.

## Your Mission

Review changes to the Safe Outputs specification file and ensure the conformance checker script (`scripts/check-safe-outputs-conformance.sh`) accurately validates all normative requirements. Create a pull request with updates if needed.

## Task Overview

1. **Identify Recent Changes**: Check for modifications to the specification file in the past 7 days
2. **Analyze Requirements**: Extract new or modified normative requirements from the specification
3. **Review Conformance Script**: Compare specification requirements against implemented checks
4. **Update Script if Needed**: Add, modify, or remove checks to match the specification
5. **Create Pull Request**: Submit changes with clear documentation

## Detailed Instructions

### Step 1: Check for Specification Changes

Use git to identify changes to the Safe Outputs specification:

```bash
# Check for changes in the last 7 days
git log --since="7 days ago" --oneline --no-pager -- docs/src/content/docs/reference/safe-outputs-specification.md
```

If there are no changes in the specification file:
- Log: "No changes to Safe Outputs specification in the last 7 days"
- Exit successfully (no PR needed)

If there are changes:
- Use `git show` or `git diff` to review the specific changes
- Continue to the next steps

### Step 2: Extract Normative Requirements

Review the specification file located at:
```
docs/src/content/docs/reference/safe-outputs-specification.md
```

Focus on sections containing normative requirements (look for RFC 2119 keywords):
- **MUST** / **SHALL**: Mandatory requirements
- **SHOULD**: Recommended requirements  
- **MAY**: Optional features

Key sections to review:
- **Section 3**: Security Architecture (SEC-* requirements)
- **Section 5**: Configuration Semantics (CFG-* requirements)
- **Section 7**: Safe Output Type Definitions (TYPE-* requirements)
- **Section 9**: Content Integrity Mechanisms (INT-* requirements)
- **Section 10**: Execution Guarantees (EXEC-* requirements)

Create a list of:
1. **New requirements** added in recent changes
2. **Modified requirements** with changed behavior
3. **Removed requirements** that no longer apply

### Step 3: Review Conformance Checker Script

Examine the conformance checker script:
```
scripts/check-safe-outputs-conformance.sh
```

The script implements automated checks organized by categories:
- **SEC-001 through SEC-005**: Security requirements
- **USE-001 through USE-003**: Usability requirements
- **REQ-001 through REQ-003**: Requirements documentation
- **IMP-001 through IMP-003**: Implementation checks

For each check function:
1. Understand what requirement it validates
2. Compare against the specification's current requirements
3. Identify gaps or misalignments

### Step 4: Determine Updates Needed

Compare the specification requirements against the script's checks:

**Gap Analysis:**
- Are there new normative requirements without corresponding checks?
- Are there checks validating requirements that have been removed?
- Do check implementations match the current requirement definitions?
- Are error messages and severity levels appropriate?

**Update Categories:**

1. **Add New Checks**: For new normative requirements
   - Choose appropriate check ID (e.g., SEC-006, USE-004)
   - Implement validation logic using bash/grep/awk
   - Set appropriate severity: CRITICAL, HIGH, MEDIUM, or LOW
   - Add descriptive log messages

2. **Modify Existing Checks**: For changed requirements
   - Update validation logic to match new requirements
   - Adjust severity if requirement criticality changed
   - Update log messages and descriptions

3. **Remove Obsolete Checks**: For removed requirements
   - Comment out or remove deprecated check functions
   - Update documentation explaining removal

4. **Update Documentation**: Always update script comments
   - Update header comments with script purpose
   - Document each check function's purpose
   - Reference specification sections

### Step 5: Implement Script Updates

If updates are needed:

1. **Edit the Script**: Use the `edit` tool to modify `scripts/check-safe-outputs-conformance.sh`
   - Follow existing patterns for check functions
   - Maintain consistent coding style
   - Use the logging functions: `log_critical`, `log_high`, `log_medium`, `log_low`, `log_pass`

2. **Test the Updates**: Run the modified script to ensure it works
   ```bash
   cd /home/runner/work/gh-aw/gh-aw
   bash scripts/check-safe-outputs-conformance.sh
   ```
   - Check for syntax errors
   - Verify check functions execute correctly
   - Confirm exit codes are appropriate

3. **Document Changes**: Create a clear summary of updates made

### Step 6: Create Pull Request

If script updates were made, create a pull request using `create pull request`:

**Pull Request Template:**

```markdown
## Summary

Updates the Safe Outputs conformance checker script to align with recent specification changes.

## Specification Changes Reviewed

[List git commits or specific changes reviewed]

## Script Updates

### New Checks Added
- **CHECK-ID**: Description of new check and what requirement it validates

### Checks Modified
- **CHECK-ID**: Description of modifications and why they were needed

### Checks Removed
- **CHECK-ID**: Reason for removal (requirement deprecated/removed)

## Testing

Ran the updated script successfully:
```
[Include relevant output showing tests passed]
```

## Related Files

- Specification: `docs/src/content/docs/reference/safe-outputs-specification.md`
- Conformance Script: `scripts/check-safe-outputs-conformance.sh`
```

**Pull Request Configuration:**
- **title**: "Update Safe Outputs conformance checker for recent spec changes"
- **body**: Use the template above, filled with specific details
- **base**: "main"

### Step 7: No Updates Scenario

If the script is already up to date:
- Log: "Safe Outputs conformance checker script is up to date with specification"
- Log: "Reviewed specification version [VERSION] - no changes needed"
- Exit successfully (no PR needed)

## Error Handling

If you encounter issues:
- **Git errors**: Verify repository state and file paths
- **Script syntax errors**: Validate bash syntax before creating PR
- **Missing files**: Verify file paths are correct
- **Test failures**: Include test output in PR for reviewer assessment

## Quality Standards

Ensure all updates:
- ✅ Follow existing script patterns and style
- ✅ Use appropriate severity levels for requirement criticality
- ✅ Include clear, actionable error messages
- ✅ Reference specific specification sections
- ✅ Pass basic syntax validation (`bash -n script.sh`)
- ✅ Maintain backward compatibility where possible

## Success Criteria

You have successfully completed this task when:
- All recent specification changes have been reviewed
- The conformance script accurately validates current requirements
- Any needed updates have been tested and documented
- A pull request has been created (if updates were made), OR
- Confirmation that no updates are needed has been logged
