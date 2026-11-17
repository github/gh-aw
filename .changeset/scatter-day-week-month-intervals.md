---
"gh-aw": minor
---

Scatter `every N days`, `every N weeks`, and `every N months` schedules. These intervals previously compiled to fixed midnight crons (for example `every 2 days` → `0 0 */2 * *`), so every workflow using them fired at 00:00 UTC in the same minute. They now use the same deterministic scattered time slot as `daily` (for example `43 5 */2 * *`); monthly intervals also scatter the day of month within 1–28. `every 1d` and `every 1w` are now equivalent to `daily` and `weekly`. Recompiling changes the cron for existing workflows using these forms once; use an explicit cron expression such as `cron: "0 0 */2 * *"` to keep an exact time.
