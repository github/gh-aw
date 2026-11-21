---
title: Private Clone
description: Configure a disposable private remediation repository with operator-supplied workflows and offline security context.
---

`gh aw private-clone` creates a standalone private copy of a public or private
repository for confidential security fixes. The copy is also the side repository:
it hosts the supplied agentic workflows, private remediation PRs, and run evidence.
It is not a GitHub fork or an advisory-linked temporary fork.

```bash
gh aw private-clone acme/project security/project-fix-42 \
  --workflows ./fix-workflows \
  --starter-workflow investigate \
  --alert 42 \
  --advisory GHSA-2345-6789-cfgh \
  --ref main
```

## Arguments and options

Both positional arguments are explicit `owner/repo` slugs on the selected GitHub
host. The destination must be new; existing repositories are never reused.

| Option | Meaning |
|---|---|
| `--workflows DIR` | Required local directory of operator-authored workflow Markdown and supporting local imports |
| `--starter-workflow NAME` | Top-level workflow (`investigate` or `investigate.md`) to dispatch after activating Actions; it must declare `workflow_dispatch`. Defaults to the only top-level workflow; required when the bundle has several |
| `--secret NAME` | Repository secret to set from the operator's same-named environment variable; repeatable. Every secret referenced by the bundle must be supplied unless `--no-start` is used |
| `--no-start` | Leave Actions disabled and dispatch nothing so the repository can be reviewed first |
| `--ref REF` | Source branch, tag, or commit; defaults to `HEAD` and resolves to an immutable SHA |
| `--alert NUMBER` | Selected code scanning alert; repeatable or comma-separated |
| `--advisory GHSA-ID` | Selected repository advisory; repeatable or comma-separated |

At least one alert or advisory is required. The local operator needs source
contents access, code scanning/advisory read access for the selected resources,
and permission to create and configure the destination repository, push its
workflow files, set repository secrets, enable Actions, and dispatch the starter.
Download failures stop setup rather than omitting context. Starter, secret, and
compilation problems are reported before the destination repository is created.
Run setup locally or in a trusted private execution environment: local compiler
diagnostics can include operator workflow content.

## Workflow bundle

Supply a directory such as:

```text
fix-workflows/
  fix.md
  review.md
  shared/
    conventions.md
```

Top-level Markdown files are compiled as workflows. Imports must resolve inside
the supplied directory; remote imports, symlinks, precompiled locks, and top-level
native Actions YAML are rejected. Keep supporting Markdown in subdirectories.
The command supplies no fix-generation workflow or prompt template.

Each root workflow receives a short pointer to the local context index. Root
workflows whose own frontmatter selects the Copilot engine (including the
default) also receive `permissions.copilot-requests: write` unless they declare
`copilot-requests` explicitly. Downloaded alert and advisory files are
re-encoded JSON with API links and dismissal comments removed. GitHub tools, including local imports, are
restricted to `allowed-repos: "${{ github.repository }}"`. Explicit external
checkout/output targets and external GitHub token/App configurations are
rejected. The starter workflow must declare `workflow_dispatch`;
`gh aw private-clone` dispatches its compiled workflow on `main` after enabling
Actions. Supply any required supporting scripts within the bundle and invoke
them through their installed paths under `.github/workflows/`.

When GitHub tools have no explicit `min-integrity`, setup supplies `approved`.

The workflows must reason from the downloaded files rather than reading upstream
alerts, advisories, or links. Missing evidence requires a new operator-prepared
snapshot. The supplied workflows are trusted operator code; static scope checks
do not prove arbitrary shell commands are safe or prevent anonymous internet
access to public upstream source.

## Downloaded context layout

```text
.github/private-clone/
  README.md
  manifest.json
  code-scanning/
    42.json
    42.instances.json
  advisories/
    GHSA-2345-6789-cfgh.json
```

The manifest records the source and private repository, exact source SHA,
download timestamp, downloaded file paths, additional referenced secret names,
and bootstrap ref. Alert instances are downloaded across all API pages into one
JSON array. Advisory files contain the selected advisory API responses.

