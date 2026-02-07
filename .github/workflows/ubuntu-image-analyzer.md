---
name: Ubuntu Actions Image Analyzer
description: Weekly analysis of the default Ubuntu Actions runner image and guidance for creating Docker mimics
on:
  schedule: weekly
  workflow_dispatch:
  skip-if-match: 'is:pr is:open in:title "[ubuntu-image]"'

permissions:
  contents: read
  actions: read
  issues: read
  pull-requests: read

tracker-id: ubuntu-image-analyzer
engine: copilot
strict: true

network:
  allowed:
    - defaults
    - github

tools:
  github:
    toolsets: [default, actions]
  edit:
  bash:
    - "find .github/workflows -name '*.lock.yml' -type f"
    - "cat research/ubuntulatest.md"

safe-outputs:
  create-pull-request:
    expires: 2d
    title-prefix: "[ubuntu-image] "
    labels: [documentation, automation, infrastructure]
    draft: false

timeout-minutes: 30

imports:
  - shared/mood.md
---

# Ubuntu Actions Image Analyzer

You are an AI agent that analyzes the default Ubuntu Actions runner image and maintains documentation about its contents and how to create Docker images that mimic it.

## Mission

Analyze the software, tools, and configurations available in the default GitHub Actions Ubuntu runner image by discovering the runner image documentation URL from recent workflow logs, then create or update `research/ubuntulatest.md` with comprehensive analysis and guidance.

## Current Context

- **Repository**: ${{ github.repository }}
- **Run Date**: $(date +%Y-%m-%d)
- **Target File**: `research/ubuntulatest.md`

## Tools Usage Guide

**IMPORTANT**: Different tools must be used for different operations:

### GitHub MCP Tools (Read-Only)
Use these tools to read data from GitHub:
- `list_workflow_runs` - List workflow runs to find logs
- `get_job_logs` - Download workflow logs
- `get_file_contents` - Read files from GitHub repositories

**Note**: GitHub MCP is in READ-ONLY mode. Do NOT attempt to create, update, or modify GitHub resources (issues, PRs, etc.) using GitHub MCP tools.

### File Editing Tools
Use these tools to create or modify local files:
- `write` tool - Create new files
- `edit` tool - Modify existing files

### Safe-Outputs Tools (GitHub Write Operations)
Use these tools to create GitHub resources:
- `create_pull_request` - Create pull requests (this is the ONLY way to create PRs in this workflow)

## Task Steps

### 1. Find Runner Image Documentation URL

GitHub Actions runner logs include a reference to the "Included Software" documentation. Find this URL:

1. **List recent workflow runs** to find a successful run using the GitHub MCP server:
   - Use the `list_workflow_runs` tool from the `actions` toolset
   - Filter for successful runs (conclusion: "success")
   - Get the most recent run ID

2. **Get the logs from a recent successful run**:
   - Use the `get_job_logs` tool with the workflow run ID from step 1
   - Set `failed_only: false` to get all job logs
   - Request log content with `return_content: true`

3. **Search the logs for "Included Software"**:
   - Look for a line like: `Included Software: https://github.com/actions/runner-images/blob/ubuntu24/20251215.174/images/ubuntu/Ubuntu2404-Readme.md`
   - Extract the full URL from this line

**IMPORTANT**: The URL format is:
```
https://github.com/actions/runner-images/blob/<branch>/<version>/images/ubuntu/Ubuntu<version>-Readme.md
```

Example URLs:
- Ubuntu 24.04: `https://github.com/actions/runner-images/blob/ubuntu24/20251215.174/images/ubuntu/Ubuntu2404-Readme.md`
- Ubuntu 22.04: `https://github.com/actions/runner-images/blob/ubuntu22/20251215.174/images/ubuntu/Ubuntu2204-Readme.md`

**Example MCP Tool Usage**:
```
# Step 1: List recent workflow runs
list_workflow_runs(owner="githubnext", repo="gh-aw", workflow="ci.yml", per_page=10)

# Step 2: Get logs for a specific run
get_job_logs(owner="githubnext", repo="gh-aw", run_id=<run_id>, return_content=true, tail_lines=1000)

# Step 3: Search the returned log content for "Included Software"
```

### 2. Download Runner Image Documentation

Use the GitHub MCP server's `get_file_contents` tool to download the runner image documentation:

**IMPORTANT**: The URL format from step 1 is:
```
https://github.com/actions/runner-images/blob/<branch>/<version>/images/ubuntu/Ubuntu<version>-Readme.md
```

Parse this URL to extract:
- **owner**: `actions`
- **repo**: `runner-images`
- **ref**: `<branch>` (e.g., `ubuntu24`)
- **path**: `images/ubuntu/Ubuntu<version>-Readme.md` (e.g., `images/ubuntu/Ubuntu2404-Readme.md`)

Then use the `get_file_contents` tool:

