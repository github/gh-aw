---
description: Full `checkout:` field reference — single/array/disabled checkout, per-entry fields, and the multi-repository allowlist (`multi`, `allowed-repos`, `allow-private-repos`, `max-repos`).
---

# Checkout Configuration (`checkout:`)

Override how the repository is checked out in the agent job. Accepts a single checkout object, an array of checkout objects, or `false`.

## Disabling checkout

Set to `false` to skip automatic checkout entirely (reduces startup time when repo access is not needed):

```yaml
checkout: false
```

Target-only checkout for sidecar/MultiRepoOps workflows: set `permissions.contents: none` to skip only the automatic workflow-repository checkout while still checking out other explicitly configured `checkout:` entries (e.g. a target repository). Unlike `checkout: false`, additional checkout entries are unaffected:

```yaml
permissions:
  contents: none
checkout:
  - repository: octo-org/target-repository
    path: target
    github-token: ${{ secrets.TARGET_REPO_PAT }}
```

## Single and multiple checkouts

Single checkout (object):

```yaml
checkout:
  fetch-depth: 0                       # Fetch full history (default: 1 = shallow clone)
  github-token: ${{ secrets.MY_PAT }}  # Override token for private repos
```

Multiple checkouts (array):

```yaml
checkout:
  - path: .
    fetch-depth: 0
  - repository: owner/other-repo
    path: ./libs/other
    ref: main
```

## Per-entry fields

- `repository:` / `repo:` (alias) - Repository in `owner/repo` format (defaults to current repository). Without `multi: true`, must resolve to exactly one repository; wildcards are not supported.
- `ref:` - Branch, tag, or SHA to check out (defaults to the triggering ref)
- `path:` - Relative path within `GITHUB_WORKSPACE` (defaults to workspace root). In `multi: true` mode this is a literal prefix: repositories land under `prefix/owner/repo`.
- `fetch-depth:` - Number of commits to fetch; `0` = full history, `1` = shallow (default). When multiple configs target the same path, the deepest value wins.
- `fetch:` - Additional Git refs to fetch after checkout (string or array of patterns)
  - `"*"` - fetch all remote branches
  - `"refs/pulls/open/*"` - all open pull-request refs
  - Branch names, glob patterns (e.g., `"feature/*"`)
  - Each pattern is checked and fetched independently for static and runtime-selected repositories. Unmatched patterns are skipped; authentication, network, and other discovery or fetch errors remain fatal. For example, `fetch: ["main", "my-prefix/*"]` still fetches `main` when no branches match `my-prefix/*`.
  - Discovery and fetch use the checkout's token without persisting credentials; fetches preserve the effective `fetch-depth` (`1` by default, `0` for full history).
- `sparse-checkout:` - Newline-separated glob patterns for sparse checkout; merged when multiple configs target the same path
- `submodules:` - Submodule handling: `"recursive"`, `"true"`, or `"false"`. `multi: true` checkouts require `false` or omission — submodule URLs can otherwise bypass the allowlist.
- `lfs:` - Download Git LFS objects (boolean, default: `false`). `multi: true` checkouts reject `lfs: true` for the same allowlist-bypass reason.
- `wiki:` - Check out the repository's wiki (boolean, default: `false`). When `true`, appends `.wiki` to the repository name.
- `github-token:` - Token for authentication (`${{ secrets.MY_PAT }}`); credentials are always removed after checkout (`persist-credentials: false` is enforced). `token:` is a deprecated alias.
- `github-app:` - GitHub App authentication (mints a short-lived installation token via `actions/create-github-app-token`); mutually exclusive with `github-token`
- `safe-outputs-github-app:` - GitHub App used only by safe-output checkout/fetch/push operations for this target; does not change activation/agent checkout authentication
- `current:` - Marks this checkout as the logical current/primary repository for the workflow (only one entry may set this)
- `pull-request:` - Set `false` to disable the compiler-generated PR branch checkout and its base-config snapshot/restore steps
- `force-clean-git-credentials:` - Persist credentials during checkout, then run a post-checkout cleanup step that removes them from root and submodule git configs

## Multi-repository checkout (`multi: true`)

Runtime-expanded repository lists (e.g. a newline-separated `repository:`/`repo:` value, or a dynamic expression) require explicit opt-in — without `multi: true`, `repository`/`repo` must resolve to exactly one `owner/repo` slug and ordinary single-checkout behavior applies.

```yaml
checkout:
  - repository: ${{ github.event.inputs.repos }}   # newline-separated list
    multi: true
    allowed-repos: ["myorg/*"]                      # required allowlist
    path: workspace
```

- `multi:` (boolean, default `false`) - Explicitly enables runtime repository-list expansion and its allowlist, visibility, path, and secondary-fetch restrictions. Requires `allowed-repos`.
- `allowed-repos:` - Required allowlist when `multi: true` is set; does not enable multi mode by itself. Accepts a newline-separated string, an array, or a whole GitHub Actions expression referencing a repository or organization configuration variable (e.g. `${{ vars.ALLOWED_REPOS }}`). Supports scoped wildcard patterns such as `owner/*` or `*/repo`, but not unrestricted wildcards such as `*` or `*/*`. Caller-controlled contexts, dynamic indexing, and composite/mixed expressions are rejected.
- `allow-private-repos:` (boolean, default `false`) - With `multi: true`, allows private or internal targets only when the hosting workflow repository is itself private or internal. Public hosting repositories always reject non-public targets, even when `true`. Every merged multi entry must opt in explicitly. Has no effect in single-repository mode.
- `max-repos:` (integer, default `10`) - Maximum number of unique repositories per `multi: true` checkout entry. Oversized lists fail before any metadata request or checkout; matching and deduplication are case-insensitive. Merged multi entries use the smallest effective limit, including the default for omitted values. Has no effect in single-repository mode.

See [network.md](network.md) for firewall/egress interactions and [workflow-constraints.md](workflow-constraints.md) for other job-level security boundaries.
