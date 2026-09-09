---
private: true
emoji: "🧪"
name: Smoke Temporary ID
description: Test temporary ID functionality for issue chaining and cross-references
on:
  schedule: every 2 days
  slash_command:
    name: smoke-temporary-id
    strategy: centralized
    events: [issues, issue_comment, pull_request, pull_request_comment]
  workflow_dispatch:
  #schedule: every 24h
  pull_request:
    types: [labeled]
    names: ["water"]
  reaction: "eyes"
  status-comment: true
permissions:
  copilot-requests: write
  contents: read
  issues: read
  pull-requests: read
engine: pi
model: copilot/auto
strict: true
network:
  allowed:
    - defaults
    - node
safe-outputs:
  allowed-domains: [default-safe-outputs]
  create-issue:
    expires: 2h
    title-prefix: "[smoke-temporary-id] "
    max: 5
    group: true
    labels: [ai-generated, automation, testing]
    close-older-issues: true
  link-sub-issue:
    max: 3
  add-comment:
    max: 2
    hide-older-comments: true
  messages:
    append-only-comments: true
    footer: "> 🧪 *Temporary ID smoke test by [{workflow_name}]({run_url})*{ai_credits_suffix}{history_link}"
    run-started: "🧪 [{workflow_name}]({run_url}) is now testing temporary ID functionality..."
    run-success: "✅ [{workflow_name}]({run_url}) completed successfully. Temporary ID validation passed."
    run-failure: "❌ [{workflow_name}]({run_url}) encountered failures. Check the logs for details."
timeout-minutes: 10
imports:
  - shared/otlp.md
  - shared/token-telemetry-check.md
  - shared/reporting.md
tools:
  cli-proxy: true
experiments:
  sub_agent_strategy:
    variants: [single_agent, sub_agents]
    description: "Test whether decomposing issue creation into sub-agents reduces cost"
    hypothesis: "H0: no change in AI credit count. H1: sub-agents reduce AI credit count by 15-25% and improve success rate."
    metric: effective_token_count
    secondary_metrics: [run_duration_seconds, issue_creation_success_rate]
    guardrail_metrics:
      - name: all_issues_created
        threshold: "==3"
      - name: temporary_id_resolution_rate
        threshold: ">=0.95"
    min_samples: 20
    weight: [50, 50]
    start_date: "2026-05-23"
    analysis_type: t_test
    tags: [cost-efficiency, sub-agents, smoke-tests]
features:
  gh-aw-detection: false
evals:
  - id: sub_agent_strategy_goal_met
    question: Does the agent output show that the objective for experiment sub_agent_strategy was successfully completed?

sandbox:
  agent:
    id: awf
---

# Smoke Test: Temporary ID Functionality

This workflow validates that temporary IDs work correctly for:
1. Creating parent-child issue hierarchies
2. Cross-referencing issues in bodies
3. Different temporary ID formats (3-8 alphanumeric characters)

**IMPORTANT**: Use the exact temporary ID format `aw_` followed by 3-8 alphanumeric characters (A-Za-z0-9).

