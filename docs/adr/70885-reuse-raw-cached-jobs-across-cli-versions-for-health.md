# ADR-70885: Reuse Raw Cached Jobs Across CLI Versions and Retain Typed Job Evidence in `gh aw health`

**Date**: 2026-10-21
**Status**: Proposed (not approved)
**PR author**: pelikhan

---

### Context

`gh aw health` groups failing runs into shared job/step clusters so that one investigation can cover failures across workflows. Evidence comes from cached run summaries and inexpensive GitHub Jobs API presence checks. Rejecting every cross-version cache discards useful raw observations; retaining only a presence boolean also loses useful failure details. However, optional typed decoding must not make a presence check fail, and an incomplete job inventory cannot establish a trustworthy shared failure boundary. The command must remain read-only and inexpensive: no artifact downloads, no workflow dispatch, and bounded API usage.

### Decision

Reuse cached Jobs API observations (`JobDetails`) across CLI versions only after matching run identity, including repository, URL, workflow path, head SHA, status, conclusion, attempt and update time. A producer-version mismatch retains raw jobs and their provenance, but excludes cached version-dependent usage, classification and sub-agent metrics (`version_mismatch_raw_jobs_only`). Do not rewrite cached producer metadata.

Determine job presence from a schema-minimal raw jobs array, independently of optional typed job/step decoding. Only failure-first representatives collect typed live evidence, with at most three 100-job pages per attempt (`healthMaximumJobPages = 3`), in addition to the one-job presence probe.

Typed enrichment is best-effort: API, budget or decoding failures must not abort an otherwise usable health report or erase known presence. Report incomplete evidence and an explicit warning. Emit job/step boundary signals from live evidence only when the inventory is complete; partial inventories remain diagnostic evidence, not clustering boundaries.

Expose producer version, report schema version, capabilities, cache compatibility, job-evidence completeness and API/job request counts. `degraded_workflows` counts workflows with either failed sub-agent invocations or threat-detection failures, while `observed_failure_workflows` separately counts workflows with sampled current-revision Actions failures. Both counters exclude intentional-failure workflows and may overlap.

### Alternatives Considered

#### Alternative 1: Keep strict CLI-version equality for all cached data

Discard every summary produced by a different CLI version. This conservatively avoids schema drift but loses usable raw observations after upgrades. Not selected because identity-checked raw-job reuse preserves evidence without trusting cross-version derived metrics.

#### Alternative 2: Re-fetch all jobs (or download artifacts) for every failing run

Ignore the cache and fetch every job, or download run artifacts, for every failing run. This could improve coverage, but unbounded refetching violates the request budget and artifact downloads violate the command's scope. Not selected; representative enrichment remains capped and may be incomplete.

#### Alternative 3: Reuse entire cached summaries, including derived metrics, across versions

Reuse the whole `RunSummary` regardless of producer version. This avoids recomputation but silently mixes metrics derived under different classification, usage or sub-agent logic. Not selected; only raw jobs survive a version mismatch.

### Consequences

Identity-checked cached jobs and complete live inventories can recover shared failure clusters without downloading artifacts. Compatible cached evidence also restores investigation log paths. Optional enrichment failures preserve the core report, while provenance, warnings and completeness distinguish observations from missing evidence.

The 300-job expansion cap intentionally sacrifices coverage: larger inventories, exhausted budgets and failed decoding can suppress job/step clustering rather than create misleading boundaries. Raw cache decoding still depends on a compatible job shape; identity checks do not guarantee schema compatibility, so incompatible evidence must be rejected rather than treated as valid.

Reports and in-memory state grow. Consumers must distinguish sub-agent or threat-detection degradation from observed Actions failures instead of interpreting `degraded_workflows` as a general failure count. `report_schema_version: 1`, capability metadata and request counts make these changes explicit, but do not guarantee complete coverage.
