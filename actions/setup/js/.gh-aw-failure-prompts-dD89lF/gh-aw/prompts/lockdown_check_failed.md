**Lockdown Check Failed**: The workflow could not start because its security requirements were not met.

This can happen when:

- **Lockdown mode (`lockdown: true`) is enabled but no custom GitHub token is configured.** A fine-grained PAT or custom token is required for lockdown mode.

  To fix, configure one of the following repository secrets:
  - `GH_AW_GITHUB_TOKEN` (recommended)
  - `GH_AW_GITHUB_MCP_SERVER_TOKEN` (alternative)
  - A custom `github-token` in the workflow frontmatter

  ```bash
  gh aw secrets set GH_AW_GITHUB_TOKEN --value "YOUR_FINE_GRAINED_PAT"
  ```

  See: https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/auth.mdx

- **The workflow is running on a public repository but was not compiled with strict mode.** Recompile the workflow with `--strict` to meet the security requirements for public repositories.

  ```bash
  gh aw compile --strict
  ```

  See: https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/security.mdx