Advisory comments, attachments, SARIF traces, and linked external resources are
not implicitly downloaded. Alert/advisory snapshots are not guaranteed to match
the selected source revision; workflows must establish applicability and stop
when evidence is insufficient. Downloaded text is untrusted data, not prompt
instructions.

## Isolation and activation

Setup fetches source locally, removes inherited workflows, installs and compiles
the supplied bundle, and creates a bootstrap commit. The destination is verified
private, standalone, and Actions-disabled before any source or confidential
context is pushed. Only the `main` branch and bootstrap tag are pushed, not an
upstream mirror. Original secrets, integrations, and git remotes are not copied.
The clone's default workflow token remains read-only; clone-local PR creation is
enabled, subject to organization policy.

After the source and context are pushed, the command sets the `--secret` values
on the new repository, enables Actions, and dispatches the starter workflow to
begin the investigation. Secret values are read from the operator's environment
and passed to `gh secret set` on stdin, never as arguments. Copilot inference uses
the clone's Actions token via `copilot-requests: write`; no upstream PAT or
Copilot PAT is provisioned. With `--no-start`, Actions stays disabled and no
workflow is dispatched; enable Actions and run the starter after review.

> [!CAUTION]
> This command activates Actions and immediately runs trusted, operator-supplied
> workflow code; other enabled triggers in the bundle may also run. Review the
> bundle and check organization-wide secrets, installed Apps, shared runner
> credentials, and private dependency credentials before invoking it. Do not
> grant the clone upstream repository access. Repository creation does not revoke
> ambient organization grants; use an isolated owner when the upstream
> organization cannot provide an appropriate security boundary.

Failed publication may leave an empty or partially configured private repository;
errors after the push state whether secrets were set and whether Actions is
enabled. The command does not delete repositories automatically. It dispatches only the
selected starter workflow; it does not publish changes or mark an upstream alert
fixed.

## Operator flow

Prepare a bundle directory, for example `fix-workflows/investigate.md`, with a
starter workflow that declares `workflow_dispatch` and reads the offline context:

```aw wrap
---
on:
  workflow_dispatch:
permissions:
  contents: read
engine: copilot
safe-outputs:
  create-pull-request:
    draft: true
---

# Investigate private security findings

Read .github/private-clone/README.md and the files it lists. For each selected
alert or advisory, confirm whether it applies to this revision, find the root
cause, and make the smallest safe fix with a regression test. Open one draft
pull request that explains the evidence. If the evidence is insufficient, stop
and explain what is missing instead of changing code.
```

Then run the end-to-end flow:

1. Pick the code scanning alerts or advisories to fix and a new private
   destination, preferably under an isolated owner.
2. Export a value for each secret the bundle references (none for the example
   above), then run the command:

   ```bash
   gh aw private-clone acme/project security/project-fix-42 \
     --workflows ./fix-workflows --alert 42
   ```

3. Watch the investigation with `gh run list --repo security/project-fix-42`
   or `gh run watch --repo security/project-fix-42`.
4. Review the draft pull request in the private repository and iterate there.
5. Export only the reviewed delta after `private-clone/bootstrap`, as described
   below.

Use `--no-start` to inspect the pushed repository first; then set secrets,
enable Actions, and run `gh workflow run investigate.lock.yml --repo DEST`.

## Export boundary

`refs/tags/private-clone/bootstrap` identifies the commit containing the supplied
automation and downloaded context. Export only the reviewed delta from this
commit to the approved fix SHA. Do not export bootstrap changes or the confidential
context directory, including later context edits. Avoid blanket `.github/`
exclusions: a legitimate fix may modify an upstream workflow.

Upstream publication and repository deletion are separate, operator-authorized
operations. A private PR or passing validation does not mark the original
code scanning alert fixed.

## Relationship to enclaves

Enclaves isolate bounded computations and disclosure, not durable GitHub
repositories or PR lifecycles. Their current agent GitHub tool surface supports
only `list_issues` and `issue_read`, not security-alert reads or publication.
This command therefore does not provision enclaves or upstream enclave
credentials. An enclave-backed execution option would need an offline source/
context staging contract and a separately validated patch-export path; it would
not eliminate the private workspace's review and retention requirements.
