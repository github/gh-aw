# ADR-72418: Isolated Subprocess Workers and Skip-by-Default Deduplication for Concurrent Org and Enterprise Rollouts

**Date**: 2026-07-14
**Status**: Draft
**Deciders**: pelikhan

---

### Context

`gh aw update --org` and `gh aw upgrade --org` previously processed target repositories through the shared org runner one at a time, closing existing marker-tagged PRs before creating new ones (see [ADR-41580](41580-marker-based-deduplication-for-org-runner-items.md)). Administrators now need to roll updates out across many repositories — including every organization in an enterprise — interactively, with bounded concurrency, without creating duplicate PRs and without exhausting GitHub API quotas. The existing per-repository apply pipeline changes the process-wide working directory, so it cannot safely run concurrently inside a single Go process. GitHub code search also caps results at 1,000 files, so search-based discovery silently truncates large organizations.

### Decision

We will run each repository rollout as an isolated child process of the current `gh aw` executable (`os.Executable()`), forwarding the original command flags plus `--repo`, `--dry-run`/`--create-pull-request`, `--replace-existing`, and a shared `--rollout-cache-dir`. A parent scheduler admits workers through a bounded semaphore (`--concurrency`), paces worker starts, rechecks the core rate-limit budget at most every 30 seconds, and stops admitting work when a worker reports HTTP 401/429 or rate-limit errors. Workers report back via a single prefixed JSON line (`gh-aw-rollout-result:`) on stdout and run in their own process group so cancellation kills `git`/`gh` descendants. Existing rollout PRs (matched by marker or legacy title/branch, including drafts and older versions) are discovered in batched GraphQL queries and **skipped by default**; with `--replace-existing`, old PRs are closed only after the replacement PR is successfully created, and each worker rechecks for existing PRs immediately before applying changes.

### Alternatives Considered

#### Alternative 1: In-Process Goroutine Workers

Running each repository as a goroutine would avoid process spawn overhead and allow direct structured results instead of parsing stdout. It was rejected because the existing apply pipeline mutates the process-wide working directory (and other global state), so concurrent goroutines would corrupt each other; refactoring the entire update/upgrade pipeline to be directory-independent would be a much larger and riskier change.

#### Alternative 2: Keep Sequential Rollout with Close-Before-Create Deduplication

Extending the existing sequential org runner (ADR-41580) with enterprise discovery and a TUI would be simpler and need no IPC protocol. It was rejected because sequential processing does not scale to enterprise-wide rollouts, and closing old PRs before creating replacements can leave a repository with no open rollout PR if creation fails.

#### Alternative 3: Search-Only Repository Discovery

Continuing to rely solely on code search for target discovery minimizes API calls for small organizations. It was rejected as the only mechanism because the 1,000-file ceiling silently truncates results; the decision keeps search but falls back to paginated `/orgs/{org}/repos` inventory when the ceiling is hit.

### Consequences

#### Positive
- Bounded, paced concurrency makes large org and enterprise rollouts practical while protecting API quotas.
- Process isolation sidesteps the global working-directory problem without refactoring the apply pipeline, and process groups give reliable cancellation.
- Skip-by-default plus create-then-close replacement prevents duplicate PRs and avoids windows with no open rollout PR.
- Repository discovery no longer silently truncates at the code-search ceiling.

#### Negative
- Results cross a stdout text protocol (`gh-aw-rollout-result:` prefix), which is more fragile than typed in-process results and depends on worker output discipline.
- Each worker pays process startup and authentication overhead; rate-limit detection relies on string-matching error text.
- Deduplication is not an atomic distributed lock: separate simultaneous CLI invocations can still race and create duplicate PRs.
- Changes the deduplication semantics established in ADR-41580 (close-before-create) for the rollout path, so two behaviors now coexist (legacy issue creation still uses the old org runner).

#### Neutral
- A temporary shared dependency-read cache is created under the git root's gitignored target directory and removed after the run.
- Dry-run becomes the default when neither `--create-pull-request` nor `--create-issue` is set; unattended non-interactive creation in CI requires `--yes`.
- Process-group termination is Unix-specific; other platforms use a no-op process configuration.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
