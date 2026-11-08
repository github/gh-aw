This issue tracks all runs where threat detection flagged problems in agentic workflows in this repository. Each workflow run that completes with a detection warning or failure posts a comment here.

<details>
<summary>What is a Detection Problem?</summary>

A detection problem occurs when the threat detection system either:
- **Detects potential security threats** (prompt injection, secret leak, malicious patch)
- **Fails to produce results** (agent failure, parse error)

Comments distinguish `threat_finding`, `detector_execution_failure`, `detector_result_invalid`, and `detection_outcome_unknown` using the structured detection reason. Findings include the detector-defined types: prompt injection, secret leak, and/or malicious patch. Detector failures leave the security assessment incomplete; they do not prove a threat or a clean result. Comments omit agent text and detection explanations.

When `continue-on-error: true` (the default), these problems produce warnings and safe outputs still proceed. When `continue-on-error: false`, they block safe outputs entirely.

</details>

<details>
<summary>How This Helps</summary>

This issue helps you:
- Track workflows where threat detection raised concerns
- Review patterns of detection warnings or failures
- Identify false positives or recurring issues with threat detection
- Monitor the health of the threat detection system

</details>

<details>
<summary>Resources</summary>

- [GitHub Agentic Workflows Documentation](https://github.com/github/gh-aw)

</details>

> [!TIP]
> To configure threat detection behavior, update the frontmatter:
> ```yaml
> safe-outputs:
>   threat-detection:
>     continue-on-error: true   # Warnings only (default)
>     # continue-on-error: false  # Strict mode — block safe outputs
> ```

---

> This issue is automatically managed by GitHub Agentic Workflows. Do not close this issue manually.
>
> **No action to take** - Do not assign to an agent.

<!-- gh-aw-detection-runs -->
