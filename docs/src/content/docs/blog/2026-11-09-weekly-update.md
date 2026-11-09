---
title: "Weekly Update – November 9, 2026"
description: "v1.2.0 and v1.2.1 bring durable workflow continuation, OpenTelemetry upgrades, and a round of security hardening."
authors:
  - copilot
date: 2026-11-09
metadata:
  seoDescription: "gh-aw v1.2.0 and v1.2.1: durable workflow continuation, OTLP session logs, safer updates, security hardening, plus PR Specification Reviewer as Agent of the Week."
---

Two releases landed in [github/gh-aw](https://github.com/github/gh-aw) this week, with a focus on durability, observability, and security. Here's what you can use today.

## Release: v1.2.1

[v1.2.1](https://github.com/github/gh-aw/releases/tag/v1.2.1) (November 6) focuses on workflow durability, safer updates, and broader engine and auth support.

- **Durable workflow continuation** ([#75097](https://github.com/github/gh-aw/pull/75097)): Artifact-backed continuation lets work carry across agents.
- **Specification-aware PR reviewer** ([#75207](https://github.com/github/gh-aw/pull/75207)) and an **experimental Atomic declarative engine** ([#75196](https://github.com/github/gh-aw/pull/75196)).
- **Smarter `create-issue`** ([#75323](https://github.com/github/gh-aw/pull/75323), [#74941](https://github.com/github/gh-aw/pull/74941)): Issue artifacts and label-scoped title deduplication.
- **GitHub App auth for repo-memory** ([#74937](https://github.com/github/gh-aw/pull/74937)) and **deployment controls on workflow environments** ([#74938](https://github.com/github/gh-aw/pull/74938)).
- **Safer updates** ([#74948](https://github.com/github/gh-aw/pull/74948), [#74945](https://github.com/github/gh-aw/pull/74945)): `gh aw update` preserves source refs, frontmatter comments, and formatting.
- **Security reviews fail closed** when unresolved ([#75335](https://github.com/github/gh-aw/pull/75335)), and a new warning flags dynamic checkout refs ([#74951](https://github.com/github/gh-aw/pull/74951)).

## Release: v1.2.0

[v1.2.0](https://github.com/github/gh-aw/releases/tag/v1.2.0) (November 4) is a security and observability release.

- **Breaking:** `pull_request_target` forks now require explicit opt-in for unrestricted use ([#74884](https://github.com/github/gh-aw/pull/74884)). Review fork-facing workflows and add the opt-in where intended.
- **Breaking:** the `-l` shorthand was removed from `project new` ([#74561](https://github.com/github/gh-aw/pull/74561)).
- **OpenTelemetry**: opt-in OTLP session logs ([#74599](https://github.com/github/gh-aw/pull/74599)) and dependency-free OTLP protobuf support for Azure Monitor ([#74594](https://github.com/github/gh-aw/pull/74594)).
- **Hardening**: bounded ZIP extraction ([#74906](https://github.com/github/gh-aw/pull/74906)) and stricter run-step expression sanitization ([#74793](https://github.com/github/gh-aw/pull/74793)).

## Notable Pull Requests Since

- [#75995](https://github.com/github/gh-aw/pull/75995) replaces the Pi durable runtime with native session chaining.
- [#75927](https://github.com/github/gh-aw/pull/75927) adds OpenAI workload identity federation for Codex.
- [#75988](https://github.com/github/gh-aw/pull/75988) supports name-based artifact upserts in `update-issue`.

## 🤖 Agent of the Week: PR Specification Reviewer

It reviews PRs for conformance and updates specifications on the PR branch so implementation and specs stay aligned.

It was just promoted in v1.2.1, and the logs show it busy: in its three most recent runs it was triggered on pull requests within about an hour of each other. Two finished successfully (about 7 minutes each), each in a single turn.

The third run failed after 26 seconds, so even the spec police occasionally trip on the way in.

💡 **Usage tip**: Scope a reviewer like this to paths that matter (`pkg/**`, `specs/**`) so it runs only when specs could drift.

→ [View the workflow on GitHub](https://github.com/github/gh-aw/blob/main/.github/workflows/pr-specification-reviewer.md)

## Try It Out

Upgrade to [v1.2.1](https://github.com/github/gh-aw/releases/tag/v1.2.1) and check the `pull_request_target` opt-in from v1.2.0. Feedback and contributions are welcome in [github/gh-aw](https://github.com/github/gh-aw).
