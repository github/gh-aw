# ADR-80170: Show Approved Agent Reasons in Workflow Failure Reports

**Date**: 2026-11-22
**Status**: Draft

## Context

The [Duplicate Code Detector failure in #80162](https://github.com/github/gh-aw/issues/80162) put both the `report_incomplete` warning and the agent's stated reason under a collapsed **Runtime evidence** section. Readers had to expand it to see the reason. Runtime evidence is useful for deeper investigation, but the completion signal and the agent's explanation serve different purposes: the signal records that completion was not confirmed; the explanation is untrusted free text, not an established root cause.

GitHub renders alerts as callouts only at the top level. A template can nest an alert directly or introduce nesting when a placeholder expands at runtime. Existing custom templates should remain usable without requiring their authors to migrate before recompiling.

## Decision

Separate the completion signal, the agent reason, and machine evidence. In bundled failure issues, put the completion warning and any eligible reason together, and keep machine diagnostics in the collapsed **Runtime evidence** section. Label the reason **Reported reason (agent-declared, unverified)** and render it as bounded literal text. Do not promote agent-supplied diagnostic details with it.

Use the same publication rule for failure issues, failure comments, and Actions step summaries: include an agent-declared reason only when agent output loads successfully, threat detection explicitly accepts the output, the detection job and its reported conclusion both indicate success, and secret redaction succeeds. A successful detector job alone is not acceptance. If detection is absent, skipped, rejects the output, or fails, or if redaction fails, omit the reason rather than falling back to unchecked text. The completion signal and publishable machine evidence do not depend on an eligible reason.

Keep native GitHub alerts at the top level. Warn, rather than fail compilation, for statically detectable nested alerts in configured templates. At render time, turn nested alerts into blockquotes with a readable label (for example, **Warning:**), preserving their content even when expansion introduces nesting. Update the affected bundled templates to avoid nesting in the first place.

Keep existing template placeholders usable without a mandatory rewrite. Custom comments using `{report_incomplete_context}` still receive the completion signal, eligible reasons, and completion evidence in that slot. Custom issues using `{runtime_evidence_context}` still receive the complete collapsed section, including alerts and any eligible reason. The bundled issue instead uses `{report_incomplete_context}` for the warning, `{agent_report_context}` for the eligible reason, `{runtime_alerts_context}` for top-level runtime alerts, and `{runtime_diagnostics_context}` for the collapsed diagnostics. Use either the legacy evidence slot or the split layout to avoid duplicates.

## Alternatives Considered

- **Keep the reason under Runtime evidence, or publish only machine-generated diagnostics.** Either keeps the report compact; omitting agent text altogether also avoids its disclosure risk. Neither makes the agent's specific explanation immediately available for triage.
- **Show the reason when the detector job succeeds, or rely on redaction alone.** A job result is not an explicit acceptance verdict, and redaction addresses secrets, not all unsafe content. Neither is sufficient on its own for any of the three publication surfaces.
- **Reject nested alerts at compile time.** This would make invalid static placements harder to miss, but would break existing templates and still miss nesting introduced by runtime expansion. A warning plus a rendering fallback keeps the reports readable without forcing migration.

## Consequences

- Readers can see the agent's explanation without expanding diagnostics when it is eligible. If it is withheld, the warning still explains that completion was not confirmed without attributing a cause.
- Visibility increases the chance that readers act on misleading agent text. Detection and redaction limit what is published; neither verifies the claim. The label identifies the text as a lead, not a diagnosis.
- Custom templates can migrate to the split layout at their own pace. Maintainers must preserve the legacy completion placeholder and both alert renderings.
- Before accepting the decision, verify that all three surfaces show an eligible reason and omit it for skipped or rejected detection, unsuccessful detection, or failed redaction. Also verify that legacy templates still show the completion warning and that dynamically nested alerts remain readable.
