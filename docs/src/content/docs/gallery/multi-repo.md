---
title: Multi-Repository Examples
description: Complete examples for managing workflows across multiple GitHub repositories, including feature synchronization, cross-repo tracking, quality monitoring, and organization-wide updates.
---

Multi-repository operations coordinate work across GitHub repositories while preserving clear access boundaries. These examples show common cross-repo patterns.

## Featured Examples

### [Triage from Side Repo](/gh-aw/gallery/multi-repo/triage-from-side-repo/)

Runs automated issue triage on a main repository from an isolated side repository, including a slash-command bridge for real-time `/triage` responses. Use it to experiment with agentic triage without modifying the main codebase.

### [Code Quality Monitoring](/gh-aw/gallery/multi-repo/code-quality-monitoring/)

Runs weekly code quality analysis from a side repository by checking out the target codebase locally, running linters and complexity checks, and creating focused issues. Use it for ongoing quality gates across repositories you do not want to modify.

### [Feature Synchronization](/gh-aw/gallery/multi-repo/feature-sync/)

Automates code synchronization from main repositories to sub-repositories or downstream services through pull requests with change detection, path filters, and bidirectional sync support. It fits monorepo alternatives, shared component libraries, multi-platform deployments, and fork maintenance.

### [Cross-Repository Issue Tracking](/gh-aw/gallery/multi-repo/issue-tracking/)

Centralizes issue tracking by creating tracking issues in a central repository with status synchronization for multi-component work. Use it for component visibility, multi-team coordination, cross-project initiatives, and upstream dependency tracking.

### [Dependabot Rollout](/gh-aw/gallery/multi-repo/dependabot-rollout/)

Rolls out a customized Dependabot configuration across many repositories using an orchestrator and worker pair from a central control repository. The orchestrator prioritizes targets, then dispatches workers that analyze each repo and create tailored pull requests for standardized config or security updates.

## Learn More

See [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/) for design patterns, [Cross-Repository Reference](/gh-aw/reference/cross-repository/) for checkout and target-repo configuration, [Safe Outputs Reference](/gh-aw/reference/safe-outputs/) for configuration options, [GitHub Tools](/gh-aw/reference/github-tools/) for API access, [Security Best Practices](/gh-aw/introduction/architecture/) for authentication guidance, and [Adding Existing Workflows](/gh-aw/guides/working-with-workflows/#adding-existing-workflows) for sharing workflows.
