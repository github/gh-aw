---
title: File-Access Lint Specification
description: Normative model for the file-access lint rule, which predicts prompt file-access instructions that the agent sandbox or tool configuration will block
sidebar:
  order: 1006
---

# File-Access Lint Specification

**Version**: 0.1.0  
**Status**: Working Draft  
**Date**: 2026-11-26  
**Last Updated**: 2026-11-26<br>
**Editors**: GitHub gh-aw Team

---

## 1. Purpose

Workflow authors write prompts that tell the agent to read, write, or run things. The frontmatter then configures what the agent can do: the agent sandbox, `tools.edit`, `tools.bash`, memory mounts, and pull-request protected files. When the prompt and the configuration disagree, the run wastes turns on denied tool calls, or fails silently.

The `file-access` rule simulates the agent's effective file-access capabilities from configuration. It then extracts file-access instructions from the Markdown prompt and predicts which ones will be blocked. Each finding is a **Warning**, because extraction from natural language is heuristic.

This document is the source of truth for implementation, testing and review. Each requirement has a stable ID. Tests in `pkg/workflow/lint/file_access_test.go` cite these IDs, and `TestFileAccessSpecTraceability` fails if a requirement ID has no test that cites it.

The key words MUST, MUST NOT, SHOULD and MAY are used as described in RFC 2119.

## 2. Scope

The rule is registered as built-in rule `file-access` in `pkg/workflow/lint`. It targets the AW AST only, and runs in:

- `gh aw lint` (standalone; uses authored frontmatter), and
- `gh aw compile` (uses compiler-resolved tools, which include imports).

It does not cover:

- validation of specific tool names or shell command names requested in the prompt. Prompt-tool validation in the compiler already reports these.
- network egress. The firewall rules cover that.

## 3. Capability model

The model is computed once per workflow. Each capability is *enabled*, *disabled*, or *unknown*. A finding that depends on a capability MUST NOT be reported when that capability is *unknown*.

| ID | Capability | Derivation |
|---|---|---|
| FAL-M1 | Agent sandbox | Disabled only when `sandbox.agent: false`. Any other authored `sandbox` value, or an absent `sandbox`, means enabled. When `sandbox` is absent and imports are pending, the state is unknown. |
| FAL-M2 | Edit tool | When `tools.edit` is declared with any value other than `false`, edit is enabled. When it is absent, edit is enabled if the sandbox is enabled or a pull-request safe output (`create-pull-request` or `push-to-pull-request-branch`) is configured. This mirrors `applyDefaultTools`. |
| FAL-M3 | Bash tool defaults | When `tools.bash` is absent and the sandbox is enabled, bash allows all commands. `bash: true` allows all commands. `bash: null` keeps the default safe commands. `bash: false` removes bash. A list adds its commands to the defaults. |
| FAL-M4 | Write-capable shell | A bash allowlist can write files when it contains `*`, `:*`, a bare or wildcard `git` entry, `git apply`, or a command whose stem is one of: `tee`, `cp`, `mv`, `touch`, `mkdir`, `sed`, `awk`, `dd`, `install`, `ln`, `truncate`, `patch`, `python`, `python3`, `node`, `perl`, `ruby`, `sh`, `bash`, `zsh`, `go`, `make`, `npm`, `npx`. The default safe commands cannot write. Shell redirection is not modeled. |
| FAL-M5 | Memory mounts | `tools.cache-memory` mounts `/tmp/gh-aw/cache-memory/` for ID `default`, and `/tmp/gh-aw/cache-memory-<id>/` for any other ID. The configuration may be `true`, a mapping or a list of `{id}` entries. `tools.repo-memory` mounts paths under `/tmp/gh-aw/repo-memory`. |
| FAL-M6 | Protected files | Protected files are blocked only when every configured pull-request safe output sets `protected-files` to `blocked`, either as a string or as `{policy: blocked}`. Paths in `exclude` remain writable. The protected set mirrors the compiler defaults: dependency manifests, `CODEOWNERS`, `DESIGN.md`, top-level `README.md`, `CONTRIBUTING.md`, `SECURITY.md` and `CODE_OF_CONDUCT.md`, any path under a top-level dot-folder, and the engine's instruction file (`AGENTS.md` for Copilot and Codex, `CLAUDE.md` for Claude). |
| FAL-M7 | Import deferral | In standalone lint, authored imports may supply `tools`, `sandbox` or `safe-outputs`. While imports are pending, capabilities that imports could change are unknown, unless the authored configuration already fixes them. Compiler-resolved tools (`Input.ResolvedTools`) are authoritative for FAL-M2 to FAL-M5. |

