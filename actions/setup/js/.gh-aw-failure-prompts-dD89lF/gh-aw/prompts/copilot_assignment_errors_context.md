{warning_line}
**Assignment Errors:**
{assignment_errors}

To resolve this, verify the token and Copilot assignment configuration:
- Set `GH_AW_AGENT_TOKEN` to a fine-grained PAT with **metadata: read** and **actions**, **contents**, **issues**, and **pull requests: write**, or use a classic PAT with `repo`
- Do not use a GitHub App installation token for Copilot assignment; the API rejects it
- Ensure the token owner can access the repository and assign users to issues
- Verify Copilot coding agent is enabled for this repository and organization policy allows bot assignments
- Docs: https://github.github.com/gh-aw/reference/copilot-cloud-agent/#authentication
- API reference: https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/use-cloud-agent-via-the-api#using-the-issues-api
