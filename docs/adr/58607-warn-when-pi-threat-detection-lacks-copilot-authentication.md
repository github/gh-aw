# ADR-58607: Warn When Pi Threat Detection Lacks Copilot Authentication

**Date**: 2026-09-04
**Status**: Draft
**Deciders**: Unknown, adr-writer agent

---

### Context

This pull request adds compiler validation and documentation for Pi workflows that enable threat detection. The PR body and code show that Pi threat detection runs on the GitHub Copilot CLI even when the main Pi inference provider is OpenAI or Anthropic, which means detection can fail silently when a workflow lacks both `permissions.copilot-requests: write` and an explicit `COPILOT_GITHUB_TOKEN` override. The implementation adds a targeted warning in `pkg/workflow/compiler_validators.go`, test coverage for warning and suppression paths in `pkg/workflow/compiler_validators_test.go`, and documentation in `docs/src/content/docs/engines/pi.md`. Because this changes the compiler contract around authentication requirements and makes an implicit engine dependency explicit to users, it should be recorded as an architectural decision.

### Decision

We will make the compiler warn when a workflow uses the Pi engine with threat detection routed through the GitHub Copilot CLI but does not provide Copilot authentication. The warning will be suppressed when the workflow grants `permissions.copilot-requests: write`, provides `COPILOT_GITHUB_TOKEN`, disables threat detection, or overrides detection to a non-Copilot engine. We will also document that threat-detection authentication is separate from the OpenAI or Anthropic credentials used by the Pi agent.

### Alternatives Considered

#### Alternative 1: Keep the current behavior and let threat detection fail at runtime

This preserves the existing compiler behavior and relies on runtime logs to expose authentication problems. It was considered because it avoids adding new validation logic and keeps the compiler quieter. It was not chosen because the PR evidence shows that workflows can otherwise succeed while threat detection fails with "No authentication information found," which is easy for authors to miss and expensive to debug after compilation.

#### Alternative 2: Make missing Copilot authentication a hard compiler error

This would block compilation until the workflow grants `copilot-requests: write` or sets `COPILOT_GITHUB_TOKEN`. It was considered because the missing credential is deterministic in the cases covered by the new validator. It was not chosen because the implementation and PR description favor a warning-level guardrail that informs authors without making all Pi threat-detection configurations uncompilable during migration or experimentation.

### Consequences

#### Positive
- Workflow authors get an early, explicit signal that Pi threat detection depends on GitHub Copilot authentication.
- The compiler now distinguishes between real misconfiguration and valid suppression cases such as disabled detection, explicit token configuration, or a non-Copilot detection engine override.
- Documentation now explains that detection credentials are separate from the primary Pi inference provider credentials, reducing setup confusion.

#### Negative
- Compiler validation grows more complex because it must inspect threat-detection enablement, engine resolution, permissions, and merged environment configuration.
- Warning text and suppression rules must stay aligned with future changes to threat-detection engine behavior and authentication configuration.
- Some users may ignore warnings, leaving runtime failures possible even though the compiler now provides better guidance.

#### Neutral
- The change introduces new unit tests that define the expected warning and suppression matrix for Pi threat detection.
- The decision does not change how detection authenticates at runtime; it only makes that dependency visible during compilation and documentation.
- Future engine-routing changes for threat detection will need to revisit this ADR and its assumptions about Copilot as the default detection backend.

---

*ADR created by [adr-writer agent]. Review and finalize before changing status from Draft to Accepted.*
