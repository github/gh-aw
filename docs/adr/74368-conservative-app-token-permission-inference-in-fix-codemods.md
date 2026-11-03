# ADR-74368: Conservative GitHub App Token Permission Inference and Recursive Workflow Discovery in `gh aw fix`

**Date**: 2026-11-03
**Status**: Draft
**Deciders**: [TODO: verify] pelikhan, Copilot coding agent

---

### Context

`gh aw fix` applies codemods to workflow Markdown files, but it only scanned the top level of `.github/workflows`. Workflows nested in subdirectories therefore kept deprecated or unsupported frontmatter (for example `tools.web-fetch` and deprecated safe-output keys) after a fix run (issue #74099). Separately, `actions/create-github-app-token` steps in custom `jobs:` often omit `permission-*` inputs. Without those inputs, the minted token gets all permissions granted to the App installation. A codemod can add explicit scopes, but if it infers the wrong scopes it either breaks the workflow (too narrow) or hides an over-privileged token behind a false sense of review (too broad).

### Decision

We will make two changes:

1. `gh aw fix` will discover workflow Markdown files recursively under the workflows directory through a new `getMarkdownWorkflowFilesRecursively` helper. Other commands will keep their existing top-level discovery behavior.
2. We will add a `github-app-token-permissions` codemod that infers `permission-*` inputs only when inference is clearly safe. The token's `steps.<id>.outputs.token` must be bound directly to `GH_TOKEN` or `GITHUB_TOKEN` in later `run:` steps. Those scripts may contain only simple `gh` command lines: no pipes, substitutions, or other shell metacharacters. Each line must be a recognized read-only `gh <group> <action>` command or a read-only `gh api` GET request to a known endpoint.

In every other case, the codemod writes the most restrictive placeholder, `permission-contents: none`, and adds a TODO comment that asks a human to review the permissions. The main reason for this design is least privilege. When the codemod is unsure, it should produce a visibly incomplete configuration rather than grant broad or guessed permissions.

### Alternatives Considered

#### Alternative 1: Infer permissions from any `gh` usage, including write commands

We could reuse the full `inferPermissionsFromShellScripts` logic and emit write scopes too. That would cover more workflows automatically. We rejected it because shell parsing is heuristic: write scopes inferred from complex scripts could silently over-privilege tokens, and partial inference could miss scopes needed at runtime.

#### Alternative 2: Always emit a placeholder and never infer

This would be simpler and would avoid any chance of incorrect inference. We rejected it because it forces a manual review even for common, easy read-only cases such as `gh pr view`. Those cases can be resolved safely and deterministically.

#### Alternative 3: Make workflow discovery recursive for all commands

We could change `getMarkdownWorkflowFiles` globally instead of adding a fix-specific helper. We rejected this to keep the change small. Changing discovery for compile, list, and run could have side effects that this PR does not evaluate.

### Consequences

#### Positive
- Nested workflows now get the same codemods as top-level workflows, which closes the gap reported in #74099.
- When inference is possible, App tokens get explicit minimal read scopes. This reduces default over-privilege.
- When the codemod cannot infer scopes, it says so clearly: the `none` placeholder plus a TODO comment.

#### Negative
- The `permission-contents: none` placeholder may break workflows at runtime until a human corrects the scopes.
- The allowlist approach is strict, so many real scripts (pipes, variables, write commands) fall back to the placeholder. This limits automation.
- Two discovery functions now exist (`getMarkdownWorkflowFiles` and `getMarkdownWorkflowFilesRecursively`), so `fix` and other commands can see different sets of workflows.

#### Neutral
- The new exported helper `workflow.InferGitHubAppTokenPermissionsFromShellScripts` adds a public API in `pkg/workflow` that reuses the existing gh CLI permission tables.
- The codemod is registered in `getLaterCodemods()` and marked `IntroducedIn: 1.5.0`.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
