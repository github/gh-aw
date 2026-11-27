**GitHub App Authentication Failed**: Failed to generate a GitHub App installation access token.

This is typically caused by an incorrect GitHub App configuration. Please verify:
- The **App ID** secret/variable is set correctly
- The **private key** secret contains a valid PEM-encoded RSA private key
- The GitHub App is **installed** on the target repository or organization
- The App has the **required permissions** for your workflow's safe-outputs

For more information, see: https://github.github.com/gh-aw/reference/safe-outputs/#github-app
