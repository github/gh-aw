# End-to-End Feature Testing for Pull Requests

This document describes how human developers can test new features end-to-end using agentic workflows directly in pull requests.

## Overview

When developing a new feature, you can test it end-to-end by:

1. Having GitHub Copilot agent modify the `dev.md` agentic workflow to use the new feature
2. Triggering the Dev workflow in the PR branch via CLI or web UI
3. Waiting for the Dev workflow to finish and for Dev Hawk to analyze the results
4. Iterating based on the feedback

This approach allows you to test features in a real GitHub Actions environment before merging to main.

## Testing Workflow

### Step 1: Instruct GitHub Copilot Agent to Modify dev.md

The `dev.md` workflow is located at `.github/workflows/dev.md` and serves as a testing playground for new features.

**How to request changes:**

In your pull request, instruct the GitHub Copilot agent to modify the `dev.md` workflow to exercise your new feature. For example:

```text
@copilot please update the dev.md workflow to test the new <feature-name> feature by:
- Adding the necessary configuration in the frontmatter
- Updating the task description to use the feature
- Including validation steps to verify the feature works correctly
```text

**What the agent should do:**

- Update the frontmatter YAML configuration to enable/configure the new feature
- Modify the task instructions to exercise the feature
- Add verification steps if applicable
- Ensure the workflow will demonstrate the feature working correctly

**After the agent makes changes:**

The agent should run `make recompile` to regenerate the `.github/workflows/dev.lock.yml` file. This compiled workflow is what GitHub Actions will execute.

### Step 2: Trigger the Dev Workflow

Once the `dev.md` workflow has been updated and the changes are committed to your PR branch, you can trigger the workflow in two ways:

#### Option A: Using the GitHub CLI

```bash
# Trigger the workflow on your PR branch
gh workflow run dev.md --ref your-branch-name

# Check the workflow run status
gh run list --workflow=dev.md --limit 5
```text

#### Option B: Using the GitHub Web UI

1. Navigate to the **Actions** tab in the GitHub repository
2. Select the **Dev** workflow from the left sidebar
3. Click the **Run workflow** dropdown button
4. Select your PR branch from the **Use workflow from** dropdown
5. Click the **Run workflow** button

### Step 3: Monitor the Dev Workflow Execution

After triggering the workflow:

**Watch the workflow run:**

- Go to the **Actions** tab to see the workflow execution in real-time
- Click on the specific run to see detailed logs
- Monitor for any errors or unexpected behavior

**Expected outcomes:**

- ✅ **Success**: The workflow completes successfully, demonstrating the feature works
- ⚠️ **Failure**: The workflow fails, indicating an issue with the feature or configuration

### Step 4: Review Dev Hawk's Analysis

After the Dev workflow completes, the **Dev Hawk** workflow will automatically run (see `.github/workflows/dev-hawk.md`). Dev Hawk is a monitoring agent that:

- Detects when the Dev workflow completes on `copilot/*` branches
- Analyzes the workflow outcome (success or failure)
- Posts a detailed comment to your pull request with:
  - Workflow status and link to the run
  - Root cause analysis for failures (using the `gh aw audit` tool)
  - Error details and recommendations
  - Actionable next steps

**What to look for in Dev Hawk's comment:**

- **Success report**: Confirms the feature is working as expected
- **Failure analysis**: Identifies what went wrong with specific error messages
- **Recommendations**: Suggests fixes or next steps to resolve issues

### Step 5: Iterate Based on Feedback

Based on the results:

**If the workflow succeeds:**

- Review the workflow output to verify the feature behaves correctly
- Check that the feature produces expected results
- Consider additional test scenarios if needed

**If the workflow fails:**

- Review Dev Hawk's analysis in the PR comment
- Examine the specific error messages and recommendations
- Instruct the GitHub Copilot agent to fix the issues:
  ```
  @copilot the dev workflow failed with [error]. Please fix the dev.md workflow to address this issue.
  ```
- After fixes are committed, re-trigger the Dev workflow (repeat from Step 2)

**Iteration cycle:**

```text
Modify dev.md → Recompile → Trigger workflow → Review results → Iterate
```text

Continue this cycle until the workflow succeeds and the feature works as expected.

## Example Testing Scenarios

### Example 1: Testing a New Tool Integration

```yaml
---
engine: copilot
tools:
  new-tool:
    config_key: value
---

# Test New Tool Integration

Use the new-tool to perform [specific task] and verify the results.
```text

### Example 2: Testing New Engine Features

```yaml
---
engine: claude
new_feature_flag: true
---

# Test New Engine Feature

Demonstrate the new engine feature by [description of test].
```text

### Example 3: Testing Safe Output Enhancements

```yaml
---
engine: copilot
safe-outputs:
  new-output-type:
    max: 5
    target: "test-*"
---

# Test New Safe Output Type

Create multiple instances of the new output type to verify rate limiting and targeting work correctly.
```text

## Best Practices

### For Effective Testing

1. **Single feature focus**: Test one feature at a time in dev.md
2. **Clear success criteria**: Define what success looks like for the test
3. **Include validation**: Add steps to verify the feature works correctly
4. **Use appropriate timeouts**: Set reasonable timeout-minutes for your test
5. **Clean up between tests**: Revert dev.md changes after testing is complete

### For Better Iteration

1. **Read Dev Hawk's analysis carefully**: It often identifies the exact issue
2. **Check workflow logs directly**: Sometimes additional context is in the full logs
3. **Test incrementally**: Start with minimal configuration, then add complexity
4. **Document unexpected behavior**: Note any issues in the PR for discussion

### For Repository Hygiene

1. **Don't merge dev.md changes**: The dev.md file should remain a minimal test harness with no feature-specific logic
2. **Reset dev.md after testing**: Restore it to the default configuration
3. **Focus PR changes on the actual feature**: Keep test changes separate from feature implementation

## Troubleshooting

### Dev Hawk Doesn't Comment

**Possible reasons:**

- The workflow wasn't triggered via `workflow_dispatch`
- The branch doesn't match the `copilot/*` pattern
- No pull request is associated with the commit

**Solution:**

- Ensure you're on a branch named `copilot/your-feature-name`
- Verify the Dev workflow was triggered manually (workflow_dispatch)
- Confirm a pull request exists for your branch

### Workflow Fails to Trigger

**Possible reasons:**

- The lock file wasn't regenerated after modifying dev.md
- The branch name is incorrect
- Permissions issues

**Solution:**

- Run `make recompile` to regenerate the lock file
- Verify you have the correct branch name
- Check that the workflow file is valid YAML

### Feature Doesn't Work as Expected

**Debugging steps:**

1. Check the workflow logs for error messages
2. Review Dev Hawk's root cause analysis
3. Use `gh aw audit <run-id>` locally to investigate further
4. Compare with existing working workflows for similar features

## Related Documentation

- [Dev Workflow](.github/workflows/dev.md) - The test harness workflow
- [Dev Hawk Workflow](.github/workflows/dev-hawk.md) - The monitoring agent
- [Testing Specification](./testing.md) - Overall testing framework
- [Contributing Guide](../CONTRIBUTING.md) - How to contribute to gh-aw

## Integration with CI/CD

While this manual testing approach is useful for rapid iteration during development, features should also have:

- **Unit tests**: In `pkg/*/` test files
- **Integration tests**: Testing the feature in isolation
- **Automated workflows**: For continuous validation

The end-to-end testing described here complements (but does not replace) automated testing.

---

**Last Updated**: 2025-12-05
