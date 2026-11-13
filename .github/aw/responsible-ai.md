---
description: Prepared Microsoft Responsible AI background and gh-aw-specific assessment rubric for recurring specification reviews.
---

# Responsible AI review background

This is a repository-owned engineering interpretation of Microsoft's public
Responsible AI (RAI) guidance, prepared on **2026-11-13**. It is not the
Microsoft Responsible AI Standard, a legal opinion, or a certification.
The six principles below are Microsoft's; the gh-aw examples, severity rubric,
and evidence contract are local review conventions.

Use this reference directly during daily reviews. Do not rediscover the
framework, browse its sources, install assessment tools, or refresh this file
on each run. Revalidate feature behavior against the checked-out specification,
implementation, and tests; this background is not proof of current behavior.
Maintainers can refresh the reference separately when Microsoft guidance or
the repository's architecture changes. Cite the preparation date rather than
claiming these sources were fetched during the current run.

## Source register

The following public Microsoft pages were consulted on the preparation date.
Summaries here are original, bounded adaptations, not reproductions.

| Source | Basis for this reference |
|---|---|
| [Principles and approach](https://www.microsoft.com/en-us/ai/principles-and-approach) | The six principles: fairness, reliability and safety, privacy and security, inclusiveness, transparency, and accountability. |
| [Tools and practices](https://www.microsoft.com/en-us/ai/tools-practices) | The public RAI Standard, impact-assessment guide and template, and human-AI experience resources are complementary governance aids. Listing these resources does not mean their full contents were reviewed here. |
| [Responsible AI for Microsoft Foundry](https://learn.microsoft.com/en-us/azure/foundry/responsible-use-of-ai-overview) | Discover and assess context-specific risks, measure them, protect model outputs and agent actions, and govern through monitoring and oversight. Foundry-specific products are not required for gh-aw. |
| [Guidelines for Human-AI Interaction](https://www.microsoft.com/en-us/research/project/guidelines-for-human-ai-interaction/) | Consider expectations at first use, routine interaction, inevitable errors, and changes over time. |
| [Planning red teaming for LLMs and their applications](https://learn.microsoft.com/en-us/azure/foundry/openai/concepts/red-teaming) | Benign and adversarial use can both cause harm; exploratory testing discovers risks but does not replace systematic measurement or mitigation verification. |

Do not attribute local checklist items or IDs to Microsoft Standard controls.
Do not infer that a Microsoft service's safeguards apply to this repository.
This workflow uses isolated local regression tests, not the live endpoint
testing discussed in general red-teaming guidance.

## System context and impact assessment

The model's text is not an authorization decision. Safe outputs separate an
agent's proposed action from validation and privileged publication. Compiler
controls translate authored workflows into jobs and configuration; their
assurances depend on preserved trust boundaries, runtime enforcement, and
clear diagnostics. Neither a secure compiler nor an output filter eliminates
all model, social, operational, or application-level risks.

For the selected feature, establish:

| Dimension | Questions and evidence |
|---|---|
| Purpose and limits | What legitimate action is supported? What is explicitly unsupported? Identify normative requirements, not imagined capabilities. |
| People and consequences | Who requests, approves, receives, maintains, or is described by the output? Consider contributors, maintainers, downstream users, and nonusers whose content may be published. |
| Data and authority | Trace inputs, repository visibility, credentials, trust labels, validation, execution, logs, artifacts, and public destinations. Separate inference access from mutation authority. |
| Misuse and ordinary errors | Consider malicious inputs and well-intentioned mistakes, including ambiguous instructions, malformed data, retries, and unexpected environment changes. |
| Human control | Identify previews, review gates, ownership, cancellation, correction, and escalation. Do not promise rollback where external effects are irreversible. |
| Lifecycle | Consider first use, normal operation, failure/recovery, and changes in schemas, providers, permissions, or retention. |

## Six-principle assessment

### Fairness

Assess unjustified differences in outcomes or burdens, not just identical
code paths. Relevant contexts include actor roles, fork status, repository
visibility, supported languages, and content-filter false positives. Legitimate
authorization differences are not automatically unfair; do not broaden access
to equalize outcomes. A language or role is not evidence of a person's
demographic attributes.

Compare benign cases that should receive equivalent treatment while holding
authorization and other relevant factors constant. Explain any intentional
difference against the documented policy. Do not collect sensitive attributes,
invent demographic distributions, or declare population fairness from a few
fixtures. Unresolved policy choices need human judgment, not an automatic fix.

### Reliability and safety

Assess harmful actions as well as harmful text. Inspect invalid and boundary
inputs, type and length limits, retries, duplicate delivery, ordering, partial
success, races, timeouts, dependency failures, and version drift. Establish
where failure must close the mutation path and where explicit recovery is safe.

Pair negative tests with a valid control. Verify observable effects and error
behavior, including whether a rejected action can still mutate state.
Distinguish a recorded tool request, validation acceptance, and completed
publication. A passing test is bounded evidence, not proof of universal safety.

### Privacy and security

Minimize the data acquired, retained, logged, and published. Trace private
content, credentials, personal data, and untrusted instructions across every
boundary relevant to the feature. Consider injection, scope escalation,
spoofed provenance, unsafe references, secret-bearing diagnostics, and
private-to-public flows. Logs and artifacts are disclosure surfaces too.

Use synthetic secrets and artificial personal-data placeholders in tests.
Check both disclosure prevention and useful, redacted failure reporting.
Authorization and output validation must hold independently of a model's
willingness to follow the prompt. Never weaken a sandbox, scope restriction,
redaction rule, or human-review gate as a remediation.

### Inclusiveness

Assess whether supported users can understand and recover from failures.
Inspect Unicode and normalization behavior, valid non-English content,
actionable plain-language diagnostics, textual alternatives to visual status,
and unnecessary assumptions about tooling or environment.

Test realistic supported inputs, not arbitrary promises of multilingual or
accessibility support. Security-sensitive identifiers can legitimately require
a constrained syntax; explain and diagnose that constraint rather than
silently discarding content. Do not claim accessibility conformance without
the corresponding human and assistive-technology evaluation.

### Transparency

Users need an accurate account of capabilities, limits, uncertainty, authorship,
and effects. Check that the specification, schema, tool description, preview,
diagnostics, audit trail, and actual behavior agree. Highlight misleading
success messages, silent truncation, hidden partial failures, or attribution
that implies a human approved an autonomous action.

Make the decision and its limits explainable using safe, minimal evidence.
Do not expose secrets, private content, hidden model reasoning, or sensitive
attack details in the name of transparency. A staged preview is not execution;
accepted publication intent is not completed publication.

### Accountability

Assess who has authority to act, who reviews consequential changes, and how
effects can be traced and corrected. Examine actor binding, repository scope,
trusted run provenance, auditability, ownership, review enforcement, and
escalation when automated judgment is insufficient.

Verify that claims of authorization and review are enforced, not merely
present in a prompt or comment. A draft PR supports human review but is not
proof that review occurred. Do not auto-merge fixes, rewrite historical
evidence, or silently resolve a policy disagreement.

## gh-aw failure-mode prompts

Use only rows relevant to the selected feature. These are investigation
prompts, not preexisting findings or a requirement to review more features.

| Surface | Candidate failure | Local evidence to seek |
|---|---|---|
| Target and actor constraints | A request names a different actor, repository, issue, or branch than the trusted policy permits. | Real handler/parser tests for authorized and unauthorized targets. |
| Content and diagnostics | Valid Unicode is corrupted; a sanitizer overrejects benign text; an error discloses sensitive input. | Benign/adversarial synthetic fixtures, actual rendered output, redacted diagnostics. |
| Execution semantics | Retries duplicate mutations; a failed prerequisite still permits dependent writes; partial success is reported as complete. | Mocked side-effect counts, dependency outcomes, and failure-path tests. |
| Compiler-generated jobs | A generated permission, environment value, checkout, or job dependency crosses an intended boundary. | Compiler construction code and focused structured-output compilation tests, not generated lock files. |
| Provenance and review | Agent-supplied metadata appears trusted, or a review requirement can be bypassed. | Trusted metadata construction, independent enforcement, and negative controls. |
| Specification and schema | A declared safeguard has no enforcing implementation, or users are promised guarantees stronger than reality. | Requirement-to-code-to-test mapping and a concrete observable discrepancy. |

## Evidence and risk rubric

For each principle, record a concrete scenario, affected parties, applicable
requirement IDs, source symbols and lines, current controls, measured or static
evidence, mitigation, residual risk, and uncertainty. Use justified
non-applicability when appropriate; do not manufacture a defect for each
principle. One finding may affect several principles and must not be counted
multiple times.

Classify evidence as **confirmed** (reproducible behavior or a complete static
data/control-flow trace), **hypothesis** (plausible but unverified), or
**unavailable** (a required input or check is missing). A test gap alone is not
a confirmed defect. Record confidence separately from impact and likelihood.
Only confirmed, actionable findings qualify for automatic remediation.

| Severity | Local meaning |
|---|---|
| Critical | Broad unauthorized privileged effects or sensitive-data disclosure with severe consequences and no effective intervening control. |
| High | A concrete serious boundary violation or harmful action affecting a significant supported use case. |
| Medium | A bounded, recoverable failure, misleading result, or unjustified exclusion with meaningful user impact. |
| Low | A limited usability, clarity, or defense-in-depth gap with a demonstrated consequence. |

Rate likelihood as **high** for ordinary supported inputs, **medium** for
credible uncommon conditions, or **low** for constrained conditions supported
by evidence. Use **unknown** when evidence is insufficient; unknown is not
low. Describe affected scope and reversibility rather than inventing numeric
probabilities. Rank by severity, likelihood, and affected scope, then
requirement ID or source location; uncertainty that could change the top
three requires explicit review rather than false precision.

For each proposed fix, connect the harm scenario to the enforcing code change
and a focused regression plus a benign control. Record exact commands,
outcomes, and untested limits. If reporting rates, include numerator,
denominator, sampling method, and uncertainty; fixture pass rates do not
measure production fairness or safety.

Residual risks, unavailable evidence, and policy decisions remain visible to
human reviewers. Escalate sensitive findings through the repository's existing
security-reporting process without publishing exploit details or personal
data. Do not mark an issue resolved merely because a patch was written.
