---
name: checkout-credential-review
description: Review code that performs git or gh operations against repository checkouts in gh-aw, checking that the right credentials are available at the right time and that sparseness, shallowness and credential-free factors are properly considered.
---

# Checkout Credential Review

Use this skill when reviewing or writing code in `pkg/workflow/`, `actions/setup/js/`, or compiled `.lock.yml` workflows that runs `git`, `gh`, or any other remote-touching operation against a repository checkout.

## Background

Each entry in a workflow's `checkout:` block may declare its own credentials (`github-token:`, `github-app:`), and the compiler wires those into the corresponding `actions/checkout` step ([pkg/workflow/checkout_step_generator.go](../../../pkg/workflow/checkout_step_generator.go)). Generated checkouts always set `persist-credentials: false`, so the on-disk repo retains **no** credentials after the step finishes — only `actions/checkout`'s own internal token is used during the clone, and it is scrubbed in its post-step.

A separate step that wants to authenticate later must either (a) re-inject a token at command level (e.g. `git -c http.extraheader=...`) or (b) be passed the per-checkout token via env. The compiler does *not* automatically thread per-checkout `github-token`s into downstream steps.

Two important contexts deliberately run with **no git credentials**:

- The **safe-outputs MCP server** and its handlers (`generate_git_bundle.cjs`, `generate_git_patch.cjs`, `create_pull_request.cjs`). Errors in these paths explicitly say "the safe-outputs MCP server has no credentials for private repositories" — fetch/push will fail for private repos.
- The **agent runtime** after `actions/checkout`. The agent prompt in [actions/setup/md/safe_outputs_push_to_pr_branch.md](../../../actions/setup/md/safe_outputs_push_to_pr_branch.md) explicitly tells the model not to attempt `git fetch`, `git pull`, `git push`, or any other authenticated git operation, and to report unavailable branches rather than try to fetch them.

Expression-valued `checkout: { dynamic: ..., allowed-repos: ... }` entries are checked out at runtime by [actions/setup/js/dynamic_checkouts.cjs](../../../actions/setup/js/dynamic_checkouts.cjs) rather than `actions/checkout`. The compiler resolves the expression once per job and passes it, plus the resolved `allowed-repos` allowlist, through `GH_AW_DYNAMIC_CHECKOUTS` / `GH_AW_DYNAMIC_CHECKOUT_ALLOWED_REPOS`. Each dynamic checkout uses a command-level token (`GH_AW_DYNAMIC_CHECKOUT_TOKEN`, falling back to `GH_TOKEN`) injected only into that git invocation; the runtime does not persist git credentials afterward unless the caller explicitly asks it to keep them (`GH_AW_DYNAMIC_CHECKOUT_PERSIST_CREDENTIALS=true`), which only the `safe_outputs` job's PR/push checkout path does.

## Review checklist

When you see a new `git`, `gh`, `execFileSync('git'…)`, or compiled `run:` block:

1. **Does it touch a remote?** Local-only commands (`symbolic-ref`, `rev-parse`, `log`, `show`, `merge-base`, `diff`, `status`) need no credentials. Anything in `fetch | pull | push | clone | ls-remote | remote (set-url|add|update)` does, plus on-demand blob fetches in partial clones.
2. **Which checkout is it operating on?** If it's a cross-repo entry from `checkout:`, the relevant credential is *that entry's* `github-token`, not the workflow's default `GITHUB_TOKEN`. Confirm the per-entry token is actually threaded into the step's env (or refuse to do remote operations and degrade gracefully).
3. **Which job/context emits it?** Agent job and safe-outputs MCP server both run without git credentials by design. Any remote git operation there must be wrapped in `try/catch`, fail soft, and surface a clear "no credentials" error rather than a raw git stderr.
4. **Sparse / shallow / monorepo concerns.** Avoid emitting steps that deepen (`git fetch --unshallow`, `--deepen=N`) or widen (`git fetch origin '+refs/heads/*'`) a sparse or shallow checkout of a large monorepo — these need credentials *and* can pull hundreds of MB. Prefer expanding `fetch:` / `fetch-depth:` / `sparse-checkout:` at compile time so it happens during `actions/checkout` with its internal token, never later.
5. **`gh` is REST, not git.** `gh api …` uses whatever `GH_TOKEN` is in the step's env — it does **not** automatically inherit per-checkout PATs. For cross-org private repos, either thread the right token in or accept the call will 404 and handle it.
6. **Dynamic checkout expressions.** A `checkout.dynamic` expression MUST NOT reference `secrets.*` directly — its resolved value lands in the single `GH_AW_DYNAMIC_CHECKOUTS` JSON payload rather than a statically declared env var, hiding the secret from static analysis; the fix is a top-level `env:` entry referenced via `env.NAME`. It also MUST NOT reference `steps.*`, since the same expression is re-evaluated independently in the agent job and the `safe_outputs` job. Any new field or code path added to `dynamic_checkouts.cjs` must keep passing `ref`/sparse-checkout patterns to `git` after a `--` terminator, keep `GIT_LFS_SKIP_SMUDGE=1` on non-LFS operations, and keep validating that checkout paths cannot escape the workspace via `..`, absolute paths, or symlinks.

## Related

- [docs/src/content/docs/reference/checkout.md](../../../docs/src/content/docs/reference/checkout.md) — "Git Credentials After Checkout"
- [docs/sparseness.md](../../../docs/sparseness.md) — sparse/blobless credential lifecycle
- [pkg/workflow/checkout_step_generator.go](../../../pkg/workflow/checkout_step_generator.go) — token wiring per checkout
- [actions/setup/md/safe_outputs_push_to_pr_branch.md](../../../actions/setup/md/safe_outputs_push_to_pr_branch.md) — agent-facing guidance
- [actions/setup/js/dynamic_checkouts.cjs](../../../actions/setup/js/dynamic_checkouts.cjs) — dynamic checkout runtime credential/path/git-arg handling
- [pkg/workflow/dynamic_checkout_context_validation.go](../../../pkg/workflow/dynamic_checkout_context_validation.go) and [pkg/workflow/dynamic_checkout_secrets_validation.go](../../../pkg/workflow/dynamic_checkout_secrets_validation.go) — compile-time `steps.*`/`secrets.*` rejection
- [docs/src/content/docs/specs/checkout-behavior-specification.md](../../../docs/src/content/docs/specs/checkout-behavior-specification.md) — §3.6 Dynamic Checkout Sets normative requirements
