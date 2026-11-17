---
title: Schedule Syntax
description: Complete reference for fuzzy schedule syntax and cron expressions
sidebar:
  order: 405
---

This reference documents the complete schedule syntax supported by GitHub Agentic Workflows, including fuzzy schedules (recommended), time constraints, and standard cron expressions.

## Overview

GitHub Agentic Workflows supports human-friendly schedule expressions that are automatically converted to cron format. The system includes two types of schedules:

- **Fuzzy schedules** (recommended) - Automatically scatter execution times across workflows to prevent load spikes
- **Fixed schedules** - Run at specific times, but create server load when many workflows use the same time

Fuzzy schedules distribute workflow execution times deterministically across all workflows in your repository. Each workflow gets a unique, consistent execution time that never changes across recompiles, preventing server load spikes.

> [!NOTE]
> GitHub Actions enforces a minimum schedule interval of 5 minutes.

## Quick Reference

| Pattern | Example | Result | Type |
|---------|---------|--------|------|
| **Daily** | `daily` | Scattered time | Fuzzy |
| | `daily on weekdays` | Mon-Fri, scattered time | Fuzzy |
| | `daily around 14:00` | 13:00-15:00 window | Fuzzy |
| | `daily around 9am on weekdays` | Mon-Fri 8am-10am | Fuzzy |
| | `daily between 9:00 and 17:00` | 9am-5pm window | Fuzzy |
| | `daily between 9:00 and 17:00 on weekdays` | Mon-Fri 9am-5pm | Fuzzy |
| **Hourly** | `hourly` | Scattered minute | Fuzzy |
| | `hourly on weekdays` | Mon-Fri, scattered minute | Fuzzy |
| | `every 2h` | Every 2 hours | Fuzzy |
| | `every 2h on weekdays` | Mon-Fri every 2 hours | Fuzzy |
| **Weekly** | `weekly` | Scattered day/time | Fuzzy |
| | `weekly on monday` | Monday, scattered time | Fuzzy |
| | `weekly on friday around 5pm` | Friday 4pm-6pm | Fuzzy |
| **Bi-weekly** | `bi-weekly` | Scattered across 2 weeks | Fuzzy |
| **Tri-weekly** | `tri-weekly` | Scattered across 3 weeks | Fuzzy |
| **Intervals** | `every 10 minutes` | Every 10 minutes, scattered start minute | Fuzzy |
| | `every 2 days` | Every 2 days, scattered time | Fuzzy |
| | `every 2w`, `every 1mo` | Scattered time (and day of month) | Fuzzy |
| **Cron** | `0 9 * * 1` | Standard cron | Fixed |

## Fuzzy Schedules

Fuzzy schedules automatically distribute workflow execution times to prevent server load spikes. The scattering is deterministic based on the workflow file path, so each workflow consistently gets the same execution time.

### Daily Schedules

Run once per day at a scattered time:

```yaml
on:
  schedule: daily
  schedule: daily on weekdays  # Monday-Friday only
```

Each workflow gets a unique time like `43 5 * * *` (5:43 AM) or `43 5 * * 1-5` (5:43 AM, Mon-Fri).

### Daily with Time Constraints

Use `around` for a ±1 hour window or `between` for custom ranges. Add `on weekdays` to restrict to Monday-Friday:

```yaml
on:
  schedule: daily around 14:00     # 13:00-15:00
  schedule: daily around 3pm       # 2pm-4pm
  schedule: daily around noon      # 11am-1pm
  schedule: daily around 9am on weekdays       # Mon-Fri 8am-10am
  schedule: daily around 14:00 on weekdays     # Mon-Fri 13:00-15:00
  schedule: daily between 9:00 and 17:00    # Business hours (9am-5pm)
  schedule: daily between 9:00 and 17:00 on weekdays   # Mon-Fri 9am-5pm
  schedule: daily between 22:00 and 02:00   # Crossing midnight (10pm-2am)
```

Special time keywords: `midnight` (00:00), `noon` (12:00)

### Hourly Schedules

