---
name: GitHub Agentic Workflows
description: GitHub Agentic Workflows (`gh-aw`) is a GitHub CLI extension for writing Agentic Workflows in markdown and compiling them to GitHub Actions.
---
## Install
```bash
gh extension install github/gh-aw
```
If failed,
```
set -euo pipefail
INSTALLER=$(mktemp)
trap 'rm -f "$INSTALLER"' EXIT
curl -fsSL https://raw.githubusercontent.com/github/gh-aw/4c53fac4c30c2d9f27ea6fab5e5ce0f15a21e78e/install-gh-aw.sh -o "$INSTALLER"
if command -v sha256sum >/dev/null 2>&1; then
  printf '%s  %s\n' 248ccebcb998c6a506548156e1bf9f02429cbbaec407d5adbdfd316ab0f866a0 "$INSTALLER" | sha256sum -c -
else
  printf '%s  %s\n' 248ccebcb998c6a506548156e1bf9f02429cbbaec407d5adbdfd316ab0f866a0 "$INSTALLER" | shasum -a 256 -c -
fi
bash "$INSTALLER"
```
## Load
Load https://github.com/github/gh-aw/blob/main/.github/skills/agentic-workflows/SKILL.md to learn how to create/update/debug/optimize Agentic Workflows.
