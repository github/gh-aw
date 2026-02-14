---
title: "Meet the Workflows: Security-related"
description: "A curated tour of security and compliance workflows that enforce safe boundaries"
authors:
  - dsyme
  - pelikhan
  - mnkiefer
date: 2026-01-13T08:00:00
sidebar:
  label: "Security-related"
prev:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-operations-release/
  label: "Operations & Release Workflows"
next:
  link: /gh-aw/blog/2026-01-13-meet-the-workflows-creative-culture/
  label: "Teamwork & Culture Workflows"
---

<img src="/gh-aw/peli.png" alt="Peli de Halleux" width="200" style="float: right; margin: 0 0 20px 20px; border-radius: 8px;" />

*Splendid!* How great to have you back at [Peli's Agent Factory](/gh-aw/blog/2026-01-12-welcome-to-pelis-agent-factory/)! Now, let me show you the *guardian chamber* - where the watchful protectors stand vigil!

In our [previous post](/gh-aw/blog/2026-01-13-meet-the-workflows-operations-release/), we explored operations and release workflows that handle the critical process of shipping software - building, testing, generating release notes, and publishing. These workflows need to be rock-solid reliable because they represent the moment when our work reaches users.

But reliability alone isn't enough - we also need *security*. When AI agents can access APIs, modify code, and interact with external services, security becomes paramount. How do we ensure agents only access authorized resources? How do we track vulnerabilities and enforce compliance deadlines? How do we prevent credential exposure? That's where security and compliance workflows become our essential guardrails - the watchful guardians that let us sleep soundly at night.

## Security-related Workflows

These agents are our security guards, keeping watch and enforcing the rules:

- **[Security Compliance](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/security-compliance.md?plain=1)** - Runs vulnerability campaigns with deadline tracking  
- **[Firewall](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/firewall.md?plain=1)** - Tests network security and validates rules - **59 daily firewall report discussions**, **5 smoke test issues**
- **[Daily Secrets Analysis](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-secrets-analysis.md?plain=1)** - Scans for exposed credentials (yes, it happens)  
- **[Daily Malicious Code Scan](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-malicious-code-scan.md?plain=1)** - Reviews recent code changes for suspicious patterns
- **[Static Analysis Report](https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/static-analysis-report.md?plain=1)** - Daily security scans using zizmor, poutine, and actionlint - **57 analysis discussions** plus **12 Zizmor security reports**

Security Compliance manages vulnerability remediation campaigns with deadline tracking, ensuring security issues are addressed within defined SLAs - perfect for those "audit in 3 weeks" panic moments.

The Firewall workflow has created **59 daily firewall report discussions** and **5 smoke test issues**, validating that our agents can't access unauthorized resources - for example, [#6943](https://github.com/github/gh-aw/discussions/6943) with the daily firewall analysis. It's the bouncer that enforces network rules.

Daily Secrets Analysis scans for exposed credentials in commits and discussions, providing an automated security net against accidental secret exposure - catching those "oops, I committed my API key" moments before they become incidents.

Daily Malicious Code Scan reviews recent code changes for suspicious patterns, adding an automated defense layer against supply chain attacks.

Static Analysis Report has created **57 analysis discussions** plus **12 Zizmor security reports**, running comprehensive daily security audits using industry-standard tools - for example, [#6973](https://github.com/github/gh-aw/discussions/6973) with the latest static analysis findings and [#3033](https://github.com/github/gh-aw/discussions/3033) with a Zizmor security analysis. This shows how traditional security tools can be integrated into an AI agent workflow.

## Using These Workflows

You can add these workflows to your own repository and remix them. Get going with our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/), then run one of the following:

**Security Compliance:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/security-compliance.md
```

**Firewall:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/firewall.md
```

**Daily Secrets Analysis:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-secrets-analysis.md
```

**Daily Malicious Code Scan:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/daily-malicious-code-scan.md
```

**Static Analysis Report:**

```bash
gh aw add-wizard https://github.com/github/gh-aw/blob/v0.42.13/.github/workflows/static-analysis-report.md
```

Then edit and remix the workflow specifications to meet your needs, regenerate the lock file using `gh aw compile`, and push to your repository. See our [Quick Start](https://github.github.com/gh-aw/setup/quick-start/) for further installation and setup instructions.

You can also [create your own workflows](/gh-aw/setup/creating-workflows).

## Learn More

- **[GitHub Agentic Workflows](https://github.github.com/gh-aw/)** - The technology behind the workflows
- **[Quick Start](https://github.github.com/gh-aw/setup/quick-start/)** - How to write and compile workflows

## Next Up: Teamwork & Culture Workflows

After all this serious talk, let's explore the fun side: agents that bring joy and build team culture.

Continue reading: [Teamwork & Culture Workflows →](/gh-aw/blog/2026-01-13-meet-the-workflows-creative-culture/)

---

*This is part 11 of a 19-part series exploring the workflows in Peli's Agent Factory.*