The effective write capability is enabled when FAL-M2 enables edit, or when FAL-M3 and FAL-M4 give a bash allowlist that can write.

## 4. Instruction extraction

| ID | Requirement |
|---|---|
| FAL-X1 | The prompt is scanned after normalizing CRLF line endings. Consecutive prose lines are joined into blocks, so a sentence that wraps across lines is analyzed as a whole. Blank lines, fences, headings, list items and table rows start a new block. A finding reports the line where its sentence starts; memory findings report the line of the path. Inside fenced code blocks, the rule extracts only memory paths, plus Docker commands when the fence language is `bash`, `sh`, `shell`, `zsh`, `console` or `shell-session`. Text inside HTML comments MUST be ignored. |
| FAL-X2 | Prose is split into sentences at `.`, `!`, `?` or `;` followed by whitespace. A sentence containing a negation (`not`, `never`, `don't`, `do not`, `avoid`, `without`, `cannot`, `no` and similar) MUST be ignored entirely, because it describes a prohibition. A sentence containing an illustrative marker (`such as`, `e.g.`, `for example`, `for instance`) MUST also be ignored, because it describes examples rather than actions. |
| FAL-X3 | Path tokens are bare words starting with `/`, `./`, `~/` or `$GITHUB_WORKSPACE/`. Inside code spans, path tokens also include relative paths containing `/`, and file names with an alphabetic extension and a base name of at least two characters. URLs, `${{ }}` expressions, `@` mentions and shell fragments are never path tokens. |
| FAL-X4 | A sentence is a write instruction when it starts with a write verb (`create`, `write`, `edit`, `modify`, `update`, `save`, `append`, `overwrite`, `store` or `install`), optionally after a list marker, bold markers or an adverb such as `please` or `then`. A write verb after `then`, `and` or a comma also counts. The sentence MUST also contain a path token or the words file, directory or folder. Its target is the first path token after `to`, `into`, `in`, `at`, `under` or `inside`. If there is none, the target is the first path token. |
| FAL-X5 | A sentence is a Docker instruction when a run-type verb (`run`, `use`, `execute`, `start`, `build`, `launch`, `pull` or `spawn`) takes `docker` as its direct object, with only determiners such as `a`, `the` or `new` in between, contains a code span starting with `docker `, or mentions `/var/run/docker.sock`. `docker` MUST be a standalone word, so hostnames such as `host.docker.internal` do not match. In shell fences, a line is a Docker instruction when, after removing a `$` prompt and `sudo`, it starts with `docker ` or `docker-compose `. Daemonless registry queries (`docker manifest`) are not Docker instructions. |

## 5. Findings

Every finding has `path: prompt`, the 1-based prompt line in `script_line`, and a related location that names the configuration responsible.

| ID | Finding | Condition | Related |
|---|---|---|---|
| FAL-R1 | Write without a write-capable tool | A prose write instruction, and an effective write capability that is disabled. | `tools.edit`, or `sandbox.agent` when the disabled sandbox removed the default |
| FAL-R2 | Write to a read-only system path | The sandbox is enabled and a write target is under `/usr`, `/opt`, `/bin`, `/sbin`, `/lib`, `/lib64`, `/etc` or `/boot`. | `sandbox.agent` |
| FAL-R3 | Docker inside the sandbox | The sandbox is enabled and the line is a Docker instruction. AWF hides `/var/run/docker.sock`. | `sandbox.agent` |
| FAL-R4 | Unconfigured memory directory | The line references a cache-memory ID that is not configured, or a repo-memory path while repo-memory is not configured, and the memory configuration is known. | `tools.cache-memory` or `tools.repo-memory` |
| FAL-R5 | Protected file blocked | FAL-M6 blocks protected files, imports are not pending, and a write target is a workspace-relative protected path that is not excluded. | `safe-outputs.<output>.protected-files` |

