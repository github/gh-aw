> [!WARNING]
> **Configured Copilot agent not found**: The Copilot CLI could not load agent {requested_agent} at startup.

This is a **configuration issue** — the workflow's `engine.agent` value does not match any agent the Copilot CLI discovered when it started.

- **Requested agent:** `{requested_agent}`
- **Agents Copilot CLI reported as available:** {available_agents}

This commonly happens when an `engine.agent` value references a plugin agent, but the pinned `plugins:` ref does not materialize loadable agent files. Some plugin sources publish generated agent/skill files only on a separate branch (for example `marketplace`) while their default branch contains only the source manifest.

<details>
<summary>How to fix this</summary>

1. Confirm the agent identifier matches exactly what the plugin exposes, including the `plugin-name:agent-name` prefix if required.
2. If the agent comes from a `plugins:` entry, verify the pinned ref points to the branch or tag that publishes materialized agent files, not a source-only branch.
3. Recompile after changing `plugins:` or `engine.agent`:

```bash
gh aw compile
```

</details>
{plugin_diagnostics}
