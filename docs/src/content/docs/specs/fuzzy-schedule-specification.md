---
title: Fuzzy Schedule Time Syntax Specification
spec: true
description: Formal specification for the fuzzy schedule time syntax following W3C conventions
sidebar:
  order: 1360
---

**Version**: 1.2.0
**Status**: Draft Specification  
**Latest Version**: [fuzzy-schedule-specification](/gh-aw/specs/fuzzy-schedule-specification/)  
**Editor**: GitHub Agentic Workflows Team

---

## Abstract

This specification defines the Fuzzy Schedule Time Syntax, a human-friendly scheduling language for GitHub Agentic Workflows that automatically distributes workflow execution times to prevent server load spikes. The syntax supports daily, hourly, weekly, and interval-based schedules with optional time constraints and timezone conversions. The specification includes a deterministic scattering algorithm that uses hash functions to assign consistent execution times to workflows based on their identifiers, ensuring predictable behavior across multiple compilations while distributing load across an organization's infrastructure.

## Status of This Document

This section describes the status of this document at the time of publication. This is a draft specification and may be updated, replaced, or made obsolete by other documents at any time.

This document is governed by the GitHub Agentic Workflows project specifications process.

## Table of Contents

1. [Introduction](#1-introduction)
2. [Conformance](#2-conformance)
3. [Core Syntax](#3-core-syntax)
4. [Time Specifications](#4-time-specifications)
5. [Timezone Support](#5-timezone-support)
6. [Scattering Algorithm](#6-scattering-algorithm)
7. [Cron Expression Generation](#7-cron-expression-generation)
8. [Safeguards](#8-safeguards)
9. [Error Handling](#9-error-handling)
10. [Compliance Testing](#10-compliance-testing)
11. [Sync Notes](#11-sync-notes)
12. [Calendar Output Schema](#12-calendar-output-schema)
13. [Norms](#13-norms)

---

## 1. Introduction

### 1.1 Purpose

The Fuzzy Schedule Time Syntax addresses the problem of server load spikes that occur when multiple workflows execute simultaneously using fixed-time schedules. Traditional cron expressions require explicit time specifications, leading developers to commonly use convenient times (e.g., midnight, on-the-hour) that create load concentration. This specification defines a natural language syntax that automatically distributes execution times while preserving schedule semantics.

### 1.2 Scope

This specification covers:

- Natural language schedule expressions for daily, hourly, weekly, and interval-based schedules
- Time constraint syntax using `around` and `between` modifiers
- Timezone conversion syntax for local-to-UTC time translation
- Deterministic scattering algorithm for execution time distribution
- Cron expression generation from fuzzy syntax
- Validation requirements and error handling

This specification does NOT cover:

- Standard cron expression syntax (handled by GitHub Actions)
- Monthly or yearly schedule patterns
- Dynamic schedule adjustment based on load metrics
- Schedule conflict resolution between workflows

### 1.3 Design Goals

This specification prioritizes:

1. **Human readability**: Natural language expressions that clearly communicate intent
2. **Load distribution**: Automatic scattering prevents simultaneous workflow execution
3. **Determinism**: Same workflow identifier always produces same execution time
4. **Predictability**: Execution times remain consistent across recompilations
5. **Timezone awareness**: Support for local time specifications with UTC conversion

---

## 2. Conformance

### 2.1 Conformance Classes

A **conforming implementation** is a parser that satisfies all MUST, MUST NOT, REQUIRED, SHALL, and SHALL NOT requirements in this specification.

A **conforming fuzzy schedule expression** is a schedule string that conforms to the syntax grammar defined in Section 3 and produces a valid fuzzy cron placeholder.

A **conforming scattering implementation** is an implementation that satisfies all scattering algorithm requirements in Section 6.

### 2.2 Requirements Notation

The key words "MUST", "MUST NOT", "REQUIRED", "SHALL", "SHALL NOT", "SHOULD", "SHOULD NOT", "RECOMMENDED", "NOT RECOMMENDED", "MAY", and "OPTIONAL" in this document are to be interpreted as described in [RFC 2119](https://www.ietf.org/rfc/rfc2119.txt).

### 2.3 Compliance Levels

**Level 1 (Basic)**: Supports daily and weekly schedules without time constraints

**Level 2 (Standard)**: Adds support for time constraints (`around`, `between`) and hourly schedules

**Level 3 (Complete)**: Includes timezone conversion, interval schedules, and bi-weekly/tri-weekly patterns

---

## 3. Core Syntax

### 3.1 Grammar Definition

A fuzzy schedule expression MUST conform to the following ABNF grammar:

```text
fuzzy-schedule  = daily-schedule / hourly-schedule / weekly-schedule / interval-schedule

daily-schedule  = "daily" [time-constraint]
weekly-schedule = "weekly" ["on" weekday] [time-constraint]
hourly-schedule = "hourly" / ("every" hour-interval)
interval-schedule = "every" (minute-interval / hour-interval / day-interval / week-interval)

time-constraint = around-constraint / between-constraint
around-constraint = "around" time-spec
between-constraint = "between" time-spec "and" time-spec

time-spec       = (hour-24 ":" minute) [utc-offset]
                / (hour-12 am-pm) [utc-offset]
                / time-keyword [utc-offset]

time-keyword    = "midnight" / "noon"
am-pm           = "am" / "pm"
utc-offset      = "utc" ("+" / "-") (hours / hours ":" minutes)

weekday         = "sunday" / "monday" / "tuesday" / "wednesday" 
                / "thursday" / "friday" / "saturday"

hour-24         = 1*2DIGIT  ; 0-23
hour-12         = 1*2DIGIT  ; 1-12
minute          = 2DIGIT    ; 00-59
hours           = 1*2DIGIT
minutes         = 2DIGIT

minute-interval = 1*DIGIT ("m" / "minutes" / "minute")
hour-interval   = 1*DIGIT ("h" / "hours" / "hour")
day-interval    = 1*DIGIT ("d" / "days" / "day")
week-interval   = 1*DIGIT ("w" / "weeks" / "week")
```

### 3.2 Daily Schedules

#### 3.2.1 Basic Daily Schedule

A basic daily schedule expression SHALL take the form:

```yaml
daily
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:DAILY * * *`

The execution time SHALL be deterministically scattered across all 24 hours and 60 minutes of the day.

#### 3.2.2 Daily Around Time

A daily around schedule expression SHALL take the form:

```yaml
daily around <time-spec>
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:DAILY_AROUND:HH:MM * * *`

The execution time SHALL be scattered within a ±60 minute window around the specified time.

**Example**:
```yaml
daily around 14:00
# Generates: FUZZY:DAILY_AROUND:14:0 * * *
# Scatters within window: 13:00 to 15:00
```

#### 3.2.3 Daily Between Times

A daily between schedule expression SHALL take the form:

```yaml
daily between <start-time> and <end-time>
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:DAILY_BETWEEN:START_H:START_M:END_H:END_M * * *`

The execution time SHALL be scattered uniformly within the specified time range, including handling of midnight-crossing ranges.

**Example**:
```yaml
daily between 9:00 and 17:00
# Generates: FUZZY:DAILY_BETWEEN:9:0:17:0 * * *
# Scatters within window: 09:00 to 17:00

daily between 22:00 and 02:00
# Generates: FUZZY:DAILY_BETWEEN:22:0:2:0 * * *
# Scatters within window: 22:00 to 02:00 (crossing midnight)
```

### 3.3 Weekly Schedules

#### 3.3.1 Basic Weekly Schedule

A basic weekly schedule expression SHALL take the form:

```yaml
weekly
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:WEEKLY * * *`

The execution SHALL be scattered across all seven days of the week and all hours/minutes of each day.

#### 3.3.2 Weekly with Day Specification

A weekly day schedule expression SHALL take the form:

```yaml
weekly on <weekday>
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:WEEKLY:DOW * * DOW`

**Example**:
```yaml
weekly on monday
# Generates: FUZZY:WEEKLY:1 * * 1
# Scatters across all hours on Monday
```

#### 3.3.3 Weekly with Time Constraints

A weekly schedule MAY include time constraints using `around` or `between`:

```yaml
weekly on <weekday> around <time-spec>
weekly on <weekday> between <start-time> and <end-time>
```

**Example**:
```yaml
weekly on friday around 17:00
# Generates: FUZZY:WEEKLY:5:AROUND:17:0 * * 5
# Scatters Friday 16:00-18:00
```

### 3.4 Hourly Schedules

#### 3.4.1 Basic Hourly Schedule

A basic hourly schedule expression SHALL take the form:

```yaml
hourly
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:HOURLY * * *`

The minute offset SHALL be scattered across 0-59 minutes but remain consistent for each hour.

**Example**:
```yaml
hourly
# Generates: FUZZY:HOURLY * * *
# Might scatter to: 43 * * * * (runs at minute 43 every hour)
```

#### 3.4.2 Hour Interval Schedules

An hour interval schedule expression SHALL take the form:

```yaml
every <N>h
every <N> hours
every <N> hour
```

Where `<N>` MUST be a positive integer.

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:HOURLY:<N> * * *`

Valid hour intervals SHOULD be: 1, 2, 3, 4, 6, 8, 12 (factors of 24 for even distribution).

**Example**:
```yaml
every 2h
# Generates: FUZZY:HOURLY:2 * * *
# Might scatter to: 53 */2 * * * (runs at minute 53 every 2 hours)
```

### 3.5 Special Period Schedules

#### 3.5.1 Bi-weekly Schedule

A bi-weekly schedule expression SHALL take the form:

```yaml
bi-weekly
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:BI-WEEKLY * * *`

The schedule SHALL execute once every 14 days with scattered time.

#### 3.5.2 Tri-weekly Schedule

A tri-weekly schedule expression SHALL take the form:

```yaml
tri-weekly
```

An implementation MUST generate a fuzzy cron placeholder: `FUZZY:TRI-WEEKLY * * *`

The schedule SHALL execute once every 21 days with scattered time.

### 3.6 Interval Schedules

An interval schedule expression SHALL take the form:

```yaml
every <N> <unit>
every <N><short-unit>
```

Where:
- `<N>` MUST be a positive integer
- `<unit>` MUST be one of: `minutes`, `minute`, `hours`, `hour`, `days`, `day`
- `<short-unit>` MUST be one of: `m`, `h`, `d`, `w`, `mo`. Weeks and months MUST use this short form (for example, `every 2w` and `every 2mo`); long-form weeks and months are not supported.

An implementation MUST generate appropriate cron expressions based on the unit:

- Minutes: `FUZZY:EVERY_MINUTE/N * * * *`, scattered to `M/N * * * *` with a hashed start minute `M` in `[0, N-1]` (minimum N=5 per GitHub Actions constraint)
- Hours: `FUZZY:HOURLY/N * * *` (scattered minute)
- Days: `FUZZY:DAILY * * *` for N=1; otherwise `FUZZY:DAILY_INTERVAL/N * * *`, scattered to `M H */N * *`
- Weeks: `FUZZY:WEEKLY * * *` for N=1; otherwise `FUZZY:DAILY_INTERVAL/(N*7) * * *`, scattered to `M H */(N*7) * *`
- Months: `FUZZY:MONTHLY_INTERVAL/N * * *`, scattered to `M H D * *` (N=1) or `M H D */N *`, with a hashed day of month `D` in `[1, 28]`

Day, week, and month intervals MUST use the same weighted daily time slot as `daily` for `H` and `M`, so that workflows sharing an interval do not fire at the same time.

**Example**:
```yaml
every 5 minutes
# Generates: FUZZY:EVERY_MINUTE/5 * * * *  → e.g. 3/5 * * * *

every 6h
# Generates: FUZZY:HOURLY/6 * * *  → e.g. 12 */6 * * *

every 2 days
# Generates: FUZZY:DAILY_INTERVAL/2 * * *  → e.g. 43 5 */2 * *
```

### 3.7 Error Norms for Invalid Schedule Expressions

The following table specifies normative behavior (MUST/SHALL requirements) for malformed or
unrecognizable fuzzy schedule expressions encountered during compilation. These norms apply
at parse time (when the compiler processes the workflow frontmatter) and at test time (when
the compliance test suite exercises the parser with invalid inputs).

| # | Error Condition | Input Example | MUST/SHALL Behavior | Error Code |
|---|----------------|---------------|---------------------|------------|
| E-01 | Unknown schedule keyword (not one of `daily`, `weekly`, `hourly`, `bi-weekly`, `tri-weekly`, `every`) | `monthly` | Implementation MUST reject with a descriptive error naming the unrecognized keyword and listing valid keywords | `UNKNOWN_KEYWORD` |
| E-02 | Out-of-range hour in 24-hour format | `daily around 25:00` | Implementation MUST reject; the error message MUST state the valid hour range (0–23) and the offending value | `HOUR_OUT_OF_RANGE` |
| E-03 | Out-of-range minute | `daily around 14:65` | Implementation MUST reject; the error message MUST state the valid minute range (0–59) and the offending value | `MINUTE_OUT_OF_RANGE` |
| E-04 | `around` keyword with no time specification | `daily around` | Implementation MUST reject; the error message MUST include an example of correct `around` usage | `MISSING_TIME_SPEC` |
| E-05 | `between` keyword with only one time argument | `daily between 9:00` | Implementation MUST reject; the error message MUST state that `between` requires both a start and an end time connected by `and` | `INCOMPLETE_RANGE` |
| E-06 | `between` range where start equals end | `daily between 14:00 and 14:00` | Implementation MUST reject; a zero-duration window cannot scatter execution times | `ZERO_DURATION_RANGE` |
| E-07 | Unknown weekday in `weekly on <day>` | `weekly on mondey` | Implementation MUST reject with a did-you-mean suggestion when the input differs from a valid weekday by one character | `UNKNOWN_WEEKDAY` |
| E-08 | Invalid interval unit | `every 5 fortnights` | Implementation MUST reject; the error message MUST list valid units (`minutes`, `hours`, `days`, `weeks` and their abbreviations) | `UNKNOWN_UNIT` |
| E-09 | Interval value below minimum allowed by GitHub Actions | `every 2 minutes` | Implementation MUST reject; the error message MUST state the minimum permitted interval (5 minutes for the `minutes` unit) and the GitHub Actions constraint source | `INTERVAL_TOO_SMALL` |
| E-10 | Non-integer interval value | `every 1.5 hours` | Implementation MUST reject; fractional interval values are not supported | `NON_INTEGER_INTERVAL` |

**Normative notes**:

- All error messages MUST be directed to the user's console (stderr) and MUST be human-readable.
- Implementations MUST NOT silently fall back to a default schedule when the input is invalid; all errors in rows E-01 through E-10 MUST cause compilation to fail with a non-zero exit code.
- Implementations SHOULD NOT attempt automatic correction of the schedule expression. Actionable correction guidance in the error message is preferred over silent fixup.

---

## 4. Time Specifications

### 4.1 Time Format Requirements

An implementation MUST support the following time formats:

#### 4.1.1 24-Hour Format

The 24-hour format SHALL use the pattern `HH:MM`:

- Hours MUST be in range 0-23
- Minutes MUST be in range 0-59
- Leading zeros MAY be omitted for hours
- Minutes MUST use two digits with leading zero if necessary

**Valid examples**: `00:00`, `9:30`, `14:00`, `23:59`

#### 4.1.2 12-Hour Format

The 12-hour format SHALL use the pattern `H[H]am` or `H[H]pm`:

- Hours MUST be in range 1-12
- AM/PM indicator MUST be lowercase `am` or `pm`
- Minutes MAY be omitted (defaults to :00)
- Colon and minutes MAY be included (e.g., `3:30pm`)

**Valid examples**: `1am`, `12pm`, `11pm`, `9am`, `3:30pm`

**Conversion rules**:
- `12am` converts to 00:00 (midnight)
- `12pm` converts to 12:00 (noon)
- `1am-11am` converts to 01:00-11:00
- `1pm-11pm` converts to 13:00-23:00

#### 4.1.3 Time Keywords

An implementation MUST support the following time keywords:

- `midnight`: Represents 00:00 (start of day)
- `noon`: Represents 12:00 (middle of day)

Keywords MUST be case-insensitive.

### 4.2 Time Range Requirements

#### 4.2.1 Window Specification

When using `around <time>`, the implementation MUST use a ±60 minute window centered on the specified time.

The window MUST handle day boundaries correctly:
- `around 00:30` creates window: 23:30 (previous day) to 01:30
- `around 23:30` creates window: 22:30 to 00:30 (next day)

#### 4.2.2 Range Specification

When using `between <start> and <end>`, the implementation MUST:

1. Accept ranges within a single day (e.g., `9:00` to `17:00`)
2. Accept ranges crossing midnight (e.g., `22:00` to `02:00`)
3. Calculate range size correctly for midnight-crossing ranges
4. Distribute scattered times uniformly within the range

For midnight-crossing ranges where start > end:
- Range size = (24*60 - start_minutes) + end_minutes

**Example**:
```yaml
between 22:00 and 02:00
# Range: 22:00, 22:01, ..., 23:59, 00:00, ..., 02:00
# Duration: 4 hours (240 minutes)
```

---

## 5. Timezone Support

### 5.1 UTC Offset Syntax

An implementation MUST support UTC offset specifications using the format:

```text
utc-offset = "utc" ("+" / "-") offset-value
offset-value = hours / hours ":" minutes
```

Where:
- `hours` MAY be 1 or 2 digits
- `minutes` MUST be 2 digits when specified
- Offset MUST be in range UTC-12:00 to UTC+14:00

**Valid examples**: `utc+9`, `utc-5`, `utc+05:30`, `utc-08:00`

### 5.2 Timezone Conversion

#### 5.2.1 Conversion Algorithm

When a UTC offset is specified, the implementation MUST:

1. Parse the local time value
2. Parse the UTC offset value (in minutes)
3. Subtract the offset from the local time to get UTC time
4. Handle day wrapping correctly

**Formula**: `UTC_time = local_time - offset`

**Example**:
```
local_time = 14:00 (2 PM)
offset = +9 hours (JST)
UTC_time = 14:00 - 9:00 = 05:00 (5 AM UTC)
```

#### 5.2.2 Day Boundary Handling

The implementation MUST handle day boundaries when converting times:

- Negative results MUST wrap to previous day (add 24 hours)
- Results ≥24:00 MUST wrap to next day (subtract 24 hours)
- Wrap operations MUST preserve minute precision

**Example**:
```
local_time = 02:00 (2 AM)
offset = +9 hours
UTC_time = 02:00 - 9:00 = -7:00 → 17:00 (previous day)
```

### 5.3 Common Timezone Abbreviations

An implementation SHOULD recognize common timezone abbreviations:

| Abbreviation | UTC Offset | Notes |
|--------------|------------|-------|
| PST | UTC-8 | Pacific Standard Time |
| PDT | UTC-7 | Pacific Daylight Time |
| EST | UTC-5 | Eastern Standard Time |
| EDT | UTC-4 | Eastern Daylight Time |
| JST | UTC+9 | Japan Standard Time |
| IST | UTC+5:30 | India Standard Time |

Implementations MAY issue warnings for ambiguous abbreviations (e.g., "PT" could be PST or PDT).

DST transition behavior:

- Abbreviation-based schedules MUST resolve to their explicit UTC offset at parse time (`PST=-08:00`,
  `PDT=-07:00`, etc.) and MUST NOT infer locale-specific daylight-saving transitions dynamically.
- During DST spring-forward and fall-back transitions, schedule scattering MUST remain stable for the
  same canonical UTC offset input and workflow identifier.
- Implementations SHOULD emit an informational warning when ambiguous abbreviations are used near DST
  transition dates so operators can switch to explicit `utc±HH[:MM]` notation.

---

## 6. Scattering Algorithm

### 6.1 Algorithm Purpose

The scattering algorithm MUST provide:

1. **Determinism**: Same workflow identifier produces same scattered time
2. **Distribution**: Scattered times distribute evenly across the allowed range
3. **Stability**: Scattered times remain constant across recompilations
4. **Uniqueness**: Different workflow identifiers produce different scattered times

The scattering algorithm uses the following formal input entities:

| Entity | Type | Constraints | Description |
|---|---|---|---|
| `workflow_identifier` | string | MUST be non-empty; SHOULD use `owner/repo/path/to/workflow.md` format | Canonical identifier hashed for deterministic scatter selection |
| `schedule_string` | string | MUST match a supported fuzzy placeholder form (`FUZZY:*`) | Parsed schedule expression that determines algorithm branch |
| `seed` | unsigned 32-bit integer | MUST be derived deterministically from `workflow_identifier` using the configured hash function | Hash-derived seed used for modulo operations |
| `window_minutes` | integer | MUST be positive; MUST NOT exceed 1440 | Candidate-minute search window for around/between scattering |

### 6.2 Hash Function Requirements

#### 6.2.1 Hash Algorithm Selection

An implementation MUST use a hash function that satisfies the following requirements:

1. **Determinism**: The hash function MUST produce the same output for the same input across all platforms and executions
2. **Distribution**: The hash function SHOULD produce uniformly distributed outputs across the hash space
3. **Stability**: The hash function MUST NOT change behavior across different versions of the implementation
4. **Integer output**: The hash function MUST produce an integer output suitable for modulo operations

**R-HASH-001**: For a fixed `workflow_identifier` and canonical fuzzy schedule expression,
implementations MUST preserve hash-derived scatter output across minor version upgrades. Any planned
change that would alter hash output for existing identifiers MUST be treated as a breaking change
and documented with migration guidance.

An implementation SHOULD use the FNV-1a (Fowler-Noll-Vo) 32-bit hash algorithm as a reference implementation:

```
hash = FNV_offset_basis
for each byte in input:
    hash = hash XOR byte
    hash = hash * FNV_prime
return hash

Where:
    FNV_offset_basis = 2166136261 (0x811c9dc5)
    FNV_prime = 16777619 (0x01000193)
```

Other suitable hash functions MAY be used, such as MurmurHash, xxHash, or CityHash, provided they meet the above requirements.

#### 6.2.2 Workflow Identifier Format

The workflow identifier used for hashing MUST be constructed as:

```
workflow_identifier = repository_slug + "/" + workflow_file_path
```

Where:
- `repository_slug` is the format `owner/repo`
- `workflow_file_path` is the relative path from repository root

**Example**: `github/gh-aw/.github/workflows/daily-report.md`

This format ensures workflows with the same filename in different repositories receive different execution times.

### 6.3 Scattering Ranges

#### 6.3.1 Daily Schedule Scattering

For `FUZZY:DAILY * * *` and `FUZZY:DAILY_WEEKDAYS * * *`, an implementation MUST use the **weighted daily time slot pool** to select execution time:

1. Construct a weighted pool of (hour, minute) time slots using three preference tiers:
   - **BEST** (weight 3): hours 02–05 UTC, odd minutes `{7, 13, 23, 37, 43, 53}` → 72 slots
   - **GOOD** (weight 2): hours 10–12 UTC, minutes `[5, 54]` → 300 slots
   - **OK** (weight 1): hours 19–23 UTC, minutes `[5, 54]` → 250 slots
   - Total pool size: 622 slots
2. Select slot: `index = hash(workflow_identifier) % pool_size`
3. Extract `(hour, minute)` from the selected slot
4. Generate cron: `<minute> <hour> * * *`  (or `* * 1-5` for weekday variant)

The pool is pre-computed once. Because each tier appears proportionally in the pool, a randomly selected slot is 3× more likely to land in the BEST window than in the OK window.

**Example**:
```
pool_size = 622
hash("github/gh-aw/workflow.md") % 622 = 84
slot[84] = (hour=2, minute=23)  # BEST tier
cron = "23 2 * * *"  (2:23 AM UTC)
```

#### 6.3.2 Daily Around Scattering

For `FUZZY:DAILY_AROUND:HH:MM * * *`:

1. Calculate target time in minutes: `target_minutes = HH * 60 + MM`
2. Define window: `[-60, +59]` minutes from target
3. Calculate hash modulo 120 (window size)
4. Calculate offset: `offset = hash_result - 60`
5. Calculate scattered time: `scattered_minutes = target_minutes + offset`
6. Handle day wrapping (keep within 0-1439)
7. Convert to hour and minute

**Example**:
```
target = 14:00 (840 minutes)
hash % 120 = 73
offset = 73 - 60 = 13
scattered = 840 + 13 = 853 minutes
hour = 853 / 60 = 14
minute = 853 % 60 = 13
cron = "13 14 * * *"  (2:13 PM, within 13:00-15:00 window)
```

#### 6.3.3 Daily Between Scattering

For `FUZZY:DAILY_BETWEEN:START_H:START_M:END_H:END_M * * *`:

1. Calculate start and end times in minutes
2. Calculate range size (handling midnight crossing)
3. Calculate hash modulo range_size
4. Add hash_result to start_minutes
5. Handle day wrapping
6. Convert to hour and minute

**For midnight-crossing ranges** (start > end):
```
range_size = (24 * 60 - start_minutes) + end_minutes
```

**Example**:
```
range = 9:00 to 17:00
start_minutes = 540, end_minutes = 1020
range_size = 1020 - 540 = 480 minutes (8 hours)
hash % 480 = 217
scattered = 540 + 217 = 757 minutes
hour = 757 / 60 = 12
minute = 757 % 60 = 37
cron = "37 12 * * *"  (12:37 PM)
```

#### 6.3.4 Hourly Schedule Scattering

For `FUZZY:HOURLY * * *`:

1. Calculate hash modulo 60
2. Use result as minute offset
3. Generate cron: `<minute> * * * *`

**Example**:
```
hash % 60 = 43
cron = "43 * * * *"  (runs at minute 43 every hour)
```

For `FUZZY:HOURLY:N * * *`:

1. Calculate hash modulo 60
2. Use result as minute offset
3. Generate cron: `<minute> */N * * *`

**Example**:
```
interval = 2 hours
hash % 60 = 53
cron = "53 */2 * * *"  (runs at minute 53 every 2 hours)
```

#### 6.3.5 Weekly Schedule Scattering

For `FUZZY:WEEKLY * * *` and `FUZZY:WEEKLY:DOW * * *`:

1. Select day-of-week: `weekday = hash(workflow_identifier) % 7` (0=Sunday, 6=Saturday)  
   For `FUZZY:WEEKLY:DOW`, the day is fixed from the expression instead.
2. Select time from the **weighted daily time slot pool** (Section 6.3.1)
3. Generate cron: `<minute> <hour> * * <day>`

Both patterns use the same weighted pool as the daily schedule, ensuring execution times prefer the BEST/GOOD/OK tiers rather than distributing flatly across the full day.

**Example**:
```
weekly on monday
day = 1 (Monday)
pool selection → (hour=2, minute=23)  # BEST tier
cron = "23 2 * * 1"  (Monday 2:23 AM UTC)
```

#### 6.3.6 Bi-weekly and Tri-weekly Scattering

For `FUZZY:BI-WEEKLY * * *` and `FUZZY:TRI-WEEKLY * * *`:

1. Select time from the **weighted daily time slot pool** (Section 6.3.1)
2. Generate cron: `<minute> <hour> */14 * *` (bi-weekly) or `<minute> <hour> */21 * *` (tri-weekly)

Both patterns use the same weighted pool to ensure execution during preferred low-traffic windows.

### 6.4 Peak Minutes Avoidance

To reduce scheduling collisions with other commonly-scheduled cron jobs, implementations MUST apply two minute-avoidance passes after computing the raw scattered minute value.

#### 6.4.1 Hour Boundary Avoidance (`avoidHourBoundary`)

Minutes near the hour boundary (0–4 and 55–59) are subject to elevated load on GitHub Actions infrastructure, especially at 00:00 UTC.

An implementation MUST remap minute values as follows:

| Input range | Output |
|-------------|--------|
| [0, 4] | minute + 5 |
| [55, 59] | minute − 5 |
| [5, 54] | unchanged |

This ensures all generated minute values are in [5, 54].

**Scope**: Applied to ALL targeted-scatter patterns (DAILY_AROUND, DAILY_BETWEEN, WEEKLY_AROUND, and their weekday variants).

#### 6.4.2 Peak Minutes Avoidance (`avoidPeakMinutes`)

Known high-traffic periods require avoidance of minutes that fall within ±3 of the peak minute values.

An implementation MUST apply the following remapping **after** `avoidHourBoundary`:

| Condition | Avoid range | Replacement |
|-----------|-------------|-------------|
| hour ∈ [6, 9] AND minute ∈ [27, 33] | [27, 33] | 34 |
| hour ∈ [14, 18] AND minute ∈ [12, 18] | [12, 18] | 19 |
| hour ∈ [14, 18] AND minute ∈ [42, 48] | [42, 48] | 49 |

**Rationale**:
- **EU morning peak** (06:00–09:59 UTC): `:30` is a commonly-used cron minute. Staying 3 minutes away (avoiding [27,33]) reduces collisions.
- **US business hours** (14:00–18:59 UTC): `:15` and `:45` are quarter-hour marks widely used by monitoring and reporting cron jobs. Staying 3 minutes away (avoiding [12,18] and [42,48]) reduces collisions.

**Application order**: `avoidHourBoundary` MUST be applied before `avoidPeakMinutes`.

**Scope**: `avoidPeakMinutes` applies only to targeted-scatter patterns. Full-day scatter patterns that use the weighted pool (Section 6.3.1) already avoid peak windows by construction, since the pool does not include EU peak hours (06–09) or US peak hours (14–18).

**Example**:
```
FUZZY:DAILY_AROUND:14:00, workflow "my-scanner"
  Raw scattered time: 14:28
  Step 1 (avoidHourBoundary): 28 → 28  (no change; 28 ∈ [5,54])
  Step 2 (avoidPeakMinutes):  28 → 34  (shifted; hour ∈ [14,18], minute 28 ∈ [27,33]
                                          — wait, hour=14, so EU rule doesn't apply;
                                          US :15 rule: 28 ∉ [12,18]; :45 rule: 28 ∉ [42,48])
  → no shift needed; result: 14:28

FUZZY:DAILY_AROUND:15:00, workflow "my-monitor"
  Raw scattered time: 15:13
  Step 1 (avoidHourBoundary): 13 → 13  (no change)
  Step 2 (avoidPeakMinutes):  13 → 19  (shifted; hour ∈ [14,18], minute 13 ∈ [12,18])
  → result: 15:19
```

### 6.5 Algorithm Requirements

An implementation MUST ensure:

1. Hash function produces same output for same input across platforms
2. Modulo operations use consistent integer division
3. Day wrapping uses consistent addition/subtraction rules
4. Minute and hour extraction uses consistent division and modulo operations
5. `avoidHourBoundary` is applied before `avoidPeakMinutes` for all targeted-scatter patterns
6. Full-day scatter patterns use the weighted daily time slot pool (Section 6.3.1)

---

## 7. Cron Expression Generation

### 7.1 Fuzzy Cron Placeholders

An implementation MUST generate fuzzy cron placeholders that can be resolved later by the scattering algorithm. Placeholders MUST use the format:

```
FUZZY:<TYPE>[:<PARAMS>] <cron-fields>
```

Where:
- `<TYPE>` identifies the schedule type
- `<PARAMS>` provides optional parameters (time, day, range)
- `<cron-fields>` includes remaining cron fields (typically `* * *`)

### 7.2 Placeholder Formats

| Schedule Type | Placeholder Format |
|---------------|-------------------|
| Daily | `FUZZY:DAILY * * *` |
| Daily around | `FUZZY:DAILY_AROUND:HH:MM * * *` |
| Daily between | `FUZZY:DAILY_BETWEEN:SH:SM:EH:EM * * *` |
| Hourly | `FUZZY:HOURLY * * *` |
| Hour interval | `FUZZY:HOURLY:N * * *` |
| Weekly | `FUZZY:WEEKLY * * *` |
| Weekly with day | `FUZZY:WEEKLY:DOW * * DOW` |
| Weekly day around | `FUZZY:WEEKLY:DOW:AROUND:HH:MM * * DOW` |
| Weekly day between | `FUZZY:WEEKLY:DOW:BETWEEN:SH:SM:EH:EM * * DOW` |
| Bi-weekly | `FUZZY:BI-WEEKLY * * *` |
| Tri-weekly | `FUZZY:TRI-WEEKLY * * *` |

### 7.3 Placeholder Resolution

An implementation MUST provide a mechanism to resolve fuzzy placeholders to concrete cron expressions using the scattering algorithm and workflow identifier.

The resolution process MUST:

1. Detect fuzzy placeholder format
2. Extract schedule type and parameters
3. Apply appropriate scattering algorithm
4. Generate valid 5-field cron expression
5. Validate resulting cron expression

### 7.4 Cron Expression Validation

Generated cron expressions MUST conform to GitHub Actions cron syntax:

- 5 fields: `minute hour day-of-month month day-of-week`
- Minutes: 0-59 or `*` or `*/N`
- Hours: 0-23 or `*` or `*/N`
- Day-of-month: 1-31 or `*` or `*/N`
- Month: 1-12 or `*` or `*/N`
- Day-of-week: 0-6 (Sunday=0) or `*`

---

## 8. Safeguards

The following safeguards are normative and apply to all scattering implementations.

**R-SAFE-001**: Implementations **MUST** enforce finite scatter windows. For `around` schedules,
the effective jitter window **MUST NOT** exceed ±60 minutes from the requested anchor time. For
`between` schedules, the scattered time **MUST** remain inside the declared closed interval.

**R-SAFE-002**: Implementations **MUST** apply collision-avoidance normalization before returning
the final minute value. At minimum, the implementation **MUST** avoid hour-boundary hotspots and
known quarter-hour peaks as defined by Section 6.4. This guarantee is deterministic for a given
workflow identifier and schedule expression.

**R-SAFE-003**: If hash input material is empty (for example, missing workflow identifier), the
implementation **MUST** fail with a descriptive error and **MUST NOT** fall back to random
scattering.

**R-SAFE-005**: If non-unique hash input causes repeated collisions across workflows, the
implementation **MUST** preserve deterministic behavior and **SHOULD** emit a warning indicating
reduced distribution quality. Implementations **MUST NOT** silently switch to non-deterministic
fallbacks to hide collisions.

---

## 9. Error Handling

### 9.1 Syntax Errors

An implementation MUST reject invalid expressions with clear error messages:

#### 9.1.1 Invalid Schedule Type

```
Error: Unknown schedule type 'monthly'
Valid types: daily, weekly, hourly, bi-weekly, tri-weekly, every
```

#### 9.1.2 Invalid Time Format

```
Error: Invalid time format '25:00' in 'daily around 25:00'
Time must be in 24-hour format (HH:MM, 0-23 hours) or 12-hour format with am/pm
```

#### 9.1.3 Invalid Weekday

```
Error: Unknown weekday 'mondey' in 'weekly on mondey'
Valid weekdays: sunday, monday, tuesday, wednesday, thursday, friday, saturday
```

#### 9.1.4 Invalid Interval

```
Error: Invalid interval '5' in 'every 5h'
Valid hour intervals: 1h, 2h, 3h, 4h, 6h, 8h, 12h
```

### 9.2 Semantic Errors

#### 9.2.1 Missing Required Components

```
Error: 'around' requires a time specification
Example: daily around 14:00
```

#### 9.2.2 Unsupported Syntax

```
Error: 'daily at <time>' syntax is not supported
Use 'daily around <time>' for fuzzy scheduling within ±1 hour window
```

#### 9.2.3 Duplicate or Conflicting Schedule Expressions

Implementations MUST reject duplicate or conflicting schedule declarations for the same workflow
definition scope (for example, both `schedule: daily around 09:00` and `schedule: weekly on monday
around 09:00`, or repeated `schedule:` entries after merge/templating).

Conforming implementations MUST emit the stable error code `ERR-CONFLICT-001` and MUST fail
compilation rather than selecting one expression implicitly.

### 9.3 Warning Messages

An implementation SHOULD issue warnings for valid but suboptimal patterns:

```
Warning: Consider using 'every 2h' instead of fixed interval
Fixed intervals create load spikes when many workflows run simultaneously
```

### 9.4 Error Recovery

An implementation SHOULD NOT attempt to correct syntax errors automatically. All errors MUST be reported to the user with actionable correction guidance.

### 9.5 Edge-Case Conformance Requirements

The following edge-case norms are mandatory in addition to §§9.1–9.4:

1. **Invalid scatter seed**: If seed derivation produces an empty, negative, or non-integer
   value, the implementation **MUST** fail compilation with a descriptive error and
   **MUST NOT** fall back to a random or default seed.
2. **Out-of-range time values**: Inputs containing hour values outside `0..23` (24-hour),
   minute values outside `0..59`, or 12-hour values outside `1..12` **MUST** be rejected
   with an error that includes the offending token and valid range.
3. **Malformed grammar input**: Expressions that violate the ABNF in §3.1 (e.g., missing
   `and` in `between`, dangling modifiers, extra tokens after a valid production) **MUST**
   fail parsing and **MUST NOT** be auto-corrected.
4. **Error code stability**: For the same malformed input class, implementations **MUST**
    return a stable error code category across runs to support deterministic compliance tests.

### 9.6 Retry and Backoff Norms for Collision/Contention Paths

When compilation or scheduling pipelines detect contention that is attributable to repeated hash
collisions (for example, repeated retries to acquire shared scheduler state for the same minute
bucket), implementations MUST apply bounded retry behavior:

1. **R-ERR-050**: Retry loops for collision/contention handling MUST be bounded to a maximum of 3
   attempts total (initial attempt + up to 2 retries).
2. **R-ERR-051**: Retry delays SHOULD use exponential backoff with jitter (initial delay at least
   100 ms, 2x multiplier, maximum delay 2 s).
3. **R-ERR-052**: When retry budget is exhausted, implementations MUST fail deterministically with a
   stable error code and MUST NOT silently fall back to non-deterministic scheduling.

---

## 10. Compliance Testing

### 10.1 Test Suite Requirements

A conforming implementation MUST pass all Level 1 tests. Implementations claiming Level 2 or Level 3 conformance MUST pass all tests for their claimed level and all lower levels.

### 10.2 Test Categories

#### 10.2.1 Syntax Parsing Tests (Level 1)

- **T-SYNTAX-001**: Parse `daily` to `FUZZY:DAILY * * *`
- **T-SYNTAX-002**: Parse `weekly` to `FUZZY:WEEKLY * * *`
- **T-SYNTAX-003**: Parse `weekly on monday` to `FUZZY:WEEKLY:1 * * 1`
- **T-SYNTAX-004**: Parse all weekday names correctly
- **T-SYNTAX-005**: Reject invalid schedule types
- **T-SYNTAX-006**: Reject invalid weekday names
- **T-SYNTAX-007**: Parse case-insensitive tokens

#### 10.2.2 Time Format Tests (Level 2)

- **T-TIME-001**: Parse 24-hour format `14:00`
- **T-TIME-002**: Parse 12-hour format `3pm`
- **T-TIME-003**: Parse 12-hour format `11am`
- **T-TIME-004**: Parse keyword `midnight` as 00:00
- **T-TIME-005**: Parse keyword `noon` as 12:00
- **T-TIME-006**: Convert `12am` to 00:00 (midnight)
- **T-TIME-007**: Convert `12pm` to 12:00 (noon)
- **T-TIME-008**: Reject invalid hours (>23 or <0)
- **T-TIME-009**: Reject invalid minutes (>59 or <0)
- **T-TIME-010**: Handle missing leading zeros (e.g., `9:30`)

#### 10.2.3 Time Constraint Tests (Level 2)

- **T-CONSTRAINT-001**: Parse `daily around 14:00`
- **T-CONSTRAINT-002**: Parse `daily between 9:00 and 17:00`
- **T-CONSTRAINT-003**: Parse `weekly on friday around 17:00`
- **T-CONSTRAINT-004**: Handle midnight-crossing ranges (`22:00 and 02:00`)
- **T-CONSTRAINT-005**: Reject `around` without time specification
- **T-CONSTRAINT-006**: Reject `between` with only one time
- **T-CONSTRAINT-007**: Reject `daily at <time>` syntax

#### 10.2.4 Timezone Tests (Level 3)

- **T-TZ-001**: Parse `utc+9` offset
- **T-TZ-002**: Parse `utc-5` offset
- **T-TZ-003**: Parse `utc+05:30` offset format
- **T-TZ-004**: Convert `14:00 utc+9` to `05:00` UTC
- **T-TZ-005**: Convert `3pm utc-5` to `20:00` UTC
- **T-TZ-006**: Handle negative UTC conversion (wrap to previous day)
- **T-TZ-007**: Handle >24:00 UTC conversion (wrap to next day)
- **T-TZ-008**: Reject invalid offsets (e.g., `utc+25`)

#### 10.2.5 Hourly and Interval Tests (Level 2/3)

- **T-HOURLY-001**: Parse `hourly` to `FUZZY:HOURLY * * *`
- **T-HOURLY-002**: Parse `every 2h` to `FUZZY:HOURLY:2 * * *`
- **T-HOURLY-003**: Parse `every 6 hours` to `FUZZY:HOURLY:6 * * *`
- **T-INTERVAL-001**: Parse `every 5 minutes` to `FUZZY:EVERY_MINUTE/5 * * * *`
- **T-INTERVAL-002**: Parse `every 2 days` to `FUZZY:DAILY_INTERVAL/2 * * *` and scatter it to `M H */2 * *`
- **T-INTERVAL-003**: Reject `every 3 minutes` (below 5-minute minimum)
- **T-INTERVAL-004**: Parse `bi-weekly` to `FUZZY:BI-WEEKLY * * *`
- **T-INTERVAL-005**: Parse `tri-weekly` to `FUZZY:TRI-WEEKLY * * *`

#### 10.2.6 Scattering Algorithm Tests (Level 1-3)

- **T-SCATTER-001**: Hash produces same output for same input
- **T-SCATTER-002**: Different inputs produce different outputs
- **T-SCATTER-003**: Hash value is within modulo range (0 to modulo-1)
- **T-SCATTER-004**: Daily schedule selects time from weighted pool (BEST/GOOD/OK tiers only)
- **T-SCATTER-005**: Around schedule stays within ±60 minute window
- **T-SCATTER-006**: Between schedule stays within specified range
- **T-SCATTER-007**: Midnight-crossing range handles day wrap correctly
- **T-SCATTER-008**: Hourly schedule produces minute in [5, 54]
- **T-SCATTER-009**: Weekly schedule selects valid day 0-6
- **T-SCATTER-010**: Same workflow gets same time across compilations
- **T-SCATTER-011**: Daily schedule lands in BEST (02–05), GOOD (10–12), or OK (19–23) window
- **T-SCATTER-012**: Minute values in [5, 54] for all patterns (hour-boundary avoidance)
- **T-SCATTER-013**: DAILY_AROUND scatter landing in EU peak hours (06–09) avoids minutes [27, 33]
- **T-SCATTER-014**: DAILY_AROUND scatter landing in US business hours (14–18) avoids minutes [12, 18] and [42, 48]
- **T-SCATTER-015**: Weekly schedule uses weighted daily time pool (preferred windows)
- **T-SCATTER-016**: Bi-weekly and tri-weekly schedules use weighted daily time pool

#### 10.2.7 Cron Generation Tests (Level 1-3)

- **T-CRON-001**: Generated cron has exactly 5 fields
- **T-CRON-002**: Minute field is in range 0-59
- **T-CRON-003**: Hour field is in range 0-23
- **T-CRON-004**: Day-of-week field is in range 0-6 or `*`
- **T-CRON-005**: Month and day-of-month are valid
- **T-CRON-006**: Interval expressions use valid `*/N` syntax

#### 10.2.8 Level 3 Compliance Group Tests

These test IDs group Level 3 requirements for implementations claiming Level 3 conformance.
Each T-FZ-L3-* ID represents a consolidated compliance requirement that maps to one or more
existing test cases in §10.2.4 and §10.2.5.

- **T-FZ-L3-001**: Timezone conversion correctness — maps to T-TZ-004, T-TZ-005. Verify
  that `14:00 utc+9` converts to `05:00` UTC and `3pm utc-5` converts to `20:00` UTC.
- **T-FZ-L3-002**: Timezone day-boundary wrap — maps to T-TZ-006, T-TZ-007. Verify that
  negative UTC conversion wraps to the previous day and UTC conversion >24:00 wraps to the
  next day.
- **T-FZ-L3-003**: Reject out-of-range UTC offsets — maps to T-TZ-008. Verify that offsets
  beyond ±14 (e.g., `utc+25`) are rejected with an error.
- **T-FZ-L3-004**: Bi-weekly parse and scatter — maps to T-INTERVAL-004, T-SCATTER-016.
  Verify `bi-weekly` parses to `FUZZY:BI-WEEKLY * * *` and that scatter resolves to a
  time in the weighted daily slot pool (BEST/GOOD/OK windows).
- **T-FZ-L3-005**: Tri-weekly parse and scatter — maps to T-INTERVAL-005, T-SCATTER-016.
  Verify `tri-weekly` parses to `FUZZY:TRI-WEEKLY * * *` and that scatter resolves to a
  time in the weighted daily slot pool (BEST/GOOD/OK windows).

### 10.3 Compliance Checklist

| Requirement | Test ID | Level | Status |
|-------------|---------|-------|--------|
| Parse basic daily | T-SYNTAX-001 | 1 | Required |
| Parse basic weekly | T-SYNTAX-002 | 1 | Required |
| Parse weekday specification | T-SYNTAX-003 | 1 | Required |
| Parse all weekday names | T-SYNTAX-004 | 1 | Required |
| Reject invalid types | T-SYNTAX-005 | 1 | Required |
| Case-insensitive parsing | T-SYNTAX-007 | 1 | Required |
| Parse 24-hour format | T-TIME-001 | 2 | Required |
| Parse 12-hour format | T-TIME-002, 003 | 2 | Required |
| Parse time keywords | T-TIME-004, 005 | 2 | Required |
| Handle 12am/12pm correctly | T-TIME-006, 007 | 2 | Required |
| Validate time ranges | T-TIME-008, 009 | 2 | Required |
| Parse around constraints | T-CONSTRAINT-001 | 2 | Required |
| Parse between constraints | T-CONSTRAINT-002 | 2 | Required |
| Handle midnight crossing | T-CONSTRAINT-004 | 2 | Required |
| Parse UTC offsets | T-TZ-001, 002, 003 | 3 | Required |
| Convert timezones correctly | T-TZ-004, 005 | 3 | Required |
| Handle timezone day wrap | T-TZ-006, 007 | 3 | Required |
| Reject invalid UTC offsets | T-TZ-008 | 3 | Required |
| Parse hourly schedules | T-HOURLY-001, 002, 003 | 2 | Required |
| Parse interval schedules | T-INTERVAL-001, 002 | 3 | Required |
| Parse bi-weekly expression | T-INTERVAL-004 | 3 | Required |
| Parse tri-weekly expression | T-INTERVAL-005 | 3 | Required |
| Hash determinism | T-SCATTER-001, 002 | 1 | Required |
| Scattering distribution | T-SCATTER-004-009 | 1-3 | Required |
| Weighted daily pool | T-SCATTER-011, 015, 016 | 1-3 | Required |
| Peak avoidance (hour boundary) | T-SCATTER-012 | 1-3 | Required |
| Peak avoidance (EU morning peak) | T-SCATTER-013 | 2-3 | Required |
| Peak avoidance (US business hours) | T-SCATTER-014 | 2-3 | Required |
| Generate valid cron | T-CRON-001-006 | 1-3 | Required |
| **Timezone conversion (Level 3 group)** | **T-FZ-L3-001** | **3** | **Required** |
| **Timezone day-boundary wrap (Level 3 group)** | **T-FZ-L3-002** | **3** | **Required** |
| **Reject out-of-range UTC offsets (Level 3 group)** | **T-FZ-L3-003** | **3** | **Required** |
| **Bi-weekly parse and scatter (Level 3 group)** | **T-FZ-L3-004** | **3** | **Required** |
| **Tri-weekly parse and scatter (Level 3 group)** | **T-FZ-L3-005** | **3** | **Required** |

### 10.4 Test Execution

Implementations SHOULD provide:

1. Automated test suite covering all compliance tests
2. Test report indicating pass/fail status for each test
3. Conformance level declaration (Level 1, 2, or 3)

---

## Appendices

### Appendix A: Complete Examples

#### A.1 Daily Schedule Examples

```yaml
# Basic daily (scattered across full day)
schedule: daily
# Fuzzy: FUZZY:DAILY * * *
# Might generate: 43 5 * * * (5:43 AM)

# Daily around specific time
schedule: daily around 14:00
# Fuzzy: FUZZY:DAILY_AROUND:14:0 * * *
# Might generate: 13 14 * * * (2:13 PM, within 1-3 PM window)

# Daily during business hours
schedule: daily between 9:00 and 17:00
# Fuzzy: FUZZY:DAILY_BETWEEN:9:0:17:0 * * *
# Might generate: 37 12 * * * (12:37 PM, within 9 AM-5 PM)

# Daily with timezone conversion (JST to UTC)
schedule: daily around 14:00 utc+9
# Fuzzy: FUZZY:DAILY_AROUND:5:0 * * *
# Converts to 5:00 AM UTC, scatters in window 4-6 AM UTC
```

#### A.2 Weekly Schedule Examples

```yaml
# Basic weekly (any day, any time)
schedule: weekly
# Fuzzy: FUZZY:WEEKLY * * *
# Might generate: 43 5 * * 1 (Monday 5:43 AM)

# Weekly on specific day
schedule: weekly on monday
# Fuzzy: FUZZY:WEEKLY:1 * * 1
# Might generate: 18 14 * * 1 (Monday 2:18 PM)

# Weekly with time constraint
schedule: weekly on friday around 17:00
# Fuzzy: FUZZY:WEEKLY:5:AROUND:17:0 * * 5
# Might generate: 42 16 * * 5 (Friday 4:42 PM, within 4-6 PM)
```

#### A.3 Hourly and Interval Examples

```yaml
# Every hour with scattered minute
schedule: hourly
# Fuzzy: FUZZY:HOURLY * * *
# Might generate: 43 * * * * (every hour at minute 43)

# Every 2 hours
schedule: every 2h
# Fuzzy: FUZZY:HOURLY:2 * * *
# Might generate: 53 */2 * * * (every 2 hours at minute 53)

# Every 5 minutes (fixed, not fuzzy)
schedule: every 5 minutes
# Generates: */5 * * * * (fixed interval)

# Bi-weekly
schedule: bi-weekly
# Fuzzy: FUZZY:BI-WEEKLY * * *
# Might generate: 43 5 */14 * * (every 14 days at 5:43 AM)
```

#### A.4 Timezone Conversion Examples

```yaml
# JST (UTC+9) business hours to UTC
schedule: daily between 9am utc+9 and 5pm utc+9
# Converts to: daily between 0:00 and 8:00 (UTC)
# Fuzzy: FUZZY:DAILY_BETWEEN:0:0:8:0 * * *

# EST (UTC-5) afternoon meeting
schedule: weekly on monday around 3pm utc-5
# Converts to: weekly on monday around 20:00 (UTC)
# Fuzzy: FUZZY:WEEKLY:1:AROUND:20:0 * * 1

# IST (UTC+5:30) morning standup
schedule: daily around 9:30am utc+05:30
# Converts to: daily around 4:00 (UTC)
# Fuzzy: FUZZY:DAILY_AROUND:4:0 * * *
```

### Appendix B: Error Code Reference

| Error Code | Description | Example |
|------------|-------------|---------|
| ERR-SYNTAX-001 | Unknown schedule type | `monthly` (not supported) |
| ERR-SYNTAX-002 | Invalid time format | `25:00` (hour out of range) |
| ERR-SYNTAX-003 | Invalid weekday | `mondey` (typo) |
| ERR-SYNTAX-004 | Missing required component | `daily around` (no time) |
| ERR-SYNTAX-005 | Unsupported syntax pattern | `daily at 14:00` (use `around`) |
| ERR-TIME-001 | Hour out of range | `25` (>23) |
| ERR-TIME-002 | Minute out of range | `60` (>59) |
| ERR-TIME-003 | Invalid 12-hour format | `13pm` (hour >12) |
| ERR-TZ-001 | Invalid UTC offset | `utc+25` (>14) |
| ERR-TZ-002 | Malformed offset syntax | `utc9` (missing +/-) |
| ERR-INTERVAL-001 | Invalid interval value | `every 0h` (must be >0) |
| ERR-INTERVAL-002 | Unsupported interval | `every 5h` (not factor of 24) |
| ERR-CONFLICT-001 | Duplicate/conflicting schedule expressions | Two `schedule:` expressions in one workflow scope |

### Appendix C: Security Considerations

#### C.1 Hash Collision Resistance

The FNV-1a 32-bit hash provides adequate collision resistance for workflow scattering purposes. The birthday paradox suggests approximately 77,000 workflows are needed for a 50% collision probability. For organizations with fewer workflows, collisions are unlikely.

If collision occurs (two workflows receive identical execution times), this does not create a security vulnerability but reduces the effectiveness of load distribution.

#### C.2 Predictability

The deterministic nature of the scattering algorithm means execution times are predictable given the workflow identifier. This is intentional for consistency but means:

- Attackers cannot cause DOS by triggering simultaneous execution
- Execution times cannot be used as secrets
- Load distribution is transparent and auditable

#### C.3 Timezone Handling

Implementations MUST handle timezone offsets with integer arithmetic to prevent floating-point rounding errors that could cause inconsistent execution times.

Implementations SHOULD validate UTC offsets are within reasonable bounds (UTC-12 to UTC+14) to prevent overflow in time calculations.

#### C.4 Input Validation

Implementations MUST validate all user inputs before processing:

- Schedule type MUST be from allowed set
- Time values MUST be within valid ranges
- Interval values MUST be positive integers
- All string inputs MUST be sanitized to prevent injection attacks

---

## 11. Sync Notes

This section maps the fuzzy schedule specification to implementation files.

| Normative Area | Implementation File(s) |
|---|---|
| §3.1 Grammar (`schedule: daily around HH[:MM][am/pm][ timezone]`) | `pkg/parser/schedule_parser.go` (`parseFuzzyScheduleExpression`, tokenizer/grammar helpers) |
| §6 Scattering algorithm | `pkg/parser/schedule_fuzzy_scatter.go` (`scatterDailyTime`, weighted slot selection and deterministic hashing) |
| Frontmatter schedule parsing and grammar handling | `pkg/parser/schedule_parser.go` |
| Deterministic fuzzy scattering and peak-minute avoidance | `pkg/parser/schedule_fuzzy_scatter.go` |
| Parser/scatter conformance tests | `pkg/parser/schedule_parser_test.go`, `pkg/parser/schedule_fuzzy_scatter_test.go` |
| Calendar/cron visualization support for compile tooling (see §12) | `pkg/cli/compile_schedule_calendar.go` |

**Hash function**: The scattering algorithm (§6.2) uses the **FNV-1a 32-bit** hash function
(`FNV_offset_basis = 0x811c9dc5`, `FNV_prime = 0x01000193`) applied to the workflow identifier
string `{owner}/{repo}/{workflow_file_path}`. This hash is implemented in
`pkg/parser/schedule_fuzzy_scatter.go`. Alternative hash functions are permitted by §6.2.1 if
they satisfy the determinism, distribution, and stability requirements, but the FNV-1a reference
implementation is normative for cross-platform consistency tests.

After changing fuzzy schedule semantics:
1. Update this specification section and any affected normative clauses.
2. Update parser/scatter implementation in the mapped files.
3. Re-run parser/scatter tests to verify behavior remains deterministic.

Integration coverage notes:

- Conforming changes SHOULD exercise end-to-end compile coverage in addition to parser-only tests so
  fuzzy expressions are validated after placeholder expansion into emitted cron schedules.
- Changes that affect calendar rendering or weighted slot selection SHOULD include integration
  assertions against `pkg/cli/compile_schedule_calendar.go` output, not only unit assertions against
  parser helpers.

**§12 Audit (2026-05-26)**: Section 12 (Calendar Output Schema) is confirmed complete and
non-stub. It defines normative MUST/SHOULD requirements for output stream, emission condition,
title line, hour header, day rows, cells, legend, and file output. The implementation reference
(`pkg/cli/compile_schedule_calendar.go`) is mapped in the sync table above.

---

## 12. Calendar Output Schema

The compile-time schedule calendar emitted by `pkg/cli/compile_schedule_calendar.go` documents the
aggregate UTC trigger density of scheduled workflows. A conforming implementation MUST treat the
calendar as a human-readable console artifact rather than a machine-readable file format.

### 12.1 Minimum-Required Fields

The following elements are **required** and MUST be present in every conforming calendar output:

| Element | Requirement |
|---|---|
| Output stream | MUST be written to `stderr` only, and MUST NOT be emitted in JSON output mode. |
| Emission condition | MUST be omitted when no scheduled workflows are present. |
| Title line | MUST render the heading `Schedule Heatmap (UTC)`. |
| Hour header | MUST contain 24 UTC hour labels from `00` through `23`, in ascending order. |
| Day rows | MUST render exactly seven rows in `Mon`, `Tue`, `Wed`, `Thu`, `Fri`, `Sat`, `Sun` order. |
| Cells | MUST render one glyph per hour slot using the implementation's intensity mapping (`·`, `░`, `▒`, `▓`, `█`). |
| Legend | MUST explain the trigger-count buckets for each glyph after the grid. |
| File output | MUST NOT create a separate file; the calendar is an inline stderr rendering only. |

### 12.2 Optional Extended Fields

The following elements are **optional** and MAY be present in conforming calendar output. Their
presence MUST NOT break consumers that expect only the minimum-required fields above.

| Element | Requirement |
|---|---|
| ANSI color styling | MAY be applied when stderr is a terminal; MUST degrade gracefully to plain text in non-TTY contexts. |
| Per-workflow summary | MAY render a secondary table listing each scheduled workflow and its scattered cron expression. |
| Workflow count line | MAY render a count line such as `N scheduled workflows` immediately before or after the heatmap grid. |
| Timezone annotation | MAY append a `(UTC)` or equivalent annotation to the title or hour header to reinforce UTC context. |

Implementations SHOULD preserve a fixed-width grid so adjacent cells remain visually aligned in
plain-text terminals. ANSI styling MAY be applied when stderr is a terminal, but the unstyled text
content MUST preserve the same row/column structure.

### 12.3 Example Output

The following is a minimal conforming calendar output for a repository with two scheduled
workflows. The exact trigger-count thresholds for intensity glyphs are implementation-defined, but
MUST be documented in the legend.

```
Schedule Heatmap (UTC)
       00 01 02 03 04 05 06 07 08 09 10 11 12 13 14 15 16 17 18 19 20 21 22 23
Mon  │ ·  ·  ░  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  · │
Tue  │ ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  · │
Wed  │ ·  ·  ░  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  · │
Thu  │ ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  · │
Fri  │ ·  ·  ░  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  · │
Sat  │ ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  · │
Sun  │ ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  ·  · │

Legend: · = 0  ░ = 1–2  ▒ = 3–5  ▓ = 6–10  █ = >10 triggers/hour
```

### 12.4 Field-Level Schema

The following field-level schema defines the minimum calendar object contract for implementations
that expose this output in structured form (for testing, diagnostics, or internal adapters).

| Field | Type | Description | Required |
|---|---|---|---|
| `title` | `string` | Calendar heading text (for example, `Schedule Heatmap (UTC)`). | Required |
| `timezone` | `string` | Timezone label used by the grid; MUST be `UTC` for conforming output. | Required |
| `hours` | `string[]` | Ordered 24-item hour label array from `00` through `23`. | Required |
| `days` | `string[]` | Ordered 7-item day label array in `Mon` through `Sun` order. | Required |
| `cells` | `string[][]` | 7×24 glyph matrix representing trigger density for each day/hour slot. | Required |
| `legend` | `string` | Human-readable bucket mapping for density glyphs. | Required |
| `workflow_count` | `number` | Count of scheduled workflows represented by the calendar. | Optional |
| `styled` | `boolean` | Whether ANSI styling was applied in the rendered output. | Optional |

### Version 1.2.0 (Draft) — 2026-05-12

- **Changed**: Daily, weekly, bi-weekly, and tri-weekly scattering now share the weighted 622-slot
  pool introduced in Sections 6.3.1 and 6.3.5–6.3.6.
- **Added**: Peak-minute avoidance rules in Section 6.4 to steer schedules away from `:00`, `:15`,
  `:30`, and `:45` hotspot minutes during documented peak windows.
- **Added**: Calendar output schema requirements (Section 12) for the compile-time heatmap rendered
  by `compile_schedule_calendar.go`.

---

## 13. Norms

This section collects normative guarantees that apply to all conforming scattering implementations
and are not covered by the algorithm or safeguard sections above. Requirements here use [RFC 2119]
key words with the same interpretation as §2.2.

### 13.1 Determinism Guarantee

**N-DET-001**: For a given workflow identifier and schedule expression, the scattered cron
expression MUST be identical on every recompilation, regardless of runtime, host OS, Go version
(within the same major version), or whether the workflow has been previously compiled. Determinism
is guaranteed by using a hash of the workflow identifier as the sole source of randomness; no
wall-clock time, process ID, or external entropy MUST be introduced into the scattering step.

**N-DET-002**: The determinism guarantee extends across recompilations that occur after unrelated
frontmatter changes. If only non-schedule fields change (e.g., `description`), the scattered
minute and hour values MUST remain unchanged for all schedule entries that were not modified.

### 13.2 Operator-Visible Error Codes

The following normative error codes MUST be surfaced to operators when the corresponding failure
condition is detected. Implementations MUST use these exact codes in error messages or structured
error objects so that operators and CI pipelines can identify the failure type unambiguously.

| Error code | Trigger condition | Normative requirement |
|---|---|---|
| `R-SAFE-003` | Hash input material is empty (e.g., missing workflow identifier) | §8 R-SAFE-003: MUST fail with a descriptive error; MUST NOT fall back to random scattering |
| `R-SAFE-005` | Non-unique hash input causes repeated collision across workflows | §8 R-SAFE-005: MUST log a diagnostic; MAY apply deterministic offset to resolve |

Operator-visible error messages for R-SAFE-003 MUST include the phrase "missing workflow
identifier" or equivalent so the root cause is immediately actionable.

### 13.3 Zero-Width Scatter Window

**N-ZERO-001**: When the declared scatter window resolves to a zero-width interval (e.g., `between
14:30 and 14:30`), the implementation MUST use the single boundary minute as the fixed output
without error. The zero-width window is treated as a deterministic fixed-time schedule; no
jitter is applied and no warning is required.

**N-ZERO-002**: A zero-width `around` schedule (i.e., `around HH:MM` with an effective ±0 window
due to clamping) MUST be treated identically to N-ZERO-001 and MUST NOT produce a different output
on successive compilations.

### 13.4 Unsupported Period-Type Rejection

**R-SAFE-004**: Implementations MUST reject unsupported period types such as `monthly` and `yearly`
at parse time. The parser MUST return a deterministic validation error and MUST NOT silently coerce
unsupported period types into supported schedules.

---

## References

### Normative References

- **[RFC 2119]** S. Bradner. "Key words for use in RFCs to Indicate Requirement Levels". RFC 2119, March 1997. [https://www.ietf.org/rfc/rfc2119.txt](https://www.ietf.org/rfc/rfc2119.txt)

- **[ABNF]** D. Crocker, P. Overell. "Augmented BNF for Syntax Specifications: ABNF". RFC 5234, January 2008. [https://tools.ietf.org/html/rfc5234](https://tools.ietf.org/html/rfc5234)

### Informative References

- **[FNV]** G. Fowler, L. C. Noll, K.-P. Vo. "FNV Hash". [http://www.isthe.com/chongo/tech/comp/fnv/](http://www.isthe.com/chongo/tech/comp/fnv/)

- **[GitHub Actions Cron]** GitHub Documentation. "Events that trigger workflows - schedule". [https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#schedule](https://docs.github.com/en/actions/using-workflows/events-that-trigger-workflows#schedule)

- **[ISO 8601]** International Organization for Standardization. "Data elements and interchange formats – Information interchange – Representation of dates and times". ISO 8601:2004.

---

## Change Log

### Version 1.2.0 (Draft)

- **Changed**: Section 6.3.1 — Replaced flat hash-modulo-1440 daily scatter with a **622-entry weighted daily time slot pool** (BEST 02–05 UTC ×3, GOOD 10–12 UTC ×2, OK 19–23 UTC ×1)
- **Changed**: Sections 6.3.5–6.3.6 — Weekly, bi-weekly, and tri-weekly scatter now uses the same weighted pool as the daily schedule
- **Added**: Section 6.4 — **Peak Minutes Avoidance** documenting:
  - `avoidHourBoundary`: shifts minutes [0,4]→[5,9] and [55,59]→[50,54]
  - `avoidPeakMinutes`: EU peak (hours 06–09) avoids ±3 min of :30 (shifts [27,33]→34); US business hours (14–18) avoids ±3 min of :15 (shifts [12,18]→19) and ±3 min of :45 (shifts [42,48]→49)
- **Renumbered**: Section 6.4 (Algorithm Requirements) → Section 6.5
- **Added**: Compliance tests T-SCATTER-011 through T-SCATTER-016 covering weighted pool behavior and peak avoidance
- **Updated**: Compliance checklist (Section 9.3) with new required rows for weighted pool and peak avoidance
- **Added**: R-HASH-001 minor-version hash-stability requirement and DST transition behavior guidance in Section 5.3
- **Added**: Section 9.6 retry/backoff norms for collision/contention error handling

### Version 1.1.0 (Draft)

- **Changed**: Hash function requirement relaxed from MUST to SHOULD for FNV-1a
- **Added**: General hash function requirements (determinism, distribution, stability, integer output)
- **Added**: Support for alternative hash functions (MurmurHash, xxHash, CityHash)
- **Changed**: Moved FNV reference from normative to informative references

### Version 1.0.0 (Draft)

- Initial specification release
- Defined core fuzzy schedule syntax grammar
- Specified scattering algorithm using FNV-1a hash
- Added timezone conversion support
- Defined three conformance levels (Basic, Standard, Complete)
- Included comprehensive test suite with 50+ test cases
- Added examples for all schedule types
- Defined error codes and handling requirements

---

*Copyright © 2024 GitHub. All rights reserved.*
