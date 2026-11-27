> [!WARNING]
> **Repeated Permission Denied**: The agent was denied permission to run {denied_count} command(s) and stopped retrying.

**Denied Commands:**
{denied_commands_list}

<details>
<summary>How to fix this</summary>

The agent ran in non-interactive mode (`--no-ask-user`) and could not request permission at runtime.

To resolve repeated permission denied errors, update the workflow prompt to avoid these commands or use approved alternatives. Use the following prompt with any coding agent:

```
The agentic workflow {workflow_id} encountered repeated permission denied errors for these commands:
{denied_commands_inline}

Please update the workflow prompt so the agent:
1. Uses built-in tools (GitHub API, file read/write) instead of the denied shell commands
2. Or achieves the same goal through alternative approaches that do not require shell permission
```

</details>
