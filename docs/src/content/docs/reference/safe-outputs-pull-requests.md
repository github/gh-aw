---
title: Safe Outputs (Pull Requests)
description: Reference for pull-request safe outputs including create-pull-request, push-to-pull-request-branch, and add-reviewer.
sidebar:
  order: 801
---

This page covers pull-request-focused safe outputs: [`create-pull-request`](#pull-request-creation-create-pull-request), [`update-pull-request`](#pull-request-updates-update-pull-request), [`close-pull-request`](#close-pull-request-close-pull-request), [`approve-workflow-run`](#approve-workflow-run-approve-workflow-run), [`merge-pull-request`](#merge-pull-request-merge-pull-request) (experimental), [`create-pull-request-review-comment`](#pr-review-comments-create-pull-request-review-comment), [`submit-pull-request-review`](#submit-pr-review-submit-pull-request-review), [`reply-to-pull-request-review-comment`](#reply-to-pr-review-comment-reply-to-pull-request-review-comment), [`resolve-pull-request-review-thread`](#resolve-pr-review-thread-resolve-pull-request-review-thread), [`push-to-pull-request-branch`](#push-to-pr-branch-push-to-pull-request-branch), and [`add-reviewer`](#add-reviewer-add-reviewer).

`create-pull-request`, `push-to-pull-request-branch`, and `merge-pull-request` enforce [Protected Files](#protected-files) by default. For other safe-output types, see [Safe Outputs](/gh-aw/reference/safe-outputs/).

## Pull Request Creation (`create-pull-request:`)

Creates a PR with the agent's code changes. If PR creation is blocked (for example, by organization settings), it falls back to opening an issue; set `fallback-as-issue: false` to disable this. The default `max: 1` is a **per-workflow-run quota shared by all agents and sub-agents**, not a per-agent limit. Raise `max` when parallel agents or multiple independent PRs are needed. Excess `create_pull_request` calls are rejected.

```yaml wrap
safe-outputs:
  create-pull-request:
    title-prefix: "[ai] "         # prefix for titles
    labels: [automation]          # labels to attach
    reviewers: [user1, copilot]   # reviewers (use 'copilot' for bot)
    team-reviewers: [platform-reviewers] # team slugs to request as reviewers
    assignees: [user1]            # assignees for the created PR and any fallback issue
    draft: true                   # create as draft — enforced as policy (default: true)
    max: 3                        # max PRs per run (default: 1)
    expires: 14                   # auto-close after N days (same-repo only; also accepts 2h, 7d, 2w, 1m, 1y)
    if-no-changes: "warn"         # "warn" (default), "error", or "ignore"
    target-repo: "owner/repo"     # upstream repository that receives the PR
    head-repo: "automation/fork"  # optional automation-owned fork that receives the branch push
    allowed-repos: ["org/repo1", "org/repo2", "automation/fork"]  # allowlisted upstream/head repositories
    base-branch: "vnext"          # PR target branch (default: github.base_ref || github.ref_name)
    allowed-base-branches:        # allow agent to override base branch at runtime (glob patterns)
      - main
      - release/*
    allowed-branches:             # restrict agent-selected source branch names (glob patterns)
      - feature/*
      - release/*
    stacked: true                 # allow stacked pull requests (default: true)
    fallback-as-issue: false      # disable issue fallback (default: true)
    auto-close-issue: false       # don't auto-add "Fixes #N" to PR description (default: true)
    normalize-closing-keywords: true # strip backticks around recognized issue-closing keywords in PR body text
    preserve-branch-name: true    # omit random salt suffix from branch name (default: false)
    recreate-ref: true            # force-recreate remote branch when it already exists (requires preserve-branch-name; default: false)
    exclude-lock-yml: true        # omit compiled workflow locks (default: false)
    excluded-files:               # strip these files from the patch entirely
      - "**/*.lock"
      - "dist/**"
    max-patch-files: 300          # max unique files in the patch (default: 100)
    max-patch-size: 2048          # max patch size in KB (default: 4096)
    github-token: ${{ secrets.UPSTREAM_PR_TOKEN }} # optional credential for upstream PR creation
    head-github-token: ${{ secrets.FORK_PUSH_TOKEN }} # optional credential for fork branch writes
    head-github-app:                 # optional GitHub App to mint the fork credential at runtime
      client-id: ${{ vars.FORK_APP_CLIENT_ID }}
      private-key: ${{ secrets.FORK_APP_PRIVATE_KEY }}
    github-token-for-extra-empty-commit: ${{ secrets.CI_TOKEN }} # optional token to push empty commit triggering CI
    signed-commits: true          # signed commits via GitHub APIs (default: true); set false to use git push directly
    protected-files: fallback-to-issue  # push branch, create review issue if protected files modified
```

`target-repo` names the upstream repository that owns the base branch. `head-repo`, when set, names the automation-owned fork that receives the pushed branch. When they differ, the created PR uses an owner-qualified head (`fork-owner:branch`), and both repositories must be allowlisted. See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for `target-repo`, `allowed-repos`, and authentication.

### Steering issues

Configure steering independently at [`safe-outputs.steer`](/gh-aw/reference/safe-outputs/#steering-issues-steer). Steering does not pre-create or override a branch, so `branch-prefix`, cross-repository targets, branch policies, multiple outputs, and checkout configuration behave as usual.

### Branch targeting

`base-branch` sets the PR target. It defaults to `github.base_ref` on pull request events and `github.ref_name` on push events. To let the agent pick the target at runtime, use `allowed-base-branches`: the agent's `base` value is accepted only if it matches a configured glob.

`allowed-branches` restricts which _source_ branch names the agent may use. The effective branch (agent-provided, or the checkout branch as a fallback) must match a configured glob.

### Stacked pull requests

A _stacked_ pull request targets another pull request's branch instead of the default base, so a chain of dependent changes can be reviewed and merged one piece at a time. For example, PR&nbsp;1 (`feature-1`) targets `main`, PR&nbsp;2 (`feature-2`) targets `feature-1`, and PR&nbsp;3 (`feature-3`) targets `feature-2`.

Set `max` above `1` and emit one `create_pull_request` output per level, in dependency order. Each base must already exist when its PR is created:

```yaml wrap
safe-outputs:
  create-pull-request:
    max: 3
    preserve-branch-name: true    # keeps `base` values predictable across the stack
```

```json
{"type": "create_pull_request", "title": "Step 1", "body": "...", "branch": "feature-1"}
{"type": "create_pull_request", "title": "Step 2", "body": "...", "branch": "feature-2", "base": "feature-1", "stack_position": 2, "stack_root": "main"}
{"type": "create_pull_request", "title": "Step 3", "body": "...", "branch": "feature-3", "base": "feature-2", "stack_position": 3, "stack_root": "main", "dependencies": ["feature-1", "feature-2"]}
```

Behavior:

- A `base` that names a branch from a PR created earlier in the same run is accepted without `allowed-base-branches`. It resolves to the branch actually pushed, which is salted unless `preserve-branch-name: true` is set. Any other `base` override requires `allowed-base-branches`.
- The base branch must exist before the PR is created, or the output fails.
- Circular dependencies are rejected.
- The PR body records the stack: a `Depends on #N` reference for each same-run dependency, and a `<!-- gh-aw-stack: ... -->` comment holding `base`, `stack_position`, `stack_root`, and `dependencies`.

#### Disabling stacked pull requests (GitHub Enterprise Server)

Stacked pull requests may be unavailable on GitHub Enterprise Server. Set `stacked: false` so misuse fails fast. Any `create_pull_request` output whose `base` differs from the default base is then rejected with an explanatory error. PRs targeting the default base keep working, and stack metadata is still recorded, so a workflow can move between instances without editing its prompt.

```yaml wrap
safe-outputs:
  create-pull-request:
    stacked: false
```

#### Migrating an existing workflow to a stack

Raise `max` to the stack size, set `preserve-branch-name: true` so each `base` matches the original branch name, and have the agent emit PRs root-first, each with `base` pointing to the previous branch. On GitHub Enterprise Server, also add `stacked: false` and keep the agent targeting the default base branch. Keep stacks to three or four PRs, use explicit `branch` and `base` names, and merge from the root upward.

### Runtime reviewers and assignees

`reviewers`, `team-reviewers`, and `assignees` accept either a static list or a single GitHub Actions expression string. This lets you route a cross-repository PR to the triggering actor or a runtime-selected team without recompiling.

```yaml wrap
safe-outputs:
  create-pull-request:
    target-repo: "acme/shared-infra"
    reviewers: ${{ github.event.pull_request.user.login }}
    team-reviewers: ${{ inputs.review-team }}
    assignees: ${{ github.event.pull_request.user.login }}
```

### Branch naming

By default, a random hex suffix is appended to the agent-provided branch name to avoid collisions. Set `preserve-branch-name: true` to omit it, for example in repositories that enforce naming conventions such as Jira keys. If the branch already exists on the remote, `recreate-ref: true` (which requires `preserve-branch-name`) force-deletes and recreates it. Use this for long-lived branches whose previous PR was merged.

### Patch limits

`excluded-files` strips matching files from the patch before the commit is created. Excluded files are also exempt from `allowed-files` and `protected-files` checks. `max-patch-files` (default `100`) and `max-patch-size` (default `4096` KB) guard against unexpectedly large commits. Raise them when the workflow intentionally generates many or large files.

When exclusions are configured, patch and bundle transports package the retained base-to-tip tree as a single commit. This keeps merge-resolution changes but not the original commit history. Pathspec matching follows Git rules, including basename patterns that match nested files.

`exclude-lock-yml: true` excludes `.lock.yml` files under `.github/workflows/` from patch and bundle transports. This avoids GitHub's workflow-write restriction on the Actions token. Other `.yml` and `.yaml` workflow files are not excluded and still need workflow-write credentials. When exclusions drop workflow YAML changes, the PR body (or the push summary and PR comment) warns you to run `gh aw compile` and commit the regenerated locks with authorized credentials before merging. If only excluded files changed, no branch is pushed. If GitHub rejects a push for lack of workflow-write permission, the error suggests `exclude-lock-yml: true`. The rejected push is not retried.

### Other notes

`draft` is a **policy**, so the agent cannot override it at runtime. `auto-close-issue` (default `true`) appends `Fixes #N` when the workflow is triggered from an issue. Set it to `false` for partial-work or multi-PR flows. `normalize-closing-keywords` removes wrapping backticks from recognized closing keywords in the PR body. Enabling `create-pull-request` also enables the git commands `checkout`, `branch`, `switch`, `add`, `rm`, `commit`, and `merge`.

PRs do not trigger CI by default; see [Triggering CI](/gh-aw/reference/triggering-ci/). You can disable `create-pull-request` at runtime without recompiling by setting the `GH_AW_POLICY_ALLOW_CREATE_PULL_REQUEST` GitHub Actions variable to `"false"` at repository, organization, or enterprise scope. See [Governance](/gh-aw/reference/governance/#disabling-create-pull-request-org-wide).

### How it works

The agent's commits are packaged as a **git bundle** and uploaded as an Actions artifact. A separate, permission-controlled `safe_outputs` job then:

1. Checks out the target repository at the base branch (shallow, depth 1), and fetches any additional `fetch:` refs declared in `checkout:` for that repository.
2. Imports the bundle offline with `git bundle unbundle`. If prerequisite commits are missing, the handler fails by default. Provision the objects during authenticated checkout, or opt in to recovery with `allow-full-history-fetch: true` on that handler (see [Bundle prerequisites and history downloads](#bundle-prerequisites-and-history-downloads)).
3. Publishes signed commits through GitHub's APIs and creates the PR. For a new branch, the first commit is created through the REST API, GitHub must verify its signature, and the branch is then created at that commit. Later commits use GraphQL `createCommitOnBranch`.

The new branch appears already containing the proposed changes, rather than briefly pointing at the base commit. This avoids skipped or cancelled CI runs against the base. If GitHub does not verify the initial signature, the handler refuses to publish the branch.

If the base advances between agent start and apply, the PR can be rebased when the necessary bundle objects are available. Otherwise, missing prerequisites fail the handler unless recovery is enabled. If a non-fast-forward race occurs during the push, the job creates a fallback PR from a temporary branch so no changes are lost.

For unsigned bundle pushes, the handler validates against the exact pinned base tip. Older-ancestor baselines are opt-in through `allow-merged-base-baseline: true` under `safe-outputs.push-to-pull-request-branch`. That setting enables only bounded metadata discovery. It does not fetch base objects or enable history recovery.

An older **patch transport** (`git format-patch` / `git am --3way`) is used when bundle data is unavailable. If `--3way` cannot resolve cleanly against an updated base, the patch is applied at the agent's original base commit, and the PR UI shows the conflicts for manual resolution.

:::caution[Shallow checkout and large monorepos]
Merge-commit detection for bundle transport inspects `origin/<branch>..<branch>` in the agent's workspace. With the default shallow checkout (`fetch-depth: 1`), `git rev-list` cannot exclude commits, so on large monorepos the range can cover the entire local history and falsely suggest merge commits.

The safe_outputs push job guards against this. In a shallow repository, a commit range over 100 commits emits a warning and is treated as containing no merges. If the implausible range reaches signed-push linearization, that step fails with the commit count. To fix it, increase `fetch-depth`. Full history (`0`) is the most reliable option:

```yaml wrap
checkout:
  fetch-depth: 0   # fetch full history so merge-commit detection sees the correct range
```
:::

:::note[Cross-repo targets]
`safe_outputs` mirrors the agent job's checkout layout. When a `checkout:` entry sets `path:`, every repository is checked out at the same location the agent used: the workflow repository at the workspace root, plus each cross-repo checkout at its `path:`. This holds whether `target-repo` names a specific repository or `"*"`. A target checked out at the workspace root (no `path:`) is placed there in both jobs.
:::

When `head-repo` is set, the preferred model is an ephemeral upstream-based branch. The safe output job resolves the upstream base SHA from `target-repo`, creates or refreshes a temporary branch in `head-repo` from that SHA, pushes the agent's commits there, and opens the PR back to the upstream base. Only the configured upstream and head pair is supported. Arbitrary reuse of unrelated fork branches is not. See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for the checkout, allowlist, and fork-credential rules.

## Pull Request Updates (`update-pull-request:`)

Updates a PR's title or body. Both fields are enabled by default. `operation` controls body updates: `replace` (default), `append`, `prepend`, or `replace-island` (updates a run-specific section delimited by HTML comments). `safe-outputs.update-pull-request.operation` sets the default when the agent omits `operation`. An explicit agent value takes precedence. If neither is set, the handler uses `replace`.

```yaml wrap
safe-outputs:
  update-pull-request:
    title: true               # enable title updates (default: true)
    body: true                # enable body updates (default: true)
    update-branch: false      # update PR branch with latest base before other updates (default: false)
    footer: false             # omit AI-generated footer from body updates (default: true)
    max: 1                    # max updates (default: 1)
    target: "*"               # "triggering" (default), "*", or number
    target-repo: "owner/repo" # cross-repository
    required-labels: [automated]     # only update if PR has ALL these labels
    required-title-prefix: "[bot] "  # only update if PR title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

**Target**: `"triggering"` (requires a PR event), `"*"` (any PR; the agent must supply `pull_request_number`), or a specific number.

When `update-branch: true` is set, the handler calls the REST `pulls.updateBranch` API to merge the latest base branch into the PR branch before applying title or body updates. This requires `contents: write`. With `contents: read` only, the field cannot be used. It can also be used alone, with `title: false` and `body: false`, to update the branch without changing the description. If GitHub reports `There are no new commits on the base branch.` or `merge conflict between base and head`, the workflow logs a warning and continues.

Title updates always replace the existing title. Disable a field by setting it to `false`. For `replace-island`, gh-aw wraps the workflow-specific section in `<!-- gh-aw-island-start:<workflowId> -->` and `<!-- gh-aw-island-end:<workflowId> -->` markers. The first run appends the marked island, and later runs replace only the content between the markers.

## Close Pull Request (`close-pull-request:`)

Closes PRs without merging, with an optional comment. Filter by labels and title prefix. Target: `"triggering"` (PR event), `"*"` (any), or a number.

```yaml wrap
safe-outputs:
  close-pull-request:
    target: "triggering"              # "triggering" (default), "*", or number
    required-labels: [automated, stale] # only close with these labels
    required-title-prefix: "[bot]"    # only close matching prefix
    max: 10                           # max closures (default: 1)
    target-repo: "owner/repo"         # cross-repository
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

## Approve Workflow Run (`approve-workflow-run:`)

Approves a workflow run in the "action required" state, such as runs for fork pull requests or pull requests created by Copilot. The agent supplies a positive integer `run_id`. The handler verifies that the run is a PR run, that its associated PRs are authorized, and that it is still awaiting approval (status `action_required` or `waiting`, or conclusion `action_required`) before calling GitHub's approval API.

```yaml wrap
safe-outputs:
  approve-workflow-run:
    max: 1
    allowed-repos: # optional: fork repositories whose PRs may be approved
      - contributor/gh-aw
    comment: true # post a PR comment when the run starts (default: true)
    staged: false
    github-token: ${{ secrets.APPROVE_WORKFLOW_RUN_TOKEN }}
    allowed-workflows:
      - pull-request-*.yml
    allowed-pull-requests:
      - "123"
    protected-files:
      exclude:
        - AGENTS.md
```

- **Token**: requires `actions: write` and an explicit external `github-token` or `github-app`, because the default `github.token` cannot approve these runs. `pull-requests: write` is needed when `comment` is enabled (the default). Otherwise `pull-requests: read` is enough.
- **Staged mode**: `staged: true` previews the approval without calling the API or consuming `max`.
- **Workflows**: `allowed-workflows` is required. It matches workflow filenames, treating `.yml` and `.yaml` as equivalent. Directory paths are rejected.
- **Pull requests**: by default, only the triggering PR is authorized. `allowed-pull-requests` adds PR numbers, as a list of strings or an expression that resolves to a list. Invalid entries are ignored.
- **Forks**: PRs whose head branch lives in the current repository, including `copilot/*` branches, are approvable by default. Fork PRs are refused unless their repository matches an `allowed-repos` entry, which supports wildcards such as `org/*`.
- **Refusals**: the safe output always refuses `pull_request_target` events. It also rejects runs that are not PR runs, come from a disallowed workflow, have an unauthorized associated PR, or modify protected files. Protected files use the standard set. Use `protected-files.exclude` to remove filenames or path prefixes from it.
- **Skips**: a run that is no longer awaiting approval, for example one a maintainer or an earlier run already approved, is skipped rather than failed.

After a successful approval, a comment announcing the run and linking to its run URL is posted on each associated PR. Set `comment: false` to disable it. Comment failures are logged as warnings and never fail the approval.

## Merge Pull Request (`merge-pull-request:`)

:::caution[Experimental]
`merge-pull-request` is experimental. `gh aw compile` emits an experimental feature warning when a workflow uses it. A merge proceeds only if every configured policy gate passes. Merges to the repository default branch are always refused.
:::

Graduating to stable requires end-to-end coverage of same-repository and cross-repository merges, staged and live parity across all policy gates, resolution of known mergeability false positives and negatives, and at least one release cycle without schema or behavior changes.

Merges a PR only after the configured gates pass: status checks, review decision, unresolved review threads, label and branch constraints, and GitHub mergeability. If any gate fails, the merge is skipped and the reason is reported. Merging an already-merged PR is idempotent and returns success.

```yaml wrap
safe-outputs:
  merge-pull-request:
    allowed-base-branches: ["agent-work/*"] # REQUIRED: owner-declared non-consequential destinations
    max: 1                            # max merges per run (default: 1, range: 1-10)
    required-labels: [ready-to-merge] # ALL listed labels must be present on the PR
    required-title-prefix: "[bot] "   # only merge PRs whose title starts with this prefix
    allowed-branches: ["feature/*"]   # glob patterns for the PR's source branch
    required-checks: ["ci/test"]      # exact check-run names; success required on the evaluated head
    required-check-apps:             # optional expected GitHub App ID for each named check
      ci/test: 15368
    required-approvers: [maintainer]  # at least one independent human approval on that head
    protected-files:                 # blocked by default; same protected set as create-pull-request
      policy: blocked                # blocked or allowed
      exclude: ["/.changeset/*.md"]   # optional exceptions using standard protected-file semantics
    target: "triggering"              # "triggering" (default, current PR) or "*" (any PR with pull_request_number)
    target-repo: "owner/repo"         # cross-repository target
    allowed-repos: ["org/other-repo"] # additional repositories the agent can merge into
    staged: false                     # if true, evaluate gates and emit preview results without performing the merge
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

**Target**: `"triggering"` (requires a PR event) or `"*"` (the agent supplies `pull_request_number`). With `target: "*"` and cross-repository configuration, the agent may also supply `repo`, which must match `target-repo` or appear in `allowed-repos`.

**Merge method**: the agent selects `merge`, `squash`, or `rebase` per call. The base branch comes from the PR.

**Destination scope**: `allowed-base-branches` is required. It is a non-empty list of static base-branch patterns that the owner has declared suitable for non-consequential automated integration. The agent cannot supply or relax it. The live base must match at least one pattern at evaluation and again at final revalidation.

- Only `*` and `**` are supported. GitHub Actions expressions, negation (`!`), character classes, braces, extglobs, `?`, `+`, and backslash escapes are rejected.
- Patterns made only of wildcards and separators, such as `"*"`, `"**"`, `"*/*"`, and `"**/*"`, are forbidden.
- Matching is case-sensitive. `*` stays within one path segment, and `**` can span `/`.
- `allowed-branches` filters the source branch independently. Neither list overrides default-branch refusal or any other gate.
- Owners must assess the downstream effects of each destination and protect this configuration from agent edits. Matching a pattern is not proof of harmless effects or of per-merge human approval.
- Existing workflows must add a scoped list and recompile. Null, omitted, empty, or unrestricted lists are rejected. The compiler does not infer a destination policy from `allowed-branches`.

**Gates**: the handler validates that the PR is not a draft and has no conflicts, then checks required status checks, the GitHub review decision, unresolved review threads, `required-labels`, `required-title-prefix`, and `allowed-branches`.

**CI checks**: `required-checks` adds exact check-run names to any checks the branch already requires. Each named check must be `success` on the evaluated head SHA. Missing, pending, skipped, neutral, failed, or stale checks block the merge. If no checks are configured or required by the branch, CI is not queried. Protected branches are refused regardless of CI results. Compilation rejects a required name that matches this workflow's own `create-check-run` name, including the default `<workflow name> (Result)`, because the agent controls that check's conclusion.

**Check producers**: `required-check-apps` maps each required name to a positive GitHub App ID. Every observed check with that name must come from that app, including during final revalidation. Without a mapping, names alone do not authenticate the producer. Several workflows can share an app, so protect CI workflow definitions from the acting agent. Unknown names and invalid IDs are rejected at compile time. The compiler grants `checks: read` even without `required-checks`, because branch requirements are discovered at runtime.

**Proof evidence (opt-in)**: `proof-evidence` binds an independent deterministic verifier's result to the authorized merge. Its `check` must appear in both `required-checks` and `required-check-apps`. External `authorization` is required, locally or inherited from the workflow or repository. Without `proof-evidence`, existing merge behavior is unchanged.

```yaml wrap
safe-outputs:
  merge-pull-request:
    allowed-base-branches: ["agent-work/*"]
    authorization:
      endpoint: https://authority.example/authorize
      audience: merge-proof
    required-checks: ["invariant-verifier"]
    required-check-apps:
      invariant-verifier: 12345 # independent verifier's GitHub App ID
    proof-evidence:
      check: invariant-verifier
      verifier-version: "1.2.3"
      configuration-sha256: "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc"
```

The selected check run's `output.summary` must contain only this JSON object (at most 8 KiB, no Markdown fences, duplicate keys, or extra fields):

```json
{
  "version": "gh-aw.merge-proof.v1",
  "repository": "owner/repo",
  "pull_request_number": 42,
  "head_sha": "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
  "verifier_version": "1.2.3",
  "configuration_sha256": "cccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccccc",
  "result": "passed"
}
```

Every field must match the resolved repository, PR number, live head, and trusted workflow configuration. Only `passed` is supported; missing, malformed, failed, `Unknown`, and unsupported results block merging even if the check conclusion is `success`. Agent-supplied evidence fields are never consumed. The verifier must compute the configuration digest over the complete invariant set and relevant configuration; `gh-aw` compares this opaque digest and does not implement domain-specific proof semantics.

The existing external authorization request uses `scope.binding: resolved_effect`. Its `scope.refs` carry the repository, PR, head, check-run ID, producer App ID, verifier version, configuration digest, `passed` result, and a SHA-256 digest of the complete binding. The same binding and exact merge inputs are covered by `input_digest`; existing caller and policy bindings remain in force. No separate authorization service or protocol is introduced. After the authority allows the request, final merge revalidation rereads the PR and selected check evidence. A changed head or proof binding invalidates the proof; an expired authorization also blocks execution. The merge API still atomically requires the evaluated head SHA.

Keep the verifier App credentials, verifier implementation, invariant configuration, and workflow policy outside the acting agent's control. App pinning alone cannot distinguish workflows sharing one App or make an agent-controlled check deterministic. This integration does not authorize default/protected-branch merges, relax file protections, or supply production deployment controls. Staged previews neither request authorization nor merge and are not evidence of a verified effect.

**Approvals**: individual reviews are always read. Any reviewer's latest effective `CHANGES_REQUESTED` review blocks the merge, independent of branch protection. A later approval or dismissal supersedes it. Missing or malformed review evidence also blocks the merge. Without `required-approvers`, the absence of blocking reviews is not affirmative human approval. For higher-impact use, set `required-approvers` to an allowlist of human logins (matching is case-insensitive). At least one must have an `APPROVED` review on the evaluated head SHA. PR authors, bot accounts, and the authenticated merge identity cannot supply this approval.

**Authorization boundary**: labels, titles, and branch names are filters, not human authorization. Keep approval accounts and their credentials outside the acting agent's control. A review reported under a human account is not proof that a person, rather than automation using that account, wrote it.

**Protected files**: before evaluation completes and again before the merge or a staged preview, the handler reads the PR's complete changed-file list. This covers added, modified, deleted, and renamed files, including both sides of a rename. The protected set, exclusions, and `aw.json` exclusions match `create-pull-request`, and they apply to the PR's changes relative to its base. The default policy is `blocked`. `protected-files: allowed` disables only this gate. The object form `{ policy, exclude }` supports scoped exceptions. The creation-only `request-review` and `fallback-to-issue` policies are not supported. Missing or incomplete file evidence blocks the merge, including PRs over GitHub's 3,000-file listing limit.

**Caution veto**: detection warnings or failures in the current run block the merge. The handler also checks the live PR body, labels, and all review bodies for prior threat-detected or detector-engine-error notices, protected-files review notices, and `[!CAUTION]` alerts. These notices block both live and staged merges, including cross-repository targets. Bot reviews, advisory `COMMENTED` reviews, dismissed reviews, and superseded reviews still count. Missing or malformed body, label, or review evidence fails closed. An empty body (`null`) is valid. No approval, passing check, or label clears this veto, even with `required-approvers`. Review flagged changes and merge through your normal manual process instead of removing the notices.

**Revision and traceability**: the merge API receives the evaluated head SHA. A mismatch fails without retry, and a new request must evaluate policy again. Before execution, and in staged previews, the handler rechecks policy fields (including the complete PR body), branch restrictions, the open upstream PR, review evidence, approvals, and required checks. Results include `decision_evidence`, which records the evaluated head and base revisions, configured gates, check-run IDs, review decision, approver logins and review IDs, caution reason codes, and timestamps. Raw PR and review bodies are not returned. GitHub enforces only the head SHA atomically. Branch policy, reviews, checks, and the base can still change after the final reads.

**Uncertain outcomes**: HTTP 405, 409, and 422 responses from the merge API are definitive failures (`merged: false`, code `merge_not_completed`), with no reconciliation or retry. For a lost response, timeout, server error, or malformed response, the handler does not retry the mutation. It makes one read-only PR lookup instead. If GitHub shows the evaluated head as merged with a valid merge revision, the result is successful with `reconciled: true`. That confirms repository state, not which actor completed the merge. Otherwise the result has `success: false`, `merged: null`, and `operation_outcome: "indeterminate"`. Treat an indeterminate outcome as an unknown side effect, and inspect the live PR before taking any further action.

**Decision audit**: successful, blocked, and staged decisions are persisted to `merge-pull-request-decisions.jsonl` in the `safe-outputs-items` artifact. Records contain the run ID and attempt, outcome, fixed failure codes, and minimized evidence: repository, PR number, head and base revisions, and timestamps. Titles, labels, branch names, reviewer identities, approval details, review bodies, and raw API errors are not persisted. `decision_evidence.authorization` records a policy version and a SHA-256 fingerprint of the policy configuration, with a fixed outcome for each gate. The fingerprint supports correlation, not proof of policy integrity. Records are capped at 64 KiB and are redacted before upload. If an audit write fails, an already-completed merge keeps its successful outcome and an explicit audit-failure diagnostic. The merge is not retried.

**Staged mode**: `staged: true` runs all gate checks and emits a preview with `staged: true` and `merged: false`, without calling the merge API. Staged and trial runs keep decision audit artifacts.

**Debugging**: enable `ACTIONS_STEP_DEBUG` to see `merge_pull_request:` debug records. They contain only fixed event and phase names, counts, and booleans. Identifiers, revisions, names, reviewer identities, and content are excluded.

**Evidence trust boundary**: results and `decision_evidence` are marked `data_trust: untrusted`. PR-derived metadata is data, never instructions or authorization to change policy. Workflow-configured policy such as `allowed-branches` and `required-title-prefix` is trusted configuration, not PR input.

**Recovery limits**: stop and re-evaluate after any policy or revision change. Reverting a merge creates a compensating change. It does not undo deployments, published artifacts, external effects, or data migrations.

See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for `target-repo`, `allowed-repos`, and authentication. See the [Safe Outputs Specification](/gh-aw/specs/safe-outputs-specification/#type-merge_pull_request) for the complete schema and operational semantics.

## PR Review Comments (`create-pull-request-review-comment:`)

Creates review comments on specific code lines in PRs. Supports single-line and multi-line comments.

```yaml wrap
safe-outputs:
  create-pull-request-review-comment:
    max: 3                    # max comments (default: 10)
    side: "RIGHT"             # "LEFT" or "RIGHT" (default: "RIGHT")
    target: "*"               # "triggering" (default), "*", or number
    target-repo: "owner/repo" # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    footer: "if-body"         # footer control: "always", "none", or "if-body"
    required-labels: [automated]     # only comment if PR has ALL these labels
    required-title-prefix: "[bot] "  # only comment if PR title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

With `target: "*"`, the agent must supply `pull_request_number` in each call, or the comment fails. For cross-repository use, the agent can also supply `repo` (`owner/repo` format), which must match `target-repo` or appear in `allowed-repos`.

## Submit PR Review (`submit-pull-request-review:`)

Submits a consolidated PR review. Inline comments buffered by `create-pull-request-review-comment` are included automatically.

```yaml wrap
safe-outputs:
  submit-pull-request-review:
    max: 1
    allowed-events: [COMMENT, REQUEST_CHANGES]  # include REQUEST_CHANGES when superseding older blocking reviews
    supersede-older-reviews: true  # dismiss older same-workflow blockers after a clean COMMENT review with no inline findings
    target: "triggering"           # or "*", or explicit PR number
    target-repo: "owner/repo"      # cross-repository
    allowed-repos: ["org/repo1"]   # additional allowed repositories
    footer: "always"               # "always", "none", or "if-body"
    required-labels: [automated]   # only submit review if PR has ALL these labels
    required-title-prefix: "[bot] " # only submit review if PR title starts with this prefix
```

Buffered inline comments satisfy safe-output dependencies only after the review is published. If GitHub returns partial or missing comment coordinates, the handler uses an optional identity lookup to preserve them. A comment whose identity cannot be matched uniquely has no `id` or `url` in the output, but it still counts as published. Comments filtered out before submission, or moved into a body-only fallback, do not count as published inline comments.

Use `allowed-events` to control review decisions (`APPROVE`, `COMMENT`, `REQUEST_CHANGES`). Prefer `allowed-events: [COMMENT]` so bot reviews stay informative and non-blocking.

If you allow `REQUEST_CHANGES`, set `supersede-older-reviews: true` to dismiss older blocking reviews from the same workflow after posting a clean `COMMENT` review with no inline findings. Only reviews older than the clean replacement are eligible. A new blocking review, or one with inline findings, does not dismiss earlier findings. This is best-effort. It uses a review-body provenance marker even when the visible footer is disabled.

With `target: "*"`, the agent must supply `pull_request_number` in each call, or the review fails. For cross-repository use, the agent can supply `repo`, which must match `target-repo` or appear in `allowed-repos`.

## Reply to PR Review Comment (`reply-to-pull-request-review-comment:`)

Replies to existing review comments on pull requests, to answer questions, respond to feedback, or acknowledge a comment. The `comment_id` must be the numeric ID of an existing review comment.

```yaml wrap
safe-outputs:
  reply-to-pull-request-review-comment:
    max: 10                              # max replies (default: 10)
    target: "triggering"                 # "triggering" (default), "*", or number
    target-repo: "owner/repo"            # cross-repository
    allowed-repos: ["org/other-repo"]    # additional allowed repositories
    footer: true                         # add AI-generated footer (default: true)
    required-labels: [automated]         # only reply if PR has ALL these labels
    required-title-prefix: "[bot] "      # only reply if PR title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

The `footer` field controls AI-generated footers: `"always"` (default) always adds one, `"none"` never does, and `"if-body"` adds one only when the review has body text. With `"if-body"`, approvals without body text stay clean, while reviews with explanatory text still carry attribution.

## Resolve PR Review Thread (`resolve-pull-request-review-thread:`)

Marks review threads as resolved after the agent has addressed the feedback. Uses the GitHub GraphQL `resolveReviewThread` mutation. Resolution is scoped to the triggering PR by default. Use `target`, `target-repo`, and `allowed-repos` for cross-repository threads.

```yaml wrap
safe-outputs:
  resolve-pull-request-review-thread:
    max: 10                              # max threads to resolve (default: 10)
    target: "triggering"                 # "triggering" (default), "*", or number
    target-repo: "owner/repo"            # cross-repository
    allowed-repos: ["org/repo1", "org/repo2"]  # additional allowed repositories
    required-labels: [automated]         # only resolve if PR has ALL these labels
    required-title-prefix: "[bot] "      # only resolve if PR title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

**Agent output format:**

```json
{"type": "resolve_pull_request_review_thread", "thread_id": "PRRT_kwDOABCD..."}
```

:::note[Integration-token limitation]
GitHub can return `Resource not accessible by integration` for `resolveReviewThread`, even with `pull-requests: write` on `GITHUB_TOKEN`. This happens, for example, with bot-authored threads in non-interactive runs. In that case, gh-aw logs a warning, soft-skips the thread, and continues processing other safe outputs. For reliable resolution, set `safe-outputs.resolve-pull-request-review-thread.github-token` to a token that can resolve review threads in your repository.
:::

## Push to PR Branch (`push-to-pull-request-branch:`)

Pushes changes to a PR's branch. Gate pushes with `required-title-prefix` and `required-labels` so that only approved PRs receive changes. Set `max` above `1` to allow multiple pushes per run.

:::caution[Fork PRs Are Restricted]
This safe output can push only to same-repository PRs, or to fork-backed PRs whose head repository exactly matches the configured `head-repo`. Other contributor forks are unsupported and fail early with a clear error.
:::

```yaml wrap
safe-outputs:
  push-to-pull-request-branch:
    target: "*"                 # "triggering" (default), "*", or number
    required-title-prefix: "[bot] "      # require title prefix
    required-labels: [automated]         # require all labels
    max: 3                      # max pushes per run (default: 1)
    if-no-changes: "warn"       # "warn" (default), "error", or "ignore"
    exclude-lock-yml: true        # same compiled-workflow protection as create-pull-request
    excluded-files:               # files to omit from the patch entirely
      - "**/*.lock"
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
    github-token-for-extra-empty-commit: ${{ secrets.CI_TOKEN }} # optional token to push empty commit triggering CI
    fallback-as-pull-request: true        # on non-fast-forward or workflow-permission timeout, create fallback PR (default: true)
    signed-commits: true                  # signed commits are required (default); set false to use git push directly
    ignore-missing-branch-failure: false  # treat deleted/missing branch errors as skipped instead of failed (default: false)
    check-branch-protection: true         # set to false to skip the branch protection pre-flight check (default: true)
    protected-files: fallback-to-issue  # create review issue if protected files modified
    target-repo: "owner/repo"    # upstream repository containing the PR
    head-repo: "automation/fork" # required for supported fork-backed follow-up pushes
    allowed-repos: ["owner/repo", "automation/fork"] # allowlisted upstream/head repositories
    head-github-token: ${{ secrets.FORK_PUSH_TOKEN }} # optional credential for fork branch writes
    head-github-app:                 # optional GitHub App to mint the fork credential at runtime
      client-id: ${{ vars.FORK_APP_CLIENT_ID }}
      private-key: ${{ secrets.FORK_APP_PRIVATE_KEY }}
```

Git commands (`checkout`, `branch`, `switch`, `add`, `rm`, `commit`, `merge`) are enabled automatically when this output is configured. An explicit `target` accepts a positive integer (`123`), a numeric string (`"123"`), or a hash-prefixed string (`"#123"`). With `target: "*"`, the same forms are accepted in the message's `pull_request_number` field. Quote hash-prefixed values in YAML so they are not read as comments.

`create_pull_request` stages a patch for processing after the agent finishes, and its response does not return a PR number. A `push_to_pull_request_branch` call cannot target a PR created earlier in the same run, even with a `temporary_id`, because the branch is not yet available remotely. Push follow-up commits in a later run, once the PR exists.

### Destination branch

The agent **does not specify the destination branch**. Both branches are derived from the triggering PR.

- The **source branch** is the branch checked out in the agent's workspace. The agent must commit its changes onto the PR's head ref before calling the tool.
- The **destination branch** is always the triggering PR's head ref. The apply-time push job resolves it with `pulls.get(pull_number).head.ref`.

This prevents failures from a wrong or synthetic branch name (see [issue #37835](https://github.com/github/gh-aw/issues/37835)).

With `signed-commits: true` (the default), pushes are replayed through GitHub's signed commit API. Set `signed-commits: false` only for repositories that do not require signed commits. That uses direct `git push`, which can preserve merge commits the signed API cannot represent. Both `create-pull-request` and `push-to-pull-request-branch` support this field.

Signed replay checks each commit's synthesized file changes before it creates a remote branch or publishes commits. Each commit is limited to 4096 additions and deletions combined, within the configured patch limits. Exceeding that bound fails the push, with no unsigned fallback.

For unsigned bundle pushes, the handler pins the current PR base tip with an exact-ref, metadata-only `git ls-remote`. It then uses that tip to exempt the base's own changes from protected-file and history checks. Only the exact pinned tip qualifies. Other ancestors keep PR-head protection, because reachability does not prove a historical base state. Agent changes and conflict resolutions remain subject to every check. Agent-created octopus merges (three or more parents) are rejected. Discovery is bounded by per-command timeouts, output caps, and a small REST request budget. When a limit is reached, the handler keeps conservative PR-head validation. Discovery never fetches base objects or deepens a shallow checkout. Local bundle import denies all Git transports, including lazy promisor downloads. The push is append-only, so merge an already-provisioned base ref rather than rebasing existing PR commits.

### Bundle prerequisites and history downloads

`create-pull-request` and `push-to-pull-request-branch` do not download missing bundle prerequisites by default. A missing prerequisite fails the handler with guidance to provision the objects in the target checkout. To enable recovery, set `allow-full-history-fetch: true` on the handler:

```yaml wrap
safe-outputs:
  push-to-pull-request-branch:
    allow-full-history-fetch: true
```

**This opt-in permits unbounded downloads**, including `--unshallow` and fetch-by-SHA. Commit depth, an exact SHA, and `blob:none` are not byte limits. Prefer configuring the required refs and history in an authenticated [`checkout:`](/gh-aw/reference/checkout/), using credentials for each target repository. Choose the checkout depth deliberately, since full history can be prohibitively expensive in a large monorepo. A bundle whose prerequisites are already present needs no recovery, including in shallow checkouts.

The setting is independent for each handler and accepts only a literal boolean. It affects only bundle prerequisite recovery. It does not affect baseline discovery, ordinary PR-head fetch and push operations, or other network paths. For `push-to-pull-request-branch`, recovery uses the validated PR head repository and its credentials, including a configured `head-repo` and `head-github-token`. Those fork credentials are scoped to recovery and restored before local import. Recovery authentication failures print fixed diagnostics, never credentials or raw remote errors.

### Cross-repo usage

`push-to-pull-request-branch` can push to PRs in another repository through `target-repo` (and optionally `allowed-repos`). For fork-backed PRs, set `head-repo` to the exact repository that owns the PR head branch. Follow-up pushes are permitted only when the PR head repository matches that value. When `target-repo` is set, the target repository must be checked out into the workspace, either with a `checkout:` entry that sets `path:` or with a custom `steps:` entry that clones it under `$GITHUB_WORKSPACE`. Use `target-repo: "*"` to let the agent choose the repository at runtime. The `safe_outputs` job then checks out all `checkout:` repositories into subdirectories automatically.

```yaml wrap
checkout:
  - fetch-depth: 0                           # checkout current (source) repo
  - repository: org/target-repo
    path: ./target-repo                      # must set path for cross-repo checkout
    github-token: ${{ secrets.CROSS_REPO_PAT }}
    fetch: ["refs/pulls/open/*"]             # fetch all open PR branches

safe-outputs:
  github-token: ${{ secrets.CROSS_REPO_PAT }}
  push-to-pull-request-branch:
    target-repo: "org/target-repo"
    required-title-prefix: "[bot] "
```

The `path:` field is required so the agent knows where the target is mounted. Without it, the checkout writes to the workspace root and overwrites the source repository, which fails the workflow.

See [Cross-Repository Operations](/gh-aw/reference/cross-repository/) for a complete example and for `target-repo`, `allowed-repos`, and authentication. Like `create-pull-request`, pushes do not trigger CI. See [Triggering CI](/gh-aw/reference/triggering-ci/).

### Checkout token for git operations

For same-repository and single-repository cross-repo flows, one checkout credential is persisted into `.git/config` for the handler's git operations. Its precedence is:

1. `create-pull-request.github-token`
2. `push-to-pull-request-branch.github-token`
3. The `safe-outputs.github-app` minted token, when a GitHub App is configured
4. `safe-outputs.github-token`
5. The default `${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}`

Only one token can govern that shared checkout. **If you configure both `create-pull-request` and `push-to-pull-request-branch` for the same repository, give them the same token.** Otherwise the higher-precedence token wins, and the other output's git operations run with a token you did not intend. Set the token once at `safe-outputs.github-token` (or `safe-outputs.github-app`) and let both outputs inherit it.

Fork-backed `create-pull-request` is different. `github-token` manages the upstream PR, and `head-github-token` or `head-github-app` writes the fork branch. If both head credentials are set, `head-github-app` wins and mints a token scoped to the head repository. Prefer least-privilege credentials: `pull-requests: write` (and `issues: write` if needed) on the upstream repository, and `contents: write` only on the automation-owned fork. Do not use the fork credential for upstream PR management, and do not allow follow-up pushes to contributor-owned forks.

:::note
This precedence applies to the handler's git `fetch` and `push`. The GitHub API calls each handler makes still honor that handler's own `github-token`.
:::

### Code-Push Failure Handling

If `push-to-pull-request-branch` or `create-pull-request` fails, processing continues with the remaining safe outputs. Later `add_comment` outputs receive a warning that the code changes were not applied. Failure details appear in the agent failure issue or comment from the conclusion job.

No setting cancels selected safe-output messages when code delivery fails. To gate work on successful delivery, add a custom safe-output job with `needs: safe_outputs` and an `if` such as `needs.safe_outputs.outputs.code_push_failure_count == '0'`.

- **`fallback-as-pull-request`** (default `true`): on a non-fast-forward push failure or an exhausted workflow-permission timeout, opens a fallback PR targeting the original PR branch. Set `false` to disable.
- **`ignore-missing-branch-failure: true`**: if the PR branch was deleted or is missing (for example, after auto-merge), returns `skipped: true` instead of failing.
- **`check-branch-protection: false`**: skips the pre-flight check. By default (`true`), the handler reads the branch's `protected` flag and rejects branches with classic protection or blocking rulesets, such as required reviews or status checks.

The pre-flight check has one exception. A non-default branch passes if its metadata disables classic protection, all of its active rules are non-blocking, and its GraphQL `Ref.refUpdateRule` is `null`. Non-blocking rule types are `copilot_code_review`, `branch_name_pattern`, `commit_message_pattern`, `commit_author_email_pattern`, `committer_email_pattern`, `file_path_restriction`, `file_extension_restriction`, `max_file_path_length`, and `max_file_size`. `required_signatures` is non-blocking when `signed-commits` is enabled. GitHub still enforces signature, naming, and content rules during the push. Any blocking or unknown rule type, missing metadata, incomplete rules list, or failed query keeps the branch blocked. The default branch is always rejected when identified, regardless of `check-branch-protection`.

Rule reads are paged. Branch metadata needs `contents: read`. [Active branch rules](https://docs.github.com/en/rest/repos/rules#get-rules-for-a-branch) require `Metadata: read`. [Classic-protection confirmation](https://docs.github.com/en/graphql/reference/git#ref) uses `refUpdateRule`, which is viewable by non-admins and does not require `Administration: read`. If these reads are unavailable, the branch stays blocked, and no broader credentials are requested. An unprotected branch needs no classic-protection lookup. If the initial branch API returns 403, the check logs a warning and continues, because GitHub still enforces protection at push time.

## Add Reviewer (`add-reviewer:`)

Adds reviewers to pull requests. Use `allowed-reviewers` to restrict to specific GitHub usernames and `allowed-team-reviewers` to restrict to specific team slugs.

```yaml wrap
safe-outputs:
  add-reviewer:
    allowed-reviewers: [user1, copilot]  # restrict to specific user/bot reviewers
    allowed-team-reviewers: [platform-reviewers] # restrict to specific team reviewers
    max: 3                       # max reviewers (default: 3)
    target: "*"                  # "triggering" (default), "*", or number
    target-repo: "owner/repo"    # cross-repository
    required-labels: [automated]     # only add reviewer if PR has ALL these labels
    required-title-prefix: "[bot] "  # only add reviewer if PR title starts with this prefix
    github-token: ${{ secrets.SOME_CUSTOM_TOKEN }} # optional custom token for permissions
```

**Target**: `"triggering"` (requires a PR event), `"*"` (any PR), or a specific number.

Use `allowed-reviewers: [copilot]` to assign the Copilot PR reviewer bot. See [Copilot Cloud Agent](/gh-aw/reference/copilot-cloud-agent/).

## Compile-Time Warnings for `target: "*"`

When `target: "*"` is used, `gh aw compile` warns about two common misconfigurations:

- **Missing wildcard fetch**: no `checkout` block has a wildcard `fetch` pattern such as `fetch: ["*"]`. Without it, the agent cannot access arbitrary PR branches at runtime and fails with permission-like errors.
- **No constraints**: neither `required-title-prefix` nor `required-labels` is set, so any PR in the repository can be pushed to without gating.

Both warnings are suppressed by the recommended configuration:

```yaml wrap
safe-outputs:
  push-to-pull-request-branch:
    target: "*"
    required-title-prefix: "[bot] "
    required-labels: [automated]
checkout:
  fetch: ["*"]
  fetch-depth: 0
```

## Protected Files

`create-pull-request`, `push-to-pull-request-branch`, and `merge-pull-request` enforce protected-file checks by default. These checks prevent supply-chain changes, such as an agent (through error or prompt injection) altering dependency definitions, CI/CD pipelines, or agent behavior files. Creation and push default to requesting review. Merge defaults to blocking.

### What is protected

The following are protected unless explicitly excluded:

- **Dependency manifests and lockfiles**, matched by basename anywhere in the repository (see the table below).
- **Engine instruction files** for the active AI engine (see the table below).
- **Repository security configuration**: `.github/`, `.agents/`, `.githooks/`, and `.husky/`, matched by path prefix. `.github/` covers all workflows, Dependabot config, and other repository-level security settings.
- **Governance files**: `CODEOWNERS` and `DESIGN.md`, matched by basename.
- **Common top-level documentation**: `README.md`, `CONTRIBUTING.md`, `SECURITY.md`, and `CODE_OF_CONDUCT.md`. The PR handlers remove `CHANGELOG.md` from the protected set by default.
- **Top-level dot-folders**: any top-level directory whose name starts with `.`, such as `.cursor/`, `.vscode/`, or `.devcontainer/`, including newly created ones.

| Runtime | Protected files |
|---------|----------------|
| Node.js (npm) | `package.json`, `package-lock.json`, `yarn.lock`, `pnpm-lock.yaml`, `npm-shrinkwrap.json` |
| Node.js (Bun) | `package.json`, `bun.lockb`, `bunfig.toml` |
| Deno | `deno.json`, `deno.jsonc`, `deno.lock` |
| Go | `go.mod`, `go.sum` |
| Python (pip/setuptools) | `requirements.txt`, `Pipfile`, `Pipfile.lock`, `pyproject.toml`, `setup.py`, `setup.cfg` |
| Python (uv) | `pyproject.toml`, `uv.lock` |
| Ruby | `Gemfile`, `Gemfile.lock` |
| Java (Maven) | `pom.xml` |
| Java (Gradle) | `build.gradle`, `build.gradle.kts`, `settings.gradle`, `settings.gradle.kts`, `gradle.properties` |
| Elixir | `mix.exs`, `mix.lock` |
| Haskell | `stack.yaml`, `stack.yaml.lock` |
| .NET | `global.json`, `NuGet.Config`, `Directory.Packages.props` |

| Engine | Protected files | Protected directories |
|--------|----------------|----------------------|
| Copilot (default) | `AGENTS.md` | — |
| Claude | `CLAUDE.md` | `.claude/` |
| Codex | `AGENTS.md` | `.codex/` |

Manifests and governance files are matched by basename only, so they are protected wherever they appear. Path-prefix rules match the full relative path from the repository root.

### Policy Options

The `protected-files` field accepts either a string policy or an object with `policy` and `exclude`. The table below covers creation and push. For `merge-pull-request`, only `blocked` (default) and `allowed` apply. Matching and exclusions are shared.

**String form**: one policy for all protected files.

| Value | Behavior |
|-------|-----------|
| `request-review` (default) | Create the PR and submit a `REQUEST_CHANGES` review listing the protected files. The work is preserved, and a human must approve before merge. The legacy `request_review` spelling is also accepted. |
| `blocked` | Hard-block: the safe output fails with an error. |
| `fallback-to-issue` | Create a review issue with instructions for a human to apply or reject the changes. |
| `allowed` | No restriction. **Use only when the workflow is explicitly designed to manage these files.** |

**Object form**: a policy plus exclusions.

```yaml wrap
safe-outputs:
  create-pull-request:
    protected-files:
      policy: fallback-to-issue   # same values as string form (default: request-review)
      exclude:
        - AGENTS.md               # allow the agent to update its own instruction file
        - CHANGELOG.md            # allow the agent to update the changelog
        - .agents/                # allow updates to the .agents/ directory
        - .cursor/                # allow updates to the .cursor/ directory
```

`exclude` entries match by **basename** (for example, `AGENTS.md`) or **path prefix** (for example, `.agents/`). To exclude only an exact repository path, start the entry with `/`. For example, `"/pyproject.toml"` allows the root file while `lib/foo/pyproject.toml` stays protected. Rooted entries also support globs, such as `"/.changeset/*.md"`. A dot-folder prefix such as `.cursor/` also opts that directory out of the top-level dot-folder rule. The remaining protected files still enforce the policy.

:::tip[Workflows that update top-level Markdown files]
`CHANGELOG.md` is excluded by default. If your workflow is designed to modify another root-level Markdown file, such as `README.md`, add it to `exclude`:

```yaml wrap
safe-outputs:
  create-pull-request:
    protected-files:
      policy: blocked
      exclude:
        - README.md      # this workflow updates the README
```
:::

To set exceptions for every workflow compiled in a repository, use `protected_files.exclude` in `.github/workflows/aw.json`:

```json title=".github/workflows/aw.json"
{
  "protected_files": {
    "exclude": [".changeset/*.md"]
  }
}
```

Repository entries are full repository-root paths. A leading `/` is rejected. `*` matches any characters except `/`, and `**` also matches `/`. Each entry must contain at least one ASCII letter, digit, underscore, or hyphen, and no whitespace. This rejects catch-all patterns such as `"*"`, `"**"`, `"**/*"`, `"/**"`, `"**.*"`, and `".*/**"`. The example above allows Markdown files directly inside `.changeset/`, while `.changeset/config.json`, nested files, and other dot-folders stay protected. Repository exclusions combine with each handler's `exclude` list, apply to both patch and post-apply checks, and never bypass `allowed-files`. Recompile workflows after changing `aw.json`.

**Fallback behavior:**

- **`create-pull-request` with `fallback-to-issue`**: pushing is skipped. A review issue is created with a PR creation intent link, a `[!WARNING]` banner explaining the fallback, and instructions to review carefully before creating the PR.
- **`push-to-pull-request-branch` with `fallback-to-issue`**: nothing is pushed. A review issue is created with the target PR link, patch download and apply instructions, and a review warning.
- Fallback issues get the `pull-request-fallback` label, along with `agentic-workflows`. Updates to an existing PR inherit the PR's expiration, or expire after 7 days if the PR has none. PR creation uses the configured PR expiration, which defaults to 7 days.

When `blocked` triggers, the 🛡️ **Protected Files** section appears in the agent failure issue or comment from the conclusion job. It lists the blocked operation and the files found, with a YAML snippet showing how to configure `protected-files: fallback-to-issue`.

### Parameterizing Policy Fields in Reusable Workflows

`protected-files` and `patch-format` accept GitHub Actions expression strings, so a reusable `workflow_call` workflow can let callers choose the policy without duplicating the file.

```yaml wrap
on:
  workflow_call:
    inputs:
      protected-files-policy:
        type: string
        default: fallback-to-issue
        description: >
          Protected-file policy: 'request-review', 'request_review' (legacy alias), 'blocked', 'fallback-to-issue', or 'allowed'.
      patch-format:
        type: string
        default: bundle
        description: Transport format: 'bundle' (default) or 'am'.
---
safe-outputs:
  push-to-pull-request-branch:
    protected-files: ${{ inputs.protected-files-policy }}
    patch-format: ${{ inputs.patch-format }}

  create-pull-request:
    protected-files: ${{ inputs.protected-files-policy }}
    patch-format: ${{ inputs.patch-format }}
```

Literal values are validated at compile time. Expression values are evaluated by GitHub Actions before the handler runs. They fail closed if they resolve to an unrecognized value. An unrecognized `protected-files` value is treated as `blocked`, the most restrictive option. An unrecognized `patch-format` value causes an explicit error before any git operations. The object form's `policy` also accepts an expression. Its `exclude` entries still apply regardless of policy.

### Restricting Changes to Specific Files with `allowed-files`

`allowed-files` is an **exclusive allowlist**. Every file the patch touches must match at least one pattern, and any other file is refused, including normal source files. It is checked independently of `protected-files`. A file that matches `allowed-files` remains subject to the [protected-file policy](#policy-options) unless a handler or repository exclusion applies.

> [!CAUTION]
> `allowed-files` is not an "additionally allow" list. `allowed-files: [".github/workflows/*"]` blocks all other files, including `src/**`. To allow both, list every pattern explicitly: `allowed-files: [".github/workflows/*", "src/**"]`.

```yaml wrap
safe-outputs:
  push-to-pull-request-branch:
    allowed-files:
      - .changeset/**      # only changeset files may be pushed

  create-pull-request:
    allowed-files:
      - .github/aw/instructions.md  # only this one file may be modified
```

Patterns support `*` (any characters except `/`) and `**` (any characters, including `/`):

| Pattern | Matches |
|---------|---------|
| `go.mod` | Exactly `go.mod` at the repository root (full path comparison) |
| `*.json` | Any JSON file at the root (e.g. `package.json`) |
| `go.*` | `go.mod`, `go.sum`, etc. at the root |
| `.github/**` | All files under `.github/` at any depth |
| `.github/workflows/*.yml` | Only YAML files directly in `.github/workflows/` |
| `**/package.json` | `package.json` at any path depth |

> [!NOTE]
> When `allowed-files` is not set, only the `protected-files` policy applies, and all non-protected files are permitted.

### Allowing Workflow File Changes with `allow-workflows`

Pushing to `.github/workflows/` paths requires the `workflows` permission. This is a **GitHub App-only permission** that `GITHUB_TOKEN` cannot grant. Set `allow-workflows: true` on `create-pull-request` or `push-to-pull-request-branch` to add `workflows: write` to the minted app token. A `safe-outputs.github-app` configuration is required, and the compiler errors if `allow-workflows: true` is set without one.

```yaml wrap
safe-outputs:
  github-app:
    client-id: ${{ vars.APP_ID }}
    private-key: ${{ secrets.APP_PRIVATE_KEY }}
  create-pull-request:
    allow-workflows: true
    allowed-files:
      - ".github/workflows/*.lock.yml"
    protected-files: allowed
```

`allow-workflows` is intentionally explicit rather than inferred from `allowed-files` patterns, so the elevated permission stays visible and auditable in the workflow source.

## Learn More

- [Cross-Repository Operations](/gh-aw/reference/cross-repository/) - Checkout, target-repo, allowed-repos, and fork-authentication rules
- [Safe Outputs](/gh-aw/reference/safe-outputs/) - Complete safe output reference
- [Triggering CI](/gh-aw/reference/triggering-ci/) - How PR-safe-outputs can request follow-up CI
