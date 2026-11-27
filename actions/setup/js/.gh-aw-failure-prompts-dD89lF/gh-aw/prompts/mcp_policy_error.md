> [!WARNING]
> **MCP Servers Blocked by Policy**: The Copilot CLI blocked MCP server connections due to an organization or enterprise policy. The agent ran without access to MCP tools (GitHub API, safe outputs, etc.).

This is a **policy configuration issue**, not a transient error — retrying will not help.

<details>
<summary>How to fix this</summary>

An organization or enterprise administrator must enable the **"MCP servers in Copilot"** policy:

1. Go to your **enterprise or organization settings**
2. Navigate to **Policies** → **Copilot**
3. Enable **"MCP servers in Copilot"**

For detailed instructions, see the [official documentation](https://docs.github.com/en/copilot/how-tos/administer-copilot/manage-mcp-usage/configure-mcp-server-access).

> **Note:** On some GitHub Enterprise instances, the **Policies** tab may only be visible at the **enterprise level**, not the organization level.

</details>
