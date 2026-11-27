> [!WARNING]
> **Credential Authentication Failed**: The firewall audit log detected authentication rejections (HTTP 401/403) from AI provider APIs. The following provider credentials appear to be missing, expired, or invalid:

{providers}

To resolve this:

1. Verify that the required API keys are configured as repository secrets
2. Confirm the secrets have not expired and are still valid
3. Check that the secret names in your workflow frontmatter match the configured repository secrets

For details on configuring engine credentials, see: [Engine Reference](https://github.github.com/gh-aw/reference/engines/)
