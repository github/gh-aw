# Troubleshooting dispatch_workflow Tools Not Appearing

## Problem

When using `dispatch-workflow` safe output configuration, the expected workflow dispatch tools (e.g., `safeoutputs-my_workflow`) do not appear in the available MCP tools list.

## Symptoms

- Workflow has `dispatch-workflow` configured in frontmatter with a list of workflows
- Expected tools like `safeoutputs-add_name`, `safeoutputs-add_emojis` are missing
- Agent reports missing_tool with message about workflow dispatch tools not being available
- Only standard safe output tools appear (missing_tool, missing_data, noop, add_comment)

## New: Similar Tool Suggestions

**As of PR #XXXX**, when an MCP tool is not found, the system now suggests similar available tools:

```
Tool 'dispatch-add-name' not found. Did you mean one of these: add_name, add_comment?
```

This helps identify:
- Typos in tool names
- Missing tool prefixes (e.g., forgot `safeoutputs-`)
- Available alternatives

## Diagnostic Steps

### 1. Check MCP Server Logs

With the enhanced logging (added in PR #XXXX), the MCP server logs will show detailed information about tool loading and registration.

**Look for these log entries:**

```
Reading tools from file: /opt/gh-aw/safeoutputs/tools.json
Successfully parsed N tools from file
  Found 3 dispatch_workflow tools:
    - add_name (workflow: add-name)
    - add_emojis (workflow: add-emojis)
    - all_workflows (workflow: all-workflows)
```

If you see this, tools.json is correct. Continue to registration logs.

```
Found dispatch_workflow tool: add_name (_workflow_name: add-name)
  dispatch_workflow config exists, registering tool
```

If you see this, the tool is being registered successfully.

```
Found dispatch_workflow tool: add_name (_workflow_name: add-name)
  WARNING: dispatch_workflow config is missing or falsy - tool will NOT be registered
  Config keys: missing_tool, noop, add_comment
  config.dispatch_workflow value: undefined
```

If you see this WARNING, the config doesn't have dispatch_workflow, which is the root cause.

### 2. Verify Workflow Configuration

Check the workflow's frontmatter:

```yaml
---
on: issues
engine: copilot
safe-outputs:
  dispatch-workflow:
    workflows:
      - add-name
      - add-emojis
      - all-workflows
    max: 3
---
```

### 3. Check Compiled Configuration

Extract the config from the compiled `.lock.yml` file:

```bash
grep -A 5 "config.json" .github/workflows/my-workflow.lock.yml | grep dispatch_workflow
```

**Expected output:**
```json
{"dispatch_workflow":{"max":3,"workflows":["add-name","add-emojis","all-workflows"],"workflow_files":{...}},"missing_data":{},"missing_tool":{},"noop":{}}
```

**If dispatch_workflow is missing from config, the workflow needs recompilation.**

### 4. Verify Target Workflows Exist

For each workflow in the `workflows` list, verify:

```bash
# Check if workflow files exist
ls -la .github/workflows/add-name.{md,yml,lock.yml}
ls -la .github/workflows/add-emojis.{md,yml,lock.yml}
ls -la .github/workflows/all-workflows.{md,yml,lock.yml}
```

**Requirements:**
- At least one of `.yml` or `.lock.yml` must exist
- If only `.md` exists, compile it first: `gh aw compile .github/workflows/add-name.md`
- The workflow must have `workflow_dispatch` in its `on:` triggers

## Common Causes and Solutions

### Cause 1: Workflow Not Recompiled

**Symptom:** dispatch_workflow missing from config.json in lock file

**Solution:**
```bash
gh aw compile .github/workflows/my-workflow.md
```

### Cause 2: Target Workflows Don't Exist

**Symptom:** Tools generated with empty inputs, but validation failed during compilation

**Solution:**
1. Create the target workflows in `.github/workflows/`
2. Ensure they have `workflow_dispatch` trigger:
   ```yaml
   on:
     workflow_dispatch:
       inputs:
         param1:
           description: "Parameter 1"
           type: string
   ```
3. Compile them: `gh aw compile .github/workflows/target-workflow.md`
4. Recompile the dispatcher workflow

### Cause 3: Target Workflows Missing workflow_dispatch

**Symptom:** Compilation error: "workflow 'X' does not support workflow_dispatch trigger"

**Solution:**
Add `workflow_dispatch` to the target workflow's triggers:
```yaml
on:
  issues:  # existing trigger
  workflow_dispatch:  # add this
    inputs:
      # optional: define inputs
```

### Cause 4: Config Not Loaded Properly

**Symptom:** MCP server logs show config.dispatch_workflow is undefined

**Solution:**
Check for file system issues:
```bash
# In the agent job, verify files exist
ls -la /opt/gh-aw/safeoutputs/
cat /opt/gh-aw/safeoutputs/config.json | jq .
cat /opt/gh-aw/safeoutputs/tools.json | jq '. | map(select(._workflow_name))'
```

## Verification

After applying fixes, verify tools are registered:

1. Recompile the workflow: `gh aw compile .github/workflows/my-workflow.md`
2. Trigger the workflow
3. Check MCP server logs for successful registration:
   ```
   Found dispatch_workflow tool: add_name (_workflow_name: add-name)
     dispatch_workflow config exists, registering tool
   Registered tool: add_name
   ```
4. Verify tools appear in agent's available tools
5. **New**: Check that similar tool suggestions appear when testing with typos

## Prevention

To prevent this issue:

1. **Always compile after adding dispatch-workflow**: 
   ```bash
   gh aw compile .github/workflows/my-workflow.md
   ```

2. **Verify compilation succeeded**:
   ```bash
   grep dispatch_workflow .github/workflows/my-workflow.lock.yml
   ```

3. **Create target workflows first** before adding them to dispatch-workflow list

4. **Use validation** to catch issues early:
   ```bash
   gh aw compile --validate .github/workflows/my-workflow.md
   ```

5. **Test tool names**: The system will now suggest similar tools if you make a typo

## Related Issues

- Issue #XXXX: dispatch_workflow tools not appearing in MCP tools
- PR #XXXX: Enhanced diagnostic logging for dispatch_workflow registration
- PR #XXXX: Added similar tool suggestions when tool not found
