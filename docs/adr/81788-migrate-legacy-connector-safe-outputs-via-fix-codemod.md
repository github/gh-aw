# ADR-81788: Migrate legacy Linear, Jira, and ADO connector safe outputs with a `gh aw fix` codemod

**Status:** Draft
**Date:** 2026-11-28
**Deciders:** pelikhan (PR author)
**PR:** [#81788](https://github.com/github/gh-aw/pull/81788)
**Linked issue:** #81709

## Context

Workflows that use legacy safe-output keys (`linear-create-issue`, `linear-update-issue`, `linear-add-comment`, `linear-token`, `jira-create-issue`, `jira-update-issue`, `jira-add-comment`, `jira-add-label`, and the `ado-*` work-item outputs) fail compilation. Issue #81709 reports that the compiler rejects them as unknown properties (`Unknown property: jira-create-issue`, `Unknown properties: linear-create-issue, linear-token`). The E2E workflows therefore fail before test dispatch.

Provider support now lives in declarative connector imports (`.github/workflows/connectors/<provider>.md`), the direction recorded in `docs/adr/74077-declarative-connectors-replace-native-provider-integrations.md`. Moving a workflow to that form requires four changes:

- add the connector import;
- rename `ado-*` outputs to `azure-devops-*`;
- move the `linear-token` secret reference to the Linear import's `writer-secret` input;
- supply the resource grants (teams, projects, site, organization) that the import's `with:` block needs.

Resource grants and credential bindings control write authority, so a mechanical rewrite should not decide them. The forces are: unblock affected workflows quickly, avoid silently widening write access, and preserve existing comments, grants, and Markdown bodies.

## Decision

Add a `legacy-connector-imports` codemod to `gh aw fix`. It is registered in `getLaterCodemods()` in `pkg/cli/fix_codemods.go` and implemented in `pkg/cli/codemod_connector_imports.go`. When run with `--write`, it:

1. Adds short-name connector imports (`connector:linear`, `connector:jira`, and `connector:azure-devops`) for each provider whose legacy outputs are present. It reuses an existing local, pinned remote, or short-name import for that provider and supports both list-form and `imports.aw` imports.
2. Renames supported `ado-*` safe outputs to `azure-devops-*`. It fails if the destination key already exists.
3. Converts a single `linear-token: ${{ secrets.NAME }}` into `writer-secret: NAME` on the Linear import. It rejects literal tokens, compound expressions, and conflicting existing `writer-secret` values.
4. For newly added Azure DevOps imports, leaves `organization`, `project`, and `project-name` empty instead of copying specimen defaults.

The codemod does not install declarations, invent resource grants, or remove unsupported legacy options. Compilation stays fail-closed until the author reviews the grants and credential names. Errors are atomic, and re-running the codemod on its own output is a no-op.

## Alternatives Considered

The PR body does not discuss alternatives. The options below are inferred from the diff.

1. **Accept legacy keys in the compiler and inject connector imports at compile time.** This would leave the source file with legacy keys and add imports the author never wrote or reviewed. The chosen design writes the change into the source, where it appears in the diff.
2. **Keep the compiler rejection and document a manual migration.** This leaves affected E2E workflows blocked until each author edits them by hand. The codemod automates the mechanical steps (import insertion, `ado-*` renames, `linear-token` conversion) and still leaves grants for review.
3. **Fully automatic migration that also fills in grants**, such as copying the ADO specimen organization/project or granting all teams and projects. This was rejected: the codemod leaves ADO organization and project empty, and the docs state that it does not invent grants.

## Consequences

Positive:

- Workflows with legacy keys can be migrated mechanically with `gh aw fix --write`, which unblocks the E2E tests in #81709 once grants are supplied.
- Write authority stays explicit. The compiler test in `pkg/cli/codemod_connector_imports_test.go` asserts that a migrated workflow still fails compilation until grants are added.
- Edits are minimal. Unit tests check that comments, existing grants, and Markdown bodies are preserved and that a second run changes nothing.

Negative:

- Migration is two steps. Authors must still fill in grants (`teams`, `projects`, `site`, ADO `organization`/`project`) and confirm credential names, and the workflow fails to compile until they do.
- Empty ADO placeholders produce valid-looking YAML that fails only at compile time, so authors must notice them.
- The codemod adds maintenance surface. It hard-codes the provider output lists and must track any future connector renames.
- Unsupported legacy options (for example, `ado-upload-workitem-attachment`) are not migrated and stay subject to normal validation.
