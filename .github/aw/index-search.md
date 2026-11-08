---
description: Trusted indexing and read-only search patterns.
---

## Indexing and Read-Only Search

Separate **index construction** from **agent search**. A deterministic publisher builds an index using trusted code from `main`, then saves it to GitHub Actions cache. Eligible agent jobs restore that index and query it through existing search tools, optionally an MCP server. The agent never builds, refreshes, or modifies the index.

This pattern reuses [`jobs:` and `pre-agent-steps:`](/gh-aw/reference/steps-jobs/) and standard Actions cache restore/save actions. It requires no new indexing service or mandatory dependency. Unlike the general [`cache:` configuration](/gh-aw/reference/syntax-tools-imports/), shared search indexes must not use top-level `cache:` (which can save at job completion) or writable [`cache-memory`](/gh-aw/reference/cache-memory/).

### Choose the Publisher Model

| Model | Use when | Freshness and cost | Reuse |
|---|---|---|---|
| Separate indexing job within the agentic workflow | Construction is cheap or efficiently incremental | Indexing adds latency to each trusted workflow run, but can catch up to that run's source revision | The agent restores the exact key published by its prerequisite job; other compatible workflows can also consume it |
| Dedicated indexing workflow or action | Construction is expensive, data changes slowly, or several workflows share it | Scheduled builds amortize cost, but consumers must tolerate and check a freshness window | Multiple workflows in the same repository restore compatible snapshots independently |

Incremental indexing restores a compatible prior snapshot **in the publisher job**, applies additions, changes, and deletions, then publishes a new immutable snapshot. It reduces work only if the existing indexer supports reliable change tracking. Record the source revision or data cursor; fall back to a full build in the publisher when the baseline is missing or incompatible.

Scheduling controls *when* indexing happens; incrementality controls *how much* work it performs. A scheduled publisher can also be incremental. Schedules can be delayed, so an interval is not a freshness guarantee. For slowly changing data, use a schedule plus an explicitly authorized manual trigger. For strict revision freshness, use a trusted `push` trigger and require a matching source revision, not merely a recent build timestamp.

### Trust and Read-Only Boundaries

Both the publisher workflow definition and the indexer, dependencies, configuration, and any reusable action must be trusted code from `main` (or an approved immutable revision of it). A `main` checkout alone does not make a PR-controlled workflow safe. Run publishers only on trusted main-branch events; reject manual dispatches on other refs. Do not publish from `pull_request`, fork runs, or `pull_request_target` runs that execute PR code. Treat PR content as data, never as indexing scripts or configuration.

Give the publisher a separate runner job with explicit least-privilege permissions. Keep indexing secrets step-scoped there, not in workflow-level `env`, agent setup, MCP configuration, job outputs, artifacts, or caches. Grant any required write scope only to that job. Cache restore/save uses the Actions cache service, not repository `contents: write`; grant repository or external write access only if the existing indexer actually needs it.

Restore-only caching prevents write-back through that action, **not direct writes through the job's cache token or local index mutation**. On GitHub.com, require threat detection and omit nonempty top-level `cache:` entries so gh-aw enforces `cache-mode: read` for the agent job. This is the cache-token boundary; a restore-only action alone is insufficient. Workflows that need dependency-cache saves retain token write access and are unsupported consumers of this shared-index pattern. GHES configurations without scoped `cache-mode` enforcement are also unsupported; use a different read-only delivery mechanism. Do not disable detection or override the read-only mode.

Configure the existing search tool or MCP server in query-only mode, with the index mounted read-only or protected by an equivalent enforced access boundary. Expose only search/query tools, not refresh/build/admin endpoints, and disable automatic indexing on startup or on a miss. A prompt or `chmod` on files owned by the agent is not sufficient isolation. Keep any necessary search scratch files separate from the index. If an existing tool cannot operate without modifying its index, it does not satisfy this shared-index pattern.

> [!CAUTION]
> PR jobs must never save, replace, delete, or otherwise mutate shared index caches, even for same-repository PRs. They may only restore eligible snapshots. Do not provide cache administration credentials or cache-save actions to the agent, and never publish files returned from agent execution. gh-aw trigger/actor restrictions still apply; cache visibility does not authorize an otherwise ineligible agent run.

### Cache Versioning and Restoration Contract

