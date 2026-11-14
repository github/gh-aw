---
title: GitHub Repository Checkout
description: Configure how actions/checkout is invoked in the agent job — disable checkout, override settings, check out multiple repositories, fetch additional refs, and mark a primary target repository.
sidebar:
  order: 852
---

The `checkout:` frontmatter field controls how `actions/checkout` runs in the agent job. Use it to change checkout settings, check out multiple repositories, or disable checkout entirely.

By default, the agent checks out the workflow's repository with a shallow fetch (`fetch-depth: 1`). On `pull_request` events it also checks out the PR's selected commit (see [PR Revision Binding](#pr-revision-binding)). On `pull_request_target` events, automatic PR checkout is **disabled by default**, and `pull-request-floating-head` does not enable it. Most workflows need no `checkout:` configuration.

Activation separately checks out workflow configuration to load runtime imports and skills. On `pull_request`, `pull_request_review`, and `pull_request_review_comment` events, this sparse checkout uses the PR **base SHA**. On other events it uses the normal activation ref. `checkout:` options do not override this activation checkout. For reusable `workflow_call` workflows, it uses the resolved callee SHA when no PR event is active, and skips cross-repository checkout if authentication may fall back to the caller's `GITHUB_TOKEN`.

Use `checkout:` to check out additional branches or repositories, or set `checkout: false` for workflows that access code only through GitHub tools.

## Checkout and Host Step Order

The agent job runs these steps in order:

1. Repository checkout, using `checkout.ref` when configured.
2. Imported `steps:` (including shared toolchain setup).
3. The workflow's own `steps:`.
4. Automatic "Checkout PR branch", when enabled for the event.
5. `pre-agent-steps:`.
6. The sandboxed engine.

> [!WARNING]
> Imported, workflow-owned, and pre-agent steps run on the host, outside the sandbox. A `checkout.ref` set to an untrusted branch can make them run branch-controlled code with the job's credentials. The compiler warns when a ref expression uses `needs.*`, `github.event.*`, `inputs.*`, `github.head_ref`, `github.base_ref`, `github.ref`, or `github.ref_name` while `steps:` or `pre-agent-steps:` are present. It exempts the default-branch expression and the supported PR base SHA/ref expressions for the current repository. A warning does not establish that a ref is trusted.

Keep the host workspace on a trusted ref. To inspect an untrusted ref, fetch it only as data. Do not check out an untrusted branch on the host or run its scripts: Git hooks and branch-controlled agent configuration can execute. For PR-head work, use automatic PR checkout, which restores the trusted agent configuration. Disabling that checkout also disables the configuration restore.

### Separate Tooling and Data Checkouts

When trusted host tooling reads a separate checkout only as data, set `data-only: true` on that checkout to suppress its checkout-ref warning:

```aw wrap
checkout:
  - path: tooling
    ref: ${{ github.event.repository.default_branch }}
  - repository: example-org/content
    ref: ${{ needs.prepare.outputs.branch }}
    path: target
    data-only: true
steps:
  - working-directory: tooling
    run: ./scripts/validate-content ../target
```

`data-only` is an author acknowledgement. It does not enforce isolation, prove the ref is trusted, or change checkout behavior, credentials, permissions, or other diagnostics. The compiler does not infer data-only use from `working-directory`, because commands can run code outside that directory. Host tooling must not execute scripts, hooks, plugins, or configuration from the data checkout, including through validators or package managers. Use non-overlapping checkout paths.

The option defaults to `false` and accepts only literal booleans. If imports or repeated entries target the same checkout, every merged request must set `data-only: true`.

## PR Revision Binding

By default, the "Checkout PR branch" step fetches a specific commit SHA from the repository's `origin`. It then verifies the checked-out `HEAD` before reporting success or recording a patch baseline. It never resolves a moving `refs/pull/N/head` during fetch.

| Trigger | Selected revision by default | Head movement |
|---------|------------------------------|---------------|
| `pull_request`, except `labeled` and `unlabeled` | Event-recorded PR head SHA | Retains the event revision, even if the branch moves or is deleted. |
| `pull_request` label events | Event-recorded PR head SHA | Must match the live head before and after fetch. |
| `pull_request_review` / `pull_request_review_comment` | Review or review-comment `commit_id` | Must match the live head before and after fetch. |
| Revision-bound `workflow_dispatch` | Supplied `aw_context.pr_head_sha` | Must match the live head before and after fetch. |
| `issue_comment`, routed comment commands, or manual PR dispatch without revision evidence | Current API head SHA | Pins the selected head and rejects a change during fetch. Does not establish revision approval. |

These rules apply only to compiler-generated automatic PR checkout. The [floating-head opt-in](#allow-floating-head) selects the current head instead.

Centralized command and label dispatch passes event-recorded revision evidence in `aw_context.pr_head_sha`. A revision-bound dispatch must supply a full 40-character SHA matching the current PR head. Missing evidence, malformed PR context, API failures, and detected head changes stop checkout. The job never continues on the previous workspace.

**Issue comments have no historical head SHA.** Comment commands therefore authorize *current-head execution*, not approval of a specific revision. They require valid comment and PR-update timestamps, warn if the PR changed after the comment, and pin and recheck the resolved head. Timestamps cannot prove which revision was reviewed, because unrelated activity also updates the PR. Workflows that require revision approval should use a revision-bearing PR or review event, or set `checkout: { pull-request: false }`. Do not rely on a comment command alone to approve privileged execution of PR code.

These checks keep the existing actor-permission floor and same-repository bot restrictions. A matching SHA proves commit identity, not that the PR code is safe to run with credentials. Fetching still needs credentials available to the checkout step. The helper does not persist credentials, widen sparse paths, or unshallow the repository.

After successful checkout, the unified session records a `workflow.pr_checkout_revision` event with `prNumber` and the full, lowercase `selectedSha`.

> [!NOTE]
> Immutable checkout requires `origin` to serve the selected commit by SHA, including unadvertised, fork, and post-deletion commits. Support depends on the Git server's upload-pack SHA policy. If the server cannot serve the commit, checkout fails with diagnostics rather than falling back to a moving PR ref.

### Allow Floating Head

To select the current PR head at execution time instead of the triggering event's revision, opt in:

```aw wrap
checkout:
  pull-request-floating-head: true
```

The value must be a literal boolean; strings and expressions are rejected. With `true`, checkout resolves the current head through the PR API and uses its commit count for fetch depth. It still fetches that specific SHA, rechecks the head after fetch, and verifies `HEAD`. A head change during fetch fails. This option does not restore mutable-ref fetching.

> [!WARNING]
> Floating-head checkout can run commits pushed after a label, review, or command. It authorizes current-head execution, not approval of those commits. Do not enable it when the triggering event is meant to approve a specific revision.

Actor permissions, fork and bot restrictions, dispatch validation, and safe-output lifecycle checks still apply. The setting applies workflow-wide when any `checkout:` entry enables it. It has no effect when `checkout: false`, `pull-request: false`, or the compiler's PR-target policy suppresses the PR checkout. Recompile after changing it.

## Custom Checkout Settings

Override default settings such as fetch depth or authentication:

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

Check out additional repositories alongside the main one:

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
| `pull-request` | boolean | Set `false` to disable automatic PR checkout and its base-config snapshot/restore steps. The normal repository checkout stays enabled. |
| `pull-request-floating-head` | boolean | Select the current API head instead of event/review revision evidence. Defaults to `false`; literal boolean only. Applies workflow-wide. See [Allow Floating Head](#allow-floating-head). |
| `repository` | string | Explicit repository in `owner/repo` format, or a newline-separated list with `multi: true`. Without `multi`, expressions must resolve to exactly one repository and need no allowlist. No wildcards. Defaults to the current repository. |
| `repo` | string | Alias for `repository`; specify only one. |
| `multi` | boolean | Enables runtime repository-list expansion and its restrictions. Defaults to `false`; literal boolean only. |
| `allowed-repos` | string \| string[] | Mandatory allowlist for `multi: true`. Does not enable multi mode by itself. Supports scoped patterns such as `owner/*` and `*/repo`, but not `"*"` or `"*/*"`. Ignored in single-repository mode. |
| `max-repos` | integer | Maximum unique repositories per `multi: true` entry. Requires `allowed-repos` in multi mode; defaults to `10`. Must be a positive literal integer. |
| `allow-private-repos` | boolean | With `multi: true`, allow private/internal targets when the list is hosted by a private/internal repository. Defaults to `false`. Public hosts always reject non-public targets. |
| `ref` | string | Branch, tag, or SHA to check out. Defaults to the triggering ref. |
| `data-only` | boolean | Acknowledge data-only consumption by host steps and suppress only a separate checkout's ref warning. Defaults to `false`. Does not enforce isolation. All merged requests for the target must opt in. See [Separate Tooling and Data Checkouts](#separate-tooling-and-data-checkouts). |
| `path` | string | Path within `GITHUB_WORKSPACE` for the checkout. Defaults to the workspace root. |
| `github-token` | string | Token for authentication. Use `${{ secrets.MY_TOKEN }}` syntax. |
| `github-app` | object | GitHub App credentials (`client-id` or `app-id` (deprecated), `private-key`, optional `owner`, `repositories`). Mutually exclusive with `github-token`. `app` is a deprecated alias. Run `gh aw fix` to migrate `app-id` to `client-id`. |
| `safe-outputs-github-app` | object | App credentials used only for safe-outputs git operations on this checkout target. Does not change agent or activation checkout auth. See [Cross-Organization safe_outputs Authentication](#cross-organization-safe_outputs-authentication-safe-outputs-github-app). |
| `fetch-depth` | integer | Commits to fetch. `0` = full history, `1` = shallow clone (default). |
| `fetch` | string \| string[] | Additional Git refs to fetch after checkout. See [Fetching Additional Refs](#fetching-additional-refs). |
| `sparse-checkout` | string | Newline-separated patterns for sparse checkout (e.g., `.github/\nsrc/`). |
| `submodules` | string/bool | `"recursive"`, `"true"`, or `"false"`. |
| `lfs` | boolean | Download Git LFS objects. |
| `current` | boolean | Marks this checkout as the agent's primary target for GitHub operations. Only one checkout may set `current: true`. |
| `force-clean-git-credentials` | boolean | When `true`, the checkout uses `persist-credentials: true` and a dedicated cleanup step scrubs repo and submodule git credentials. See [Cleaning Submodule Credentials](#cleaning-submodule-credentials). |

## Runtime Repository Lists

An entry with `multi: true` expands `repository` (or `repo`) at runtime into one checkout per newline-separated `owner/repo` slug. It requires `allowed-repos`, so scheduled workers can process a changing repository set supplied through variables, inputs, or earlier step outputs.

Without `multi: true`, the runtime requires exactly one slug. Repository expressions need no `allowed-repos` or non-root path, and normal single-checkout behavior applies. Specifying `allowed-repos` alone does not enable expansion.

```yaml wrap
checkout:
  repo: ${{ inputs.repo }}
  path: target
  current: true
```

`repository` and `repo` take explicit names, not wildcards. Wildcards apply only in `allowed-repos`, which filters the selected list and never discovers repositories.

`allowed-repos` may use literal patterns or a standalone reference to a repository or organization variable, such as `${{ vars.ALLOWED_WORKER_REPOS }}`. Caller-controlled inputs, step or job outputs, and computed or combined expressions are rejected at compile time. Only the selected repository list may come from caller-controlled data.

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

With `WORKER_REPOS` containing `org/api` and `org/web` on separate lines, the checkouts land at `targets/org/api` and `targets/org/web`. With `multi: true`, `path` must be a literal, non-root prefix. Matching and deduplication are case-insensitive. An empty selection produces no checkouts, not all allowlisted repositories.

Every selected repository must pass the allowlist and GitHub API metadata checks before checkout. Redirected names are checked again. Runtime lists allow only public targets by default. Set `allow-private-repos: true` to also allow private or internal targets, but only when the hosting repository is private or internal. **Public workflow repositories cannot check out private or internal repositories**, even with this flag. Merged entries permit non-public targets only if every entry opts in. Failed or incomplete visibility checks stop the job.

Enabled submodules and LFS are rejected at compile and runtime for repository lists. Repository-controlled `.gitmodules` URLs or LFS endpoints could bypass `allowed-repos` with the checkout token. Static checkouts keep both features. `current: true` and `wiki: true` are not supported for repository lists. Existing destination directories and symlinked path ancestors are rejected rather than cleaned or followed.

`max-repos` caps each entry at 10 unique slugs by default, applied after deduplication and before metadata requests. Oversized lists fail the whole batch; they are not truncated. Merged entries for the same expression and path use the smallest effective cap.

A disallowed repository fails the entire checkout step before any clone. It is never silently skipped. The failure appears in the agent failure issue, subject to `safe-outputs.report-failure-as-issue`. Invalid selectors or allowlists, unsafe paths, missing credentials, failed visibility checks, and exceeded limits report through the same path. GitHub App authentication requires explicit `owner` and `repositories` scoping.

The agent and PR safe-output jobs each build a checkout manifest with the expanded repositories, their workspace paths, and default branches. Before agent execution, finalization verifies and cleans every expanded checkout's git and submodule config. Missing configs, unavailable cleanup helpers, and residual credentials stop the job. PR safe-output jobs keep their effective PR checkout credentials; the agent receives credential-free checkouts.

## Fetching Additional Refs

By default, `actions/checkout` performs a shallow clone of a single ref. Use `fetch:` to retrieve other refs after checkout, for example when a scheduled workflow must push to open pull-request branches.

A dedicated git fetch step runs after `actions/checkout`. It authenticates with the checkout token (or `github.token`) through a transient `http.extraheader`. No credentials are written to disk.

Each pattern is fetched independently, for both static and runtime-selected repositories. Patterns that match no complete remote ref name are skipped without blocking others, so `fetch: ["main", "my-prefix/*"]` still fetches `main` when nothing matches `my-prefix/*`. Authentication, network, and other fetch errors still fail the step. Fetches use the checkout's `fetch-depth`. Expression-valued patterns are passed as data, not interpolated into shell scripts.

| Value | Description |
|-------|-------------|
| `"*"` | All remote branches. |
| `"refs/pulls/open/*"` | All open pull-request head refs (GH-AW shorthand). |
| `"main"` | A specific branch name. |
| `"feature/*"` | A glob pattern matching branch names. |

```yaml wrap
checkout:
  - fetch: ["*"]                 # fetch all branches
    fetch-depth: 0               # full history, so all commits and PR details are visible
```

```yaml wrap
checkout:
  - repository: githubnext/gh-aw-side-repo
    github-token: ${{ secrets.GH_AW_SIDE_REPO_PAT }}
    fetch: ["refs/pulls/open/*"] # fetch all open PR refs
    fetch-depth: 0
```

:::note
If a branch you need is not covered by a `fetch:` pattern, the agent cannot fetch it. In a private or internal repo, it can only reach it inefficiently, file by file, through the GitHub MCP server. Add a matching `fetch:` pattern (for example `fetch: ["*"]` or `fetch: ["refs/pulls/open/*"]`) and recompile.
:::

## Git Credentials After Checkout

The generated checkout uses `persist-credentials: false`, so `actions/checkout` removes its git credentials once checkout completes. The agent then runs without credentials for the checked-out repository. In private repositories, these operations fail:

- `git fetch`, `git pull`, `git clone`, and direct `git push`
- Checking out a remote branch that was not already fetched
- Deepening a shallow clone (`git fetch --unshallow`)
- On-demand blob fetches in partial (blobless) clones

Fetch what the workflow needs at checkout time, using `fetch-depth` and [`fetch:`](#fetching-additional-refs). Write changes through safe-output tools such as [`push-to-pull-request-branch`](/gh-aw/reference/safe-outputs-pull-requests/), not a direct `git push`. The agent does not configure credential helpers or run `git credential fill`. Credential errors are reported as a limitation.

For a central workflow repository that edits an external repository, combine `repository`, `path`, `current: true`, and checkout-scoped `github-app` authentication. The workflow repository can stay checked out for tooling. Use `permissions.contents: none` only to suppress its default checkout. Checkout credentials and safe-output publication credentials are configured separately.

Credential setup verifies the expected checkout identity through `GH_AW_GIT_REPOSITORY`. It checks effective fetch and push URLs before and after authentication, rejects plaintext HTTP and empty identities, and fails on a mismatch rather than retargeting the checkout. If post-authentication verification fails, it restores the original remote. Workspaces without a checkout or `origin` remote skip setup. For path expressions, the last checkout targeting the workspace root determines the identity.

Set `GH_AW_GIT_CREDENTIALS_DEBUG: "true"` on a credential-configuration step (or set `RUNNER_DEBUG=1`) to emit privacy-preserving events to stderr under `workflow:git_credentials`. Events omit repository names, paths, remote URLs, and tokens. The helper disables shell and Git tracing before handling credentials, so do not trace it manually.

:::note[Shallow checkout and push-to-pull-request-branch]
`push-to-pull-request-branch` compares `origin/<branch>..<branch>` to detect merge commits. With the default shallow clone, `origin/<branch>` has no traversable ancestry, so the range covers the entire local history. In large repositories this can select the wrong transport. When the range is implausibly large, merge-commit detection returns false with a warning. If the range reaches the signed-push linearization step, that step refuses with an error. Set `fetch-depth: 0` for the correct range.
:::

## Disabling Checkout (`checkout: false`)

Set `checkout: false` to suppress both the default checkout and the "Checkout PR branch" step. Use it for workflows that reach repositories through MCP servers or other mechanisms that need no local clone:

```yaml wrap
checkout: false
```

Custom dev-mode steps, such as "Checkout actions folder", are unaffected.

## PR Head Checkout and Branch Deletion

PR head checkout fetches the commit selected by the [revision-binding rules](#pr-revision-binding), including for same-repository PRs. Deleting the source branch does not change the selected revision, but the commit must still be available from `origin`. A queued review rejects a detected head change rather than checking out a later revision. The local branch keeps the PR's source branch name. Checkout never recreates the remote branch or falls back to the base branch.

A failed PR checkout stops the agent job. It never continues on the base checkout. If an originally open PR closes and the fetch fails with a missing-ref or `upload-pack: not our ref <sha>` error, a live closure check records lifecycle invalidation separately from ordinary errors. Authentication, permission, network, and unknown errors remain failures. Workflows triggered on an already closed or merged PR still require a successful checkout.

For work triggered while a PR was open, the safe-output manager checks live PR state before each handler, deferred retry, and buffered review submission. If the PR has closed, remaining effects are recorded as skipped. Closed-event workflows and staged previews are preserved. A failed state lookup is an error, not proof of closure. PR-branch pushes also refuse to recreate deleted target branches. These checks cannot make a PR-state lookup and a later GitHub write atomic.

For body-only review loops, a bot listed in `on.bots` can trigger PR checkout through `pull_request: edited` without pushing a commit. The sender must be a Bot whose login matches the triggering actor, and the runtime, PR head, and PR base repository IDs must match. Unlisted bots and fork PRs still need write-or-higher collaborator permission. Filter body edits with an `if:` condition on `github.event.changes.body` to avoid re-reviewing unrelated edits.

## Disabling the PR Branch Checkout

To keep the normal repository checkout but suppress the "Checkout PR branch" step and its base-config snapshot and restore steps, set `pull-request: false`:

```yaml wrap
checkout:
  pull-request: false
```

## Target-Only Checkout (`permissions.contents: none`)

Sidecar (MultiRepoOps) workflows often operate on a target repository without checking out the workflow's own repository. Setting `permissions.contents: none` suppresses only the default workflow-repository checkout and the "Checkout PR branch" step. Other `checkout:` entries, such as a target repository, are still checked out:

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

This differs from `checkout: false`, which disables all checkout, including additional repositories.

### Safe-Output Patches and Object-Pack Recovery

Safe-output patch and bundle generation is local-only. It never fetches refs or objects from the agent-writable checkout, and it disables all Git transports, hooks, and signing during generation. Prepare required refs with `checkout.fetch` and enough history with `checkout.fetch-depth` before agent execution. Partial clones must also have the required blobs locally. Incremental updates need the pre-agent PR-head baseline recorded by the PR checkout step, or an existing `origin/<branch>` tracking ref. Missing baselines are reported rather than replaced with a broader diff.

When local generation cannot resolve required refs or objects, non-staged PR outputs can return `result: "queued"` with `recovery: "object-pack"`. This is not publication success. The agent packages available local objects without fetching. The trusted safe-outputs job then fetches the authorized baseline into a new isolated bare repository, validates the objects, and regenerates the patch and bundle under the usual file, size, and publication policies. Agent repository configuration, remotes, hooks, and alternates are never copied.

Recovery selects only objects reachable from the pinned tip, excluding anything reachable from local baseline hints. Hints reduce transfer size but never authorize a publisher baseline or a remote request. The default limits (100,000 objects and 64 MiB uncompressed) apply to the selected objects, not the whole repository. Recovery rejects redacted or mismatched previews. Incremental recovery requires the triggering PR's exact pre-agent head SHA; an uploaded baseline or newer remote head cannot replace it.

Recovery needs checkout provisioning when the event has no trusted baseline, when outputs are staged, when the selected change is large, when recovery budgets are exhausted, or when agent-created content is missing. Fallback issues keep the original uploads immutable. Their recovery commands require a clean, complete clone, and object-pack envelopes cannot be applied directly with `git fetch` or `git am`.

#### Repository-configured recovery limits

Repository variables (`Settings > Secrets and variables > Actions > Variables`) set the recovery budgets without changing frontmatter. The compiler binds them into the agent runtime, the nested safe-outputs container, upload validation, and the trusted publisher. Uploaded metadata and agent-written environment files cannot override them. Recompile existing workflows to pick up the matching setup runtime.

| Repository variable | Default | Controls |
|---|---:|---|
| `GH_AW_OBJECT_PACK_MAX_BYTES` | `67108864` | Uncompressed object budget, hydration, and derived pack, preview, and buffer bounds |
| `GH_AW_OBJECT_PACK_MAX_OBJECTS` | `100000` | Selected and parsed objects, and derived tree and scan budgets |
| `GH_AW_OBJECT_PACK_TIMEOUT_MS` | `60000` | Each command timeout; the shared network deadline is three times this value |
| `GH_AW_OBJECT_PACK_FETCH_DEPTHS` | `1,16,64,256,1024,4096,16384,65536` | Strictly increasing list of history search depths |

Budgets scale from the defaults. Derivation formulas are in the [safe-outputs specification](/gh-aw/specs/safe-outputs-specification/). For example, `gh variable set GH_AW_OBJECT_PACK_MAX_BYTES --body 134217728` raises the byte budget to 128 MiB. Larger budgets increase runner memory, CPU, and network use. No setting relaxes repository authorization, preview validation, or the rule that all fetching finishes before agent objects are imported.

Empty or unset variables keep the defaults. Numeric settings accept positive decimal integers only; `0` does not disable a limit. Invalid settings fail closed rather than being ignored or clamped.

GitHub Actions debug logging includes `[object_pack_transport]` stages and object and byte counts. These traces omit repository identities, refs, commit IDs, paths, credentials, and object contents.

## `pull_request_target` Checkout

For `pull_request_target` workflows, the "Checkout PR branch" step is auto-disabled when no `checkout:` key is present. This prevents failures when the PR head branch is deleted or inaccessible (fork PRs).

To check out a trusted ref such as the base SHA, opt in with a `checkout:` mapping:

```yaml wrap
on:
  pull_request_target:
    types: [opened, synchronize]
checkout:
  repository: ${{ github.repository }}
  ref: ${{ github.event.pull_request.base.sha }}
```

The "Checkout PR branch" step remains disabled for `pull_request_target`. Do **not** check out `github.event.pull_request.head.sha` or `refs/pull/.../head` in a privileged `pull_request_target` job. That runs untrusted code with write permissions.

### Explicit Trusted Checkout Policy

Strict mode rejects `pull_request_target` checkouts unless checkout is disabled, targets the supported base repository and ref, or matches a literal pair in `on.pull_request_target.allowed-checkouts`. Declare the policy in the main workflow's frontmatter. It is not inherited from imports.

```aw wrap
on:
  pull_request_target:
    types: [closed]
    branches: [main]
    forks: ["dotnet/*"]
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

Each `allowed-checkouts` entry requires a literal `owner/repository` and a non-empty literal branch, tag, or commit SHA. Matching is exact and case-sensitive, and repository and ref pairs cannot be combined across entries. Expressions, wildcards, and `refs/pull/...` refs are rejected. Do not set `checkout.fetch` on these checkouts, because follow-up fetches can add refs beyond the trusted checkout. Wiki checkouts are not covered.

`acknowledge-risk` defaults to `false`. Setting it to `true` suppresses **only** the strict-mode dangerous-trigger warning. It does not allow other checkouts or bypass fork-pattern validation, actor authorization, or other strict checks.

> [!CAUTION]
> This policy records maintainer trust, not proof that checked-out code is safe. Prefer immutable commit SHAs, since literal branches and tags can move. Keep workflow code and helpers on trusted base refs, read PR changes as data, and never run PR-controlled code with privileged credentials. Checkout authentication and safe-output publication must be configured separately.

## Marking a Primary Repository (`current: true`)

When a workflow in a central repository targets a different repository, `current: true` tells the agent which repository is its primary target. The agent uses it as the default for GitHub operations such as creating issues, opening PRs, and reading content, unless the prompt says otherwise. Without it, the agent defaults to the repository where the workflow runs.

```yaml wrap
checkout:
  - repository: org/target-repo
    path: ./target
    github-token: ${{ secrets.CROSS_REPO_PAT }}
    current: true
```

> [!IMPORTANT]
> `current: true` only annotates the agent's system prompt. It does **not** change the working directory. For local tools such as tests or linters, add an explicit `cd` to the prompt:
>
> ```
> Navigate into the folder where the target repository has been checked out into: cd ${{ github.workspace }}/target
> ```

## Cleaning Submodule Credentials

By default, checkout steps set `persist-credentials: false`, and `actions/checkout` removes credentials in its post-step. In repositories with submodules or sparse checkouts, that post-step can fail with missing submodule URL or path errors.

Set `force-clean-git-credentials: true` to use an explicit cleanup step instead. The compiler emits the checkout with `persist-credentials: true` and adds a `Clean git credentials after checkout` step. The cleanup removes credential helpers and `http.*.extraheader` entries from `.git/config` and any `.git/modules/*/config`, including nested submodules.

Cleanup is fail-closed. A failed helper, invalid git configuration, or residual credential stops the job before agent execution. A read-only verification then checks the effective configuration, including included files, without logging token values.

```yaml wrap
checkout:
  - repository: org/monorepo-with-submodules
    submodules: recursive
    force-clean-git-credentials: true
```

## Cross-Organization safe_outputs Authentication (`safe-outputs-github-app`)

By default, the safe-outputs job uses `GITHUB_TOKEN` to check out repositories. The exception is the default workspace checkout when the global `safe-outputs.github-app` serves `create-pull-request` or `push-to-pull-request-branch` for the workflow repository. In that case the minted App token is persisted so pushes run as the App and trigger workflows. Additional checkouts, per-handler apps, and cross-repository targets never inherit the global App token. This prevents cross-org token confusion.

For workflows that target another organization, set `safe-outputs-github-app` on the checkout entry:

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
- The agent job checks out `OrgB/target-repo` with the app token scoped to `OrgB`.
- The safe-outputs job checks out `OrgB/target-repo` with the `safe-outputs-github-app` token.
- The safe-outputs job checks out the **workflow repository** with `GITHUB_TOKEN`, avoiding the cross-org authentication failure.
- `safe-outputs.github-app` is used only for PR creation and push operations, not for checkout.

## Checkout Merging

Multiple `checkout:` entries that target the same repository and path are merged into one workspace directory. This suits monorepos where parts need different settings, such as sparse checkout for some paths and full checkout for others.

Merge rules:

- **Fetch depth**: Deepest value wins (`0` = full history always takes precedence).
- **Fetch refs** and **sparse patterns**: Union of all patterns, with duplicates removed.
- **LFS**: Enabled if any entry enables it.
- **Submodules**: First non-empty value wins for each `(repository, path)`.
- **Ref, token, and App**: First-seen wins.

## Learn More

- [Cross-Repository Operations](/gh-aw/reference/cross-repository/): Reading and writing across multiple repositories
- [Authentication Reference](/gh-aw/reference/auth/): PAT and GitHub App setup
- [Multi-Repository Examples](/gh-aw/gallery/multi-repo/): Complete working examples
- [Checkout Behavior Specification](/gh-aw/specs/checkout-behavior-specification/): Formal checkout semantics across activation, agent, and safe-outputs jobs
