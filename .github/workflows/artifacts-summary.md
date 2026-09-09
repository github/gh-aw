---
private: true
emoji: "📦"
description: Generates a comprehensive summary of GitHub Actions artifacts usage across all workflows in the repository
on:
  workflow_dispatch:
  schedule: weekly on sunday around 06:00
max-daily-ai-credits: 10000
permissions:
  contents: read
  actions: read
  copilot-requests: write
engine:
  id: codex
  model-provider: openai
model: openai/gpt-5.3-codex
network:
  allowed:
    - defaults
    - node
sandbox:
  agent:
    id: awf
    runtime: cloud-hypervisor
tools:
  cli-proxy: true
  edit:
  bash: true
  github:
    mode: local
    toolsets: [actions, repos]
safe-outputs:
  create-discussion:
    expires: 1d
    category: "artifacts"
    max: 1
    close-older-discussions: true
timeout-minutes: 15
strict: true
imports:
  - shared/reporting.md
  - shared/safe-output-app.md
  - shared/otlp.md
  - shared/graders.md
features:
  gh-aw-detection: true
evals:
  - id: artifacts_analyzed
    question: Did the agent analyze GitHub Actions artifacts usage across all workflows in the repository?
  - id: summary_produced
    question: Was a comprehensive summary report of artifacts usage produced?

---

### Artifacts Summary

**Report Formatting**: Use h3 (###) or lower for all headers in your report
to maintain proper document hierarchy. Wrap long sections in
`<details><summary>View Full Details</summary>` tags to improve readability.


Generate a comprehensive summary table of GitHub Actions artifacts usage in the repository ${{ github.repository }}.

#### Task Requirements

1. **Analyze all workflows** in the repository to identify which ones generate artifacts
2. **Collect artifact data** for recent workflow runs (last 30 days recommended)
3. **Generate a summary table** with the following columns:
   - Workflow Name
   - Total Artifacts Count
   - Total Size (in MB/GB)
   - Average Size per Artifact
   - Latest Run Date
   - Status (Active/Inactive)

#### Analysis Instructions

Please:

1. **List all workflows** in the repository using the GitHub API
2. **For each workflow**, get recent runs and their artifacts
3. **Calculate statistics**:
   - Total number of artifacts per workflow
   - Total size of all artifacts per workflow
   - Average artifact size
   - Most recent run date
4. **Create a markdown table** with the summary
5. **Include insights** such as:
   - Which workflows generate the most artifacts
   - Which workflows use the most storage
   - Trends in artifact usage
   - Recommendations for optimization

#### Output Format

Create an issue with a markdown table like this:

```markdown
# Artifacts Usage Report

| Workflow Name | Artifacts Count | Total Size | Avg Size | Latest Run | Status |
|---------------|-----------------|------------|----------|------------|--------|
| workflow-1    | 45             | 2.3 GB     | 52 MB    | 2024-01-15 | Active |
| workflow-2    | 12             | 456 MB     | 38 MB    | 2024-01-10 | Active |

## Insights & Recommendations
[Your analysis and recommendations here]
```

#### Important Notes

- Focus on workflows that actually generate artifacts (skip those without any)
- Convert sizes to human-readable formats (MB, GB)
- Consider artifact retention policies in your analysis
- Include both successful and failed runs in the analysis, ignore cancelled runs