## 6. Diagnostics

| ID | Requirement |
|---|---|
| FAL-D1 | Findings MUST have `Warning` severity in both strict and non-strict workflows, and MUST NOT carry a threat rule ID. The rule MUST be deterministic, MUST NOT mutate its input, and MUST honor context cancellation. Messages MUST be stable sentences that name the blocked capability and a remedy. They echo only authored paths, never expression values or secrets. |

## 7. Conformance cases

Each case is a frontmatter and prompt pair, with the expected findings. These cases are encoded in `TestFileAccessConformance`.

| Case | Frontmatter | Prompt | Expected |
|---|---|---|---|
| C1 | `tools: {edit: false, bash: false}` | ``Create a file `notes/summary.md`.`` | FAL-R1 at line 1 |
| C2 | (none; defaults) | ``Create a file `notes/summary.md`.`` | none |
| C3 | `sandbox: {agent: false}` | ``Write the report to `report.md`.`` | FAL-R1 |
| C4 | `sandbox: {agent: false}`, `tools: {bash: [tee]}` | ``Write the report to `report.md`.`` | none |
| C5 | `tools: {edit: false, bash: [cat]}` | ``Do not write to `report.md`.`` | none (negation) |
| C6 | (none) | `Install the tool into /usr/local/bin.` | FAL-R2 |
| C7 | `sandbox: {agent: false}`, `tools: {edit: true}` | `Install the tool into /usr/local/bin.` | none |
| C8 | (none) | `Run docker build to package the app.` | FAL-R3 |
| C9 | (none) | A `bash` fence containing `docker run alpine` | FAL-R3 at the fence line |
| C10 | (none) | `Save notes in /tmp/gh-aw/cache-memory/notes.md.` | FAL-R4 |
| C11 | `tools: {cache-memory: true}` | `Save notes in /tmp/gh-aw/cache-memory/notes.md.` | none |
| C12 | `tools: {cache-memory: [{id: logs}]}` | `Use /tmp/gh-aw/cache-memory-other/.` | FAL-R4 |
| C13 | `safe-outputs: {create-pull-request: {protected-files: blocked}}` | ``Update `package.json` to bump the version.`` | FAL-R5 |
| C14 | as C13, plus `exclude: [package.json]` in the object form | as C13 | none |
| C15 | `imports: [shared/tools.md]`, `tools: {edit: false}` | ``Create a file `notes/summary.md`.`` | none (deferred) |
| C16 | `tools: {edit: false}`, with resolved tools `{edit: false, bash: ["*"]}` | ``Create a file `notes/summary.md`.`` | none (resolved bash can write) |

## 8. Known limitations and future work

- Shell redirection (`echo x > file`) and `sudo` are not modeled. Allowlists that rely on redirection are treated as unable to write.
- Engine-specific native write tools beyond `edit` are not modeled.
- Read restrictions are not modeled. The sandbox mounts the host filesystem read-only, so reads are never predicted to fail.
- Path globs in `protected-files.exclude` are matched by exact path, base name, or directory prefix only.
- Prompt fragments added by `{{#import}}` or `{{#runtime-import}}` are not expanded.
- Future work: model `tools.edit` path allowlists if they are introduced, and add sentence-level intent classification.

## 9. Change process

Any behavior change to `pkg/workflow/lint/file_access*.go` MUST update this document in the same pull request. That includes adding, changing or retiring requirement IDs, and adding conformance cases for new findings. Retired IDs MUST NOT be reused.
