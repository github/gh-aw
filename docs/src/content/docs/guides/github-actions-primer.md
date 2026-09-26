---
title: GitHub Actions Primer
description: A comprehensive guide to understanding GitHub Actions, from its history and core concepts to testing workflows and comparing with agentic workflows
sidebar:
  order: 1
---

**GitHub Actions** is GitHub's automation platform for building, testing, and deploying code from your repository. Workflows are defined as YAML files and can run on repository events, schedules, or manual triggers. Agentic workflows compile from markdown into GitHub Actions YAML, so they use the same foundation while adding AI-driven decisions and stronger guardrails.

## Core Concepts

### YAML Workflows

A **YAML workflow** is an automated process defined in `.github/workflows/`. Each workflow consists of jobs that execute when triggered by events. Workflows must be stored on the **main** or default branch to be active and are versioned alongside your code.

**Example** (`.github/workflows/ci.yml`):

```yaml
name: CI
on:
  push:
    branches: [main]
  pull_request:
    branches: [main]

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - name: Run tests
        run: npm test
```

### Jobs

A **job** is a set of steps that runs on the same runner. Jobs run in parallel by default, but `needs:` can create dependencies. Each job gets a fresh VM, and results are shared with artifacts. Standard GitHub Actions jobs default to a 360-minute timeout; the agent execution step in agentic workflows defaults to 20 minutes.

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - run: npm run build

  test:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v7
      - run: npm test
```

### Steps

**Steps** are individual tasks within a job. They run sequentially, can execute shell commands or pre-built actions, and share the same filesystem and environment. A failed step stops the job by default.

```yaml
steps:
  # Action step - uses a pre-built action
  - uses: actions/checkout@v7

  # Run step - executes a shell command
  - name: Install dependencies
    run: npm install

  # Action with inputs
  - uses: actions/setup-node@v4
    with:
      node-version: '20'
```

## Security Model

### Workflow Storage and Execution

Workflows must be stored in `.github/workflows/` on the **default branch** to be active and trusted. This gives workflow changes normal code review, preserves an audit trail, and keeps the default branch as the trust boundary.

```yaml
# Workflows on main branch can access secrets
on:
  push:
    branches: [main]

jobs:
  deploy:
    runs-on: ubuntu-latest
    environment: production
    steps:
      - run: echo "Has access to production secrets"
```

### Permission Model

GitHub Actions follows the **principle of least privilege** with explicit permission declarations. Fork pull requests are read-only by default, and required permissions should be declared explicitly.

```yaml
permissions:
  contents: read       # Read repository contents
  issues: write        # Create/modify issues
  pull-requests: write # Create/modify PRs

jobs:
  example:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Job has specified permissions only"
```

With GitHub Agentic Workflows, **write permissions are not used directly**. Instead, workflows declare **safe outputs**, which validate, constrain, and sanitize GitHub write operations.

### Secret Management

**Secrets** are encrypted environment variables stored at the repository, organization, or environment level. They are masked in logs, available only where GitHub permits them, and can be further scoped by environment.

```yaml
jobs:
  deploy:
    runs-on: ubuntu-latest
    steps:
      - name: Deploy to production
        env:
          API_KEY: ${{ secrets.API_KEY }}
        run: ./deploy.sh
```

## Testing and Debugging Workflows

### Testing from Branches with workflow_dispatch

The **`workflow_dispatch`** trigger allows manual workflow execution from a selected branch, which is especially useful for development and testing:

```yaml
name: Test Workflow
on:
  workflow_dispatch:
    inputs:
      environment:
        description: 'Target environment'
        required: true
        default: 'staging'
        type: choice
        options:
          - staging
          - production
      debug:
        description: 'Enable debug logging'
        required: false
        type: boolean

jobs:
  test:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Testing in ${{ inputs.environment }}"
      - run: echo "Debug mode: ${{ inputs.debug }}"
```

To run it, open the **Actions** tab, select the workflow, click **Run workflow**, then choose a branch and provide inputs.

> [!TIP]
> Enable debug logging by setting repository secrets `ACTIONS_STEP_DEBUG: true` and `ACTIONS_RUNNER_DEBUG: true`.

**Note:** The workflow must already exist on the default branch before you can run it manually. On non-default branches, only `workflow_dispatch` is available; other event triggers do not activate from branch-only workflow changes.

### Debugging Workflow Runs

View logs in the **Actions** tab by opening a run, then a job, then individual steps. Use workflow commands for structured output:

```yaml
steps:
  - name: Debug context
    run: |
      echo "::debug::Debugging workflow context"
      echo "::notice::This is a notice"
      echo "::warning::This is a warning"
      echo "::error::This is an error"

  - name: Debug environment
    run: |
      echo "GitHub event: ${{ github.event_name }}"
      echo "Actor: ${{ github.actor }}"
      printenv | sort
```

## Agentic Workflows vs Traditional GitHub Actions

Agentic workflows compile to GitHub Actions YAML and run on the same infrastructure, but they add stronger security controls, a simpler authoring model, and AI-driven decision-making.

| Feature | Traditional GitHub Actions | Agentic Workflows |
|---------|----------------------------|-------------------|
| **Definition Language** | YAML with explicit steps | Natural language markdown |
| **Complexity** | Requires YAML expertise, API knowledge | Describe intent in plain English |
| **Decision Making** | Fixed if-then logic | AI-powered contextual decisions |
| **Security Model** | Token-based with broad permissions | Sandboxed with safe-outputs |
| **Write Operations** | Direct API access with `GITHUB_TOKEN` | Sanitized through safe-output validation |
| **Network Access** | Unrestricted by default | Allowlisted domains only |
| **Execution Environment** | Standard runner VM | Enhanced sandbox with MCP isolation |
| **Tool Integration** | Manual action selection | MCP server automatic tool discovery |
| **Testing** | `workflow_dispatch` on branches | Same, plus local compilation |
| **Auditability** | Standard workflow logs | Enhanced with agent reasoning logs |

## Next Steps and Resources

Start with the **[Quick Start](/gh-aw/setup/quick-start/)**, then review **[Workflow Structure](/gh-aw/reference/workflow-structure/)** and **[Safe Outputs](/gh-aw/reference/safe-outputs/)**. For deeper background, see **[Security Best Practices](/gh-aw/introduction/architecture/)**, **[Design Patterns](/gh-aw/patterns/issue-ops/)**, and the **[Glossary](/gh-aw/reference/glossary/)**.

For GitHub-native details, refer to the official **[GitHub Actions Documentation](https://docs.github.com/en/actions)**, **[Workflow Syntax](https://docs.github.com/en/actions/reference/workflow-syntax-for-github-actions)**, and **[Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)** guides.
