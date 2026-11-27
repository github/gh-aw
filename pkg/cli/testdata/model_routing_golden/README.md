# Model-routing audit goldens

These fixtures pin the routing and token-usage analysis used by `gh aw audit` and
the multi-run `gh aw logs` summary. They are minimized, sanitized extracts of the
listed workflow runs; they retain only the inputs needed by those analyses.

| Case | Source run | Scenario |
| --- | ---: | --- |
| `copilot-routed-gpt-responses` | `37886511291` | Copilot routing through `/responses`, classifier credits, and legacy raw-token fallback |
| `claude-awf-selected-messages` | `37971713032` | Claude selected through `/v1/messages`, including the legacy endpoint normalization |
| `claude-awf-steering-notices` | `37971713032` | Edge case only: synthetic copy of `claude-awf-selected-messages` with hand-added AWF `steering` fields (agent `ai_credit` 80% and `timeout` 90%, detection `token` 95%); the legacy variant lists no notices. `copilot-budget-exhausted-steering` covers real `ai_credit` notices; this case remains because no real run has a `timeout` notice or a detection-phase `token` notice |
| `pi-claude-two-subagents` | `37971726317` | Pi session with two Claude subagents and tool-execution correlation |
| `copilot-subagent-failed-and-alias` | `37971738805` | Failed and aliased Copilot subagent routing, cost merging, and dated served model IDs |
| `legacy-no-session-routing` | `37505153056` | Legacy run without unified routing events; routing and classifier costs come from proxy logs. Its unified sub-agent lifecycle events lack `agentId`; audit links each instance to its per-agent metrics and configured effort through the matching `data.toolCallId` start in `agent-session.jsonl` |
| `copilot-budget-exhausted-steering` | `38174216426` | Copilot CLI routed to gpt-5.6-sol with `max-ai-credits: 30`: real AWF `ai_credit` steering notices at 80/90/95%, a proxy `ai_credits_limit_exceeded` failure, three requests rejected with HTTP 403, and 30.1812 AIC including a 0.6796 AIC classifier |
| `codex-routed-provider-unavailable` | `37728959715` | Codex routed to gpt-5.6-luna (high) on `/responses`: one aborted request, a proxy `provider_unavailable` failure, and an upstream 400; predates `workflow.info` routing, so the status comes from the proxy log alone |
| `copilot-sdk-claude-subagent-completions` | `38181851405` | Copilot SDK mode with the main session on gpt-5.6-luna (xhigh); sub-agent `file-summarizer` on claude-haiku-4.5 through `/chat/completions` and `quick-checker` on gpt-5.6-luna |
| `copilot-cli-builtin-subagents` | `38164386960` | Copilot CLI routed to gpt-5.6-sol (medium) with the built-in `explore`, `research` and `task` sub-agents on gpt-5.6-luna and `code-review` and `general-purpose` on gpt-5.6-sol in the unified session; audit names their rows by agent definition (`agentName`) and lists the chosen display names in `display_names` |
| `copilot-routed-fallback-no-route` | synthetic | Copilot CLI routed fallback selection (`selection_source=fallback`, `fallback_reason=no_route`, `fallback_tier=standard`, `router: null`, `ranked_choices: null`) with zero classifier requests and one main-agent `/responses` request; legacy variant without `usage/aw_session.jsonl` omits fallback provenance |

Audit does not read the conclusion job's failure issue, `agent-stdio.log`,
`awf-config.json`, or `agent_execution_exit_code.txt`, so the goldens cannot pin
them. They are recorded here for the routed-failure cases:

| Case | Harness exit code | Failure issue title |
| --- | ---: | --- |
| `copilot-budget-exhausted-steering` | 78 | `[aw] Task exceeded max AI credits` |
| `codex-routed-provider-unavailable` | 78 | `[aw] Task reported incomplete result` (does not mention routing) |

## Known discrepancies pinned by the goldens

