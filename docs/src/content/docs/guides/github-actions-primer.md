---
title: GitHub Actions Primer
description: A comprehensive guide to understanding GitHub Actions, from its history and core concepts to testing workflows and comparing with agentic workflows
sidebar:
  order: 1
---

This guide introduces GitHub Actions - GitHub's native automation platform - and explains how it forms the foundation for agentic workflows. Understanding GitHub Actions helps you leverage its capabilities effectively while appreciating the enhanced security and AI-powered features that agentic workflows provide.

## What is GitHub Actions?

**GitHub Actions** is GitHub's integrated automation platform that enables you to build, test, and deploy code directly from your repository. It allows you to automate workflows in response to repository events, schedules, or manual triggers - all defined in YAML files stored in your repository.

## Core Concepts

Understanding these fundamental concepts is essential for working with both traditional GitHub Actions and agentic workflows.

### YAML Workflows

A traditional **YAML workflow** is an automated process defined in a YAML file stored in your repository's `.github/workflows/` directory. Each workflow consists of one or more jobs that execute when triggered by specific events.

**Example YAML workflow file** (`.github/workflows/ci.yml`):

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
      - uses: actions/checkout@v4
      - name: Run tests
        run: npm test
```

**Key characteristics:**

- Must be stored in `.github/workflows/` directory on the **main** or **default branch** to be active
- Define triggers (events), jobs, and execution environments
- Can be manually triggered, scheduled, or event-driven
- Are versioned alongside your code

Agentic workflows compile from markdown files into secure GitHub Actions YAML workflows, inheriting these core concepts while adding AI-driven decision-making and enhanced security.

### Jobs

A **job** is a set of steps that execute on the same runner (virtual machine). Jobs run in parallel by default but can be configured to run sequentially with dependencies.

**Job example:**

```yaml
jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm run build

  test:
    needs: build
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v4
      - run: npm test
```

**Job characteristics:**
- Each job runs in a fresh virtual machine or container
- Jobs can depend on other jobs using `needs:`
- Results can be shared between jobs using artifacts
- Default timeout is 360 minutes (6 hours)

### Steps

**Steps** are individual tasks within a job. They run sequentially and can execute commands, run scripts, or use pre-built actions from the GitHub Marketplace.

**Types of steps:**

```yaml
steps:
  # Action step - uses a pre-built action
  - uses: actions/checkout@v4
  
  # Run step - executes a shell command
  - name: Install dependencies
    run: npm install
  
  # Multi-line script
  - name: Build and test
    run: |
      npm run build
      npm test
      
  # Action with inputs
  - uses: actions/setup-node@v4
    with:
      node-version: '20'
```

**Step characteristics:**
- Execute in order within a job
- Share the same filesystem and environment
- Can pass data between steps using outputs
- Failed steps stop job execution by default

## Security Model

GitHub Actions implements a comprehensive security model designed to protect your repositories, secrets, and infrastructure.

### Workflow Storage and Execution

> [!CAUTION]
> Workflows **must** be stored in the `.github/workflows/` directory on the **default branch** (typically `main`) or other protected branches to be active and trusted.

**Why workflows require main branch storage:**

1. **Code Review**: Changes to workflows undergo the same review process as code changes
2. **Audit Trail**: Git history provides a complete record of workflow modifications
3. **Privilege Escalation Prevention**: Prevents attackers from modifying workflows in feature branches to access secrets
4. **Trust Boundary**: The default branch represents reviewed, trusted automation code

**Branch protection for workflows:**

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

GitHub Actions uses the **principle of least privilege** with explicit permission declarations:

```yaml
permissions:
  contents: read        # Read repository contents
  issues: write        # Create/modify issues
  pull-requests: write # Create/modify PRs
  
jobs:
  example:
    runs-on: ubuntu-latest
    steps:
      - run: echo "Job has specified permissions only"
```

**Default permissions:**
- Fork pull requests: **read-only** to repository contents
- Repository workflows: Configurable in repository settings
- Recommended: Explicitly declare all required permissions

### Secret Management

**Secrets** are encrypted environment variables stored at the repository, organization, or environment level:

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

**Secret security features:**
- Encrypted at rest using GitHub's infrastructure
- Never exposed in logs (automatically redacted)
- Only accessible to workflows on default/protected branches
- Scoped by environment for additional protection

## Testing and Debugging Workflows

Unlike code that can be tested locally, workflows require a GitHub Actions environment. GitHub provides several mechanisms for testing and debugging.

### Testing from Branches with workflow_dispatch

The **`workflow_dispatch`** trigger allows manual workflow execution from any branch, making it invaluable for development and testing:

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

**Using workflow_dispatch for testing:**

1. **Create test workflow** with `workflow_dispatch` trigger and **merge to main branch**
2. **Navigate to Actions** tab in GitHub repository
3. **Select your workflow** from the left sidebar
4. **Click "Run workflow"** dropdown on the right
5. **Choose your branch** (you can select any branch to run from) and provide input values
6. **Click "Run workflow"** button to execute

> [!TIP]
> Enable debug logging for workflow runs by setting repository secrets:
> - `ACTIONS_STEP_DEBUG: true` - Enables step debug logging
> - `ACTIONS_RUNNER_DEBUG: true` - Enables runner diagnostic logging

**Limitations of branch-based testing:**
- Must use `workflow_dispatch` - event triggers don't activate on non-default branches
- Workflow definition must be merged to main branch before it can be executed

### Debugging Workflow Runs

**Viewing workflow logs:**

1. Navigate to the **Actions** tab in your repository
2. Click the workflow run to view details
3. Click a job to see individual step logs
4. Click a step to expand detailed output

**Debug logging commands:**

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

### Comparison Table

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

## Next Steps

Now that you understand GitHub Actions fundamentals, explore how agentic workflows build upon this foundation:

- **[Quick Start](/gh-aw/setup/quick-start/)** - Create your first agentic workflow
- **[Security Best Practices](/gh-aw/introduction/architecture/)** - Deep dive into agentic security model
- **[Safe Outputs](/gh-aw/reference/safe-outputs/)** - Learn about validated GitHub operations
- **[Workflow Structure](/gh-aw/reference/workflow-structure/)** - Understand markdown workflow syntax
- **[Examples](/gh-aw/examples/scheduled/research-planning/)** - Real-world agentic workflow patterns

## Additional Resources

**GitHub Actions Documentation:**
- [GitHub Actions Documentation](https://docs.github.com/en/actions) - Official reference
- [Workflow Syntax](https://docs.github.com/en/actions/reference/workflow-syntax-for-github-actions) - Complete YAML reference
- [Actions Marketplace](https://github.com/marketplace?type=actions) - Pre-built actions
- [Security Hardening](https://docs.github.com/en/actions/security-guides/security-hardening-for-github-actions) - Security best practices

**Agentic Workflows:**
- [Architecture Overview](/gh-aw/introduction/architecture/) - Detailed security architecture
- [Glossary](/gh-aw/reference/glossary/) - Key terms and concepts
- [Agentics Collection](https://github.com/githubnext/agentics) - Example workflows
