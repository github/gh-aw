---
title: "Connectors: Agentic Workflows Beyond GitHub"
description: "Bring context from Linear and other services into agentic workflows, without handing an agent the keys to your entire workspace."
authors:
  - copilot
date: 2026-11-12
metadata:
  seoDescription: "Explore experimental gh-aw connectors with a Linear bug-triage example using scoped reads, separate credentials, and a staged comment."
---

A bug lands in Linear. The code lives on GitHub. Before anyone can investigate, someone has to read the report, work out what's missing, and turn it into a useful next step.

That's a good job for an agent. But helping with one bug shouldn't mean handing it the keys to your entire tracker.

The new experimental **connectors** in GitHub Agentic Workflows bring that outside context into your workflows, with explicit boundaries around what an agent can read and what it can ask to change. Declarations are available for Linear, Jira, Azure DevOps, Slack, Teams, and other services.

## Bring the context, keep the boundaries

A connector is a Markdown file, not a new integration service to deploy. You can review it, commit it alongside your workflow, and choose the capabilities that task actually needs.

For Linear triage, that might mean reading one issue and its comments, then proposing a single response. Not searching every team. Not changing priorities. Not closing issues.

The important split is between **reading evidence** and **taking action**. Readers go under `tools`. Changes go through `safe-outputs`, outside the agent's writer-credential boundary. Importing a connector alone enables neither.

## From a Linear report to a useful next step

Imagine an intake workflow that answers a practical question: *Is this bug ready to investigate, or does the reporter need to add something first?*

It reads the approved report and discussion, looks for reproduction steps and expected versus actual behavior, and drafts a short comment with the next action. If an equivalent assessment already exists, it skips the comment. If the discussion is incomplete or a read fails, it stops rather than guessing.

Here's the connector-specific configuration from the [complete Linear triage example](https://github.com/github/gh-aw/blob/main/pkg/cli/workflows/linear-golden.md):

```aw wrap
imports:
  - uses: ".github/workflows/connectors/linear.md"
    with:
      issues: [22222222-2222-4222-8222-222222222222]
      organizations: [11111111-1111-4111-8111-111111111111]
      teams: [33333333-3333-4333-8333-333333333333]
tools:
  linear: [get-issue, list-comments]
safe-outputs:
  staged: true
  linear-add-comment:
    max: 1
```

Two readers. One possible comment. One approved destination.

This is an excerpt, not a standalone workflow. The complete example includes the manual trigger, permissions, network settings, credential bindings, and prompt. To try it, use a connector-capable compiler and matching runtime, copy the reviewed [Linear declaration](https://github.com/github/gh-aw/blob/main/.github/workflows/connectors/linear.md) to its imported path, and replace the specimen UUIDs consistently in the configuration and prompt. Pin the source you adopt to a reviewed commit.

Supply a read-only Linear key through the `LINEAR_READ_API_KEY` Actions secret and configure the engine's inference access. Credentials stay in secrets, not in the Markdown.

## Preview the comment before posting it

Notice `staged: true`. It lets you inspect the proposed comment without posting it to Linear or resolving a writer credential. The reads are still live when the workflow runs; staging previews the write, not the entire execution.

The `max: 1` limit is enforced by the dispatcher, not just requested in the prompt. Issue text and comments remain untrusted evidence, and IDs found in a response don't grant access to more resources.

Moving to live comments is a deliberate configuration change: provision the example's separate `LINEAR_COMMENT_WRITE_API_KEY` with Create comments access, disable staging, and recompile. Staged comments aren't automatically delivered later.

## Start with one task

The useful starting point isn't "connect everything." It's one recurring handoff: a Linear bug that needs reproduction details, a Slack thread that needs action items, or an incident that needs an engineering follow-up.

All included connectors are experimental, and provider coverage varies. They don't provision credentials or install webhook receivers. Start with manual dispatch and a staged output, then review what the workflow actually produces.

Pick a scenario from the [example catalog](/gh-aw/reference/connectors/#golden-workflows), or explore the [connector reference](/gh-aw/reference/connectors/) for setup details. Bring the context your workflow needs, and keep the authority no broader than the task.
