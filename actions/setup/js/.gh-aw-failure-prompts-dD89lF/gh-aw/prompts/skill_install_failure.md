**Skill Installation Failed**: One or more frontmatter skills could not be installed before the agent ran. This typically happens when:

- The skill repository does not exist or is not accessible with the configured token
- The skill reference is invalid or points to a non-existent branch/tag/commit
- The `gh` CLI version is too old (requires a recent version that supports `gh skill install`)
- Network connectivity to the skill host is blocked by the firewall

**Failed skills:**
{skills}

To resolve this, verify that:
1. The skill repository reference is correct (e.g. `owner/repo` or `owner/repo/skill/path@sha`)
2. The token configured for the skill has read access to the skill repository
3. The skill repository and branch/tag/commit exist on GitHub

See: https://github.com/github/gh-aw/blob/main/docs/src/content/docs/reference/skills.mdx
