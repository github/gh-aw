---
title: GitHub Agentic Workflows Gallery
description: Find GitHub Agentic Workflows by task, including issue triage, pull-request review, CI investigation, documentation, dependency analysis, reporting, maintenance, and security review.
---

The GitHub Agentic Workflows gallery shows how Markdown workflows can run AI agents through GitHub Actions for repository tasks that require reasoning, interpretation, investigation, or generation. Use this catalog to choose a starting point; each entry explains when the pattern is useful and links to maintained guidance or workflow source.

## Gallery by Task

| Task | When to use it |
| --- | --- |
| [Issue Triage](/gh-aw/gallery/ai-issue-triage/) | Automatically classify new issues, identify duplicates, apply bounded labels, and ask for missing information. |
| [Pull Request Review](/gh-aw/gallery/automated-pr-review/) | Automatically inspect diffs for concrete defects and post review feedback through controlled safe outputs. |
| [Documentation Maintenance](/gh-aw/gallery/docs-automation/) | Automatically detect drift between code and documentation and propose reviewable updates. |
| [CI Failure Investigation](/gh-aw/gallery/ci-failure-investigation/) | Automatically analyze failed GitHub Actions runs, correlate logs, and open diagnostic issues with likely causes. |
| [Code Improvement](/gh-aw/gallery/code-improvement/) | Automatically find unnecessary complexity or duplicated logic and propose focused changes for human review. |
| [Dependency Analysis](/gh-aw/gallery/dependency-analysis/) | Automatically review direct Go-module usage and upstream changes, then create actionable follow-up issues. |
| [Metrics and Analytics](/gh-aw/gallery/metrics-analytics/) | Automatically collect workflow activity and store structured snapshots for health and performance analysis. |
| [Repository Reporting](/gh-aw/gallery/ai-release-notes/) | Automatically summarize repository or release activity on an event or schedule. |
| [Repository Maintenance](/gh-aw/gallery/maintaining-repos/) | Automated repository assistance by reviewing a backlog, performing bounded maintenance tasks, and proposing controlled changes on a schedule. |
| [Security Review](/gh-aw/gallery/security-review/) | Automatically combine repository evidence with AI interpretation to report suspicious changes through code scanning. |
| [Triage from Side Repo](/gh-aw/gallery/multi-repo/triage-from-side-repo/) | Automatically triage a main repository from an isolated side repository through a slash-command bridge. |
| [Code Quality Monitoring](/gh-aw/gallery/multi-repo/code-quality-monitoring/) | Automatically analyze code quality across repositories and create focused, actionable issues. |
| [Feature Synchronization](/gh-aw/gallery/multi-repo/feature-sync/) | Automatically synchronize code and configuration across repositories through reviewable pull requests. |
| [Cross-Repository Issue Tracking](/gh-aw/gallery/multi-repo/issue-tracking/) | Automatically aggregate and synchronize issue status in a central repository. |
| [Dependabot Rollout](/gh-aw/gallery/multi-repo/dependabot-rollout/) | Automatically roll out tailored Dependabot configuration across multiple repositories. |

## Gallery Workflows and AI Engines

Most gallery workflows specify the default Copilot engine or omit `engine:` entirely, so the published set is not evenly distributed across engines. Workflows are engine-portable: to run one on Claude, Codex, Gemini, or Pi, change `engine:` in the workflow frontmatter and configure that engine's authentication secret. Engine-specific options such as `engine.agent` or `engine.harness` are not portable — see the [engine feature comparison](/gh-aw/reference/engines/#engine-feature-comparison) before switching.

## Use a Gallery Workflow Safely

Before enabling a workflow, review its trigger, AI engine authentication, tools, network access, permissions, and safe outputs. Compile the Markdown source with `gh aw compile`, inspect both the `.md` and generated `.lock.yml` files, and begin with the narrowest permissions and outputs that satisfy the task.

Follow the [quickstart](/gh-aw/setup/quick-start/) to install `gh-aw`, read [Create a New Workflow](/gh-aw/setup/creating-workflows/) to adapt an example, compare [AI engines](/gh-aw/reference/engines/), and review the [security architecture](/gh-aw/introduction/architecture/) and [FAQ](/gh-aw/reference/faq/) before deployment.
