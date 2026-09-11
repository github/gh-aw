# ADR-60321: Use Detection Model Alias for Threat Detection Jobs

**Date**: 2026-09-11
**Status**: Draft
**Deciders**: gh-aw maintainers

---

### Context

This pull request updates generated workflow lock files for many repository workflows so that threat-detection jobs stop inheriting the primary Pi or OpenAI model selection. The PR description states that PR Sous Chef's threat-detection job was using the Pi agent's `gpt-5.4` model, which caused HTTP 400 failures and prevented expected AI-credit accounting. The visible file patches consistently add `detection_agent_model":"detection"` metadata and replace detection-specific environment values such as `GH_AW_MODEL_DETECTION_CODEX` or `COPILOT_MODEL` from a concrete model name to the `detection` alias. Because this changes how the workflow system selects and records models for detection-phase execution across many workflows, the PR is making a repository-level workflow-runtime decision rather than a one-off bug fix.

### Decision

We will configure threat-detection workflow jobs to use the dedicated `detection` model alias instead of inheriting or hardcoding the primary agent model. We will also record that detector model explicitly in generated workflow metadata so compiled lock files and runtime environment remain aligned. This isolates detection-phase execution from general agent model selection and preserves the expected credit-accounting and compatibility behavior described in the PR.

### Alternatives Considered

#### Alternative 1: Keep using the primary agent model for detection jobs

The repository could continue allowing detection jobs to inherit the same model configured for the main Pi, Codex, Copilot, or Claude agent run. This was considered because it avoids adding a separate detector-model concept and keeps workflow metadata simpler. It was not chosen because the PR evidence states that inheriting the primary model caused HTTP 400 failures in threat-detection and broke AI-credit accounting.

#### Alternative 2: Hardcode a specific concrete detection model per engine

Another option would be to replace inherited models with explicit engine-specific model strings such as `openai/gpt-5.3-codex` or another fixed detector model in each generated workflow. This was considered because many current lock files already contain concrete model names and that approach would require fewer semantic changes than introducing an alias. It was not chosen because the diff consistently moves to the neutral `detection` alias, which decouples workflow intent from any one vendor-specific model identifier and allows runtime mapping to be managed centrally.

#### Alternative 3: Fix only PR Sous Chef instead of updating shared workflow outputs

The team could scope the change to the single failing workflow or job named in the PR body. This was considered because the reported failure surfaced in PR Sous Chef specifically. It was not chosen because the file list shows broad lock-file regeneration across many workflows, indicating the underlying decision applies to the shared workflow compilation output rather than one isolated workflow.

### Consequences

#### Positive
- Threat-detection jobs will no longer depend on whichever primary model a separate agent phase selected.
- The generated workflow metadata and runtime environment will both declare the dedicated detection model selection consistently.
- Centralizing detector selection behind the `detection` alias should reduce repeated workflow-specific fixes when supported detector models change.

#### Negative
- The repository now depends on the `detection` alias being supported and mapped correctly wherever these workflows execute.
- Generated lock files change in many places for a single conceptual decision, increasing review volume and regeneration churn.
- Troubleshooting may require contributors to understand the distinction between the primary agent model and the detection-phase model.

#### Neutral
- The implementation is expressed in generated `.lock.yml` workflow outputs and metadata rather than a new standalone runtime component.
- Existing non-detection primary model settings remain unchanged by this decision.
- Future workflow recompilation will continue to propagate this detector-model convention across affected workflows.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
