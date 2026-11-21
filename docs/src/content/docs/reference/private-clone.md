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
  --starter-workflow investigate.md \
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
| `--starter-workflow FILE.md` | Required top-level workflow to dispatch after activating Actions; it must declare `workflow_dispatch` |
| `--ref REF` | Source branch, tag, or commit; defaults to `HEAD` and resolves to an immutable SHA |
| `--alert NUMBER` | Selected code scanning alert; repeatable or comma-separated |
| `--advisory GHSA-ID` | Selected repository advisory; repeatable or comma-separated |

At least one alert or advisory is required. The local operator needs source
contents access, code scanning/advisory read access for the selected resources,
and permission to create and configure the destination repository, push its
workflow files, enable Actions, and dispatch the starter. Download failures stop
setup rather than omitting context.
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

Each root workflow receives `permissions.copilot-requests: write` and a short
pointer to the local context index. GitHub tools, including local imports, are
restricted to `allowed-repos: "${{ github.repository }}"`. Explicit external
checkout/output targets and external GitHub token/App configurations are
rejected. The selected starter workflow must declare `workflow_dispatch`;
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

After the source and context are pushed, the command enables Actions and
dispatches the selected starter workflow to begin investigation. Copilot
inference uses the clone's Actions token via `copilot-requests: write`; no
upstream PAT or Copilot PAT is provisioned. Any required secrets must already be
available to the new repository through an approved organization or enterprise
configuration.

> [!CAUTION]
> This command activates Actions and immediately runs trusted, operator-supplied
> workflow code; other enabled triggers in the bundle may also run. Review the
> bundle and check organization-wide secrets, installed Apps, shared runner
> credentials, and private dependency credentials before invoking it. Do not
> grant the clone upstream repository access. Repository creation does not revoke
> ambient organization grants; use an isolated owner when the upstream
> organization cannot provide an appropriate security boundary.

Failed publication may leave an empty or partially configured private repository.
The command does not delete repositories automatically. It dispatches only the
selected starter workflow; it does not publish changes or mark an upstream alert
fixed.

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
