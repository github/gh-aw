---
"gh-aw": major
---

Make AI-credit evidence explicit in `logs --json` output.

**Breaking change:** `runs[].aic` and `summary.total_aic` are now nullable numbers. Consumers must handle `null` rather than treating it as zero. Check `summary.aic_coverage` (`available`, `partial`, or `unavailable`) to determine whether `summary.total_aic` includes every run.
