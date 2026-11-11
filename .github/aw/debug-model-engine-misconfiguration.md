---
description: Diagnose AWF model/endpoint 400s, cross-family sub-agent failures, model:auto failures, and version-pinned install 404s.
disable-model-invocation: true
---

# Model and Engine Misconfiguration

Use this checklist for AWF 400s mentioning models or endpoints, silent cross-family
sub-agent failures, `model: auto` failures, install-step 404s after a version pin,
or questions about version fields. Referenced from [debug-agentic-workflow.md](debug-agentic-workflow.md#identify-the-first-failing-boundary).

Read the [shared debugging strategy](debug-agentic-workflow.md) before running
these commands or applying fixes. For CLI setup and run-URL scoping, use
[debug-bootstrap.md](debug-bootstrap.md). This checklist supplies model-specific evidence;
it does not authorize execution, publication, or live testing.
Preserve full run/job/step URLs; add `--repo HOST/OWNER/REPO` to bare-ID audit
examples below rather than inferring the target from the current checkout.

`not advertised`/model-not-found: [verify same-run availability](debug-playbook.md#model-availability)
before changing model pins or runtimes.

Read the [run artifacts](#5-read-the-run-artifacts) before proposing changes.
Explanation-only sessions inspect evidence without installing or upgrading.
Preserve intentional pins unless a verified relevant fix requires a change
within the user's authorized scope.

## 1. Check the gh-aw compiler version first

Read `compiler_version` in the lock file's `gh-aw-metadata` header and `cli_version`
in `aw_info.json`. These identify the compiler used for the run; the locally
installed `gh aw version` alone does not establish that the lock was recompiled.

Compare with the newest published gh-aw release, **including prereleases**:

```bash
gh release list --repo github/gh-aw --limit 20
```

Select the newest applicable tag from the release list; increase
the limit if needed. `gh extension install` and `gh extension upgrade` select the
latest non-prerelease by default, so a successful upgrade can still leave gh-aw
several releases behind. `--force --pin TAG` installs a specific prerelease even
when the extension is already installed; later upgrades retain that pin.

If the compiler predates the relevant fix, **upgrade gh-aw and recompile before
trying other fixes**. In the customer case, v0.89.21 predated the wire-API
inference fix in [github/gh-aw#64177](https://github.com/github/gh-aw/pull/64177);
v0.91.7 was the newest prerelease reported then. Do not change only a runtime pin
or retry the old lock and expect the compiler fix to apply.

Only when that upgrade is needed and installation is permitted:

```bash
# Historical customer-case tag, not a permanent latest-version recommendation:
gh extension install github/gh-aw --force --pin v0.91.7
gh aw version
gh aw compile WORKFLOW --dry-run
```

## 2. Match the resolved model to the wire API

The Copilot harness resolves aliases such as `auto` through the AWF alias map and
model catalog to a concrete model ID. An alias is not a fixed model: inspect the
run's resolution rather than assuming what `auto` selected.

When model routing is enabled (`model: router`, `router/economy`, `router/balanced`,
`router/robust`, or `engine.model-routing`), AWF's per-model endpoint metadata
(`/reflect` `supported_endpoints`) takes precedence. AWF checks compatibility at
startup and fails fast with `Model endpoint mismatch: … Pin a compatible model,
remove the COPILOT_PROVIDER_WIRE_API override, or upgrade gh-aw.` This check
precedes the generic CLI wire-API inference below.

For the normal Copilot harness path, `COPILOT_PROVIDER_WIRE_API` is chosen in this
order:

1. An explicit `engine.env.COPILOT_PROVIDER_WIRE_API` override.
2. The resolved model's catalog `wire_api`.
3. For a `-utility` model absent from the catalog, the base model's catalog entry.
4. The `gpt-5+` name rule, which selects `responses`.
5. The Copilot CLI default, `/chat/completions`, if nothing selected a wire API.

`responses` selects `/responses`; `completions` selects `/chat/completions`.
The CLI uses one wire API for the whole session, including sub-agents. A main
model and sub-agent model cannot use different endpoints in the same session;
replace an incompatible sub-agent model with one supporting the main session's
endpoint. When Copilot SDK mode (`engine.copilot-sdk: true`) is available, each
model uses a provider for its own wire API, so a Claude sub-agent can run under a
GPT main model without the CLI's session-wide endpoint constraint.

Investigate these signatures as model/endpoint or model-policy misconfiguration,
not a transient failure or a prompt problem:

- `Cannot translate Copilot request feature`
- `Unsupported Responses custom tool`
- `model_policy_violation`
- `not accessible via the … endpoint`
- `Routing model "<model>" to /chat/completions is incompatible`

Confirm the actual model and request path before diagnosing: the error may come
from a sub-agent rather than the main model. For `model_policy_violation`, also
inspect the configured model allowlist/denylist and the rejected model: a policy
rejection alone does not establish an endpoint mismatch.

On AWF v0.28.50, a cross-family sub-agent can fail with the incompatible-routing
400 while the main agent retries the work using its own model. The run may then
succeed without showing the sub-agent failure in its final output. Check for
`subagent.failed` events in `usage/aw_session.jsonl`, and for the **Sub-agent
Failed** finding and `deviated` requests in `gh aw audit RUN_ID`.

### AI-credit pricing rejection is separate from alias or endpoint resolution

`unknown_model_ai_credits` or `Model "<model>" has no AI credits pricing` means
AWF's active AI-credit guard could not price the selected model. An alias in
`apiProxy.models` only selects model IDs; it does not supply their pricing.
Inspect the same run's `awf-config.json`, requested/resolved model, provider
catalog, and AWF version before changing aliases or wire APIs.

For example, Contribution Check run `39068401375` used AWF `v0.28.62` and emitted
the `kiwi` alias, but its `report-formatter` sub-agent received this pricing 400.
The config had no provider pricing overlay, `modelPricingCatalog`, or explicit
`defaultAiCreditsPricing`. This establishes a pricing rejection, not a missing
compiler alias or an endpoint mismatch. Correct pricing must come from verified
provider/runtime metadata or an explicitly configured, accurate pricing table;
do not invent a fallback rate, disable the budget, or substitute an older model.

The parent can recover and leave Actions green. After collecting compact usage
with `gh aw logs --artifacts usage`, inspect `gh aw health --logs-dir <cache-dir>`
for `subagent_failures_observed`, invocation counts, and affected run URLs.
Health reuses matching cached summaries without downloading more artifacts;
missing cached evidence does not establish successful delegation.

## 3. Apply fixes in this order

1. **Upgrade gh-aw and recompile.** Only if the run's compiler predates a
   verified relevant fix; otherwise retain the version and continue diagnosis.
2. **Pin a model that supports the required endpoint.** Check the current catalog
   and observed request path; do not assume all aliases or GPT models are interchangeable.
3. **Remove a conflicting `COPILOT_PROVIDER_WIRE_API` override.** Let the updated
   harness infer the endpoint unless an override is demonstrably required.
4. **Use sub-agent models from the main model's family in CLI mode.** Verify
   endpoint compatibility too; see
   [github/gh-aw#67460](https://github.com/github/gh-aw/issues/67460) and
   [github/copilot-cli#5103](https://github.com/github/copilot-cli/issues/5103).
   If available, Copilot SDK mode (`engine.copilot-sdk: true`) supports different
   model families through per-model providers.

**Switching to an older model is a last resort**, only if these fixes do not work.
Leading with `model: gpt-4.1` trades capability for a workaround and leaves the
underlying misconfiguration in place. Report any remaining endpoint constraint.

## 4. Identify the version field from the failing install step

| Setting | Meaning | Failing step and remedy |
| --- | --- | --- |
| `engine.version` | Agent CLI version; for Copilot, the Copilot CLI (1.0.x in the customer case), not gh-aw or AWF. | **Install GitHub Copilot CLI** 404s: remove this pin to use the compiled default, or verify the exact Copilot CLI release exists. |
| `sandbox.agent.version` | AWF release version in `vX.Y.Z` form; must match a [github/gh-aw-firewall release](https://github.com/github/gh-aw-firewall/releases). | **Install AWF binary** failures: remove this pin to use the compiled default, or verify the AWF release and asset exist. |

There is no `engine.copilot.version` field. Do not move a gh-aw version or an AWF
version into `engine.version`. In most cases, remove the misplaced pin and
recompile to use the compiler's compatible defaults. Removing a runtime pin does
not upgrade gh-aw itself.

## 5. Read the run artifacts

| Artifact | Evidence |
| --- | --- |
| `agent-stdio.log` | `[copilot-harness]` lines for model alias resolution and the chosen `COPILOT_PROVIDER_WIRE_API`, including explicit overrides. |
| `sandbox/firewall/logs/api-proxy-logs/token-usage.jsonl` | Model and path for each request: which endpoint each call actually used. |
| `usage/aw_session.jsonl` | Agent events, including `subagent.failed` when a delegated task failed even if the run's final output appears successful. |
| `aw_info.json` | `model`, `requested_model`, `cli_version` (gh-aw compiler), `version` (agent CLI), and `awf_version` (AWF). |
| `gh aw audit RUN_ID` | Combined view of run metadata, errors, and downloaded evidence, including **Sub-agent Failed** findings and `deviated` requests. |

Older versions may omit fields or harness diagnostics; absence is not proof of
correct routing. Keep evidence redacted and distinguish observed facts from
inference. Regression fixtures and contract tests for the customer case live in
`.github/skills/agentic-workflows/tests/`; run them with
`python3 -m unittest discover -s .github/skills/agentic-workflows/tests -v`.
