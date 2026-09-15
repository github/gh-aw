---
title: "Effective Tokens replaced by AI Credits"
description: "gh-aw uses AI Credits (AIC) as the primary spend metric after migrating away from Effective Tokens (ET)."
authors:
  - copilot
date: 2026-06-08
metadata:
  seoDescription: "gh-aw now reports AI Credits (AIC) as the primary spend metric, aligned with GitHub Copilot billing and models.dev pricing."
  linkedPostText: "ET replaced by AIC in latest build"
---

In gh-aw, Effective Tokens (ET) have been
replaced by AI Credits (AIC) as the primary spend metric.

> [!IMPORTANT]
> AIC is now the default cost metric in gh-aw output. ET is retained
> here only as historical migration context.

This change reflects GitHub Copilot billing and models.dev pricing.
It makes spend tracking directly aligned to monetary cost instead of
a normalized token proxy.

## What this means in practice

- `gh aw audit` and `gh aw logs` report AI Credits as the primary
  spend metric.
- Effective Tokens are retired in maintained surfaces and should be
  treated as historical context.
- Cost reporting and budget discussions should use AIC values.

## Verify the change

Current gh-aw versions no longer ship the automatic ET-to-AIC fixer.
Update obsolete budget fields in workflow frontmatter by hand,
converting `max-effective-tokens` to `max-ai-credits` and
`max-daily-effective-tokens` to `max-daily-ai-credits`
(10,000 ET = 1 AIC).

To confirm the migration took effect:

1. Review the rewritten frontmatter:

   ```bash
   git diff .github/workflows
   ```

   ```diff
   -max-effective-tokens: 1000000
   +max-ai-credits: 100
   -max-daily-effective-tokens: 5000000
   +max-daily-ai-credits: 500
   ```

2. Recompile so the lock files pick up the new limits:

   ```bash
   gh aw compile
   ```

If a workflow still uses `max-effective-tokens` or
`max-daily-effective-tokens`, update those values by hand.

## Metric reference

- **AI Credits (AIC)**: primary spend metric (1 AIC = $0.01 USD)
- **Effective Tokens (ET)**: deprecated legacy metric

## Where to read more

- [Cost Management](/gh-aw/reference/cost-management/)
- [Auditing Workflows](/gh-aw/reference/audit/)
- [AI Credits Specification](/gh-aw/specs/ai-credits-specification/)
