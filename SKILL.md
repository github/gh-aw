---
name: GitHub Agentic Workflows
description: GitHub Agentic Workflows (`gh-aw`) is a GitHub CLI extension for writing Agentic Workflows in markdown and compiling them to GitHub Actions.
---
## Debug an existing workflow
Before installation, follow [the debugging bootstrap](.github/aw/debug-bootstrap.md)
at this file's guidance repository/ref. It selects diagnosis, health, compilation,
or existing-run analysis without requiring a new workflow or automatic upgrade.
Use its available-tool path when no shell is available; preserve the supplied run URL.

## Install
```bash
gh extension install github/gh-aw
```
If failed,
```
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash
```
## Load
Load https://github.com/github/gh-aw/blob/main/.github/skills/agentic-workflows/SKILL.md to learn how to create/update/debug/optimize Agentic Workflows.
