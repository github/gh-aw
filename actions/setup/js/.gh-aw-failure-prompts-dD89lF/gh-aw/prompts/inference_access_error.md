
**Inference Access Denied**: The Copilot CLI failed because the token does not have access to inference. This can happen when:

- Your organization has restricted Copilot access
- The `COPILOT_GITHUB_TOKEN` does not have a valid Copilot subscription
- Required policies have not been enabled by your administrator

To resolve this, verify that the `COPILOT_GITHUB_TOKEN` secret belongs to an account with an active Copilot subscription and check your [Copilot settings](https://github.com/settings/copilot).

