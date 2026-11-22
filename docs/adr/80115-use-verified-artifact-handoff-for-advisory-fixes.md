# ADR-80115: Use Verified Artifact Handoff for Advisory Fixes

**Date**: 2026-11-22

**Status**: Draft

**Deciders**: [TODO: verify]

## Context

GitHub does not run Actions or PR status checks in temporary private advisory forks. A dedicated private side repository can prepare and test an unpublished advisory fix without giving Actions write access to the upstream repository or access to advisory fork credentials. The handoff must preserve confidentiality and let the maintainer approve the exact patch, tested base, and destination.

## Decision

Use a bounded text patch rather than a live branch or an automatically opened PR. A trusted manual workflow in the private side repository checks out the pinned upstream commit with read-only access and produces a short-retention artifact containing `handoff/fix.patch` and review notes.

The experimental `gh aw advisory inspect` command verifies that the artifact belongs to the successful first attempt of a manual run in the specified private repository and that the downloaded archive matches GitHub's SHA-256 digest. It resolves the existing temporary private fork from the unpublished advisory and displays handoff metadata and an approval digest. Raw diff output requires `--print-patch`, terminal stdin and stdout, and an exact `yes` response. This prevents ordinary pipe or file capture but does not prove a human is present; display markers do not make the contents trusted. The digest binds the patch bytes, source identifiers, base, fork identity, and destination branch.

If the advisory does not already have a temporary private fork, the maintainer can explicitly request one with `gh aw advisory record --create-fork`. The command verifies the source artifact before requesting the fork and waits for GitHub to confirm the advisory association before saving the record.

`gh aw advisory transfer` repeats the handoff checks, requires the matching digest, checks the fork's base branch against the tested commit, and applies the patch in an isolated Git index. It uses the maintainer's Git credentials to create a branch without overwriting an existing one. It does not create a fork, open a PR, merge changes, or publish the advisory. The maintainer opens the PR from the advisory in GitHub.

## Alternatives Considered

- **Run the workflow in the advisory fork.** This would keep the fix and CI in the same repository, but Actions and PR status checks are unavailable there.
- **Reuse `gh aw pr transfer`.** The general-purpose command can create a normal fork and open a PR. Those behaviors do not fit a handoff restricted to an existing temporary private advisory fork.
- **Copy a patch manually.** This needs no artifact verification machinery but leaves the maintainer to check the patch's origin, base, and destination. The CLI retains `--patch` for a separately verified local diff. That mode skips artifact provenance checks but still requires approval and validates the fork, base, and branch.

## Consequences

- Actions can prepare and test the fix without write access to either upstream or the advisory fork. The maintainer must restrict side-repository access, including access to logs and artifacts.
- Transfer checks for base drift before and after pushing, blocks changes under `.github` regardless of case, and refuses to overwrite a destination branch. A post-push check can fail after branch creation, so the maintainer must check for the branch before retrying. The patch is limited to 4 MiB and cannot contain binary changes; the artifact archive is limited to 8 MiB.
- Maintainers must review the diff and test notes, approve the digest, run the transfer, and open the PR. The digest binds the handoff to what was inspected; it does not attest that the fix is correct or that tests passed.
- Artifact handoff depends on the GitHub API for run provenance, artifact digests, and the advisory's `private_fork` association. Live collaborator authentication and push to a disposable GitHub advisory fork remain unverified.