{{#if experiments.sub_agent_strategy == 'single_agent'}}

## Single-Agent Mode

Create all issues in this context.

### Test 1: Create Parent Issue

```json
{
  "type": "create_issue",
  "temporary_id": "aw_test01",
  "title": "Test Parent: Temporary ID Validation",
  "body": "This is a parent issue created to test temporary ID functionality.\n\nSub-issues:\n- #aw_test02\n- #aw_test03\n\nAll references should be replaced with actual issue numbers."
}
```

### Test 2: Create Sub-Issue 1

```json
{
  "type": "create_issue",
  "temporary_id": "aw_test02",
  "parent": "aw_test01",
  "title": "Sub-Issue 1: Test Temporary ID References",
  "body": "This is sub-issue 1.\n\nParent: #aw_test01\nRelated: #aw_test03\n\nAll temporary IDs should be resolved to actual issue numbers."
}
```

### Test 3: Create Sub-Issue 2

```json
{
  "type": "create_issue",
  "temporary_id": "aw_test03",
  "parent": "aw_test01",
  "title": "Sub-Issue 2: Test Different ID Length",
  "body": "This is sub-issue 2 with an 8-character temporary ID.\n\nParent: #aw_test01\nRelated: #aw_test02\n\nTesting that longer temporary IDs (8 chars) work correctly."
}
```

{{/if}}
{{#if experiments.sub_agent_strategy == 'sub_agents'}}

## Sub-Agent Mode

Launch 3 background `task` agents to create issues in parallel, then wait for completion.

You are the coordinator. Your job:

1. Launch 3 background `task` agents (one per issue) with complete temporary ID instructions
2. Wait for all 3 agents to complete (you will receive automatic completion notifications)
3. After all complete, add a summary comment to the parent issue using temporary ID `aw_test01`

### Agent 1: Create Parent Issue

Launch a background task agent using the `task` tool (`agent_type: task`, `mode: background`) with the following prompt:

> Create a parent issue with temporary ID `aw_test01`. Title: "Test Parent: Temporary ID Validation". Body: "This is a parent issue created to test temporary ID functionality.\n\nSub-issues:\n- #aw_test02\n- #aw_test03\n\nAll references should be replaced with actual issue numbers." Use `safeoutputs create_issue` with `temporary_id: aw_test01`.

### Agent 2: Create Sub-Issue 1

Launch a second background task agent using the `task` tool (`agent_type: task`, `mode: background`) with the following prompt:

> Create a sub-issue with temporary ID `aw_test02` and parent `aw_test01`. Title: "Sub-Issue 1: Test Temporary ID References". Body: "This is sub-issue 1.\n\nParent: #aw_test01\nRelated: #aw_test03\n\nAll temporary IDs should be resolved to actual issue numbers." Use `safeoutputs create_issue` with `temporary_id: aw_test02` and `parent: aw_test01`.

### Agent 3: Create Sub-Issue 2

Launch a third background task agent using the `task` tool (`agent_type: task`, `mode: background`) with the following prompt:

> Create a sub-issue with temporary ID `aw_test03` and parent `aw_test01`. Title: "Sub-Issue 2: Test Different ID Length". Body: "This is sub-issue 2 with an 8-character temporary ID.\n\nParent: #aw_test01\nRelated: #aw_test02\n\nTesting that longer temporary IDs (8 chars) work correctly." Use `safeoutputs create_issue` with `temporary_id: aw_test03` and `parent: aw_test01`.

### After All Agents Complete

Once you receive completion notifications for all 3 agents, verify their success and add a summary comment.

{{/if}}

## Final Step: Add Summary Comment

Regardless of strategy, add a comment to the parent issue summarizing test results:

```json
{
  "type": "add_comment",
  "issue_number": "aw_test01",
  "body": "## Test Results\n\n✅ Parent issue created with temporary ID `aw_test01`\n✅ Sub-issue 1 created with temporary ID `aw_test02` and linked to parent\n✅ Sub-issue 2 created with temporary ID `aw_test03` and linked to parent\n✅ Cross-references resolved correctly\n\n**Validation**: Check that:\n1. All temporary ID references (#aw_*) in issue bodies are replaced with actual issue numbers (#123)\n2. Sub-issues show parent relationship in GitHub UI\n3. Parent issue shows sub-issues in task list\n\nTemporary ID format validated: `aw_[A-Za-z0-9]{3,8}`"
}
```

## Expected Outcome

1. Parent issue #aw_test01 created and assigned actual issue number (e.g., github/gh-aw#1234)
2. Sub-issues #aw_test02 and #aw_test03 created with actual issue numbers
3. All references like `#aw_test01` replaced with actual numbers like `#1234`
4. Sub-issues properly linked to parent with `parent` field
5. Comment added to parent verifying the test results

**Success Criteria**: All 3 issues created, all temporary ID references resolved, parent-child relationships established.