Actions caches are repository- and ref-scoped, not a cross-repository index service. Eligible runs can restore caches from their current ref and the repository's default branch; PR runs can also restore eligible base-branch caches. A cache created under a PR merge ref is available only to that PR's reruns, not to `main` or other PRs. Checking out `main` does not change the run's cache scope. Publish on `main`, and ensure consumers can see that branch's caches; if the default branch differs, adapt the publisher to the actual trusted default branch.

Caches are not secret storage. Fork PR workflows can potentially read default/base-branch caches. Publish only data safe for every eligible reader; private source credentials or sensitive indexed content require a different access-controlled delivery mechanism, not a clever cache key.

Use a key such as `docs-search-v1-linux-<source-sha>-<build-id>`. The prefix identifies the corpus, index schema/tool compatibility, and platform. The suffix identifies an immutable source snapshot and build. Actions also matches an internal cache version derived from paths and compression tooling: producers and consumers must use compatible paths and runners, not just the same key. Existing entries cannot be updated in place; publish a new key for each changed snapshot. Serialize publishers where needed to avoid competing builds.

Restore the exact key from the trusted publisher record; do not derive it from the consumer's `github.sha` or `github.run_id`. Do not use `restore-keys` for shared consumers, especially PR consumers: GitHub searches eligible current-ref caches before default-branch caches, so a prefix can select an unintended snapshot. A matching key alone does not prove provenance.

Store a manifest with schema/tool version, corpus, source revision or cursor, build time, and checksums. The trusted publisher must also publish a digest of the complete snapshot through a trusted channel independent of the cache, such as a prerequisite job output or an authenticated publisher record. Before starting search, trusted validation must compare the restored snapshot's digest with that independently obtained value and check compatibility, provenance, and the workflow's maximum age or exact-revision requirement. A manifest claim, cache key, or digest stored only inside the cache is not authentication.

Caches can expire or be evicted; they are not durable storage. A miss, incompatible index, or stale snapshot must fail with a diagnostic identifying the expected key/version and freshness requirement, or take a **documented read-only fallback** such as GitHub code search or bounded text search of available files. Mark fallback results as degraded and state the unavailable index revision. Never silently treat a miss as empty results or ask the agent to rebuild. Index repair belongs to the trusted publisher's next authorized run.

See GitHub's [cache access restrictions and matching rules](https://docs.github.com/en/actions/using-workflows/caching-dependencies-to-speed-up-workflows#restrictions-for-accessing-a-cache).

### Example: Cheap Indexing Before an Agent

The following is an integration template, not a bundled indexer. Replace `./scripts/search-index` with the repository's **existing trusted** indexer/validator. Its `build` operation must produce the index and manifest; `validate` must reject missing, incompatible, or stale data and never write to the index. Configure the repository's existing read-only search tool or MCP import separately, using the access boundary described above.

```aw wrap title=".github/workflows/indexed-docs-search.md"
---
on:
  push:
    branches: [main]
  workflow_dispatch:
permissions:
  contents: read
network: defaults
checkout:
  ref: main
  pull-request: false
jobs:
  index:
    if: github.ref == 'refs/heads/main' && (github.event_name == 'push' || github.event_name == 'workflow_dispatch')
    runs-on: ubuntu-latest
    permissions:
      contents: read
    outputs:
      cache-key: ${{ steps.key.outputs.value }}
      snapshot-digest: ${{ steps.digest.outputs.sha256 }}
    steps:
      - uses: actions/checkout@<approved-ref>
        with:
          ref: ${{ github.sha }}
          persist-credentials: false
      - id: key
        env:
          SOURCE_SHA: ${{ github.sha }}
          BUILD_ID: ${{ github.run_id }}
        run: echo "value=docs-search-v1-linux-${SOURCE_SHA}-${BUILD_ID}" >> "$GITHUB_OUTPUT"
      - name: Build and validate the snapshot
        run: |
          ./scripts/search-index build --output /tmp/gh-aw/search-index
          ./scripts/search-index validate --index /tmp/gh-aw/search-index
      - id: digest
        run: echo "sha256=$(./scripts/search-index digest --index /tmp/gh-aw/search-index)" >> "$GITHUB_OUTPUT"
      - uses: actions/cache/save@<approved-ref>
        with:
          path: /tmp/gh-aw/search-index
          key: ${{ steps.key.outputs.value }}
  agent:
    needs: [index]
pre-agent-steps:
  - uses: actions/cache/restore@<approved-ref>
    with:
      path: /tmp/gh-aw/search-index
      key: ${{ needs.index.outputs.cache-key }}
      fail-on-cache-miss: true
  - name: Validate before opening read-only search
    env:
      EXPECTED_SOURCE_SHA: ${{ github.sha }}
      EXPECTED_SHA256: ${{ needs.index.outputs.snapshot-digest }}
    run: ./scripts/search-index validate --index /tmp/gh-aw/search-index --source "$EXPECTED_SOURCE_SHA" --expected-sha256 "$EXPECTED_SHA256"
---

# Indexed Documentation Search

Use only the configured read-only search tool to query the restored documentation
index. Never build, refresh, or modify it. Report missing or stale index errors;
do not substitute empty results.
```