```yaml
on:
  schedule: hourly    # Runs every hour with scattered minute (e.g., 58 */1 * * *)
  schedule: hourly on weekdays  # Mon-Fri only (e.g., 58 */1 * * 1-5)
```

Each workflow gets a consistent minute offset (0-59) to prevent simultaneous execution.

### Interval Schedules

Add `on weekdays` to restrict interval schedules to Monday-Friday:

```yaml
on:
  schedule: every 2h    # Every 2 hours at scattered minute (e.g., 53 */2 * * *)
  schedule: every 2h on weekdays  # Mon-Fri every 2 hours (e.g., 53 */2 * * 1-5)
  schedule: every 6h    # Every 6 hours at scattered minute (e.g., 12 */6 * * *)
  schedule: every 6h on weekdays  # Mon-Fri every 6 hours
```

Supported intervals: `1h`, `2h`, `3h`, `4h`, `6h`, `8h`, `12h`

### Weekly Schedules

```yaml
on:
  schedule: weekly              # Scattered day/time (e.g., 43 5 * * 1)
  schedule: weekly on monday    # Monday at scattered time (e.g., 43 5 * * 1)
  schedule: weekly on friday    # Friday at scattered time (e.g., 18 14 * * 5)
```

Supported weekdays: `sunday`, `monday`, `tuesday`, `wednesday`, `thursday`, `friday`, `saturday`

### Weekly with Time Constraints

```yaml
on:
  schedule: weekly on monday around 09:00   # Monday 8am-10am
  schedule: weekly on friday around 5pm     # Friday 4pm-6pm
```

### Bi-weekly and Tri-weekly Schedules

```yaml
on:
  schedule: bi-weekly     # Every 14 days at scattered time (e.g., 43 5 */14 * *)
  schedule: tri-weekly    # Every 21 days at scattered time (e.g., 18 14 */21 * *)
```

Each workflow gets a deterministic time that repeats every 14 or 21 days, scattered across the full period to distribute load.

## IANA Timezone Field

For cron-based schedule items, use the optional `timezone` field to interpret the cron expression in a specific timezone rather than UTC:

```yaml
on:
  schedule:
    - cron: "30 9 * * 1-5"
      timezone: "America/New_York"   # 9:30 AM EST/EDT Mon-Fri
    - cron: "0 14 * * *"
      timezone: "Asia/Tokyo"         # 2:00 PM JST daily
    - cron: "0 8 * * 1"
      timezone: "Europe/London"      # 8:00 AM GMT/BST on Mondays
```

