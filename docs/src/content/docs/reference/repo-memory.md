---
title: Repo Memory
description: Guide to using repo-memory for persistent file storage via Git branches with unlimited retention.
sidebar:
  order: 1510
---

Repo memory provides persistent file storage via Git branches with unlimited retention. The compiler auto-configures branch cloning/creation, file access at `/tmp/gh-aw/repo-memory-{id}/`, commits/pushes, and merge conflict resolution (your changes win).

## Enabling Repo Memory

```aw wrap
---
tools:
  repo-memory: true
---
```

Creates branch `memory/default`, mounted at `/tmp/gh-aw/repo-memory-default/`. Files are stored under `memory/default/` in the branch and are committed and pushed after the workflow completes.

## Work-queue workers

Workers using `tools.work-queue.worker: true` cannot use standalone `tools.repo-memory` persistence, because its push job is not attributed to a Claim. Removing this restriction, or adding a declaration beside that job, does not make the writes Claim-scoped.

For persistent snapshots, use a matching `safe-outputs.scripts` or custom preparation job with `safe-outputs.claim-adapters` (`effect-type: git_tree`), pinned to an existing native base commit, a fixed target repository, and a separate branch prefix. Credential-free preparation emits declared content; the trusted handler creates an immutable Claim branch from that base. The target must equal the worker profile's `effect_scope`. Preparation and restored history are not authority.

The repository's [ESLint Refiner workflow](https://github.com/github/gh-aw/blob/main/.github/workflows/eslint-refiner.md) keeps its legacy `memory/eslint-refiner` JSON/JSONL history and publishes `eslint-refiner.json` snapshots under `memory/eslint-refiner-runs/claims/`. A read-only setup step restores both histories by exact commit and blob identity, failing rather than dropping history on truncated trees, foreign repositories, malformed refs, altered bytes, or exceeded bounds. Limits: 128 snapshots, 1 MiB per file, 16 MiB total; archive older history through an approved operator before exceeding them.

The migration keeps memory continuity but does not update the old canonical branch. The Claim adapter cannot overwrite an existing Claim branch, and the `memory/eslint-refiner-runs` prefix avoids conflicts with `memory/eslint-refiner`. Remove the unscoped memory push job, and do not use a moving branch name as the base revision.

## Advanced Configuration

```aw wrap
---
tools:
  repo-memory:
    branch-name: memory/custom-agent-for-aw
    branch-prefix: tracking  # Custom prefix instead of "memory"
    description: "Long-term insights"
    file-glob: ["*.md", "*.json"]
    max-file-size: 1048576  # 1MB (default 100KB)
    max-file-count: 50      # default 100
    max-patch-size: 1048576  # 1MB max (default 10KB)
    target-repo: "owner/repository"
    create-orphan: true     # default
    allowed-extensions: [".json", ".txt", ".md"]  # Restrict file types (default: empty/all files allowed)
    format-json: true       # Pretty-print .json files (default: false)
    validation:
      timeout-minutes: 1
      json-schemas:
        - file: state.json
          schema:
            type: object
            required: [version, items]
            additionalProperties: false
            properties:
              version: { enum: [1] }
              items:
                type: array
                items: { type: string }
      script: |
        const data = JSON.parse(fs.readFileSync(path.join(memoryRoot, "state.json"), "utf8"));
        if (!Array.isArray(data.items)) throw new Error("state.json must contain an items array");
---
```

