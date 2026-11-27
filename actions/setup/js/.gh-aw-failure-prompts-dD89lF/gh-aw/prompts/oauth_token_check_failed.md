> [!WARNING]
> **OAuth Token Detected**: One or more tokens configured for this workflow are OAuth tokens (`gho_...`), which are not suitable for automation.

OAuth tokens are not allowed because:

- They are typically over-provisioned and grant broad access to user resources
- They may expire when the user logs out or changes their password
- They cannot be scoped to specific repositories or permissions

**How to fix:** Replace the OAuth token(s) with a fine-grained Personal Access Token.

1. Create a new fine-grained PAT at: https://github.com/settings/personal-access-tokens/new
2. Grant it the minimum required permissions for your workflow
3. Update your repository secret(s) with the new token value

```bash
gh aw secrets set GH_AW_GITHUB_TOKEN --value "github_pat_..."
```

Check the [workflow run logs]({run_url}) for details on which token(s) need to be replaced.