These are current audit behavior, not intended behavior. Each is allowed by the
case's `legacyRoutingAllowance` in `model_routing_golden_test.go`, and the test
fails once the discrepancy no longer occurs. When fixing one, remove the
allowance and this entry and review the golden diff.

- `copilot-budget-exhausted-steering` reports `status: rejected` with the
  session (`usage/aw_session.jsonl`) and `status: failed` without it; both
  variants report `failure.code: ai_credits_limit_exceeded`. The `rejected`
  status comes from `aw_info.json` and the session `workflow.info`, where
  `resolveModelRoutingOutcome` in `actions/setup/js/parse_token_usage.cjs`
  compared the final budget failure record, which has no `wire_model`, with the
  advisory `awf-routing-outcome.json` and relabeled the outcome as
  `harness_selection_mismatch`. That code never appears in the audit report.
- `copilot-sdk-claude-subagent-completions` and `copilot-cli-builtin-subagents`
  lose `effective_endpoint` and `selected_endpoint` in the legacy variant. In
  these runs only `agent/aw_info.json` carries `model_routing`, and
  `applyAwInfoModelRouting` in `pkg/cli/model_routing.go` skips that file. The
  older fixtures still carry `model_routing` in the root `aw_info.json`.

## Verify and refresh expected output

Run `make verify-model-routing-golden` to analyze every committed fixture and
compare the results with its `expected*.json` file. The test also verifies that
legacy fixtures without `usage/aw_session.jsonl` retain their documented
information loss and that the logs aggregate is independent of run order.

After an intentional analysis change, run `make update-model-routing-golden`.
Review every changed expected file in the diff; regeneration is not a substitute
for checking that the changed routing, credit, token, warning, and subagent
values are correct.

The first six fixtures in the table predate the capture tool, so the first
recapture of one of them may rewrite its fixture files without changing its analysis.
For example, a recapture uses `example-org/routing-fixture` instead of the
committed fixtures' `example-org/routing-sandbox`, retains a different set of
fields, redacts `task_complexity`, and omits `usage/aw_info.json` for
`claude-awf-selected-messages` and `copilot-subagent-failed-and-alias` because
it was not present in those source downloads. When reviewing that refresh,
confirm that the `expected*.json` files are unchanged and run
`make verify-model-routing-golden`. The capture command also checks for
unredacted source content before writing the fixture.

`claude-awf-steering-notices` is not a capture: its source run predates AWF
steering records, so a recapture would drop the hand-added `steering` fields.
Edit its `token-usage.jsonl` and `usage/aw_session.jsonl` together instead.

## Capture or add a case

Capture a workflow run with:

```bash
python3 scripts/model-routing-golden.py --run 123456789 --case new-case
python3 scripts/model-routing-golden.py --run https://github.com/owner/repo/actions/runs/123456789 --case new-case
```

The command uses the local `gh-aw logs --stdin --artifacts all` download path,
filters and redacts the resulting files, and compares routing and token-usage
analysis on the full download with analysis on the minimized fixture before it
writes anything. Use `--replace` to replace an existing case. For offline tool
development, pass a downloaded-run directory with `--source-dir`; the synthetic
input under `testdata/model_routing_golden_capture/` exercises this path.

To add a case, capture it, add its description and expected costs to
`modelRoutingGoldenCases` in `model_routing_golden_test.go`, then run
`make update-model-routing-golden`. Run `make fmt` as well: `make lint` checks
the fixture JSON with prettier, which only changes whitespace. Review both the
sanitized fixture and the generated golden diff. Capture redacts content, local paths, conversation
hashes, signatures, encrypted fields, email addresses, and repository identities;
it intentionally retains AWF credit values, model IDs, endpoints, efforts,
token counts, timestamps, and request IDs because the analysis depends on them.

Use verify-only mode in CI or before submitting fixture changes:

```bash
python3 scripts/model-routing-golden.py --verify
python3 scripts/model-routing-golden.py --verify --case new-case
```
