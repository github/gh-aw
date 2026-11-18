---
"gh-aw": patch
---

Bump the default gh-aw-firewall version to v0.29.16, fixing the arm64 Codex enclave image build.

The [v0.29.14–v0.29.16 comparison](https://github.com/github/gh-aw-firewall/compare/v0.29.14...v0.29.16) leaves both upstream AWF config schemas unchanged. The optional `--home` flag is not emitted by gh-aw, so existing feature-version gates remain unchanged.

Separate follow-ups: reconcile pre-existing embedded-schema drift (including `network.verifySbxEgress`, `apiProxy.copilotTokenBroker`, and `apiProxy.modelCatalogue`) with the upstream schemas. Update `/reflect` consumers, especially wildcard selection in `actions/setup/js/resolve_model_alias.cjs`, to consume `automatic_selection_eligible` and `automatic_selection_reason` under the new [automatic-selection eligibility contract](https://github.com/github/gh-aw-firewall/blob/v0.29.16/docs/awf-config-spec.md#128-automatic-selection-is-restricted-to-models-that-can-serve-an-agent).
