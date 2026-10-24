---
title: GitHub Repository Checkout
description: Configure how actions/checkout is invoked in the agent job — disable checkout, override settings, check out multiple repositories, fetch additional refs, and mark a primary target repository.
sidebar:
  order: 852
---

The `checkout:` frontmatter field controls how `actions/checkout` is invoked in the agent job. Configure custom checkout settings, check out multiple repositories, or disable checkout entirely.

By default, the agent checks out the repository where the workflow is running with a shallow fetch (`fetch-depth: 1`). If triggered by a `pull_request` event, it also checks out the PR head ref. For `pull_request_target` events, checkout of the PR head branch is **disabled by default** — the head branch may be deleted (merged/closed PRs) or inaccessible (fork PRs), causing the step to hard-fail. For most workflows, this default checkout is sufficient and no `checkout:` configuration is necessary.

Before the agent job, activation separately checks out workflow configuration to load runtime imports and skills. On `pull_request`, `pull_request_review`, and `pull_request_review_comment` events, this sparse checkout uses the PR **base SHA**, not the PR head; on other events it retains the normal activation ref. For reusable `workflow_call` workflows, it uses the resolved callee SHA when there is no active PR event, and skips cross-repository checkout if authentication may fall back to the caller's repository-scoped `GITHUB_TOKEN`. The `checkout:` options below describe the agent job and do not override this activation checkout.

Use `checkout:` when you need to check out additional branches, check out multiple repositories, or to disable checkout entirely for workflows that don't need to access code or can access code dynamically through the GitHub Tools.

## Custom Checkout Settings

You can use `checkout:` to override default checkout settings (e.g., fetch depth, sparse checkout) without needing to define a custom job:

```yaml wrap
checkout:
  fetch-depth: 0                              # Full git history
  github-token: ${{ secrets.MY_TOKEN }}        # Custom authentication
```

Or use GitHub App authentication:

```yaml wrap
checkout:
  fetch-depth: 0
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
```

You can also use `checkout:` to check out additional repositories alongside the main repository:

```yaml wrap
checkout:
  - fetch-depth: 0
  - repository: owner/other-repo
    path: ./libs/other
    ref: main
    github-token: ${{ secrets.CROSS_REPO_PAT }}
```

## Configuration Options

