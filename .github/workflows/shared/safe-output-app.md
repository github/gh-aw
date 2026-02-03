---
#safe-outputs:
#  app:
#    app-id: ${{ vars.APP_ID }}
#    private-key: ${{ secrets.APP_PRIVATE_KEY }}
---

<!--
# Shared Safe Output App Configuration

This shared workflow provides repository-level GitHub App configuration for safe outputs.

## Configuration Variables

This shared workflow expects:
- **Repository Variable**: `APP_ID` - The GitHub App ID
- **Repository Secret**: `APP_PRIVATE_KEY` - The GitHub App private key

## Usage

Import this configuration in your workflows to enable GitHub App authentication for safe outputs.

The configuration will be automatically merged into your workflow's safe-outputs section.

## Benefits

- **Centralized Configuration**: Single source of truth for app credentials
- **Easy Updates**: Change credentials in one place
- **Consistent Usage**: All workflows use the same configuration pattern
- **Repository-Scoped**: Uses repository-specific variables and secrets
-->