The custom `index` job finishes before the compiler-generated `agent` job; no indexing runs in `steps:` or `pre-agent-steps:` in the agent job. The latter only restores and validates before MCP startup. Pin any external actions/dependencies, keep the validator on trusted code, and compile the adapted workflow normally. If indexing needs credentials, bind them only to the publisher's build step. For an incremental indexer, insert restore-only baseline retrieval before `build`, keeping its updates entirely in `jobs.index`.

### Example: Dedicated Scheduled Publisher and Shared Consumers

For expensive indexes, move the publisher into a conventional Actions workflow. This fragment reuses the **entire `index` job above**, including its trusted checkout, explicit permissions, build/validation, and cache save; replace its `if` with the scheduled/manual guard below. An existing reusable indexing action can implement those same steps inside this job, but must not run inside the agent job.

```yaml wrap title=".github/workflows/publish-search-index.yml (publisher wiring)"
name: Publish search index
on:
  schedule:
    - cron: '17 3 * * *'
  workflow_dispatch:
permissions:
  contents: read
concurrency:
  group: docs-search-v1-publisher
  cancel-in-progress: false
jobs:
  index:
    if: github.ref == 'refs/heads/main' && (github.event_name == 'schedule' || github.event_name == 'workflow_dispatch')
    # Copy runs-on, permissions, outputs, and steps from jobs.index above.
```

Consumers omit `jobs.index` and `jobs.agent.needs: [index]`. They retain the existing query-only tool configuration and use this restore/validation fragment instead. First, an existing trusted lookup retrieves the publisher record from a channel independent of the cache; that record supplies the exact cache key, source revision, build time, and SHA-256 digest. Do not use a cache or artifact as the source of this record. All consumers agree on the corpus, schema, platform, manifest, and maximum age; they do not invoke or dispatch the publisher.

```aw wrap title="Shared consumer frontmatter fragment"
checkout:
  ref: main
  pull-request: false
pre-agent-steps:
  - id: publisher_record
    name: Read the trusted publisher record
    run: ./scripts/search-index publisher-record --output "$GITHUB_OUTPUT"
  - uses: actions/cache/restore@<approved-ref>
    id: search_index
    with:
      path: /tmp/gh-aw/search-index
      key: ${{ steps.publisher_record.outputs.cache-key }}
      fail-on-cache-miss: true
  - name: Check the selected snapshot
    env:
      RESTORED_KEY: ${{ steps.search_index.outputs.cache-matched-key }}
      TRUSTED_KEY: ${{ steps.publisher_record.outputs.cache-key }}
      EXPECTED_SOURCE_SHA: ${{ steps.publisher_record.outputs.source-sha }}
      EXPECTED_SHA256: ${{ steps.publisher_record.outputs.sha256 }}
    run: ./scripts/search-index validate --index /tmp/gh-aw/search-index --cache-key "$RESTORED_KEY" --expected-cache-key "$TRUSTED_KEY" --source "$EXPECTED_SOURCE_SHA" --expected-sha256 "$EXPECTED_SHA256" --max-age-hours 48
```

The publisher-record lookup is conceptual: use an existing authenticated trusted channel and trusted code from `main`. The digest must be computed over the restored snapshot and compared with the independently retrieved publisher record. The exact key and digest are both required; a successful restore does not imply authenticity or freshness.

For PR consumers, this fragment deliberately disables automatic PR checkout so host validation uses trusted `main` code. Inspect PR changes as data through existing read-only tools. If a workflow needs compiler-managed PR checkout, keep its validator/search configuration in a separate trusted location and never execute scripts from the PR workspace. PR search results describe the published main snapshot, not unindexed PR changes; document that limitation or use a bounded read-only fallback for changed files.