| Field | Type | Description |
|-------|------|-------------|
| `repository` | string | Explicit repository in `owner/repo` format, or a newline-separated list with `multi: true`. Without `multi`, expressions must resolve to exactly one repository and need no allowlist. Does not support wildcards. Defaults to the current repository. |
| `repo` | string | Alias for `repository`; specify only one. Does not support wildcards. |
| `multi` | boolean | Explicitly enables runtime repository-list expansion and its restrictions. Defaults to `false`; must be a literal boolean. |
| `allowed-repos` | string \| string[] | Mandatory allowlist for `multi: true`; does not enable multi mode by itself. Accepts newline-separated patterns, an array, or an expression; supports scoped patterns such as `owner/*` and `*/repo`, but not unrestricted `"*"` or `"*/*"`. Ignored in single-repository mode. |
| `max-repos` | integer | Maximum unique repositories per `multi: true` checkout entry. Requires `allowed-repos` in multi mode; defaults to `10`. Must be a positive literal integer. Ignored in single-repository mode. |
| `allow-private-repos` | boolean | With `multi: true`, allow private/internal targets in a runtime list hosted by a private/internal repository. Requires `allowed-repos` in multi mode; defaults to `false` and must be a literal boolean. Public hosts always reject non-public multi targets, even when `true`. Ignored in single-repository mode. |
| `ref` | string | Branch, tag, or SHA to checkout. Defaults to the triggering ref. |
| `path` | string | Path within `GITHUB_WORKSPACE` to place the checkout. Defaults to workspace root. |
| `github-token` | string | Token for authentication. Use `${{ secrets.MY_TOKEN }}` syntax. |
| `github-app` | object | GitHub App credentials (`client-id` or `app-id` (deprecated), `private-key`, optional `owner`, `repositories`). Mutually exclusive with `github-token`. `app` is a deprecated alias for the field name. Run `gh aw fix` to auto-migrate `app-id` to `client-id`. |
| `safe-outputs-github-app` | object | Optional per-checkout GitHub App credentials used exclusively for safe_outputs git operations on this checkout target (`client-id` or `app-id` (deprecated), `private-key`, optional `owner`, `repositories`). Does not change agent/activation checkout auth. See [Cross-Organization safe_outputs Authentication](#cross-organization-safe_outputs-authentication-safe-outputs-github-app) for cross-org usage. |
| `fetch-depth` | integer | Commits to fetch. `0` = full history, `1` = shallow clone (default). |
| `fetch` | string \| string[] | Additional Git refs to fetch after checkout. See [Fetching Additional Refs](#fetching-additional-refs). |
| `sparse-checkout` | string | Newline-separated patterns for sparse checkout (e.g., `.github/\nsrc/`). |
| `submodules` | string/bool | Submodule handling: `"recursive"`, `"true"`, or `"false"`. |
| `lfs` | boolean | Download Git LFS objects. |
| `current` | boolean | Marks this checkout as the primary working repository. The agent uses this as the default target for all GitHub operations. Only one checkout may set `current: true`; the compiler rejects workflows where multiple checkouts enable it. |
| `force-clean-git-credentials` | boolean | When `true`, the checkout step is generated with `persist-credentials: true` and followed by a dedicated cleanup step that scrubs both repo and submodule git credentials. Use this for submodule-heavy or sparse checkouts where the default `persist-credentials: false` post-step cleanup fails. See [Cleaning Submodule Credentials](#cleaning-submodule-credentials). |

## Runtime Repository Lists

An entry with `multi: true` expands `repository` (or `repo`) at runtime into one checkout per newline-separated `owner/repo` slug. It requires `allowed-repos`. This allows scheduled workers to process a changing repository set supplied through variables, inputs, or earlier step outputs.

Without `multi: true` (including `multi: false`), the runtime requires exactly one `owner/repo` slug. Repository expressions do not require `allowed-repos` or a non-root path prefix. Normal single-checkout authentication, private-repository access, LFS, submodules, `current`, and `wiki` behavior applies; none of the multi-repo restrictions below apply. Merely specifying `allowed-repos` does not enable expansion or constrain a single checkout.

```yaml wrap
checkout:
  repo: ${{ inputs.repo }}
  path: target
  current: true
```

`repository` and `repo` select explicit repository names, not wildcard patterns. Their expressions must resolve to concrete `owner/repo` slugs. Wildcards are supported only in `allowed-repos`, which filters the selected list and never discovers or selects repositories.

`allowed-repos` policies may use literal patterns or standalone references to repository or organization variables, such as `${{ vars.ALLOWED_WORKER_REPOS }}`. Caller-controlled inputs, step or job outputs, computed expressions, and expressions combined with literal text are rejected at compilation. Only the selected repository list may come from caller-controlled data.

```aw wrap
on:
  schedule: daily
checkout:
  - multi: true
    repository: ${{ vars.WORKER_REPOS }}
    path: targets
    allowed-repos: ${{ vars.ALLOWED_WORKER_REPOS }}
    max-repos: 5
    github-token: ${{ secrets.CROSS_REPO_PAT }}
```

For `WORKER_REPOS` containing `org/api` and `org/web` on separate lines, the checkouts are placed at `targets/org/api` and `targets/org/web`. With `multi: true`, `path` must be a literal, non-root prefix and `allowed-repos` is mandatory, even if the selector is literal or resolves to just the workflow repository. The allowlist may contain scoped wildcard patterns such as `org/*` or `*/api`. Unrestricted patterns such as `*` and `*/*` are rejected, including when an expression resolves to them at runtime. Matching and deduplication are case-insensitive; an empty selected list produces no additional checkouts, not all allowlisted repositories.

Every selected repository must pass the allowlist and GitHub API metadata checks before the checkout action is generated. Redirected repository names are checked against the allowlist again. Runtime lists allow only public targets by default. Set `allow-private-repos: true` on an entry to also allow private/internal targets when the hosting repository is private/internal. **Public workflow repositories cannot check out private or internal repositories**, even when this flag is `true` and the supplied token can access them. The flag requires `allowed-repos` and a literal boolean; it does not change ordinary static checkouts. Both checkout jobs enforce the same policy, and merged entries permit non-public targets only if every entry explicitly opts in. Omission or `false` prevents an imported entry from broadening access. Failed or incomplete visibility checks stop the job.

Enabled submodules are rejected at compilation and runtime for all repository lists because repository-controlled submodule URLs can bypass `allowed-repos` using the checkout token. Generated checkouts and follow-up fetches disable submodule recursion without removing repository-controlled `.gitmodules` files. Omit `submodules` or set it to `false`; static checkout submodule support is unchanged.

`max-repos` caps each runtime entry at 10 unique selected slugs by default, after case-insensitive deduplication and before metadata requests or checkout. Oversized lists fail the entire batch; they are not truncated. Set a positive literal integer to change the cap. Entries merged for the same repository expression and path use the smallest effective cap, including the default when omitted. Separate runtime entries have independent caps.

A disallowed repository fails the entire checkout step before any selected repository is cloned; it is never silently skipped. The rejection is included in the workflow's agent failure issue, subject to the existing `safe-outputs.report-failure-as-issue` settings. Invalid runtime selectors or allowlists, unsafe paths, missing credentials, failed visibility checks, and exceeded repository limits also report their preparation failure through this path. Unexpected dependency errors report a generic preparation failure rather than exposing raw dependency diagnostics in the issue. Failures in the agent and safe-output checkout jobs both use this reporting path.

Runtime entries support the normal ref, history, sparse-checkout, fetch, and authentication options. `lfs: true` is rejected at compilation and runtime for all repository lists: repository-controlled LFS endpoints could download content outside `allowed-repos` using the checkout token. Static checkouts continue to support LFS. GitHub App authentication requires explicit `owner` and `repositories` scoping. `current: true` and `wiki: true` are not supported for repository lists. Existing destination directories and symlinked path ancestors are rejected rather than cleaned or followed.

The agent and PR safe-output jobs each build a checkout manifest recording the expanded repositories, their exact workspace paths, and their default branches. Safe-output handlers use that manifest to locate repositories even under deep prefixes. Before agent execution, finalization explicitly cleans and verifies every expanded checkout's git config and submodule configs using the validated paths, regardless of prefix depth. Missing configs, unavailable cleanup helpers, and residual credentials stop the job. PR safe-output jobs use the same expansion and directory layout, retaining their effective PR checkout credentials, including global safe-output GitHub App authentication unless an entry specifies its own credentials; the agent receives credential-free checkouts.

## Fetching Additional Refs

By default, `actions/checkout` performs a shallow clone (`fetch-depth: 1`) of a single ref. For workflows that need to work with other branches — for example, a scheduled workflow that must push changes to open pull-request branches — use the `fetch:` option to retrieve additional refs after the checkout step.

A dedicated git fetch step is emitted after the `actions/checkout` step. Authentication re-uses the checkout token (or falls back to `github.token`) via a transient `http.extraheader` credential — no credentials are persisted to disk, consistent with the enforced `persist-credentials: false` policy.

Each `fetch:` pattern is checked and fetched independently, for both static and runtime-selected repositories. Patterns that match no complete remote ref names are skipped without preventing other patterns from being fetched; suffix-only matches do not count. Authentication, network, and other discovery or fetch errors still fail the step. Additional fetches use the checkout's effective `fetch-depth` (`1` by default; `0` for full history). Expression-valued patterns are passed as data, not interpolated into shell scripts.

For example, `fetch: ["main", "my-prefix/*"]` fetches `main` even when no branches match `my-prefix/*`. A missing literal branch is also skipped; an unmatched pattern does not create a local ref.

| Value | Description |
|-------|-------------|
| `"*"` | All remote branches. |
| `"refs/pulls/open/*"` | All open pull-request head refs (GH-AW shorthand). |
| `"main"` | A specific branch name. |
| `"feature/*"` | A glob pattern matching branch names. |

```yaml wrap
checkout:
  - fetch: ["*"]                 # fetch all branches (default checkout)
    fetch-depth: 0               # fetch full history to ensure we can see all commits and PR details
```

```yaml wrap
checkout:
  - repository: githubnext/gh-aw-side-repo
    github-token: ${{ secrets.GH_AW_SIDE_REPO_PAT }}
    fetch: ["refs/pulls/open/*"]      # fetch all open PR refs after checkout
    fetch-depth: 0               # fetch full history to ensure we can see all commits and PR details
```

```yaml wrap
checkout:
  - repository: org/target-repo
    github-token: ${{ secrets.CROSS_REPO_PAT }}
    fetch: ["main", "feature/*"] # fetch specific branches
    fetch-depth: 0               # fetch full history to ensure we can see all commits and PR details
```

:::note
If a branch you need is not available after checkout and is not covered by a `fetch:` pattern, and you're in a private or internal repo, then the agent cannot access its Git history except inefficiently, file by file, via the GitHub MCP. For private repositories, it will be unable to fetch or explore additional branches. If the branch is required and unavailable, configure the appropriate pattern in `fetch:` (e.g., `fetch: ["*"]` for all branches, or `fetch: ["refs/pulls/open/*"]` for PR branches) and recompile the workflow.
:::

## Git Credentials After Checkout

The generated checkout step uses `persist-credentials: false`, so the git credentials that `actions/checkout` used are removed once checkout completes. The agent then runs without credentials for the checked-out repository, and any git operation that must authenticate to the remote fails. In private repositories this includes:

- `git fetch`, `git pull`, `git clone`, and direct `git push`
- Checking out or switching to a remote branch that was not already fetched
- Deepening a shallow clone (`git fetch --unshallow`)
- On-demand blob fetches in partial (blobless) clones — operations on files absent from the initial checkout

Fetch everything the workflow needs at checkout time using `fetch-depth` and [`fetch:`](#fetching-additional-refs), and write changes through safe-output tools such as [`push-to-pull-request-branch`](/gh-aw/reference/safe-outputs-pull-requests/) rather than a direct `git push`. The agent is instructed not to configure credential helpers or run `git credential fill`, because authentication cannot succeed; credential errors are reported as a limitation instead of worked around.

:::note[Shallow checkout and push-to-pull-request-branch]
`push-to-pull-request-branch` inspects the commit range `origin/<branch>..<branch>` in the agent's workspace to detect merge commits and select the appropriate transport. With the default shallow clone (`fetch-depth: 1`), `origin/<branch>` has no traversable ancestry, so `git rev-list` reports the entire local history as the range. On large monorepos (thousands of commits) this can falsely trigger bundle or rewrite paths. When the range is implausibly large in a shallow checkout, merge-commit detection returns false (with a warning) to avoid incorrect transport selection; if the range later reaches the signed-push linearization step, that step is refused with a clear error. Set `fetch-depth: 0` to ensure the correct range is visible.
:::

## Disabling Checkout (`checkout: false`)

Set `checkout: false` to suppress both the default `actions/checkout` step and the PR-specific "Checkout PR branch" step entirely. Use this for workflows that access repositories through MCP servers or other mechanisms that do not require a local clone:

```yaml wrap
checkout: false
```

This is equivalent to omitting the checkout step from the agent job. Custom dev-mode steps (such as "Checkout actions folder") are unaffected.

## PR Head Checkout and Branch Deletion

PR head checkout fetches the repository's retained `refs/pull/<number>/head`
ref, including for same-repository PRs. A queued review can therefore check out
the PR head even if the PR merges and its source branch is deleted before the
agent starts. The local branch keeps the PR's source branch name; checkout does
not recreate the remote branch or fall back to the base branch.

A failed PR checkout stops the agent job; it never continues on the previous
base checkout. If an originally open PR closes and its remote ref disappears
during checkout, the run records lifecycle invalidation separately from ordinary
checkout errors. Authentication, permission, network, and unknown errors remain
failures. Workflows triggered on an already closed or merged PR use the retained
PR head ref and still require a successful checkout.

For work triggered while a PR was open, the safe-output manager checks live PR
state before each handler, deferred retry, and buffered review submission. If the
PR has closed, remaining effects are recorded as skipped, not applied. Closed-event
workflows and staged previews are preserved. Configured safe-output tokens need
read access to the triggering PR; a failed state lookup is an error, not proof of
closure. PR-branch pushes also refuse to recreate deleted target branches.
These checks cannot make a PR-state lookup and a later GitHub write atomic.

## Disabling the PR Branch Checkout

To keep the normal repository checkout but suppress the compiler-generated "Checkout PR branch" step and its associated base-config snapshot/restore steps, set `pull-request: false` under `checkout`:

```yaml wrap
checkout:
  pull-request: false
```

## Target-Only Checkout (`permissions.contents: none`)

Sidecar (MultiRepoOps) workflows often need to operate on a target repository without checking out the repository that hosts the workflow itself. Setting `permissions.contents: none` suppresses only the default workflow-repository checkout and the "Checkout PR branch" step; any other explicitly configured `checkout:` entries (such as a target repository) are still checked out normally:

```yaml wrap
permissions:
  contents: none
checkout:
  - repository: octo-org/target-repository
    path: target
    github-app:
      client-id: ${{ vars.TARGET_APP_CLIENT_ID }}
      private-key: ${{ secrets.TARGET_APP_PRIVATE_KEY }}
      owner: octo-org
      repositories:
        - target-repository
```

This differs from `checkout: false`, which disables checkout entirely (including any additional configured repositories). With `permissions.contents: none`, only the workflow's own repository is skipped.

## `pull_request_target` Checkout

For `pull_request_target` workflows, the "Checkout PR branch" step is auto-disabled when no `checkout:` key is present in frontmatter. This prevents hard failures when the PR head branch is deleted (merged/closed events) or inaccessible (fork PRs).

To check out a trusted ref such as the base SHA, opt in explicitly with a `checkout:` mapping:

```yaml wrap
on:
  pull_request_target:
    types: [opened, synchronize]
checkout:
  repository: ${{ github.repository }}
  ref: ${{ github.event.pull_request.base.sha }}
```

This produces a configured checkout pointing at the safe base ref; the "Checkout PR branch" step remains disabled for `pull_request_target`. Do **not** use `github.event.pull_request.head.sha` or `refs/pull/.../head` in a privileged `pull_request_target` job — that would check out untrusted code in a context with write permissions.

### Explicit Trusted Checkout Policy

Strict mode rejects `pull_request_target` checkouts unless checkout is explicitly disabled, targets the supported base repository/ref, or matches a literal pair in `on.pull_request_target.allowed-checkouts`. Declare this policy in the main workflow's frontmatter; it is not inherited from imports. The default restrictions remain unchanged when the policy is omitted.

```aw wrap
on:
  pull_request_target:
    types: [closed]
    branches: [main]
    acknowledge-risk: true
    allowed-checkouts:
      - repository: dotnet/AspNetCore.Docs
        ref: main
if: github.event.pull_request.merged == true

checkout:
  - repository: dotnet/AspNetCore.Docs
    ref: main
    path: .
    current: true
```

`on.pull_request_target.allowed-checkouts` defaults to no additional trusted checkouts. Each entry requires a literal `owner/repository` and a non-empty literal branch, tag, or commit SHA. Matching is exact and case-sensitive; repository/ref pairs cannot be combined across entries. Expressions, wildcards, and `refs/pull/...` refs are rejected in the policy. Every configured checkout must independently satisfy the policy or the existing base-checkout rules. Do not configure `checkout.fetch` for these checkouts: follow-up fetches can add refs beyond the trusted checkout, so they do not satisfy the policy. External repositories with omitted or PR-dependent refs remain rejected. Wiki checkouts are not covered by this allowlist.

`on.pull_request_target.acknowledge-risk` defaults to `false`. Setting it to `true` suppresses **only** the strict-mode dangerous-trigger warning, including for `checkout: false` or a supported base checkout. It neither allows other checkouts nor disables unrelated warnings, strict validation, or safe-update checks. An allowlist without acknowledgment still emits the trigger warning.

> [!CAUTION]
> This policy records maintainer trust, not proof that checked-out code is safe. Prefer immutable commit SHAs; literal branches and tags can move. Keep workflow code and helpers on trusted base refs, read source PR changes as data, and never execute PR-controlled code with privileged credentials. Checkout authentication (`github-app` or `github-token`) and safe-output publication must still be configured separately. This example illustrates compilation policy, not runtime authentication or publication validation.

## Marking a Primary Repository (`current: true`)

When a workflow running from a central repository targets a different repository, use `current: true` to tell the agent which repository to treat as its primary working target. The agent uses this as the default for all GitHub operations (creating issues, opening PRs, reading content) unless the prompt instructs otherwise. When omitted, the agent defaults to the repository where the workflow is running.

```yaml wrap
checkout:
  - repository: org/target-repo
    path: ./target
    github-token: ${{ secrets.CROSS_REPO_PAT }}
    current: true                                    # agent's primary target
```

> [!IMPORTANT]
> `current: true` only annotates the agent's system prompt to identify the target repository — it does **not** automatically change the working directory. If the agent needs to run local tools (tests, linters, build scripts) against the checked-out repository, add an explicit `cd` instruction to the prompt:
>
> ```
> Navigate into the folder where the target repository has been checked out into: cd ${{ github.workspace }}/target
> ```
>
> Without this instruction, the agent starts in `$GITHUB_WORKSPACE` (the side repository checkout) and must infer the correct directory on its own.

## Cleaning Submodule Credentials

By default, generated checkout steps set `persist-credentials: false`, which causes `actions/checkout` to remove credentials in its post-step. In repositories with submodules or sparse checkouts, that post-step can fail with missing submodule URL or path errors.

Set `force-clean-git-credentials: true` on a checkout target to opt into an explicit cleanup step instead. The compiler emits the checkout with `persist-credentials: true`, then injects a `Clean git credentials after checkout` step immediately after it. The cleanup removes the credential helper and `http.*.extraheader` entries from both `.git/config` and any `.git/modules/*/config`, including nested submodules.

Cleanup is fail-closed: a failed helper, invalid git configuration, or residual
credential helper, authentication header, or authenticated URL stops the job
before agent execution. A separate read-only verification checks the effective
configuration, including included config files, without logging token values.
The final pre-agent credential cleanup has the same verification requirement.
With no checkout or git config files, cleanup and verification are no-ops.

```yaml wrap
checkout:
  - repository: org/monorepo-with-submodules
    submodules: recursive
    force-clean-git-credentials: true
```

## Cross-Organization safe_outputs Authentication (`safe-outputs-github-app`)

By default, the safe_outputs job uses `GITHUB_TOKEN` to check out repositories — `safe-outputs.github-app` and `safe-outputs.github-token` are **not** used for checkout and only govern PR/push API operations. This design prevents cross-org token confusion when the safe-outputs app is scoped to a target organization that differs from the workflow repository's organization.

For workflows that target a different organization, use `safe-outputs-github-app` on the relevant checkout entry to supply a checkout token for the safe_outputs job:

```yaml wrap
checkout:
  - repository: OrgB/target-repo
    github-app:
      client-id: ${{ vars.APP_CLIENT_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      owner: OrgB
    safe-outputs-github-app:
      client-id: ${{ vars.APP_CLIENT_ID }}
      private-key: ${{ secrets.APP_PRIVATE_KEY }}
      owner: OrgB
    current: true

safe-outputs:
  github-app:
    client-id: ${{ vars.APP_CLIENT_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
    owner: OrgB
  create-pull-request:
    target-repo: OrgB/target-repo
```

In this configuration:
- The agent job checks out `OrgB/target-repo` using the app token scoped to `OrgB`.
- The safe_outputs job checks out `OrgB/target-repo` using the `safe-outputs-github-app` token.
- The safe_outputs job checks out the **workflow repository** using `GITHUB_TOKEN` (the default), not the `OrgB`-scoped app token — avoiding the cross-org authentication failure.
- `safe-outputs.github-app` is used only for PR creation and push operations, not for checkout.

## Checkout Merging

Multiple `checkout:` configurations can target the same path and repository. This is useful for monorepos where different parts of the repository must be merged into the same workspace directory with different settings (e.g., sparse checkout for some paths, full checkout for others).

When multiple `checkout:` entries target the same repository and path, their configurations are merged with the following rules:

- **Fetch depth**: Deepest value wins (`0` = full history always takes precedence)
- **Fetch refs**: Merged (union of all patterns; duplicates are removed)
- **Sparse patterns**: Merged (union of all patterns)
- **LFS**: OR-ed (if any config enables `lfs`, the merged configuration enables it)
- **Submodules**: First non-empty value wins for each `(repository, path)`; once set, later values are ignored
- **Ref/Token/App**: First-seen wins

## Learn More

- [Cross-Repository Operations](/gh-aw/reference/cross-repository/) - Reading and writing across multiple repositories
- [Authentication Reference](/gh-aw/reference/auth/) - PAT and GitHub App setup
- [Multi-Repository Examples](/gh-aw/gallery/multi-repo/) - Complete working examples
- [Checkout Behavior Specification](/gh-aw/specs/checkout-behavior-specification/) - Formal checkout semantics across activation, agent, and safe_outputs jobs
