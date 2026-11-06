# ADR-75446: Auto-select rootless installation without passwordless sudo

**Date**: 2026-11-06
**Status**: Proposed
**Deciders**: Maintainers reviewing [PR #75446](https://github.com/github/gh-aw/pull/75446)

## Context

Copilot installation previously selected rootless mode explicitly for ARC/DinD,
but native self-hosted runners without passwordless sudo still attempted
privileged operations. A custom primary-agent command does not remove the
external threat-detection job's requirement for the standard Copilot executable.
Both installers need consistent privilege selection without interactive prompts
or changes to their release-integrity checks.

## Decision

Share privilege selection through `actions/setup/sh/install_privileges.sh`.
An explicit `--rootless` takes precedence. Otherwise, root installs directly,
a non-root runner with a successful `sudo -n true` probe uses `sudo -n` for
privileged operations, and other runners automatically select rootless mode.
Rootless installation uses `$HOME/.local/bin`, verifies that the directory is
writable, and exposes the selected executable through `$GITHUB_PATH`.

Retain the existing version selection, checksum verification, and trusted
Copilot toolcache policy. External Copilot threat detection continues to install
the standard executable independently of a primary agent's custom command.
An arbitrary preinstalled executable on `PATH` does not bypass installation.

Rootless installation cannot repair state owned by another user. If creation
or ownership repair of `$HOME/.copilot` fails, stop before installation with the
affected path, runner UID/GID, and administrator remediation. Failed cleanup of
stale `/tmp/awf-*-chroot-home` or `/tmp/awf-chroot-*` directories warns explicitly
and retains best-effort behavior; administrators must remove inaccessible stale
state before reusing the runner. This decision does not expand the cleanup scope.

## Alternatives considered

**Require explicit topology or `--rootless` configuration.** This leaves native
no-sudo runners dependent on configuration unrelated to their actual privileges.
Runtime detection supports those runners while preserving explicit overrides.

**Always install rootless.** This would change the installation location for
root and passwordless-sudo runners unnecessarily. Preserve their existing
`/usr/local/bin` installation behavior.

**Skip installation when an executable exists on `PATH`.** This would bypass
the existing version and integrity policy and would not guarantee that external
threat detection receives the standard executable it requires.

## Consequences

Native no-sudo runners no longer require a topology setting to select a writable
installation location. Privilege probes and subsequent sudo calls are
non-interactive, and both installers share one policy.

The fallback still requires writable runner-owned home state. It does not
promise automatic recovery from earlier privileged AWF runs; failures now
identify the administrative repair needed.

User-local installation does not itself establish a sandbox trust boundary.
Sandbox mounts and subsequent host command lookup need separate protection;
installation-time checksum verification does not protect an executable from
later sandbox modification. This privilege-selection decision does not resolve
that separate review finding.
