---
title: Research & Planning
description: Scheduled research reports, team status updates, and automated planning - weekly/daily intelligence gathering
sidebar:
  badge: { text: 'Scheduled', variant: 'tip' }
---

Research and planning workflows help teams stay informed, coordinate activities, and maintain strategic direction through automated intelligence gathering and status reporting.

## When to Use Research & Planning Workflows

- **Weekly research** - Stay current with industry trends and competitors
- **Daily status reports** - Automatic team activity summaries
- **Planning updates** - Keep project plans current and visible
- **Intelligence gathering** - Automated research on specific topics

You can write your own workflows customized for your team's specific needs. Here are some sample workflows from the Agentics collection:

### Weekly Research

Automatically collects latest trends, competitive analysis, and relevant research every Monday, keeping teams informed about industry developments without manual research overhead. [Learn more](https://github.com/githubnext/agentics/blob/main/docs/weekly-research.md)

### Daily Team Status

Analyzes repository activity, pull requests, and team progress to provide automated visibility into team productivity and project health. [Learn more](https://github.com/githubnext/agentics/blob/main/docs/daily-team-status.md)

### Daily Plan

Maintains and updates project planning issues with current priorities, ensuring project plans stay current and accessible to all team members. [Learn more](https://github.com/githubnext/agentics/blob/main/docs/daily-plan.md)

### Basic Research

Searches for information on a given topic, analyzes results, and creates structured summaries with relevant sources. Triggered manually via workflow_dispatch with research topic input. Workflow file: `.github/workflows/research.md`

### Model Context Protocol (MCP) Inspector

Analyzes all [MCP](/gh-aw/reference/glossary/#mcp-model-context-protocol) configuration files, extracts server details, and generates comprehensive inventory reports to maintain visibility into available [MCP servers](/gh-aw/reference/glossary/#mcp-server) and their capabilities. Runs weekly on Mondays at 10am UTC, or manually via workflow_dispatch. Workflow file: `.github/workflows/mcp-inspector.md`
