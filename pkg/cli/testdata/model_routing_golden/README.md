# Model-routing audit goldens

These fixtures pin the routing and token-usage analysis used by `gh aw audit` and
the multi-run `gh aw logs` summary. They are minimized, sanitized extracts of the
listed workflow runs; they retain only the inputs needed by those analyses.

| Case | Source run | Scenario |
| --- | ---: | --- |
| `copilot-routed-gpt-responses` | `37886511291` | Copilot routing through `/responses`, classifier credits, and legacy raw-token fallback |
| `claude-awf-selected-messages` | `37971713032` | Claude selected through `/v1/messages`, including the legacy endpoint normalization |
| `pi-claude-two-subagents` | `37971726317` | Pi session with two Claude subagents and tool-execution correlation |
| `copilot-subagent-failed-and-alias` | `37971738805` | Failed and aliased Copilot subagent routing, cost merging, and dated served model IDs |
| `legacy-no-session-routing` | `37505153056` | Legacy run without unified routing events; routing and classifier costs come from proxy logs |

## Verify and refresh expected output

Run `make verify-model-routing-golden` to analyze every committed fixture and
compare the results with its `expected*.json` file. The test also verifies that
legacy fixtures without `usage/aw_session.jsonl` retain their documented
information loss and that the logs aggregate is independent of run order.

After an intentional analysis change, run `make update-model-routing-golden`.
Review every changed expected file in the diff; regeneration is not a substitute
for checking that the changed routing, credit, token, warning, and subagent
values are correct.

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
`make update-model-routing-golden`. Review both the sanitized fixture and the
generated golden diff. Capture redacts content, local paths, conversation
hashes, signatures, encrypted fields, email addresses, and repository identities;
it intentionally retains AWF credit values, model IDs, endpoints, efforts,
token counts, timestamps, and request IDs because the analysis depends on them.

Use verify-only mode in CI or before submitting fixture changes:

```bash
python3 scripts/model-routing-golden.py --verify
python3 scripts/model-routing-golden.py --verify --case new-case
```
