Failed to assign {agent_name}: Copilot assignment permission requirements not met

Copilot assignment needs a Personal Access Token (PAT) that can update issue assignees:
  Fine-grained PAT:
    - Read access to metadata
    - Read and write access to actions, contents, issues, and pull requests
  Classic PAT:
    - repo scope

Remediation:
  1. Set GH_AW_AGENT_TOKEN to a PAT with the permissions above
  2. GitHub App installation tokens are not supported for Copilot assignment
  3. Ensure the token owner can access the repository and assign users to issues
  4. Verify Copilot coding agent is enabled and org policy allows bot assignments
