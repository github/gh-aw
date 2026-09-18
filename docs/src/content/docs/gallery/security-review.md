---
title: Automated security review
description: Use Daily Malicious Code Scan as a gh-aw example that reviews recent changes and reports suspicious code automatically.
---

Automated security review with gh-aw can combine an agent's contextual analysis with a constrained, GitHub-native reporting channel. This example is a portable adaptation of the [Daily Malicious Code Scan workflow](https://github.com/githubnext/agentics/blob/main/workflows/daily-malicious-code-scan.md).

```aw wrap title=".github/workflows/daily-malicious-code-scan.md"
---
on:
  schedule: daily

permissions:
  contents: read
  pull-requests: read
  security-events: read

safe-outputs:
  create-code-scanning-alert:
    max: 20
---

# Daily Malicious Code Scan

Review code changes from the last three days for evidence of secret exfiltration, unexpected network access, suspicious system commands, obfuscation, hidden backdoors, or privilege escalation.

Use repository and pull request context to distinguish intentional behavior from anomalies. Create a code scanning alert only when there is concrete file and line evidence. Include the category, severity, evidence, likely impact, confidence, and recommended remediation. Do not report speculative or style-only concerns.
```

`create-code-scanning-alert` converts findings to SARIF and uploads them to GitHub code scanning. The agent does not receive general repository write access. Treat agent findings as leads for maintainer investigation, not as proof that code is malicious.

## Learn More

- [Daily Malicious Code Scan source workflow](https://github.com/githubnext/agentics/blob/main/workflows/daily-malicious-code-scan.md)
- [Code scanning alert safe output](/gh-aw/reference/safe-outputs/#code-scanning-alerts-create-code-scanning-alert)
- [Security architecture](/gh-aw/introduction/architecture/)
- [Audit commands](/gh-aw/reference/audit/)
