# Copilot Schema Validation Error

## Issue Description

**Error Message:**
```
Model call failed: Invalid schema for function 'safeoutputs-add_labels': In context=(), object schema missing properties.
```

**Affected Workflows:** AI Moderator and other workflows using safe-outputs with `add-labels`

**Status:** Known intermittent issue in Copilot CLI v0.0.384

## Symptoms

- Workflow runs fail with "Invalid schema for function" error
- Error mentions "object schema missing properties"
- Failure is intermittent - some runs succeed with identical schema
- Affects safe-outputs tools exposed through MCP gateway

## Root Cause

This is an intermittent schema validation bug in GitHub Copilot CLI version 0.0.384. The tool schema is correctly formed and includes all required fields including `properties`, but Copilot CLI's validator occasionally rejects it.

### Investigation Details

1. **Schema Verification**: The `add_labels` tool schema in `actions/setup/js/safe_outputs_tools.json` is correctly formatted:
   ```json
   {
     "name": "add_labels",
     "inputSchema": {
       "type": "object",
       "required": ["labels"],
       "properties": {
         "labels": { "type": "array", "items": { "type": "string" } },
         "item_number": { "type": "number" }
       },
       "additionalProperties": false
     }
   }
   ```

2. **No Transformation Issues**: 
   - Go compilation preserves schema exactly
   - MCP server passes schema through unchanged
   - MCP gateway forwards schema without modification

3. **Intermittent Nature**: Analysis of recent workflow runs shows both successes and failures with identical schema, confirming this is not a configuration issue.

## Workarounds

### Option 1: Retry Failed Runs
The simplest workaround is to manually re-run failed workflow executions. Since the issue is intermittent, a retry will likely succeed.

### Option 2: Use Alternative Model
Try switching to a different model in the workflow frontmatter:
```yaml
engine:
  id: copilot
  model: gpt-4o  # Instead of gpt-5-mini
```

### Option 3: Wait for Copilot CLI Update
Monitor Copilot CLI releases for a fix to the schema validation logic. This issue may be resolved in a future version.

## Impact

- **Severity**: Low - Issue is intermittent and retrying usually succeeds
- **Frequency**: Occasional - appears in ~10-20% of runs
- **Workaround Available**: Yes - manual retry

## Related Files

- Schema definition: `actions/setup/js/safe_outputs_tools.json`
- Schema generation: `pkg/workflow/safe_outputs_config_generation.go`
- MCP server: `actions/setup/js/safe_outputs_mcp_server.cjs`
- Affected workflow: `.github/workflows/ai-moderator.md`

## Status Updates

- **2026-01-18**: Issue identified and documented
- **Copilot CLI Version**: 0.0.384
- **Expected Resolution**: Awaiting Copilot CLI update

## References

- Workflow run example: https://github.com/github/gh-aw/actions/runs/21110741074
- Error logs show 6 retry attempts before final failure
- Total retry wait time: ~93 seconds
