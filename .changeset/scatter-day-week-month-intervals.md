---
"gh-aw": minor
---

Scatter `every N days`, `every Nw`, and `every Nmo` schedules. These intervals previously compiled to fixed midnight crons (for example `every 2 days` → `0 0 */2 * *`), so every workflow using them fired at 00:00 UTC in the same minute. They now use the same deterministic scattered time slot as `daily` (for example `43 5 */2 * *`); monthly intervals also scatter the day of month within 1–28. `every 1d` and `every 1w` are now equivalent to `daily` and `weekly`. Recompiling changes the cron for existing workflows using these forms once, including moving `every 1w` from Sunday to a hashed weekday and `every 1mo` from the first of the month to a hashed day in 1–28. Use `weekly on sunday` to preserve Sunday with scattered time, or an explicit cron expression such as `cron: "0 0 1 * *"` to preserve the first of the month at midnight.
