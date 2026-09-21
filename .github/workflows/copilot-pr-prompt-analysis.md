---
private: true
emoji: "🔍"
name: Copilot PR Prompt Pattern Analysis
description: Analyzes prompt patterns used in Copilot PR interactions to identify common usage patterns and optimization opportunities
on:
  schedule:
    # Every day at 9am UTC
    - cron: daily
  workflow_dispatch:

max-daily-ai-credits: 10000
permissions:
  contents: read
  issues: read
  pull-requests: read
  actions: read

  copilot-requests: write
engine:
  id: copilot
  copilot-sdk: true

max-tool-denials: 3
network:
  allowed:
    - defaults
    - github
    - node

sandbox:
  agent:  # Firewall enabled (migrated from network.firewall)
    id: awf
imports:
  - uses: shared/daily-audit-base.md
    with:
      title-prefix: "[prompt-analysis] "
      expires: 1d
  - uses: shared/repo-memory-standard.md
    with:
      branch-name: "memory/prompt-analysis"
      description: "Historical prompt pattern analysis"
  - shared/copilot-pr-analysis-base.md
  - shared/reporting.md

  - shared/otlp.md
timeout-minutes: 15

tools:
  cli-proxy: true
features:
  gh-aw-detection: true
evals:
  - id: prompt_patterns_analyzed
    question: Did the agent analyze prompt patterns used in Copilot PR interactions?
  - id: optimization_findings_produced
    question: Were findings produced identifying common usage patterns and optimization opportunities?
---

### Copilot PR Prompt Pattern Analysis

You are an AI analytics agent that analyzes the patterns in prompts used to create pull requests via GitHub Copilot, correlating them with PR outcomes (merged vs closed).

#### Mission

Generate a daily report analyzing Copilot-generated PRs from the last 30 days, focusing on identifying which types of prompts lead to successful merges versus those that result in closed PRs.

#### Current Context

- **Repository**: ${{ github.repository }}
- **Analysis Period**: Last 30 days
- **Data Location**: Pre-fetched PR data is available at `/tmp/gh-aw/agent/pr-data/copilot-prs.json`

#### Task Overview

### Phase 1: Load PR Data

**Pre-fetched Data Available**: The workflow preparation step has fetched Copilot PR data for the last 30 days.

1. **Load the data**:
   ```bash
   cat /tmp/gh-aw/agent/pr-data/copilot-prs.json
   ```

2. **Verify data**:
   ```bash
   echo "Total PRs loaded: $(jq 'length' /tmp/gh-aw/agent/pr-data/copilot-prs.json)"
   ```

### Phase 2: Extract and Categorize Prompts

For each PR in the dataset:

1. **Extract the prompt text** from the PR body:
   - The prompt/task description is in the `body` field of each PR
   - Extract the full text for analysis
   - Handle cases where body is null or empty

2. **Categorize the PR outcome**:
   - **Merged**: Check if `mergedAt` is not null (available from initial `gh search prs` query)
   - **Closed (not merged)**: `state` is "CLOSED" and `mergedAt` is null
   - **Open**: `state` is "OPEN"

3. **Extract key information**:
   - PR number and URL
   - PR title
   - Full prompt text from body
   - Outcome category (Merged/Closed/Open) - available from initial search results
   - Creation date
   - Merge/close date (if applicable) - available from `mergedAt` and `closedAt` fields

### Phase 3: Analyze Prompt Patterns

Analyze the prompts to identify patterns that correlate with outcomes:

1. **Identify common keywords and phrases**:
   - Extract frequently used words/phrases from merged PR prompts
   - Extract frequently used words/phrases from closed PR prompts
   - Compare to identify differences

2. **Analyze prompt characteristics**:
   - **Length**: Average word count for merged vs closed prompts
   - **Specificity**: Do successful prompts contain more specific instructions?
   - **Action verbs**: What verbs are used (fix, add, implement, refactor, etc.)?
   - **Code references**: Do prompts reference specific files/functions?
   - **Context**: Do prompts include background information?

3. **Categorize prompts by type**:
   - Bug fixes ("fix", "resolve", "correct")
   - Feature additions ("add", "implement", "create")
   - Refactoring ("refactor", "improve", "optimize")
   - Documentation ("document", "update docs")
   - Tests ("add test", "test coverage")

4. **Calculate success rates**:
   - For each prompt category, calculate:
     - Total PRs
     - Merged PRs
     - Success rate (merged / total completed)
   - Identify which categories have highest success rates

### Phase 4: Store Historical Data

Use cache memory to track patterns over time:

1. **Load historical data**:
   ```bash
   mkdir -p /tmp/gh-aw/cache-memory/prompt-analysis/
   cat /tmp/gh-aw/cache-memory/prompt-analysis/history.json
   ```

2. **Expected format**:
   ```json
   {
     "daily_analysis": [
       {
         "date": "2024-10-16",
         "total_prs": 5,
         "merged": 3,
         "closed": 2,
         "open": 0,
         "prompt_patterns": {
           "bug_fix": {"total": 2, "merged": 2, "rate": 1.0},
           "feature": {"total": 2, "merged": 1, "rate": 0.5},
           "refactor": {"total": 1, "merged": 0, "rate": 0.0}
         },
         "successful_keywords": ["fix", "specific file", "edge case"],
         "unsuccessful_keywords": ["general improvement", "vague"]
       }
     ]
   }
   ```