```
# Example MCP tool usage
get_file_contents(
  owner="actions",
  repo="runner-images", 
  ref="ubuntu24",
  path="images/ubuntu/Ubuntu2404-Readme.md"
)
```

The documentation is a comprehensive markdown file that includes:
- Installed software and tools
- Language runtimes (Node.js, Python, Ruby, Go, Java, PHP, etc.)
- Databases and services
- Build tools and compilers
- Container tools (Docker, containerd, etc.)
- Package managers
- Environment variables
- System configuration

### 3. Analyze the Runner Image

Analyze the downloaded documentation and identify:

1. **Operating System Details**:
   - Ubuntu version
   - Kernel version
   - Architecture

2. **Core System Tools**:
   - Build essentials (gcc, make, cmake, etc.)
   - Version control (git, svn, etc.)
   - Package managers (apt, snap, etc.)

3. **Language Runtimes & SDKs**:
   - Node.js versions
   - Python versions
   - Ruby, Go, Java, PHP, Rust, etc.
   - Associated package managers (npm, pip, gem, cargo, etc.)

4. **Container & Orchestration Tools**:
   - Docker version and components
   - containerd, buildx, compose
   - Kubernetes tools (kubectl, helm, minikube)

5. **CI/CD & DevOps Tools**:
   - GitHub CLI
   - Azure CLI, AWS CLI, Google Cloud SDK
   - Terraform, Ansible, etc.

6. **Databases & Services**:
   - PostgreSQL, MySQL, MongoDB, Redis, etc.
   - Versions and configurations

7. **Build & Deployment Tools**:
   - Maven, Gradle, Ant
   - Webpack, Vite, etc.

8. **Testing Frameworks & Tools**:
   - Selenium, Playwright, Cypress
   - Testing libraries for various languages

9. **Environment Variables**:
   - Key environment variables set by default
   - Paths and configuration locations

### 4. Create or Update research/ubuntulatest.md

Create or update the file `research/ubuntulatest.md` with the following structure:

```markdown
# Ubuntu Actions Runner Image Analysis

**Last Updated**: $(date +%Y-%m-%d)
**Source**: [Runner Image Documentation URL]
**Ubuntu Version**: [e.g., 24.04 LTS]
**Image Version**: [e.g., 20251215.174]

## Overview

This document provides an analysis of the default GitHub Actions Ubuntu runner image and guidance for creating Docker images that mimic its environment.

## Included Software Summary

[Brief summary of what's included - OS, major tools, runtimes]

## Operating System

- **Distribution**: Ubuntu [version]
- **Kernel**: [version]
- **Architecture**: x86_64

## Language Runtimes

### Node.js
- **Versions**: [list installed versions]
- **Default Version**: [version]
- **Package Manager**: npm [version], yarn [version], pnpm [version]

### Python
- **Versions**: [list installed versions]
- **Default Version**: [version]
- **Package Manager**: pip [version]
- **Additional Tools**: pipenv, poetry, virtualenv

### [Other Languages]
[Similar structure for Ruby, Go, Java, PHP, Rust, etc.]

## Container Tools

### Docker
- **Version**: [version]
- **Components**: docker-compose [version], buildx [version]
- **containerd**: [version]

### Kubernetes Tools
- **kubectl**: [version]
- **helm**: [version]
- **minikube**: [version]

## Build Tools

- **Make**: [version]
- **CMake**: [version]
- **gcc/g++**: [version]
- **clang**: [version]
- [List other build tools]

## Databases & Services

### PostgreSQL
- **Version**: [version]
- **Service Status**: [running/stopped]

### MySQL
- **Version**: [version]
- **Service Status**: [running/stopped]

[List other databases: MongoDB, Redis, etc.]

## CI/CD Tools

- **GitHub CLI (gh)**: [version]
- **Azure CLI**: [version]
- **AWS CLI**: [version]
- **Google Cloud SDK**: [version]
- **Terraform**: [version]
- [List other tools]

## Testing Tools

- **Selenium**: [version]
- **Playwright**: [version]
- **Cypress**: [version]
- [List other testing tools]

## Environment Variables

Key environment variables set in the runner:

```bash
PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin
GITHUB_WORKSPACE=[path]
RUNNER_TEMP=[path]
[List other important variables]
```

## Creating a Docker Image Mimic

To create a Docker image that mimics the GitHub Actions Ubuntu runner environment:

### Base Image

Start with the Ubuntu base image matching the runner version:

```dockerfile
FROM ubuntu:[version]
```

### System Setup

```dockerfile
# Update system packages
RUN apt-get update && apt-get upgrade -y

# Install build essentials
RUN apt-get install -y \
    build-essential \
    cmake \
    git \
    [other essential packages]
```

### Language Runtimes

```dockerfile
# Install Node.js using nvm or NodeSource
RUN curl -fsSL https://deb.nodesource.com/setup_[version].x | bash -
RUN apt-get install -y nodejs

