> [!WARNING]
> **Copilot organization billing is unavailable**: The workflow uses `permissions.copilot-requests: write`, but GitHub Copilot rejected the built-in `GITHUB_TOKEN`.

To resolve this:

1. Ask an organization owner to check **Organization settings → Copilot → Policies → Copilot CLI** and enable **Allow use of Copilot CLI billed to the organization**.
2. If organization billing is unavailable, use individual billing instead: set `permissions.copilot-requests: none`, configure a `COPILOT_GITHUB_TOKEN` repository secret with a fine-grained personal access token from a user with an active Copilot subscription, and recompile the workflow.

See [Copilot billing](https://github.github.com/gh-aw/reference/billing/) for complete setup instructions.
