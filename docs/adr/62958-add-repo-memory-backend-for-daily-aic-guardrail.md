# ADR-62958: Add repo-memory backend for daily AIC guardrail

**Date**: 2026-09-23
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

The daily AI Credits guardrail currently computes a trailing 24-hour total by scanning prior workflow runs and downloading usage artifacts when cache data is missing. The PR description and diff show that this becomes expensive for high-volume workflows because every miss drives more GitHub API traffic and per-run artifact lookups. This change adds an object-form `max-daily-ai-credits.backend` setting, compiler wiring, and a JavaScript ledger reader/writer so workflows can use repo-memory as a git-backed rolling ledger instead of artifact-backed scans. The decision is whether to keep the existing API-scan path as the only implementation or add a separate repo-memory backend optimized for repeated high-volume guardrail checks.

### Decision

We will add an opt-in `repo-memory` backend for `max-daily-ai-credits` that stores daily AIC observations in date-bucketed JSONL files under repo-memory and reads the current and previous UTC buckets to evaluate the trailing 24-hour window. gh-aw will compile workflows using this backend to clone the selected repo-memory ledger before the guardrail step, pass backend-specific environment variables to the guardrail script, skip artifact scan-cache restore/publish, and append the current run's AIC to the ledger before the existing repo-memory upload flow. We chose this approach because it directly reduces repeated GitHub API scans and artifact downloads for high-volume workflows while preserving the existing scan-based backend as the default path.

### Alternatives Considered

#### Alternative 1: Keep using only the existing artifact/API scan backend

This was the incumbent design and remains simpler because it uses one implementation path based on workflow-run history plus artifact reads. It was not chosen because the PR explicitly addresses the scaling cost of cache misses for high-volume workflows, and the diff adds dedicated ledger read/write logic, compiler wiring, and schema support to avoid repeated scans.

#### Alternative 2: Replace the existing backend entirely with repo-memory

This was a realistic alternative because repo-memory can provide rolling totals without per-run artifact lookups once ledger state exists. It was not chosen because the PR keeps repo-memory opt-in, validates it only when `tools.repo-memory` is configured, and preserves the existing scan-cache flow for workflows that do not select the new backend.

### Consequences

#### Positive
- High-volume workflows can calculate the daily AIC guardrail from git-backed repo-memory state instead of repeatedly scanning prior runs and downloading usage artifacts.
- The compiler now wires repo-memory activation and post-agent ledger persistence automatically when the backend is selected.
- Validation and tests now cover backend schema acceptance, repo-memory requirements, compiled workflow behavior, and ledger read/write logic.

#### Negative
- The daily AIC guardrail now has two backend paths to maintain, which increases implementation and testing complexity.
- Correctness depends on repo-memory state being cloned, writable, and persisted in the expected order before the existing repo-memory upload flow.
- Ledger data is constrained to the selected repo-memory branch structure and JSONL format, adding another persisted state contract to support over time.

#### Neutral
- The existing artifact/API scan backend remains the default behavior for workflows that do not configure `backend: repo-memory`.
- The repo-memory backend reads only the current and previous UTC day buckets and filters entries by repository, workflow, timestamp, run ID, and valid AIC values.
- Workflow authors must pair `max-daily-ai-credits.backend: repo-memory` with `tools.repo-memory`, and compilation fails fast when that dependency is missing.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
