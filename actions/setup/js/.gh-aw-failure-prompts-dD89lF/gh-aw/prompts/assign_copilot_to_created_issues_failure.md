
**Copilot Assignment Failed**: The workflow created an issue but could not assign the Copilot coding agent because the workflow token could not update issue assignees.

**Failed assignments:**
{issues}

To resolve this:
1. Set `GH_AW_AGENT_TOKEN` to a fine-grained PAT with read access to `metadata` and read/write access to `actions`, `contents`, `issues`, and `pull requests`, or use a classic PAT with the `repo` scope.
2. Do not use a GitHub App installation token for Copilot assignment; the API rejects it.
3. Ensure the token owner can access the repository and assign users to issues.
4. Verify Copilot coding agent is enabled for this repository and organization policy allows bot assignments.

```bash
gh aw secrets set GH_AW_AGENT_TOKEN --value "YOUR_AGENT_PAT"
```

See:
- https://github.github.com/gh-aw/reference/copilot-cloud-agent/#authentication
- https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/use-cloud-agent-via-the-api#using-the-issues-api