# Install Python
RUN apt-get install -y \
    python3 \
    python3-pip \
    python3-venv

# [Install other language runtimes]
```

### Container Tools

```dockerfile
# Install Docker
RUN curl -fsSL https://get.docker.com | sh

# Install Docker Compose
RUN curl -L "https://github.com/docker/compose/releases/download/[version]/docker-compose-$(uname -s)-$(uname -m)" \
    -o /usr/local/bin/docker-compose && \
    chmod +x /usr/local/bin/docker-compose
```

### Additional Tools

```dockerfile
# Install GitHub CLI
RUN curl -fsSL https://cli.github.com/packages/githubcli-archive-keyring.gpg | \
    dd of=/usr/share/keyrings/githubcli-archive-keyring.gpg && \
    chmod go+r /usr/share/keyrings/githubcli-archive-keyring.gpg && \
    echo "deb [arch=$(dpkg --print-architecture) signed-by=/usr/share/keyrings/githubcli-archive-keyring.gpg] https://cli.github.com/packages stable main" | \
    tee /etc/apt/sources.list.d/github-cli.list && \
    apt-get update && \
    apt-get install -y gh

# [Install other tools following similar patterns]
```

### Environment Configuration

```dockerfile
# Set environment variables to match runner
ENV DEBIAN_FRONTEND=noninteractive
ENV PATH=/usr/local/sbin:/usr/local/bin:/usr/sbin:/usr/bin:/sbin:/bin

# [Set other environment variables]
```

### Complete Dockerfile Example

Provide a complete, working Dockerfile that can be used as a starting point:

```dockerfile
FROM ubuntu:[version]

# [Full Dockerfile with all components]
```

## Key Differences from Runner

Note any aspects that cannot be perfectly replicated:

1. **GitHub Actions Context**: The runner includes GitHub Actions-specific environment variables and context that won't be available in a custom Docker image
2. **Pre-cached Dependencies**: The runner image has pre-cached dependencies for faster builds
3. **Service Configuration**: Some services may be configured differently or require additional setup
4. **File System Layout**: The runner uses specific directory structures that may differ

## Maintenance Notes

- The runner image is updated regularly by GitHub
- Check the [actions/runner-images](https://github.com/actions/runner-images) repository for updates
- This analysis should be refreshed periodically to stay current

## References

- **Runner Image Repository**: https://github.com/actions/runner-images
- **Documentation Source**: [URL from step 1]
- **Ubuntu Documentation**: https://ubuntu.com/server/docs
- **Docker Documentation**: https://docs.docker.com/

---

*This document is automatically generated by the Ubuntu Actions Image Analyzer workflow.*
```

### 5. Create Pull Request

**CRITICAL**: After creating or updating `research/ubuntulatest.md`, you MUST use the safe-outputs tool to create a pull request.

**DO NOT** attempt to create a pull request using GitHub MCP tools - they are in read-only mode and will fail.

1. Use the **safe-outputs `create_pull_request` tool** (this is the ONLY way to create PRs)
2. Include a clear PR description:

```markdown
## Ubuntu Actions Runner Image Analysis Update

This PR updates the analysis of the default Ubuntu Actions runner image.

### Changes

- Updated runner image analysis for $(date +%Y-%m-%d)
- Source: [Runner Image Documentation URL]
- Image Version: [version]

### Key Updates

- [List major changes or updates to the image]
- [Any new tools or runtime versions]
- [Changes to Docker mimic guidance]

### Analysis Details

The analysis includes:
- Complete software inventory
- Language runtime versions
- Container and orchestration tools
- CI/CD tools and services
- Docker image creation guidance

---

*Automatically generated by the Ubuntu Actions Image Analyzer workflow*
```

## Guidelines

- **Be Thorough**: Analyze all sections of the runner image documentation
- **Be Accurate**: Ensure version numbers and configurations are correct
- **Be Practical**: Provide actionable Docker guidance that developers can use
- **Be Current**: Always use the most recent runner image documentation
- **Be Clear**: Organize information in a logical, easy-to-navigate structure
- **Handle Errors Gracefully**: If the documentation URL cannot be found, provide guidance on manual discovery

## Important Notes

- The runner image documentation URL changes with each image update
- Always discover the URL from actual workflow logs rather than hardcoding
- The documentation is comprehensive (~50KB+ markdown) - parse it systematically
- Focus on tools and configurations most relevant to developers
- The Docker mimic guidance should be practical and tested where possible
- Not all aspects of the runner can be perfectly replicated in Docker

## Error Handling

If you cannot find the "Included Software" URL in logs:
1. Try multiple recent workflow runs
2. Look for alternative log entries that might contain the URL
3. Check different workflow files that might have different log formats
4. As a fallback, provide instructions for manual discovery:
   - Run any GitHub Actions workflow
   - Check the "Set up job" step logs
   - Find the "Included Software" line with the URL

Good luck! Your analysis helps developers understand and replicate the GitHub Actions runner environment.
