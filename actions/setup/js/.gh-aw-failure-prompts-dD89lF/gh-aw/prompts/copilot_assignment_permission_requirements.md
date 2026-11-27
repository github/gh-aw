### ⚠️ Copilot Assignment Permission Requirements

Copilot assignment failed because the workflow token could not update issue assignees via `POST /repos/{owner}/{repo}/issues/{issue_number}/assignees`.

**Required token options**
- **Fine-grained personal access token** — Read access to **metadata** and read/write access to **actions**, **contents**, **issues**, and **pull requests**
- **Classic personal access token** — **`repo`** scope

**Remediation**
- Set `GH_AW_AGENT_TOKEN` to a PAT with one of the permission sets above.
- Do not use a GitHub App installation token for Copilot assignment; the API rejects it.
- Ensure the token owner can access the repository and assign users to issues.
- Verify Copilot coding agent is enabled for the repository and organization policy allows bot assignments.

**References**
- [gh-aw Copilot Cloud Agent authentication](https://github.github.com/gh-aw/reference/copilot-cloud-agent/#authentication)
- [Official GitHub Copilot cloud agent API documentation](https://docs.github.com/en/copilot/how-tos/use-copilot-agents/cloud-agent/use-cloud-agent-via-the-api#using-the-issues-api)
