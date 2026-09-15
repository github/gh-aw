---
title: "Effective Tokens replaced by AI Credits"
description: "The latest gh-aw build uses AI Credits (AIC) as the primary spend metric and keeps Effective Tokens (ET) as a legacy compatibility field."
authors:
  - copilot
date: 2026-06-08
metadata:
  seoDescription: "gh-aw now reports AI Credits (AIC) as the primary spend metric, aligned with GitHub Copilot billing and models.dev pricing."
  linkedPostText: "ET replaced by AIC in latest build"
---

In the latest gh-aw build, Effective Tokens (ET) have been
replaced by AI Credits (AIC) as the primary spend metric.

> [!IMPORTANT]
> AIC is now the default cost metric in gh-aw output. ET remains
> available only as a legacy compatibility field.

This change reflects GitHub Copilot billing and models.dev pricing.
It makes spend tracking directly aligned to monetary cost instead of
a normalized token proxy.

## What this means in practice

- `gh aw audit` and `gh aw logs` report AI Credits as the primary
  spend metric.
- Effective Tokens are deprecated in documentation and should be
  treated as legacy compatibility output.
- Cost reporting and budget discussions should use AIC values.

For repositories that need automatic workflow updates, run:

```bash
gh aw fix --write
```

## Verify the change

`gh aw fix --write` rewrites the obsolete budget fields in workflow
frontmatter, converting `max-effective-tokens` to `max-ai-credits`
and `max-daily-effective-tokens` to `max-daily-ai-credits`
(10,000 ET = 1 AIC). A successful run prints a summary such as:

```text
✓ Fixed 3 of 12 workflow files
```

To confirm the migration took effect:

1. Re-run the command without `--write`. When nothing is left to
   migrate it reports:

   ```bash
   gh aw fix
   ```

   ```text
   ✓ No workflow fixes needed
   ```

2. Review the rewritten frontmatter:

   ```bash
   git diff .github/workflows
   ```

   ```diff
   -max-effective-tokens: 1000000
   +max-ai-credits: 100
   -max-daily-effective-tokens: 5000000
   +max-daily-ai-credits: 500
   ```

3. Recompile so the lock files pick up the new limits:

   ```bash
   gh aw compile
   ```

If a workflow still uses `max-effective-tokens` or
`max-daily-effective-tokens` after the fix, its value is an
expression, which the migration intentionally skips. Update those
values by hand.

## Metric reference

- **AI Credits (AIC)**: primary spend metric (1 AIC = $0.01 USD)
- **Effective Tokens (ET)**: deprecated legacy metric

## Where to read more

- [Cost Management](/gh-aw/reference/cost-management/)
- [Auditing Workflows](/gh-aw/reference/audit/)
- [AI Credits Specification](/gh-aw/specs/ai-credits-specification/)
