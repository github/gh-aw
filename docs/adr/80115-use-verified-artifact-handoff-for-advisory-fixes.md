# ADR-80115: Use Verified Artifact Handoff for Advisory Fixes

**Date**: 2026-11-22
**Status**: Draft
**Deciders**: pelikhan (PR author); [TODO: verify other reviewers]

---

### Context

Maintainers sometimes need to fix an unpublished security advisory. GitHub hosts that work in a temporary private fork, but it does not run GitHub Actions or PR status checks there, so the fix cannot be built or tested by the usual CI path. The fix therefore has to be prepared and tested in a separate private side repository that the maintainer controls. The advisory details must stay confidential, the side repository must not be able to write to the upstream repository, and the patch that reaches the advisory fork must be exactly the reviewed patch, applied to the exact tested base commit.

### Decision

We will hand off advisory fixes as a bounded text patch, not as a live branch or an automatic pull request. A manually dispatched workflow in the private side repository produces a short-retention artifact containing `handoff/fix.patch` and review notes. A new `gh aw advisory inspect` command verifies the source run, repository, artifact, and GitHub SHA-256 digest, then shows the patch and an approval digest. `gh aw advisory transfer` requires that digest, checks the base commit and fork identity, and pushes only a new branch to the existing temporary private fork. The primary driver is confidentiality with maintainer control: the command never creates a fork, merges changes, opens a PR, or publishes the advisory.

### Alternatives Considered

#### Alternative 1: Run the fix workflow inside the advisory fork

Run the build, tests, and any agent work directly in the temporary private advisory fork. This was considered because it would keep the fix in the same place as the advisory. It was rejected because GitHub does not run Actions or PR status checks in these forks, and running agent workflows there would also put workflow permissions inside the advisory boundary.

#### Alternative 2: Reuse the generic `gh aw pr transfer` command

Move a pull request's changes to another repository with the existing transfer command. This was considered because the command already exists and handles PR-based handoffs. It was rejected because it can create a normal fork and open a public-facing PR, which would leave the private advisory scope.

#### Alternative 3: Copy the patch and notes manually

Have the maintainer download the artifact or copy the diff and test notes by hand into the advisory fork. This was the closest alternative because it needs no new CLI code, and the new `--patch` mode keeps a local, separately verified diff path for that case. It was not chosen as the default because it provides no binding between the run, artifact digest, base commit, and destination. It also gives no check against base drift, workflow file changes, or overwriting an existing branch, and it depends on manual copying of confidential content.

### Consequences

#### Positive
- Fix validation runs in the private side repository, where Actions can run, and the maintainer receives a reviewed patch rather than an unverified branch.
- Transfer checks the artifact's source run, digest, base commit, and fork identity before any write. It refuses changes under `.github/workflows/` or `.github/actions/`, and it refuses to overwrite an existing branch.
- Confidential patch content leaves the private boundary only after explicit maintainer approval, and no public PR is opened automatically.

#### Negative
- The change adds roughly 500 lines of Go in `pkg/cli` that shell out to `git` and `gh`. It depends on GitHub API behavior for advisory `private_fork` metadata and artifact digests, and the code has a special case for `fork:false` on temporary advisory forks.
- The maintainer still has several manual steps: inspect, record the digest, run transfer, and open the PR in the GitHub UI. This is slower than full automation.
- The approval digest shows that the patch and destination did not change after review. It does not show that the fix is correct, and the test notes are agent output rather than an independent attestation.
- Live authentication and a push against a disposable advisory fork have not been verified, according to the PR's validation notes.

#### Neutral
- The command is registered as a top-level `gh aw advisory` subcommand in the utilities group, with `inspect` and `transfer` subcommands.
- Documentation adds a side-repository workflow example to `docs/src/content/docs/patterns/multi-repo-ops.md` and command reference to `docs/src/content/docs/setup/cli.md`. The example is opt-in through `"maintenance": false` in the side repository's `.github/aw.json`.
- Patches are capped at 4 MiB and artifacts at 8 MiB. Binary patches are rejected, so fixes that depend on binary files must be handled outside this command.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
