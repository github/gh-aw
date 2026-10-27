---
description: Diagnose gh-aw diagnostic codes using cached audits, targeted evidence, and code-specific remedies. No dispatch.
disable-model-invocation: true
---

# Debug Playbook

Fix first failing boundary; follow [debugging gates](debug-agentic-workflow.md).
Do not dispatch workflows or execute artifacts. Keep secrets/prompts/configuration/reasoning private.

## Start here

Keep supplied URL in `RUN_URL`, including its fragment.
Derive `REPO` (`HOST/OWNER/REPO`) and `RUN_ID` from it; qualify bare IDs first.

```bash
: "${RUN_URL:?Set URL}"
: "${REPO:?Set HOST/owner/repo}" "${RUN_ID:?Set ID}"
EVIDENCE="${TMPDIR:-/tmp}/gh-aw-debug-evidence"
gh aw audit "$RUN_URL" --no-baseline --timeout 2m \
  --output "$EVIDENCE" --json
```

Read completeness/`overview`/`key_findings`/`recommendations`/`errors`.
Timeout: incomplete JSON report; fetch evidence.
Read only the linked `playbook_url`/code row; [cluster fleets](analyzing-runs.md).
Preserve codes/severity; advisory/pending/skipped/policy-denied aren't necessarily bugs.
Unknown: [unmapped codes](#unmapped-codes).

## Audit findings

| Codes | Check | Remedy |
| --- | --- | --- |
| `workflow_failed` | First failed step; native error. | Route specific cause; conclusion isn't root cause. |
| `workflow_timeout` | Step/budget/result. | Remove blockers/narrow scope; justify timeout changes. |
| `multiple_errors` | First error versus cascades. | Fix cause, not repeats. |
| `mcp_server_failures`, `tools_not_available` | MCP startup, registration, failed calls. | Fix tool/server/config/auth; no blind permission expansion. |
| `model_unavailable`, `model_endpoint_unsupported` | Complete same-run inventory: absent model/incompatible endpoint. | [Model availability](#model-availability); verify candidate/API, edit pin, recompile. |
| `model_availability_unknown` | Missing/incomplete/malformed/ambiguous/contradictory reflection. | Inspect discovery/auth; do not declare removal/switch models without evidence. |
| `model_resolution_failed` | Failed `model.resolution`; missing same-role startup-stop evidence. | Advisory: verify pins; unproven cause. |
| `subagent_model_not_observed` | Spawn, child usage, evidence coverage. | Missing observations don't prove nonexecution. |
| `subagent_model_unavailable` | Child model, request, provider policy/catalog. | Verify child separately; follow [model/engine guide](debug-model-engine-misconfiguration.md). |
| `subagent_failed` | Child error; parent dispatch. | Fix child cause; parent success != child success. |
| `blocked_network_requests` | Destination, caller, denial, necessity. | Fix URL/tool or authorize narrow destination; retain firewall. |
| `threat_detection_job_failed` | Detector startup/runtime versus verdict. | Repair detector; failure isn't a clean verdict. |
| `threat_detected` | Private structured verdict/artifact. | Stop unsafe effects; follow [security guidance](debug-security-review.md). Never publish payloads. |
| `high_token_usage`, `many_iterations` | Tokens/cache/turns, repeated calls. | [Optimize](optimize-agentic-workflow.md) scope/context/loops before raising caps. |
| `agentic_resource_heavy_for_domain`, `agentic_overkill_for_agentic` | Assessment, deterministic alternatives. | Script mechanical work; retain necessary judgment. |
| `agentic_poor_agentic_control`, `agentic_partially_reducible` | Stop rules, tools, reducible stages. | Bound loops; extract deterministic work; preserve outcomes. |
| `agentic_model_downgrade_available` | Capability/API needs; measured alternatives. | Benchmark; no automatic downgrade. |
| `agentic_delegated_context_present` | Delegation/context. | Deduplicate; advisory, not necessarily error. |
| `workflow_succeeded` | Required downstream effect. | Verify missing effect; conclusion alone is insufficient. |

## Agent, model, and proxy classifications

Inspect `agent-stdio.log`, `agent_execution.json`, native errors/reflection/proxy events.
Assistant/tool prose isn't native error.
Inventory: [Model availability](#model-availability).

| Codes | Check | Remedy |
| --- | --- | --- |
| `inference_access_error`, `inference_access_denied`, `authentication_failed` | Native 401/403, credential names, permissions/policy, proxy guards. | Fix access/config; budget/policy denial doesn't warrant credential rotation. |
| `mcp_policy_error` | Blocked MCP server; organization policy. | Use approved tool/authorized policy correction; no bypass. |
| `model_not_supported_error`, `model_endpoint_mismatch_error`, `model_endpoint_mismatch` | Model, API, tool protocol, same-run inventory/routes. | Follow [model/engine guide](debug-model-engine-misconfiguration.md); endpoint change doesn't establish availability. |
| `http_400_response_error` | Provider diagnostic, endpoint, feature/schema. | Fix unsupported request; 400 alone doesn't prove model removal. |
| `unsupported_endpoint`, `invalid_selection`, `uncorroborated_endpoint`, `uncorroborated_selection`, `harness_selection_mismatch` | Selection versus harness model/API/metadata. | Align selection/API; never invent evidence. |
| `unsupported_effort` | Effort versus engine/model support. | Use supported effort/mapping; no silent discard. |
| `missing_model_pricing_error`, `unknown_model_ai_credits` | Proxy pricing table and `missing_model_pricing_model_name`. | Correct supported pricing/version/config; never disable credit accounting. |
| `invocation_cap_exceeded`, `max_runs_exceeded` | Used/max pooled inference calls. | Narrow work/loops; exhausted run cannot recover through identical retries. |
| `max_ai_credits_exceeded`, `ai_credits_rate_limit_error`, `ai_credits_limit_exceeded` | Used/max credits; cap. | Stop; reduce work or authorize future budget. |
| `max_cache_misses_exceeded` | Miss counters; prompt/cache changes. | Stabilize context/cache; no exhausted-guard retry. |
| `effective_tokens_limit_exceeded` | Effective tokens; prompt/turn growth. | Reduce context/iterations; retain guard. |
| `permission_denied_limit_exceeded` | Denied calls; allowed tools. | Stop denied calls; use allowed tools, not larger denial budget. |
| `model_policy_violation`, `content_filter` | Provider/proxy reason; endpoint mismatch separately. | Correct request within policy; no credential swap/bypass. |
| `awf_api_proxy_blocking_requests` | Specific budget/policy/filter guard. | Address guard; blocking alone doesn't establish authentication failure. |
| `capi_quota_exceeded_error` | Native 429; exhausted retries. | Respect quota/reset; no retry storm. |
| `capi_server_error`, `engine_outage`, `infrastructure_error` | Exhausted native 5xx/infrastructure timeline. | Report cause; bounded backoff, no unapproved redispatch. |
| `agentic_engine_timeout` | Step/SDK budget versus post-result `watchdogFired=true`. | Fix genuine stall; post-result watchdog alone doesn't establish timeout. |
| `sandbox_runtime_crash` | Exit/signal, resources, runtime revision. | Fix crash/resources; retain redacted diagnostic/version. |
| `goal_already_active` | Unfinished goal. | Update/complete existing goal; no duplicate. |
| `shell_expansion_guard_rejected` | Private denied command; allowed syntax. | Use allowed tools without dangerous expansion; no obfuscation/bypass. |
| `ERR_MODEL_PIN_REFLECT_REQUIRED` | Missing `--reflect`. | Supply same-run reflection. |

## Model availability

Read `engine_config` ([Start here](#start-here)). Pi `not advertised`:
confirmed absence → `model_unavailable`; incomplete/invalid/ambiguous evidence →
`model_availability_unknown`. No auto-substitution.
Inspect quoted job/step URLs/lines; missing context:
`gh run view "$RUN_ID" --repo "$REPO" --log`.

### Inspect cached models

```bash
gh aw models --json --refresh-observed=false --logs-dir "$EVIDENCE"
# Broaden only if needed:
gh aw models --repo "$REPO" --logs-dir "$EVIDENCE" --refresh-count 5
```

Keep `catalog`, `aliases`, `observed` distinct; aggregates don't prove same-run access.
Prefer cache; refresh downloads artifacts.

### Verify exact same-run inventory

Read same-run `$EVIDENCE/run-$RUN_ID/sandbox/firewall/awf-reflect.json` (or audit-reported path).

```bash
: "${MODEL:?Set exact model}" "${PROVIDER:?Set provider}"
jq --arg model "$MODEL" --arg provider "$PROVIDER" '
  {models_fetch_complete, endpoints: [
    .endpoints[]?
    | select(.configured == true and .provider == $provider)
    | {provider, models,
       routing: [.routing_models[]? | select(.model_id == $model)]}
  ]}' "$EVIDENCE/run-$RUN_ID/sandbox/firewall/awf-reflect.json"
```

Strip provider prefix/query; preserve dated/version IDs.
Resolve provider aliases (`github` → `copilot`); inspect `auto`/`opus`
`model_aliases`/harness resolution. Alias != concrete ID; `routing_models` != inventory.

| Evidence | Meaning |
| --- | --- |
| Complete discovery; one matching configured endpoint; valid array; exact ID absent | Unavailable; inspect candidate endpoints. |
| Missing/incomplete/malformed/contradictory reflection; ambiguous/wrong endpoint; null inventory | Availability is unknown; inspect discovery/auth. |
| ID present; API absent from `supported_endpoints` | Endpoint mismatch. Follow [model/engine guide](debug-model-engine-misconfiguration.md). |
| ID present; inference rejected | Inspect request model/API/status/policy/auth error. |

`token-usage.jsonl`: empty file + fatal startup rejection supports no inference;
missing file proves nothing. Missing/expired/denied artifacts remain unavailable.
Use targeted `gh run download` for `agent`/`info` only if audit retrieval fails.

### Compare one success

```bash
: "${BASE_RUN_ID:?Set successful ID}"
gh aw audit "$BASE_RUN_ID" "$RUN_ID" --repo "$REPO" \
  --output "$EVIDENCE" --json
```

First run = reference. Compare model/provider, credential source (names only),
revision, runtimes, inventory; success doesn't establish model identity.
Different inventories with same AWF version: not proof of regression/retirement.
Require evidence for removal time/cause.
Trace pins: `git log -S 'model: PROVIDER/MODEL' -- WORKFLOW.md`.

Absent pin: verify same-provider API/capabilities/policy.
Authorized patch: edit source/recompile; compilation cannot test inference.
Runtime defect: [model/engine guide](debug-model-engine-misconfiguration.md).

## Completion and safe-output errors

Trace registration → invocation → persistence → validation → publication.
Nonempty `safeoutputs.jsonl` doesn't prove publication.

| Codes | Check | Remedy |
| --- | --- | --- |
| `agent_startup_failure` | First setup/engine error. | Fix startup; missing output need not be agent choice. |
| `engine_driver_failure`, `driver_crash` | Driver exit/signal; final native error. | Fix driver first; nonzero exit alone doesn't establish outage. |
| `secret_redaction_failure` | Redaction status; sanitized diagnostic. | Repair redaction before publication; never expose raw logs/disable masking. |
| `safeoutputs_cli_error` | Arguments, registration, denial. | Fix supported invocation/allowlist; no unauthorized `gh` writes. |
| `invalid_safe_outputs` | Record errors; generated schema. | Fix emitted type/fields; retain validation. |
| `missing_terminal_safe_output` | Expected terminal tool, denied calls, last session event. | Repair publication path or prompt completion contract; silence is not an intentional noop. |
| `request_rejection`, `prompt_exhaustion`, `unknown` | Native error, budget, missing evidence. | Resolve cause; retain unknown without evidence. |
| `ERR_VALIDATION`, `E001` | Fields/types, target IDs, schema. | Fix producer; no silent coercion. |
| `E002`, `MAX_COUNT_REACHED` | Operation cap; accepted records. | Reduce count; never replay applied mutations. |
| `E003` | Named count/size limit. | Split/reduce authorized payload. |
| `ERR_PERMISSION`, `E004`, `APPROVE_WORKFLOW_RUN_PERMISSION_DENIED`, `DISCUSSIONS_TOKEN_SCOPE_MISMATCH` | Target/scope, permissions, token type. | Fix authorized permissions; never expose tokens/evade guards. |
| `E006` | Body/string length. | Shorten; retain required content. |
| `E008` | URL count/limit. | Reduce links; no concealment. |
| `ERR_API`, `E007` | GitHub status; operation guard. | Resolve API/target cause; policy skip != failed mutation. |
| `ERR_CONFIG`, `ERR_ADMISSION` | Config/schema, OIDC permissions, admission decision. | Fix config/service; honor denials/outage policy; recompile; withhold credentials/environment. |
| `ERR_COMPILED_WORKFLOW_PROVENANCE_REQUIRED`, `ERR_COMPILE_AGENTIC_VERSION_BLOCKED`, `ERR_COMPILE_AGENTIC_VERSION_OUTDATED` | Compiler version versus provenance/compatibility minimums/blocklist. | Update gh-aw; recompile/publish source+lock; retain enforcement. |
| `ERR_GRADER_SESSION_INVALID` | Unified event array: type/data/provenance. | Fix producer/caller; never fabricate sessions. |
| `ERR_SAFE_OUTPUT_TOOL_COPY_FAILED` | Tool deep-copy failure. | Fix definition/loader; retain copy isolation. |
| `ERR_SAFE_OUTPUTS_GATEWAY_CONFIG` | Numeric `MCP_GATEWAY_PORT`; present `MCP_GATEWAY_AGENT_ID` (private). | Repair configuration; retain readiness/authentication checks. |
| `ERR_NOT_FOUND`, `TARGET_NOT_FOUND` | Target/repository; visibility. | Correct target/access; 404 may conceal inaccessible resources. |
| `ERR_PARSE` | JSON/NDJSON framing/schema/producer. | Fix producer/format; retain private evidence. |
| `ERR_SYSTEM`, `E099` | First file/git/process failure. | Fix I/O/runtime/checkout; wrapper is not cause. |
| `E009`, `CONFIG_HASH_MISMATCH` | Source versus lock hash. | Recompile/publish source+lock together; no manual hash edit. |
| `E010`, `RATE_LIMIT_EXCEEDED` | Retry budget; reset headers. | Stop exhausted retries; respect reset/backoff; reduce API work. |
| `POLICY_FILE_PROTECTION_DENIED` | Allowed/protected files and attempted patch. | Respect intentional skip; constrain change, do not disable protection. |
| `NO_CONTEXT` | Trigger context; explicit target. | Supply authorized target/context-appropriate output. |
| `NO_LABELS_PROVIDED` | Requested/existing labels. | Supply valid labels; don't claim mutation. |
| `REQUIRED_LABELS_MISMATCH`, `REQUIRED_TITLE_PREFIX_MISMATCH` | Labels/title versus guards. | Respect deliberate skip; fix mistaken target/config only. |
| `TARGET_LOCKED`, `NOT_AWAITING_APPROVAL` | Lock/approval state. | Respect state; no unrelated unlock/approval. |
| `DAILY_AIC_QUOTA_STOP`, `quota_reserve`, `quota_exhausted`, `rate_limited` | Response quota; retry-not-before. | Stop/defer within bounds; missing accounting != zero usage. |
| `AIC_USAGE_UNKNOWN` | Missing usage/accounting. | Fix collection/access; remaining budget stays unknown. |
| `permission_denied` | Business response; operation authority. | Fix authorized credentials/permissions, not quota. |

## Merge guard codes

Check current PR head/base/checks/reviews/guards; diagnosis isn't merge authorization.

| Codes | Check | Remedy |
| --- | --- | --- |
| `source_branch_invalid`, `target_base_branch_invalid`, `branch_not_allowed` | Syntax/patterns; actual head/base. | Fix authorized target/config. |
| `target_branch_protected`, `target_branch_default`, `target_branch_has_no_open_pr` | Protection/default restrictions; required PR. | Use permitted PR path; retain protections. |
| `pr_is_draft` | Draft state. | Author-authorized readiness only. |
| `merge_conflicts`, `not_mergeable` | Live mergeability/conflicts; unknown != conflict. | Resolve on reviewed head; bounded wait for known state. |
| `missing_required_labels`, `title_prefix_mismatch` | Required labels/title. | Authorized correction; retain handler guards. |
| `required_checks_missing`, `required_checks_failing` | Check identity/SHA/completion/result. | Fix current-head checks; no bypass. |
| `pending_reviewers`, `blocking_review_state`, `unresolved_review_threads` | Pending/blocked reviews; open threads. | Address feedback; retain reviewer authority/policy. |
| `merge_not_completed`, `merge_operation_error` | API response; live state. | Reconcile before retry; possibly merged. |

## Parser and ledger diagnostics

Check producer/version/location/completeness.

| Codes | Check | Remedy |
| --- | --- | --- |
| `malformed_jsonl`, `malformed_agy_event`, `malformed_message_content` | Framing/event shape; truncation. | Fix compatibility; partial evidence != zero activity. |
| `malformed_tool_request`, `malformed_tool_response`, `partial_tool_capture` | Call/result IDs; capture boundaries. | Fix capture; missing counterpart stays unknown. |
| `invalid_usage`, `invalid_source_metadata` | Usage; source attribution. | Fix producer; never invent tokens/model attribution. |
| `invalid-json`, `invalid-envelope`, `invalid-schema`, `unsupported-version` | Ledger framing/envelope/schema/version. | Fix compatible producer; retain bad records/history. |
| `invalid-sha`, `missing-parent`, `cycle`, `truncated-record` | Digest, parents, DAG, newline. | Fix authorized producer/storage; never fabricate hashes/history. |

## Work-queue codes

Follow [queue playbook](work-queue.md); inspect read-only.
Verify repo/branch/tip, epoch, fingerprint, Work/Claim/Delivery IDs, run binding/receipts.
Pending projection != claim failure.
Never rewrite history, fabricate evidence, reuse credentials/request IDs across work,
or bypass profiles/effect scopes.

| Codes | Check | Remedy |
| --- | --- | --- |
| `ERR_WORK_QUEUE_DELIVERY_READBACK_REQUIRED` | Missing trusted readback. | Wire independent verifier; otherwise block delivery. |
| `unsupported_protocol`, `codec_invalid`, `invalid_json`, `invalid_utf8`, `invalid_unicode`, `duplicate_key`, `noncanonical_number` | Version, canonical bytes, keys, Unicode/numbers. | Align producer/reader; regenerate input, not history. |
| `ledger_invalid`, `ledger_foreign`, `ledger_nonextending` | Queue identity, DAG, ancestry. | Stop publication; recover trusted queue/tip via protocol. |
| `queue_missing`, `policy_missing`, `work_queue_pool_not_installed` | Queue/pool; compiled/installed policy. | Bootstrap queue/policy; absent pool: quiesce; administrator-authorized Policy update adds pool. |
| `repository_unavailable`, `repository_scope_invalid`, `branch_invalid`, `git_transport_failed` | Repo/branch, credentials, Git failure. | Restore authorized transport; reread uncertain tip. |
| `actor_unauthorized`, `admission_unauthorized`, `unauthorized_operation`, `ownership_unauthorized`, `work_unauthorized`, `dependency_unauthorized`, `projection_unauthorized`, `resource_unauthorized` | Principal, role/profile, operation/resource scope. | Use authorized actor/path; no impersonation/scope expansion. |
| `policy_invalid`, `policy_epoch_invalid`, `policy_proposal_mismatch`, `work_queue_policy_proposal_mismatch`, `profile_invalid`, `pool_invalid`, `dispatcher_invalid` | Schema/revision, epoch, pools/dispatcher. | Correct trusted config via supported deployment/update. |
| `work_queue_profile_not_approved`, `work_queue_worker_not_compiler_approved`, `work_queue_dispatch_credential_identity_unavailable` | Installed-profile check; caller-compiled dispatch allowlist. | Quiesce; update Policy for profiles; fix allowlist/credentials; reject unverified identity. |
| `deployment_invalid`, `deployment_conflict` | Schema, revision, expected tip. | Fix trusted deployment; reconcile before CAS retry. |
| `policy_not_quiescent`, `admission_paused`, `grants_paused` | Claims/dispatches; pause controls. | Respect pause; reconcile before authorized Policy change. |
| `work_expired` | Work deadline; queue time. | Refresh fair prefix; never revive expired Work. |
| `hard_clear_snapshot_invalid`, `hard_clear_conflict` | Preview revision; current tip/receipt. | Reconcile; preview/authorize afresh; don't reset newer snapshots. |
| `hard_clear_not_paused` | Admission/grant controls. | Pause both before authorized hard clear. |
| `hard_clear_unresolved`, `hard_clear_uncertain` | Reservations, Claims, barriers, effects, request receipt. | Reconcile trusted evidence before retry; never discard unresolved effects. |
| `graph_invalid`, `graph_pool_conflict`, `node_conflict`, `replacement_invalid`, `work_conflict`, `work_identity_invalid`, `work_invalid`, `identity_invalid` | Graph/node, pool, payload/fingerprint, replacement. | Fix submission contract; retain accepted identity. |
| `dependency_cycle`, `dependency_duplicate`, `dependency_invalid`, `dependency_missing`, `dependency_result_unavailable`, `dependency_credentials_missing` | DAG, dependency, receipt, scoped credentials. | Fix graph/legitimate result/access; never fabricate completion. |
| `dependency_verifier_required`, `delivery_verifier_required`, `nonlaunch_verifier_required`, `remediation_verifier_required` | Trusted verifier; evidence type. | Use approved verifier; absence blocks effect, never disable verification. |
| `ERR_WORK_QUEUE_CLAIM_SCOPE`, `assignment_invalid`, `assignment_immutable`, `claim_invalid`, `claim_missing`, `claim_conflict`, `claim_ineffective` | Immutable array of 1-16 Claims; owner, epoch, deadline. | Fix producer/reconcile; preserve effective Claim identities/scopes. |
| `claim_scope_invalid`, `claim_scope_required`, `claim_effects_unauthorized`, `claim_control_delivery_invalid` | Claim handle/factory, scope, control delivery. | Retain original write scope and validated control intent. |
| `child_entitlement`, `run_binding_conflict`, `run_invalid` | Parent/child entitlement; workflow/run/attempt identity. | Correct authorized binding; reject reused/mismatched authority. |
| `condition_invalid`, `control_invalid`, `packing_invalid`, `selection_invalid`, `priority_conflict` | Policy; canonical control/scheduler inputs. | Recompute authoritative state; use allowed transition. |
| `request_invalid`, `request_missing`, `request_fingerprint`, `request_reuse`, `request_reused`, `reason_invalid`, `timestamp_invalid` | Identity/fingerprint, schema, reason/timestamp. | Exact retry only for identical intent; otherwise fresh ID. |
| `publication_conflict`, `publication_invalid`, `publication_unresolved` | Git tip, CAS, published commit. | Reread/reconcile before retry; publication may have succeeded. |
| `checkpoint_invalid`, `credential_generation_reused` | Checkpoint proof/ancestry; unique credential generation. | Recover verified checkpoint/authorized new generation; no stale authority. |
| `dispatch_invalid`, `dispatch_missing`, `launch_started`, `launch_unresolved` | Reservation, receipt, possibly started child. | Reconcile immutable identity; no duplicate launch. |
| `claim_delivery_unverified`, `delivery_failed`, `verification_unresolved`, `delivery_conflict`, `delivery_barrier_conflict`, `delivery_contract_required`, `delivery_evidence_required`, `evidence_invalid`, `evidence_required`, `terminal_evidence_required`, `nonlaunch_evidence_required` | Claim state/effects; independent receipts; barrier/contract. | Reconcile before retry; never force terminal state. |
| `result_invalid`, `completion_conflict`, `cancellation_conflict`, `ownership_terminal`, `work_terminal`, `work_missing` | Work/ownership state; finish/result intent. | Respect terminal transitions; reconcile duplicate/conflicting completion/cancellation. |
| `retry_invalid`, `retry_exhausted`, `release_invalid`, `release_open_claims`, `remediation_invalid` | Retry/release policy, attempts, open claims. | Stop exhausted retries; reconcile claims; authorized recovery only. |
| `ERR_WORK_QUEUE_RECONCILIATION`, `observation_invalid`, `observation_conflict`, `reconciliation_invalid`, `reconciliation_pending`, `inspection_invalid`, `resource_unverified` | Reason, run/Claim binding, provenance/freshness. | Bounded trusted reconciliation; unverified stays blocked; never force release. |
| `resource_invalid`, `resource_limit`, `assignment_limit`, `evidence_limit`, `graph_limit`, `work_queue_graph_limit`, `ledger_limit`, `observation_limit`, `operation_limit`, `payload_limit`, `pending_limit`, `projection_limit`, `reconciliation_limit` | Named limit; actual count. | Bound/batch/compact through supported operations; retain limits. |
| `issue_binding_invalid`, `issue_binding_conflict` | Queue-issue binding; ownership. | Correct authorized binding; never adopt another work item's issue. |
| `projection_invalid`, `projection_journal_invalid`, `projection_journal_conflict`, `projection_scope_changed`, `projection_write_ambiguous` | Journal, native acknowledgement, target, CAS. | Reconcile receipts; never duplicate uncertain writes, remove uncertain locks or rewrite history. |
| `projection_target_unavailable`, `projection_unavailable` | Actual failure; safe status/request ID. | Diagnose cause; preserve mirrors and locks. |
| `projection_receipt_pending`, `projection_comment_pending`, `projection_label_pending`, `projection_coordination_pending`, `projection_pagination_pending`, `projection_rate_pending` | Checkpoint, pacing, existing mirrors/receipts. | Reconcile exact request; never recreate mirrors or mistake pending for terminal failure. |

## Unmapped codes

| Codes or signature | Check | Remedy |
| --- | --- | --- |
| HTTP `401`, `403`, `404`, `429`, `5xx` | Operation, response, proxy guard, policy. | Distinguish auth/SSO, hidden resource, quota, outage; status alone is insufficient. |
| JSON-RPC `-32700`, `-32600`, `-32601`, `-32602`, `-32603`, `isError` | Protocol versus tool failure. | Fix parse/request/method/params/client/schema/server; tool errors need not be protocol errors. |
| `EACCES`, `ENOENT`, `ETIMEDOUT`, process exit/signal | Operation/path; fatal versus caught. | Fix boundary; caught `/run/awf-config-audit` warning does not prove later inference failure. |
| Unlisted code, compile/schema/scanner error | Exact identifier at producing revision; emitter/caller. | Trace cause; add regression/code row; report gaps, never guess. |

No run: inspect file/line; follow [compile-only gates](debug-compilation.md).
Compilation doesn't verify inference/authorize execution.

Report code/boundary/revision, evidence/uncertainty, remedy/files/execution.
No blind retries/downgrades/credential-rotation/guard-bypass.