`branch-prefix` must be 4-32 alphanumeric, hyphen, or underscore characters and cannot be `copilot`. `max-patch-size` caps the diff size per push, `validation.json-schemas` enforces per-file JSON/JSONL checks, and `validation.script` runs an optional JavaScript domain validator (see [Custom validation](#custom-validation)).

Branches under `memory/adaptive-routing/` are reserved for [adaptive routing](/gh-aw/reference/adaptive-routing/) histories, which only gh-aw writes. A repo-memory entry that uses that branch or a branch under it fails to compile, and a branch-name expression that resolves to one fails at run time. Adaptive routing itself requires `tools.repo-memory`.

## GitHub App authentication

`github-app` is optional in the object form and in each array entry. Without it, memory operations use `github.token`. Configure an installed GitHub App to write memory branches protected by rulesets:

```aw wrap
tools:
  repo-memory:
    github-app:
      client-id: ${{ vars.MEMORY_APP_CLIENT_ID }}
      private-key: ${{ secrets.MEMORY_APP_PRIVATE_KEY }}
```

The app needs **Contents: read and write** permission and must be installed on the memory repository. Push tokens request only `contents: write`. `client-id` (or legacy `app-id`) and `private-key` are required. Optional `owner` and `repositories` override the installation scope, which otherwise derives from `target-repo` or the current repository. Each array entry mints its own token, and additional app permissions cannot be requested.

For an explicit `target-repo`, restore steps mint a separate Contents-read token from the same app, allowing private cross-repository clones. Restores without `target-repo` continue using `github.token` unless `github-app-read` is configured. Tokens are revoked by the token action's cleanup step.

Use `github-app-read` and `github-app-write` to select different apps for reads and writes. Both accept the same credential and scope fields as `github-app`, in object and array forms:

```aw wrap
tools:
  repo-memory:
    github-app-read:
      client-id: ${{ vars.MEMORY_READ_APP_CLIENT_ID }}
      private-key: ${{ secrets.MEMORY_READ_APP_PRIVATE_KEY }}
    github-app-write:
      client-id: ${{ vars.MEMORY_WRITE_APP_CLIENT_ID }}
      private-key: ${{ secrets.MEMORY_WRITE_APP_PRIVATE_KEY }}
```

Each override replaces `github-app` only for its operation; an omitted override uses `github-app`, or `github.token` if no shared app is configured. An override with `ignore-if-missing: true` falls back to `github.token`, not the shared app. The read app needs **Contents: read** for restores (including the current repository) and daily-credit ledger clones. The write app needs **Contents: read and write** for pushes, including the Git and API reads needed to commit. Only the write app needs the ruleset bypass.

Daily-credit activation and hydration clones also use Contents-read tokens. App-authenticated push jobs inherit the workflow's `environment`, so environment-scoped app credentials are available.

A literal `target-repo` authorizes that repository. For expression targets, the resolved repository must be the current repository or a literal name in the selected app's `repositories` under its literal `owner` (default: current owner); wildcards and expressions cannot define this allowlist. Read and write apps authorize targets independently before minting, and each token is scoped to the selected repository.

`ignore-if-missing: true` skips minting when either credential is missing and falls back to `github.token`. This cannot bypass an app-only ruleset or reach another private repository. The push job needs only `contents: read` when every entry uses a write app without this fallback; otherwise it keeps `contents: write`.

### Protect memory branches with a ruleset

Create an active branch ruleset targeting `memory/**` (or the configured branch prefix). Enable **Restrict updates** and **Restrict creations**. Add the installed memory app to the bypass list with **Always allow**; optionally add an administrator team for recovery. The built-in GitHub Actions token is not the app bypass actor.

Every writing workflow and array entry needs a bypass-listed app; use a narrower pattern if other memories must keep using `github.token`. [Adaptive routing](/gh-aw/reference/adaptive-routing/#branch-rulesets) histories are saved with `github.token`, so a `memory/**` ruleset must also let GitHub Actions update `memory/adaptive-routing/**`, or exclude that pattern.

Keep the app's private key available only to trusted workflows and environments. A branch ruleset controls writers, not the content an authorized workflow accepts.

Restored memory is untrusted data, not agent instructions. Choose repositories whose memory writers are trusted, and protect their memory branches. App authentication does not establish content integrity, so workflows that need provenance must verify it before using restored content.

## Structured Ledger

New workflows should use `tools.ledger`, independent of repo-memory file storage. Legacy `ledger` configuration is accepted only in a single-entry `tools.repo-memory` array; the object-form `tools.repo-memory.ledger` is rejected at compile time. Built-in state models are `log`, `set`, `map`, `table`, `counter`, and `notes`:

```aw wrap
---
tools:
  ledger:
    findings:
      type: table
      key: id
      schema:
        type: object
        required: [id, status]
        properties:
          id: { type: string }
          status: { type: string }
---
```

Each ledger stores immutable records on its own `ledgers/<name>` Git branch. Trusted preparation builds a disposable, read-only SQLite database at `/tmp/gh-aw/ledgers/<name>/ledger.db`: query `state` for current state and `records` for history. Schemas validate operation values, not envelopes; `table` requires a string primary-key field.

Configure bounded storage limits and maintenance cadence per ledger:

```yaml
tools:
  ledger:
    findings:
      type: log
      schema: .github/schemas/ledger.schema.json
      max-segment-kb: 100   # default 100 KiB
      max-record-kb: 32     # default 32 KiB
      max-patch-kb: 10      # default 10 KiB
      compaction:
        schedule: weekly
        min-segments: 32
        max-segments: 128
```

Agents submit mutations only through the configured ledger safe-output tools: `ledger_map_put` and `ledger_map_delete` for maps, `ledger_note_add` and `ledger_note_vote` for notes, and typed `ledger_append` operations for other built-in types. Writes are deferred, so the immediate response confirms queuing, not durability. The trusted `push_ledger_changes` job validates accepted requests, reconciles them with the latest branch state, and pushes. Queued writes are not immediately visible in the run's projection.

Ledger compaction never runs in the agent or persistence job. Standalone `tools.ledger` ledgers are compacted by Agentic Maintenance through an untrusted plan job and a trusted apply job; see [Ledger compaction](/gh-aw/reference/ledger-compaction/).

Custom `replay.script`, `replay.config`, and `compaction.script` settings are rejected. Use schemas for domain values and the built-in compaction policy for segment selection. Repo-memory `validation.script` remains supported as a separate file-storage validator.

Raw-record ledgers can keep their generic projection by omitting `type` and querying `records.payload` with SQLite JSON functions. Adding a built-in type does not convert historical raw payloads into operations. See [Ledger replay projections](/gh-aw/reference/ledger-replay/) for type semantics and migration guidance.

**File Glob Matching Rules**: patterns match the **relative path** inside the artifact directory, so do **not** include the branch name.

| Pattern | Matches |
|---------|---------|
| Slashless, e.g. `*.json` | Root-level files only (depth 0) |
| Contains `/`, e.g. `metrics/**`, `data/*.csv` | Full relative path, using standard globs |
| Absolute, starting with `/` | Rejected at compile time and runtime |

With the default `["*.json", "*.md"]`, the root-level `processed-discussions.json` is persisted, but `discussion-task-miner/processed-discussions.json` is not. Use `["**/*.json", "**/*.md"]` to include nested files.

## Multiple Configurations

```aw wrap
---
tools:
  repo-memory:
    - id: insights
      branch-prefix: daily  # Creates daily/insights branch
      file-glob: ["*.md"]
    - id: state
      file-glob: ["*.json"]
      max-file-size: 524288  # 512KB
---
```

Each entry mounts at `/tmp/gh-aw/repo-memory-{id}/`. The required `id` sets the folder name, and `branch-name` defaults to `{branch-prefix}/{id}`. Globs follow the file glob rules above. Files excluded by the glob or extension filters produce warnings that list their paths.

## Behavior

Branches auto-create as orphans by default, or clone with `--depth 1`. After validating `file-glob`, `max-file-size`, and `max-file-count`, gh-aw auto-commits and pushes when changes are present and threat detection passes.

On a shared branch, `file-glob` and `allowed-extensions` limit normalization, validation, and persistence to eligible files. Other files, including those managed by other workflows, are preserved.

### Custom validation

Use `validation.json-schemas` for required structural checks. Each entry has an inline `schema` and an optional relative `file` path or glob (`*` matches within a segment; `**` spans segments). Without `file`, the schema applies to every eligible `.json` and `.jsonl` file (excluding `.git`), and `format` should be omitted so it is inferred per file. Explicit paths must match `file-glob` and `allowed-extensions` and exist, and every glob must match at least one file. A `json` file must contain one non-empty JSON document. A `jsonl` file is checked record by record: LF and CRLF are accepted, a final newline is optional, an empty file has zero records, and blank or malformed lines fail with a line number. Schema checks never coerce, default, strip, or rewrite values. They run after filtering and `format-json` normalization, before `validation.script`, and again before upload and in the push job.

Format is inferred from each matched file's `.json` or `.jsonl` extension (case-insensitive), so one glob may match both. `format: json` or `format: jsonl` overrides inference and is required for other extensions.

The schema vocabulary is deliberately small: `type` (including type lists and `null`), primitive `enum`, `required`, nested `properties`, `additionalProperties: false`, object-schema `items`, and standalone `oneOf` or `anyOf`. Schemas nest at most 32 levels, and alternatives cannot have sibling constraints. Numeric enum values must be exactly representable in JavaScript. Keywords such as `$ref`, `const`, `format`, `pattern`, bounds, and `uniqueItems` are rejected at compile time; use a script for cross-file rules, uniqueness, or timestamps.

Use `validation.script` when storage and schema checks are not enough. It is a JavaScript body run with Node.js over the complete configured memory directory, after `format-json` normalization and before upload or commit. It runs in the agent job and again in the push job as defense in depth.

The script also checks the cloned baseline before the agent starts, without formatting or filtering the checkout. A baseline failure is shown to the agent with the validator's output so it can repair memory and verify with `push_repo_memory`. If an invalid baseline stays unchanged, the agent job warns and skips its memory artifact rather than failing after the agent exits. Changed memory must pass validation before upload and again before push.

Globals include Node.js `fs` and `path`, plus `memoryRoot`/`memoryDir`, `memoryId`, `memoryKind` (`"repo"`), `memoryPhase` (`"baseline"`, `"upload"`, or `"push"`), and `memorySourceJobResult`. Paths and the working directory point to a temporary copy of the eligible memory files, without the checkout's `.git` directory. Changes to the copy are not persisted. The copy is not a sandbox: validators run as trusted code with the runner's filesystem permissions.

Use `memoryPhase` (set by the runtime) to tell upload limits from branch checks. `"push"` sees the complete branch overlay, including historical files omitted from the upload, which repo-memory never deletes. Keep integrity checks in every phase, and apply retention or aggregate limits only in `"upload"` (or `"baseline"` for diagnostics).

`memorySourceJobResult` holds the agent job's GitHub Actions result during downstream push validation and is empty during baseline and upload validation. Requiring `memorySourceJobResult === "success"` in `"push"` blocks publishing after failed preparation or execution. This is opt-in, and it does not prove that queued outputs or persistence succeeded.

The validator receives only basic runner paths and `GH_AW_MEMORY_ROOT`, `GH_AW_MEMORY_DIR`, `GH_AW_MEMORY_ID`, and `GH_AW_MEMORY_KIND`; GitHub tokens and write credentials are withheld. Network access follows the runner's normal policy. The default timeout is 1 minute, configurable with `validation.timeout-minutes` (1-5).

Throwing, returning `false`, timing out, exiting nonzero, or modifying a memory file rejects persistence. Validator output is reported separately from built-in storage checks, so agents can tell domain-schema failures from size or count failures.

In the push job, validation failures set `validation_failed` to `true` and record both output streams in `validation_error` and the **Repo-memory push failed** summary. Configured `GH_TOKEN`, `GITHUB_TOKEN`, and `GITHUB_APP_TOKEN` values and HTTP authorization credentials are redacted from diagnostics; validators should never print secrets.

Commits use the [GitHub GraphQL `createCommitOnBranch` mutation](https://docs.github.com/en/graphql/reference/mutations#createcommitonbranch), so they are **Verified** with GitHub's GPG key and satisfy signed-commit rulesets.

:::note[Signed-commit fallback limitation]
The GraphQL mutation does not support symlinks, executable files (`chmod +x`), or submodule entries. Artifacts containing them fall back to a plain `git push`, which signed-commit rulesets usually reject. Keep memory files to regular text formats such as `.json`, `.jsonl`, `.txt`, `.md`, and `.csv`.
:::

## Comparison with Cache Memory

| Feature | Cache Memory | Repo Memory |
|---------|--------------|-------------|
| Storage | GitHub Actions Cache | Git Branches |
| Retention | 7 days | Unlimited |
| Size Limit | 10GB/repo | Repository limits |
| Version Control | No | Yes |
| Performance | Fast | Slower |
| Best For | Temporary/sessions | Long-term/history |

For fast 7-day caching without version control, see [Cache Memory](/gh-aw/reference/cache-memory/).

## Troubleshooting

- **Branch not created**: Ensure `create-orphan: true` is enabled, or create the branch manually.
- **Validation or patch-size failures**: Keep changes within `file-glob`, `max-file-size` (100KB default), `max-file-count` (100 default), and `max-patch-size` (10KB default).
- **Changes not persisting**: Confirm the directory path and let the workflow finish. Check the push job's **Repo-memory push failed** summary for the underlying error. Updates rejected by validation are not committed or pushed.
- **Merge conflicts**: Each save applies only this run's changes. Files this run did not change keep their latest branch version, even if another run saved them meanwhile. When both runs changed the same file, `.jsonl` files keep rows from both (`merge=union`), and other types keep this run's version of the lines both changed. If the starting commit is unknown or no longer in history, the push job copies this run's files over the latest branch and logs a warning that rows saved by other runs may be lost.
- **Many runs finishing at once**: Pushes are not serialized. A save commits only if the branch head is unchanged since it was prepared; otherwise the push re-reads the head, rebases with JSONL union merging, and retries with exponential backoff. If all 11 attempts fail, the save fails and the job reports an error. To avoid conflicts, have each run write its own file, such as `runs/${{ matrix.worker }}.jsonl`. Adaptive routing history uses an internal save option that reapplies changes to the newest files instead of rebasing; agent memory cannot use it.
- **GH013 — Commits must have verified signatures**: The artifact included a symlink, executable file, or submodule entry, forcing a plain `git push` fallback. Remove the unsupported entry and re-run.

## Security

Do not store sensitive data in repo memory. Access follows repository permissions, so prefer private repositories, avoid secrets, set limits such as `file-glob`, `max-file-size`, `max-file-count`, and `max-patch-size`, consider branch protection, and use `target-repo` for isolation.

## Examples

See [Deep Report](https://github.com/github/gh-aw/blob/main/.github/workflows/deep-report.md) and [Daily Firewall Report](https://github.com/github/gh-aw/blob/main/.github/workflows/daily-firewall-report.md) for long-term insights and historical data tracking.

## Learn More

For configuration details, see [Frontmatter](/gh-aw/reference/frontmatter/); for output automation, see [Safe Outputs](/gh-aw/reference/safe-outputs/).
