---
private: true
emoji: "🔍"
description: Analyzes workflow examples to identify patterns, best practices, and potential improvements
on:
  schedule: weekly on monday around 09:00
  workflow_dispatch:
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read
engine: claude
timeout-minutes: 30
tools:
  github:
    mode: local

imports:
  - uses: shared/meta-analysis-base.md
    with:
      toolsets: [default, actions]
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[workflow-analysis] "
      expires: 1d


  - shared/otlp.md
features:
  gh-aw-detection: true
sandbox:
  agent:
    runtime: cloud-hypervisor
---

# Weekly Workflow Analysis

Analyze GitHub Actions workflow runs from the past week and identify improvement opportunities.

## Instructions

Use the agentic-workflows tool to:

1. **Check workflow status**: Use the `status` tool to see all workflows in the repository
2. **Download logs**: Use the `logs` tool with parameters like:
   - `workflow_name`: Specific workflow to analyze
   - `count`: Number of runs to analyze (e.g., 20)
   - `start_date`: Filter runs from last week (e.g., "-1w")
   - `engine`: Filter by AI engine if needed
3. **Audit failures**: Use the `audit` tool with `run_id` to investigate specific failed runs

## Analysis Tasks

Analyze the collected data and provide:

- **Failure Patterns**: Common errors across workflows
- **Performance Issues**: Slow steps or bottlenecks
- **Resource Usage**: Token usage and costs for AI-powered workflows
- **Reliability Metrics**: Success rates and error frequencies
- **Optimization Opportunities**: Suggestions for improving workflow efficiency

Create a discussion with your findings and actionable recommendations for improving CI/CD reliability and performance.


### Output Format

Use `###` (or lower) headers only.

Structure reports as: overview → key metrics/issues → collapsible detail → next actions.

Wrap long content with `<details><summary><b>View Details</b></summary>...</details>`.