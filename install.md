# Repository Setup for Agentic Workflows

This prompt guides you, a coding agent, to configure a repository for **GitHub Agentic Workflows (gh-aw)**.

## Your Task

Configure this repository to use agentic workflows by:

1. Installing the gh-aw CLI extension
2. Running the initialization command
3. Committing and pushing the changes

## Step 1: Install gh-aw CLI Extension

Download the installer from an immutable commit and verify its SHA-256 before running it:

```bash
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

**What this does**: Downloads and installs the gh-aw binary to `~/.local/share/gh/extensions/gh-aw/`

**Verify installation**:

```bash
gh aw version
```

You should see version information displayed. If you encounter an error, check that:

- GitHub CLI (`gh`) is installed and authenticated
- The installation script completed without errors
- `~/.local/share/gh/extensions` is in your PATH

## Step 2: Initialize Repository for Agentic Workflows

Run the initialization command:

```bash
gh aw init
```

**What this does**:

- Configures `.gitattributes` to mark `.lock.yml` files as generated
- Creates `.github/skills/agentic-workflows/SKILL.md` as the dispatcher skill for AI workflows
- Configures VSCode settings in `.vscode/settings.json`
- Creates GH-AW MCP server configuration in `.github/mcp.json`
- Creates `.github/workflows/copilot-setup-steps.yml` with setup instructions

**Note**: The command may prompt for additional configuration or secrets. If secrets are needed, `gh aw init` will provide instructions for setting them up. You don't need to configure secrets as part of this initial setup.

## Step 3: Review Changes

Check what files were created:

```bash
git status
```

You should see new/modified files including:

- `.gitattributes`
- `.github/skills/agentic-workflows/SKILL.md`
- `.vscode/settings.json`
- `.github/mcp.json`

Commit the initialization changes:

```bash
git add .
git commit -m "Initialize repository for GitHub Agentic Workflows"
git push
```

If there is branch protection on the default branch, create a pull request instead and report the link to the pull request.

## Troubleshooting

### Installation fails

- **Issue**: `gh aw version` shows "unknown command"
- **Solution**: Verify GitHub CLI is installed with `gh --version`, then re-run the installation script

### Missing authentication

- **Issue**: GitHub API rate limit or authentication errors
- **Solution**: Ensure GitHub CLI is authenticated with `gh auth status`

### Permission errors

- **Issue**: Cannot write to installation directory
- **Solution**: Check that `~/.local/share/gh/extensions` is writable or run with appropriate permissions

## What's Next?

After successful initialization, the user can:

- **Add workflows from repos**: `gh aw add githubnext/agentics`
- **Create new workflows**: `gh aw new <workflow-name>` or use the `agentic-workflows` skill
- **Use the AI skill**: Invoke the `agentic-workflows` skill in GitHub Copilot Chat
- **Read documentation**: Visit `https://github.github.com/gh-aw/`

## Reference

- **Verified installation script**: `https://raw.githubusercontent.com/github/gh-aw/4c53fac4c30c2d9f27ea6fab5e5ce0f15a21e78e/install-gh-aw.sh`
- **Documentation**: `https://github.github.com/gh-aw/`
- **Repository**: `https://github.com/github/gh-aw`
- **Detailed setup guide**: See `install.md` in the gh-aw repository
