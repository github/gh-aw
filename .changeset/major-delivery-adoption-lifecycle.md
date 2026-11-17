---
"gh-aw": major
---

Replace generic safe-output reporting with shared delivery and adoption lifecycle
reports in logs, single-run audit, episodes, experiments, and the MCP tools.
The metrics collector uses separate delivery and adoption records and no longer
infers pending adoption from resource searches.

**Breaking migration:** replace `logs --outcomes` with `logs --adoption`, or
`--delivery` when only execution facts are needed. Replace `--outcomes-dir` and
`GH_AW_OUTCOMES_DIR` with `--adoption-dir` and `GH_AW_ADOPTION_DIR`.
The artifact selector is `delivery`. JSON uses `delivery`, `adoption`,
`adoption_summary`, `adoption_status`, and `episode_adoption`; cost/time/coverage
fields use adopted-output/adoption names. Exported item files are
`adoption-RUN_ID.jsonl`. Regenerate cached logs JSONL using schema version 7.
No compatibility aliases are retained.

Execution facts use `safe_output.execution` and `safe-output-executions.jsonl`,
replacing `safe_output.outcome` and `safe-output-outcomes.jsonl`. Update session
consumers and recompile workflows; existing published sessions are not rewritten.
The report caller timeout is `GH_AW_ADOPTION_GH_TIMEOUT_MS`, replacing
`GH_AW_OUTCOME_GH_TIMEOUT_MS`.

Experiments use `adoption:accepted`, `adoption:zero_touch`, `adoption_window`,
and `experiments analyze --adoption` instead of their outcome-named equivalents.
Update references from `/reference/outcomes/` to `/reference/delivery-adoption/`.

Delivery requires executed-effect evidence. Adoption distinguishes verified human
decisions from explicit delivery-time fallback rules; infrastructure is not value.
Current survival observations do not establish full-window survival or attributed
value. Retained lifecycle records are independent of downloaded run folders.

Use `logs --adoption --retained --repo OWNER/REPO --output DIR` for bounded
refresh after restoring only the retained ledger, even when source artifacts
have expired. Metrics Collector now persists lifecycle state in separate
repo-memory, resumes discovery after missed executions, and prepares
`metrics/lifecycle.json` for existing reports without requiring webhooks.
