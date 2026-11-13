---
title: "Connectors: A Clear Integration Point for Services"
description: "Connectors give services a shared, declarative integration point in gh-aw, with YAML definitions for access, capabilities, and boundaries."
authors:
  - copilot
date: 2026-11-12
metadata:
  seoDescription: "Explore connectors, the experimental declarative service integration point in gh-aw, with a small YAML example and explicit read/write boundaries."
---

Every service integration has to answer the same questions: where does the service live, how does authentication work, and what can a workflow do with it?

Without a clear integration point, those answers end up scattered across scripts, custom tools, and prompts. Each new service becomes another special case.

The new experimental **connectors** give services a shared integration point in GitHub Agentic Workflows. A **declarative YAML definition** describes the service contract. gh-aw's compiler and runtime use that contract to expose selected capabilities to workflows.

The goal isn't a different integration mechanism for every provider. It's one place to define how a service fits into gh-aw.

## One contract, shared infrastructure

A connector brings the pieces of an integration together: fixed endpoints, credential routes, resource grants, read capabilities, and safe outputs. It can describe bounded REST or GraphQL readers, read-only HTTP MCP tools, and typed mutations. Trigger declarations use the shared webhook gateway path rather than inventing a separate dispatch mechanism for each service.

The provider-specific details live in the declaration. The compiler and runtime supply the common machinery for validation, tool exposure, and safe-output execution. Adding a supported operation is a declaration change, not a reason to build a new provider-specific execution path.

That also makes the integration reviewable. A new endpoint, a different secret reference, or an added write operation is visible in a diff. The contract can be versioned and reused alongside the workflows that consume it.

## A small look at the YAML

In gh-aw, connector YAML lives in the frontmatter of an importable Markdown file under `.github/workflows/connectors/`. Here's a short excerpt from the [Linear declaration](https://github.com/github/gh-aw/blob/main/.github/workflows/connectors/linear.md):

```yaml wrap
connectors:
  linear:
    experimental: true
    endpoints:
      api: https://api.linear.app
    credentials:
      reader:
        phase: agent
        header: Authorization
        scheme: raw
        scopes: [read]
        secret: ${{ github.aw.import-inputs.reader-secret }}
    resources:
      issues: ${{ github.aw.import-inputs.issues }}
```

The shape matters more than the provider: a named endpoint, an explicit credential phase, and resource bindings supplied by the consuming workflow. Secret values stay in GitHub Actions secrets.

This isn't a complete connector. The full declaration adds typed operation inputs, request mappings, response limits, resource checks, and import input definitions. Its commented documentation explains setup and provider-specific limits.

Linear, Jira, Azure DevOps, Slack, Teams, and other services already have declarations using this shared model. Their capabilities differ, but they have a common place to plug in.

## Define once, select per workflow

The connector defines what's available. The workflow selects what it needs. Importing alone enables no provider tools or writes.

Reads go under `tools`; mutations are selected separately under `safe-outputs`, outside the agent's writer-credential boundary. Resource bindings constrain access, and IDs returned by the service don't expand those grants.

For example, the [Linear triage workflow](https://github.com/github/gh-aw/blob/main/pkg/cli/workflows/linear-golden.md) selects two readers and one staged comment. Another workflow can consume the same declaration with different selections and approved resources. Staging previews the write without delivering it; reads remain live.

The service contract is reusable. The authority stays specific to the task.

## A place to build integrations

Connectors are experimental, and they aren't turnkey service installations. Declarations don't provision credentials, install webhook receivers, or turn scope metadata into provider permissions. Start with a connector-capable compiler and matching runtime, a reviewed source revision, manual dispatch, and staged outputs.

If you're bringing a service into gh-aw, start with the [connector reference](/gh-aw/reference/connectors/) or the [create-connector skill](https://github.com/github/gh-aw/blob/main/.github/skills/create-connector/SKILL.md). The [example catalog](/gh-aw/reference/connectors/#golden-workflows) shows how workflows consume the contract.

Define the service once, in YAML. Let workflows choose the capabilities they need. Keep integration behavior in a clear, reviewable place.
