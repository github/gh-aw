---
title: GitHub Actions Primer
description: A comprehensive guide to understanding GitHub Actions, from its history and core concepts to testing workflows and comparing with agentic workflows
sidebar:
  order: 1
---

**GitHub Actions** is GitHub's integrated automation platform for building, testing, and deploying code from your repository. It enables automated workflows triggered by repository events, schedules, or manual triggers — all defined in YAML files in your repository. Agentic workflows compile from markdown files into secure GitHub Actions YAML, inheriting these core concepts while adding AI-driven decision-making and enhanced security.

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

A **job** is a set of steps that execute on the same runner (virtual machine). Jobs run in parallel by default but can depend on each other with `needs:`. Each job runs in a fresh VM, and results are shared between jobs using artifacts. Default timeout is 360 minutes for standard GitHub Actions jobs; the agent execution step in agentic workflows defaults to 20 minutes.

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

**Steps** are individual tasks within a job, running sequentially. They can execute shell commands or use pre-built actions from the GitHub Marketplace. Steps share the same filesystem and environment; a failed step stops the job by default.

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

Workflows must be stored in `.github/workflows/` on the **default branch** to be active and trusted. This ensures changes undergo code review, maintains an audit trail, prevents privilege escalation from feature branches, and treats the default branch as a trust boundary.

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

GitHub Actions uses the **principle of least privilege** with explicit permission declarations. Fork pull requests are read-only by default; all required permissions should be explicitly declared.

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

With GItHub Agentic Workflows, **write permissions are not used explicitly**. Instead much more restricted capabilities to write to GitHub are declared through **safe outputs**, which validate, constrain and sanitize all GitHub API interactions.

### Secret Management

**Secrets** are encrypted environment variables stored at the repository, organization, or environment level. They are never exposed in logs, only accessible to workflows on default/protected branches, and scoped by environment for additional protection.

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

The **`workflow_dispatch`** trigger allows manual workflow execution from any branch, invaluable for development and testing:

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

To run: navigate to the **Actions** tab → select your workflow → click **Run workflow** → choose your branch and provide inputs.

> [!TIP]
> Enable debug logging by setting repository secrets `ACTIONS_STEP_DEBUG: true` and `ACTIONS_RUNNER_DEBUG: true`.

**Note:** The workflow definition must be merged to the main branch before it can be executed. Only `workflow_dispatch` works on non-default branches — event triggers do not.

### Debugging Workflow Runs

View logs in the **Actions** tab by clicking a run, then a job, then individual steps. Use workflow commands for structured output:

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

While agentic workflows compile to GitHub Actions YAML and run on the same infrastructure, they introduce significant enhancements in security, simplicity, and AI-powered decision-making.

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

- **[Quick Start](/gh-aw/setup/quick-start/)** - Create your first agentic workflow
- **[Security Best Practices](/gh-aw/introduction/architecture/)** - Deep dive into agentic security model
- **[Safe Outputs](/gh-aw/reference/safe-outputs/)** - Learn about validated GitHub operations
- **[Workflow Structure](/gh-aw/reference/workflow-structure/)** - Understand markdown workflow syntax
- **[Design Patterns](/gh-aw/patterns/issue-ops/)** - Real-world agentic workflow patterns
- **[Glossary](/gh-aw/reference/glossary/)** - Key terms and concepts
- **[GitHub Actions Documentation](https://docs.github.com/en/actions)** - Official reference
- **[Workflow Syntax](https://docs.github.com/en/actions/reference/workflow-syntax-for-github-actions)** - Complete YAML reference
- **[Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions)** - Security best practices