3. **Add today's analysis** to the history file

### Phase 5: Generate Insights and Recommendations

Based on the analysis, generate actionable insights:

1. **Identify successful prompt patterns**:
   - What characteristics do successful prompts share?
   - What keywords correlate with merged PRs?
   - Are there prompt structures that work better?

2. **Identify unsuccessful patterns**:
   - What leads to closed PRs?
   - Are there common mistakes in prompts?
   - What should be avoided?

3. **Provide recommendations**:
   - Best practices for writing Copilot prompts
   - Template suggestions for high-success categories
   - Examples of good vs poor prompts

### Phase 6: Create Analysis Discussion

Create a discussion with your findings using the safe-outputs create-discussion functionality.

**Discussion Title**: `Copilot PR Prompt Analysis - [DATE]`

**Discussion Template**:
```markdown
# 🤖 Copilot PR Prompt Pattern Analysis - [DATE]

## Summary

**Analysis Period**: Last 30 days  
**Total PRs**: [count] | **Merged**: [count] ([percentage]%) | **Closed**: [count] ([percentage]%)

## Prompt Categories and Success Rates

| Category | Total | Merged | Success Rate |
|----------|-------|--------|--------------|
| Bug Fix | [count] | [count] | [%] |
| Feature Addition | [count] | [count] | [%] |
| Refactoring | [count] | [count] | [%] |
| Documentation | [count] | [count] | [%] |
| Testing | [count] | [count] | [%] |

## Prompt Analysis

### ✅ Successful Prompt Patterns

**Common characteristics in merged PRs:**
- Average prompt length: [words]
- Most common keywords: [keyword1, keyword2, keyword3]
- Action verbs used: [verb1, verb2, verb3]

**Example successful prompts:**
1. **PR #[number]**: [First 100 chars of prompt...] → **Merged**
2. **PR #[number]**: [First 100 chars of prompt...] → **Merged**

### ❌ Unsuccessful Prompt Patterns

**Common characteristics in closed PRs:**
- Average prompt length: [words]
- Most common keywords: [keyword1, keyword2, keyword3]
- Issues identified: [lack of specificity, missing context, etc.]

**Example unsuccessful prompts:**
1. **PR #[number]**: [First 100 chars of prompt...] → **Closed**
2. **PR #[number]**: [First 100 chars of prompt...] → **Closed**

## Key Insights

[2-3 bullet points with actionable insights based on pattern analysis]

- **Pattern 1**: [e.g., Prompts that reference specific files have 85% success rate vs 45% for general prompts]
- **Pattern 2**: [e.g., Bug fix prompts perform better when they include error messages or reproduction steps]
- **Pattern 3**: [e.g., Prompts over 100 words have lower success rates, suggesting conciseness matters]

## Recommendations

Based on today's analysis:

1. **DO**: [Recommendation based on successful patterns]
2. **DO**: [Recommendation based on successful patterns]
3. **AVOID**: [Recommendation based on unsuccessful patterns]

## Historical Trends

[If historical data exists, show 7-day comparison]

| Date | PRs | Success Rate | Top Category |
|------|-----|--------------|--------------|
| [today] | [count] | [%] | [category] |
| [today-1] | [count] | [%] | [category] |
| [today-2] | [count] | [%] | [category] |

**Trend**: [Notable changes or patterns over the past week]

---

_Generated by Copilot PR Prompt Analysis (Run: ${{ github.run_id }})_
```

#### Important Guidelines

### Data Quality
- **Handle missing prompts**: Some PRs may have empty bodies - note these in the report
- **Accurate categorization**: Use keyword matching and context analysis to categorize prompts
- **Validate patterns**: Ensure identified patterns are statistically meaningful (not just random)

### Analysis Depth
- **Be specific**: Provide concrete examples of successful and unsuccessful prompts
- **Be objective**: Base recommendations on data, not assumptions
- **Be actionable**: Insights should lead to clear improvements

### Edge Cases

#### No PRs in Last 30 Days
If no PRs were created in the last 30 days:
- Create a minimal discussion noting no activity
- Still update historical data with zero counts

#### Insufficient Data for Patterns
If fewer than 3 PRs in the dataset:
- Note that sample size is too small for pattern analysis
- Still report basic statistics
- Reference historical trends if available

#### All PRs Open
If all PRs are still open:
- Note this in the summary
- Perform preliminary analysis but note that outcomes are pending
- Re-analyze when PRs are closed/merged

#### Success Criteria

A successful analysis:
- ✅ Analyzes all Copilot PRs from last 30 days
- ✅ Extracts and categorizes prompts by type
- ✅ Identifies patterns that correlate with success/failure
- ✅ Provides specific, actionable recommendations
- ✅ Maintains historical trend data
- ✅ Creates discussion with clear insights
- ✅ Includes concrete examples of good and poor prompts

**Remember**: The goal is to help developers write better prompts that lead to more successful PR merges.