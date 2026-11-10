---
name: GitHub Agentic Workflows
description: GitHub Agentic Workflows (`gh-aw`) is a GitHub CLI extension for writing Agentic Workflows in markdown and compiling them to GitHub Actions.
---
## Local SDLC
For substantial issue work where the user wants a local, self-contained software
development lifecycle, follow [the Squad issue bootstrap](.github/aw/squad.md).
Include its prompt verbatim in the generated issue for a coding agent; do not install Squad
or create a gh-aw workflow while preparing the issue.

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
