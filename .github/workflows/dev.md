---
private: true
emoji: "💻"
on:
  workflow_dispatch:
  label_command:
    name: dev
    strategy: decentralized
  schedule:
    - cron: 'daily around 9:00'  # ~9 AM UTC
name: Dev
description: Daily status report for gh-aw project
timeout-minutes: 30
strict: false
model: gpt-5.3-codex
engine:
  id: codex
permissions:
  contents: read
  issues: read
  pull-requests: read

safe-outputs:
  create-issue:
    expires: 7d
    title-prefix: "[Daily Report] "

imports:
  - shared/otlp.md
  - shared/reporting.md
tools:
  github:
    mode: local
  cli-proxy: true

evals:
  - id: project_activity_analyzed
    question: Did the agent analyze recent gh-aw project activity for the daily status report?
  - id: status_report_created
    question: Did the agent create a daily status report with relevant project updates?
sandbox:
  agent:
    runtime: cloud-hypervisor
---

<!--
# GitHub Agentic Workflows — README Summary

Write agentic workflows in natural language markdown and run them in GitHub Actions.

## Key Concepts
- **Quick Start**: Step-by-step guide at https://github.github.com/gh-aw/setup/quick-start/
- **Overview**: Agentic workflows let AI automate repository tasks using natural language prompts.
  See https://github.github.com/gh-aw/introduction/how-they-work/
- **Guardrails**: Workflows run with read-only permissions by default. Write operations require
  sanitized `safe-outputs`. Security layers include sandboxed execution, input sanitization,
  network isolation, SHA-pinned supply chain, tool allow-listing, and compile-time validation.
  Human approval gates are available for critical operations.
  See https://github.github.com/gh-aw/introduction/architecture/
- **Documentation**: https://github.github.com/gh-aw/ — machine-readable llms.txt also available.
- **Contributing**: See CONTRIBUTING.md for development setup and contribution guidelines.

## Related Projects
- **AWF** (Agent Workflow Firewall): network egress control — https://github.com/github/gh-aw-firewall
- **MCP Gateway**: unified HTTP gateway for MCP server calls — https://github.com/github/gh-aw-mcpg
- **gh-aw-actions**: shared GitHub Actions library — https://github.com/github/gh-aw-actions
-->

# Daily Status Report

Generate a daily status report for the gh-aw project, focusing on documentation quality.

**Requirements:**

1. **Find documentation problems reported in issues**: Search GitHub issues for mentions of documentation bugs, unclear instructions, missing documentation, or incorrect documentation. Look for patterns like "docs", "documentation", "unclear", "wrong", "missing", "broken", "outdated".

2. **Cross-reference with current documentation**: For each documentation problem found in issues, search the repository documentation to find the relevant section that the issue is referencing or that could answer the question raised.

3. **Compile a report** summarizing:
   - Issues that report documentation problems (with issue numbers and titles)
   - The corresponding documentation sections that may need updating
   - Any issues where the documentation actually already contains the answer (and the issue could be closed with a pointer)
   - Gaps where no documentation exists for a reported problem

4. Post the report as an issue with the date in the title. **If no documentation problems are found in issues**, call `noop` with "No documentation problems found in open issues — no action needed" instead of creating a report issue.

Keep the report informative but concise.