The `timezone` field accepts any [IANA timezone identifier](https://en.wikipedia.org/wiki/List_of_tz_database_time_zones) (e.g., `America/New_York`, `Europe/London`, `Asia/Tokyo`, `UTC`). The compiler converts the cron expression to UTC using the specified timezone rules, including automatic daylight saving time handling.

> [!NOTE]
> The `timezone` field applies only to cron-based schedule items (`- cron: "..."`) in the list form. For fuzzy schedules written as strings (e.g., `daily around 9am`), use the inline `utc+N` / `utc-N` offset syntax instead.

## UTC Offset Support

Use `utc+N` or `utc-N` (or `utc+HH:MM`) to convert local times to UTC:

```yaml
on:
  schedule: daily around 14:00 utc+9                  # 2:00 PM JST
  schedule: daily around 9am utc-5                    # 9:00 AM EST
  schedule: daily between 9am utc-5 and 5pm utc-5     # Business hours EST
  schedule: weekly on monday around 08:00 utc+05:30   # Monday 8:00 AM IST
```

Common offsets: PT/PST/PDT (`utc-8`/`utc-7`), EST/EDT (`utc-5`/`utc-4`), JST (`utc+9`), IST (`utc+05:30`)

## Fixed Schedules

For fixed-time schedules, use standard cron syntax:

```yaml
on:
  schedule:
    - cron: "0 2 * * *"    # Daily at 2:00 AM UTC
    - cron: "30 6 * * 1"   # Monday at 6:30 AM UTC
    - cron: "0 9 15 * *"   # 15th of month at 9:00 AM UTC
```

## Interval Schedules

Use `every N [unit]` syntax for various intervals:

```yaml
on:
  # Minutes (minimum 5 minutes, fuzzy - scattered start minute)
  schedule: every 5 minutes    # 3/5 * * * *
  schedule: every 10m          # 7/10 * * * * (short format)

  # Hours (fuzzy - scattered minute)
  schedule: every 1h           # 58 */1 * * * (minute 58)
  schedule: every 2 hours      # 53 */2 * * * (minute 53)

  # Days (fuzzy - scattered time)
  schedule: every 1d           # 43 5 * * * (same as daily)
  schedule: every 2 days       # 43 5 */2 * *

  # Weeks (fuzzy - scattered time)
  schedule: every 1w           # 43 5 * * 1 (same as weekly)
  schedule: every 2w           # 43 5 */14 * *

  # Months (fuzzy - scattered day 1-28 and time)
  schedule: every 1mo          # 43 5 17 * *
  schedule: every 2mo          # 43 5 17 */2 *
```

Day, week, and month intervals use the same scattered time slots as `daily`, so many workflows on `every 2 days` no longer all fire at 00:00 UTC. For an exact time, use a cron expression such as `cron: "0 0 */2 * *"`.

The crons above are illustrative: actual times, weekdays, and monthly dates depend on the workflow identifier. Weeks and months require the short forms (`every 2w`, `every 2mo`), not `every 2 weeks` or `every 2 months`.

Recompiling existing workflows changes their schedule once. In particular, `every 1w` moves from Sunday to a hashed weekday, and `every 1mo` moves from the first of the month to a hashed day in 1–28. To preserve those calendar dates, use `weekly on sunday` or an explicit cron such as `cron: "0 0 1 * *"`.

Valid minute intervals: `5m`, `10m`, `15m`, `20m`, `30m`
Valid hour intervals: `1h`, `2h`, `3h`, `4h`, `6h`, `8h`, `12h`

## Time Formats

Supports 24-hour (`HH:MM`), 12-hour (`Ham`, `Hpm`), and keywords (`midnight`, `noon`):

```yaml
Examples:
  00:00, 09:30, 14:00    # 24-hour format
  1am, 3pm, 11pm         # 12-hour format
  midnight, noon         # Keywords

With UTC offset:
  14:00 utc+9            # JST to UTC
  3pm utc-5              # EST to UTC
  9am utc+05:30          # IST to UTC
```

## Standard Cron Expressions

Format: `minute hour day-of-month month day-of-week`

```yaml
on:
  schedule:
    - cron: "0 9 * * 1"       # Monday at 9:00 AM
    - cron: "*/15 * * * *"    # Every 15 minutes
    - cron: "0 0 * * *"       # Daily at midnight
    - cron: "0 14 * * 1-5"    # Weekdays at 2:00 PM
```

See [GitHub's cron syntax documentation](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#schedule).

## Multiple Schedules

```yaml
on:
  schedule:
    - cron: daily
    - cron: weekly on monday
    - cron: "0 0 15 * *"      # Monthly on 15th
```

## Shorthand Format

Use `on: daily` as shorthand, which automatically expands to include both schedule and `workflow_dispatch`:

```yaml
on: daily

# Expands to:
on:
  schedule:
    - cron: "FUZZY:DAILY * * *"
  workflow_dispatch:
```

## Validation & Warnings

The compiler warns about patterns that create load spikes:

```text
⚠ Schedule uses fixed daily time (0:0 UTC). Consider using fuzzy
  schedule 'daily' instead to distribute workflow execution times.

⚠ Schedule uses hourly interval with fixed minute offset (0).
  Consider using fuzzy schedule 'every 2h' instead.

⚠ Schedule uses fixed weekly time (Monday 6:30 UTC). Consider using
  fuzzy schedule 'weekly on monday' instead.
```

## Learn More

- [Triggers](/gh-aw/reference/triggers/) - Complete trigger configuration
- [Frontmatter](/gh-aw/reference/frontmatter/) - Workflow configuration reference
- [GitHub Actions Schedule Events](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#schedule) - GitHub's schedule documentation
