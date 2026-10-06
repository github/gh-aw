---
title: Sharing Workflows in the Organization
description: Share, reuse, and govern workflows across repositories and organizations.
---

Sharing workflows across an organization involves several independent layers. Each layer can be adopted independently; teams do not need all of them at once.

The recommended enterprise pattern is to maintain one central `agentic-workflows` repository with versioned workflow templates and shared components. Consuming repositories then use `gh aw add` to install full workflows and `imports:` to pull in common modules.

## Sharing Layers

### 1. Copy and install whole workflows

A repository can pull in a complete workflow from another repository:

```bash
gh aw add acme-org/agentic-workflows/ci-doctor@v1.2.0
```

The `source:` field is automatically added to the installed workflow's frontmatter so the origin and version are tracked. Use `gh aw add-wizard` for interactive installation; use `gh aw add` for scripted or CI use.

See [Adding Existing Workflows](/gh-aw/guides/working-with-workflows/#adding-existing-workflows) for installation commands and options.

### 2. Reusable workflow components

Shared building blocks — tool configurations, MCP server definitions, safety policies, and prompt snippets — can be imported into any workflow:

```yaml
imports:
  - acme-org/shared-workflows/shared/security-setup.md@v2.1.0
  - acme-org/shared-workflows/shared/mcp/tavily.md@v1.0.0
```

Remote imports are cached under `.github/aw/imports/` by commit SHA after the first fetch, enabling reproducible offline compilation. The compiled `.lock.yml` records the exact commit SHA of every remote import, so the lock file and import cache together guarantee reproducibility regardless of upstream branch movement. Cached imports are reused until you explicitly update them.

See [Imports Reference](/gh-aw/reference/imports/) for path formats, merge semantics, and field-specific behavior.

### 3. Parameterized templates

Shared workflows that declare an `import-schema` accept runtime parameters via `uses`/`with`:

```yaml
imports:
  - uses: acme-org/shared-workflows/shared/reviewer.md@v1
    with:
      languages: ["go", "typescript"]
      severity: "high"
```

One shared component can serve many workflows with different configurations.

See [Imports Reference](/gh-aw/reference/imports/#calling-a-parameterized-shared-workflow) for schema declaration and validation details.

### 4. Versioning and update flow

| Ref type | Behavior |
| --- | --- |
| Exact release tag (`@v1.2.0`) | Pins to one immutable release until you change `source:` explicitly. |
| Moving release ref (`@v1`) | Follows newer compatible releases in that major line when you run `gh aw update`. |
| Branch ref (`@develop`) | Tracks the latest commit on a branch for development integration. |
| SHA pin (`@abc123def`) | Gives strict reproducibility and never moves without an explicit change. |

To pull upstream changes into an already-installed workflow:

```bash
gh aw update ci-doctor          # update one workflow
gh aw update                    # update all tracked workflows
```

Updates use a 3-way merge by default to preserve local edits. Use `--no-merge` to replace the local copy with the upstream version without merging. When the recorded `source:` uses a moving major ref such as `@v1`, `gh aw update` stays within that major line unless `--major` is passed.

### 5. Private and internal sharing controls

Not all workflows are safe to share across organizations. Use `private: true` in frontmatter to block installation into other repositories via `gh aw add`, rely on repository visibility to control discoverability, and keep org-internal catalogs in private or internal repositories so only authorized members can install them.

See [Private Workflows](/gh-aw/reference/frontmatter/#private-workflows-private) for configuration details.

### 6. Cross-repository execution model

Separately from sharing definitions, workflows can read other repositories, check out target code, and write safe outputs to target repositories using explicit authentication and allowlists.

```yaml
safe-outputs:
  create-issue:
    target-repo: "acme-org/target-repo"
    allowed-repos: ["acme-org/repo1", "acme-org/repo2"]
```

Cross-repository operations require appropriate GitHub token permissions and explicit `allowed-repos` declarations. See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for authentication, permissions, and safe output configuration.

## Recommended Enterprise Pattern

Keep one central `agentic-workflows` repository with versioned templates and shared components under `workflows/` and `shared/`. Consumers install workflows with `gh aw add acme-org/agentic-workflows/<workflow>@<version>`, import common modules via `imports:`, use tags for stable production and branches for development, and mark internal-only workflows `private: true`. Platform teams keep ownership and update control; consumers get reproducibility through version pins and keep local customizations through 3-way merge.

## Governance Questions

When workflows are shared across an organization, the key decisions are operational: who owns the source workflow and reviews changes, how updates are tested and promoted, which repositories may consume or dispatch shared workflows, how secrets and permissions are standardized, and when a team may fork instead of staying on the shared version.

## Learn More

- [Adding Existing Workflows](/gh-aw/guides/working-with-workflows/#adding-existing-workflows)
- [Imports Reference](/gh-aw/reference/imports/)
- [Cross-Repository Operations](/gh-aw/reference/cross-repository/)
- [Private Workflows](/gh-aw/reference/frontmatter/#private-workflows-private)
- [MultiRepoOps](/gh-aw/patterns/multi-repo-ops/)
