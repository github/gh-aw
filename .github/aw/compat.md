# Blocked gh-aw versions

The following releases are blocked by `.github/aw/compat.json` and fail during
workflow activation.

| Versions | Reason |
| --- | --- |
| `v0.82.8` through `v0.85.3` | Affected by [GHSA-8h78-hpm7-29gg](https://github.com/github/gh-aw/security/advisories/GHSA-8h78-hpm7-29gg). `v0.85.4` is the first unaffected release. |

## Remediation

Upgrade to [`v0.85.4`](https://github.com/github/gh-aw/releases/tag/v0.85.4) or
later, verify the installed version, then regenerate and review the repository's
compiled workflows:

```bash
gh extension upgrade gh-aw
gh aw version
gh aw upgrade
git diff -- .github/workflows
```

Confirm that `gh aw version` reports `v0.85.4` or later and commit the regenerated
`.lock.yml` files. Blocking the affected compiler versions prevents their
workflows from activating but does not regenerate existing workflow artifacts.
See [Upgrading Workflows](https://github.com/github/gh-aw/blob/main/docs/src/content/docs/guides/working-with-workflows.mdx#upgrading-workflows)
for the supported upgrade process.

## Temporary mitigations

No advisory-supported temporary mitigation could be verified. Upgrade and
regenerate compiled workflows as described above.
