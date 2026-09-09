---
private: true
name: Example Failure Category Filter
on:
  workflow_dispatch:


safe-outputs:
  report-failure-as-issue:
    - agent_failure           # Only report genuine agent-side failures
    - missing_safe_outputs    # Only report when outputs are missing
    - missing_tool            # Only report when functionality is missing
    - missing_data            # Only report when required data is unavailable
    # Excluded categories (won't create issues):
    # - report_incomplete: Infrastructure/tool failures
    # - inference_access_error: AI server transient errors
    # - ai_credits_rate_limit_error: AI rate limits
    # - mcp_policy_error: MCP policy violations
  create-issue:
imports:
  - shared/reporting.md
sandbox:
  agent:
    runtime: cloud-hypervisor
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
---

# Example: Failure Category Filtering

This workflow demonstrates the `report-failure-as-issue` category filtering feature with both inclusion and exclusion syntax.

## Context

For scheduled workflows that frequently encounter transient infrastructure failures:
- Docker registry timeouts
- AI server 5xx errors
- Firewall startup failures
- MCP image pull intermittent failures

Traditional `report-failure-as-issue: false` suppresses ALL failure reports, including genuine agent bugs.

## Solution

Use category filtering to only report actionable failures:

### Inclusion Syntax (include only these categories)

```yaml
safe-outputs:
  report-failure-as-issue:
    - agent_failure
    - missing_safe_outputs
    - missing_tool
    - missing_data
```

### Exclusion Syntax (exclude these categories, report all others)

```yaml
safe-outputs:
  report-failure-as-issue:
    - "!inference_access_error"        # Exclude AI server transient errors
    - "!ai_credits_rate_limit_error"   # Exclude AI rate limits
    - "!report_incomplete"             # Exclude infrastructure failures
    - "!mcp_policy_error"              # Exclude MCP policy violations
```

### Mixed Syntax (include these, but not those)

```yaml
safe-outputs:
  report-failure-as-issue:
    - agent_failure                    # Include agent failures
    - missing_safe_outputs             # Include missing outputs
    - "!unknown_model_ai_credits"      # But exclude unknown model AI credits
```

This prevents noise while preserving actionable signals.

## Task

Create an issue summarizing this feature.