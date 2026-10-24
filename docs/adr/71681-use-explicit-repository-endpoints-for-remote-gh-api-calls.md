# ADR-71681: Use Explicit Repository Endpoints for Remote `gh api` Calls

**Date**: 2026-10-24
**Status**: Accepted
**Deciders**: Not recorded

---

### Context

`gh aw experiments list/analyze --repo owner/repo` reads experiment branches, workflow files, and state/eval files from a remote repository by shelling out to `gh api`. The affected code combined `repos/{owner}/{repo}/...` placeholders with `--repo`, which `gh api` does not support; without an explicit endpoint, placeholder resolution also depends on the ambient checkout or `GH_REPO`. The same defect affected the shared branch file reader (`readRemoteRepoBranchFileContext`) used for state/eval reads. Repository identity and host selection are separate: `owner/repo` fixes the repository path but leaves host selection to `gh`, whereas a host-qualified repository or explicit hostname selects a host. Branch names and file paths can contain characters such as `#`, spaces, `+`, and `{}`, and path validation must prevent traversal from changing the intended repository endpoint.

### Decision

Use `repositoryAPIArgs` for the experiment fetches in `pkg/cli/experiments_fetch.go` and the shared branch file reader, constructing explicit `repos/<owner>/<repo>/<endpoint>` paths rather than relying on repository placeholders or unsupported `gh api --repo`. Validate caller-supplied paths before invoking `gh`, reject traversal and absolute file paths, and escape repository components and dynamic path segments; contents requests use `--method GET --raw-field ref=...` to preserve literal refs without changing the HTTP method. Host precedence is an explicit hostname, then the repository's host prefix, then `gh`'s ambient host selection: preserve an unknown host rather than defaulting it to github.com, and retain an explicitly selected github.com host even when `GH_HOST` differs, including subsequent branch commit metadata and artifact reads. This bounded change retains the existing `workflow.ExecGH` transport and authentication behavior; it does not mandate migrating other API call sites to this helper.

### Alternatives Considered

#### Alternative 1: Keep `{owner}/{repo}` placeholders and set `GH_REPO` per command

After removing the unsupported `--repo` flag, setting `GH_REPO` per subprocess could supply the repository used for placeholder resolution with little endpoint rewriting. This is viable, but retains implicit environment-based repository selection and requires careful environment propagation at each call site. Explicit endpoints make repository selection visible in the arguments; either approach still needs deliberate host selection, path validation, and literal ref handling.

#### Alternative 2: Call the GitHub REST API directly via a Go client library

Using a Go HTTP/REST client (for example, go-gh's REST client) would avoid CLI argument construction and make request parameters explicit. It was not chosen because the affected code already uses `workflow.ExecGH`, including `gh` authentication, host configuration, and `--paginate`/`--jq` behavior. Replacing that transport would expand this fix and require translating response filtering and pagination; a client library would still need correct repository, host, and path handling.

### Consequences

#### Positive
- Explicit repository paths remove dependence on the checkout and `GH_REPO` for repository identity; explicit hosts override ambient `GH_HOST` without disabling ambient host selection for unqualified repositories.
- Shared endpoint validation, repository normalization, and host handling reduce duplicated argument construction in the affected fetches.
- Validated, escaped paths prevent endpoint traversal, while literal refs preserve special characters without placeholder expansion.

#### Negative
- Host selection must remain consistent across downstream reads; treating an explicitly selected github.com host as unspecified would reintroduce ambient-host leakage.
- The helper and its callers must coordinate validation and escaping because they construct endpoint strings rather than typed REST requests.
- Tests are tightly coupled to exact `gh` argument vectors, so harmless argument reordering will require test updates.

#### Neutral
- Pagination, legacy `state.json` fallback, and local (non-API) mode behaviour are preserved.
- The helper remains package-private in `pkg/cli/branch_file_reader.go`; this decision does not establish a repository-wide API abstraction or claim that other API call sites have been audited or migrated.
- This record finalizes the scoped implementation decision, not approval to merge the pull request.
