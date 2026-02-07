# Repository Setup for Agentic Workflows

This prompt guides you, a coding agent, to configure a repository for **GitHub Agentic Workflows (gh-aw)**.

## Your Task

Configure this repository to use agentic workflows by:

1. Installing the gh-aw CLI extension
2. Running the initialization command
3. Committing and pushing the changes

## Step 1: Install gh-aw CLI Extension

Run the installation script from the main branch of the gh-aw repository:

```bash
curl -sL https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh | bash
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

Run the initialization command with the Copilot engine:

```bash
gh aw init --engine copilot
```

**What this does**:

- Configures `.gitattributes` to mark `.lock.yml` files as generated
- Creates `.github/aw/github-agentic-workflows.md` with comprehensive documentation
- Creates `.github/agents/agentic-workflows.agent.md` as an AI assistant for workflows
- Creates workflow management prompts in `.github/aw/` directory
- Configures VSCode settings in `.vscode/settings.json`
- Creates MCP server configuration in `.vscode/mcp.json`
- Creates `.github/workflows/copilot-setup-steps.yml` with setup instructions

**Note**: The command may prompt for additional configuration or secrets. If secrets are needed, `gh aw init` will provide instructions for setting them up. You don't need to configure secrets as part of this initial setup.

## Step 3: Review Changes

Check what files were created:

```bash
git status
```

You should see new/modified files including:

- `.gitattributes`
- `.github/aw/github-agentic-workflows.md`
- `.github/agents/agentic-workflows.agent.md`
- `.vscode/settings.json`
- `.vscode/mcp.json`
- And several other configuration files

## Step 4: Commit and Push Changes

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
- **Create new workflows**: `gh aw new <workflow-name>` os using the agent
- **Use the AI agent**: Type `/agent` in GitHub Copilot Chat and select `agentic-workflows`
- **Read documentation**: View `.github/aw/github-agentic-workflows.md`

## Reference

- **Installation script**: `https://raw.githubusercontent.com/github/gh-aw/main/install-gh-aw.sh`
- **Documentation**: `https://github.github.com/gh-aw/`
- **Repository**: `https://github.com/github/gh-aw`
- **Detailed setup guide**: See `install.md` in the gh-aw repository
