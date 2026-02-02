---
title: Fuzzy Schedule Time Syntax Specification
description: Formal specification for the fuzzy schedule time syntax following W3C conventions
sidebar:
  order: 1360
---

# Fuzzy Schedule Time Syntax Specification

**Version**: 1.1.0  
**Status**: Draft Specification  
**Latest Version**: [fuzzy-schedule-specification](/gh-aw/reference/fuzzy-schedule-specification/)  
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
8. [Error Handling](#8-error-handling)
9. [Compliance Testing](#9-compliance-testing)

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

```abnf
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
```

Where:
- `<N>` MUST be a positive integer
- `<unit>` MUST be one of: `minutes`, `minute`, `m`, `hours`, `hour`, `h`, `days`, `day`, `d`, `weeks`, `week`, `w`

An implementation MUST generate appropriate cron expressions based on the unit:

- Minutes: `*/N * * * *` (minimum N=5 per GitHub Actions constraint)
- Hours: `FUZZY:HOURLY:N * * *` (scattered minute)
- Days: `0 0 */N * *` (fixed midnight)
- Weeks: `0 0 */N*7 * *` (fixed Sunday midnight)

**Example**:
```yaml
every 5 minutes
# Generates: */5 * * * *

every 6h
# Generates: FUZZY:HOURLY:6 * * *

every 2 days
# Generates: 0 0 */2 * *
```

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

```abnf
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

---

## 6. Scattering Algorithm

### 6.1 Algorithm Purpose

The scattering algorithm MUST provide:

1. **Determinism**: Same workflow identifier produces same scattered time
2. **Distribution**: Scattered times distribute evenly across the allowed range
3. **Stability**: Scattered times remain constant across recompilations
4. **Uniqueness**: Different workflow identifiers produce different scattered times

### 6.2 Hash Function Requirements

#### 6.2.1 Hash Algorithm Selection

An implementation MUST use a hash function that satisfies the following requirements:

1. **Determinism**: The hash function MUST produce the same output for the same input across all platforms and executions
2. **Distribution**: The hash function SHOULD produce uniformly distributed outputs across the hash space
3. **Stability**: The hash function MUST NOT change behavior across different versions of the implementation
4. **Integer output**: The hash function MUST produce an integer output suitable for modulo operations

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

For `FUZZY:DAILY * * *`:

1. Calculate hash modulo 1440 (24 hours * 60 minutes)
2. Convert result to hour and minute components
3. Generate cron: `<minute> <hour> * * *`

**Example**:
```
hash("github/gh-aw/workflow.md") % 1440 = 343
hour = 343 / 60 = 5
minute = 343 % 60 = 43
cron = "43 5 * * *"  (5:43 AM)
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

For `FUZZY:WEEKLY * * *`:

1. Calculate hash modulo (7 * 24 * 60) = 10080 (week in minutes)
2. Extract day-of-week: `day = (hash_result / 1440) % 7`
3. Extract time: `time_in_minutes = hash_result % 1440`
4. Convert time to hour and minute
5. Generate cron: `<minute> <hour> * * <day>`

For `FUZZY:WEEKLY:DOW * * DOW`:

1. Day is fixed from expression
2. Calculate hash modulo 1440 (day in minutes)
3. Convert to hour and minute
4. Generate cron: `<minute> <hour> * * <day>`

**Example**:
```
weekly on monday
day = 1 (Monday)
hash % 1440 = 343
hour = 5, minute = 43
cron = "43 5 * * 1"  (Monday 5:43 AM)
```

#### 6.3.6 Bi-weekly and Tri-weekly Scattering

For `FUZZY:BI-WEEKLY * * *`:

1. Calculate hash modulo 1440
2. Convert to hour and minute
3. Generate cron: `<minute> <hour> */14 * *`

For `FUZZY:TRI-WEEKLY * * *`:

1. Calculate hash modulo 1440
2. Convert to hour and minute
3. Generate cron: `<minute> <hour> */21 * *`

### 6.4 Algorithm Requirements

An implementation MUST ensure:

1. Hash function produces same output for same input across platforms
2. Modulo operations use consistent integer division
3. Day wrapping uses consistent addition/subtraction rules
4. Minute and hour extraction uses consistent division and modulo operations

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

## 8. Error Handling

### 8.1 Syntax Errors

An implementation MUST reject invalid expressions with clear error messages:

#### 8.1.1 Invalid Schedule Type

```
Error: Unknown schedule type 'monthly'
Valid types: daily, weekly, hourly, bi-weekly, tri-weekly, every
```

#### 8.1.2 Invalid Time Format

```
Error: Invalid time format '25:00' in 'daily around 25:00'
Time must be in 24-hour format (HH:MM, 0-23 hours) or 12-hour format with am/pm
```

#### 8.1.3 Invalid Weekday

```
Error: Unknown weekday 'mondey' in 'weekly on mondey'
Valid weekdays: sunday, monday, tuesday, wednesday, thursday, friday, saturday
```

#### 8.1.4 Invalid Interval

```
Error: Invalid interval '5' in 'every 5h'
Valid hour intervals: 1h, 2h, 3h, 4h, 6h, 8h, 12h
```

### 8.2 Semantic Errors

#### 8.2.1 Missing Required Components

```
Error: 'around' requires a time specification
Example: daily around 14:00
```

#### 8.2.2 Unsupported Syntax

```
Error: 'daily at <time>' syntax is not supported
Use 'daily around <time>' for fuzzy scheduling within ±1 hour window
```

### 8.3 Warning Messages

An implementation SHOULD issue warnings for valid but suboptimal patterns:

```
Warning: Consider using 'every 2h' instead of fixed interval
Fixed intervals create load spikes when many workflows run simultaneously
```

### 8.4 Error Recovery

An implementation SHOULD NOT attempt to correct syntax errors automatically. All errors MUST be reported to the user with actionable correction guidance.

---

## 9. Compliance Testing

### 9.1 Test Suite Requirements

A conforming implementation MUST pass all Level 1 tests. Implementations claiming Level 2 or Level 3 conformance MUST pass all tests for their claimed level and all lower levels.

### 9.2 Test Categories

#### 9.2.1 Syntax Parsing Tests (Level 1)

- **T-SYNTAX-001**: Parse `daily` to `FUZZY:DAILY * * *`
- **T-SYNTAX-002**: Parse `weekly` to `FUZZY:WEEKLY * * *`
- **T-SYNTAX-003**: Parse `weekly on monday` to `FUZZY:WEEKLY:1 * * 1`
- **T-SYNTAX-004**: Parse all weekday names correctly
- **T-SYNTAX-005**: Reject invalid schedule types
- **T-SYNTAX-006**: Reject invalid weekday names
- **T-SYNTAX-007**: Parse case-insensitive tokens

#### 9.2.2 Time Format Tests (Level 2)

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

#### 9.2.3 Time Constraint Tests (Level 2)

- **T-CONSTRAINT-001**: Parse `daily around 14:00`
- **T-CONSTRAINT-002**: Parse `daily between 9:00 and 17:00`
- **T-CONSTRAINT-003**: Parse `weekly on friday around 17:00`
- **T-CONSTRAINT-004**: Handle midnight-crossing ranges (`22:00 and 02:00`)
- **T-CONSTRAINT-005**: Reject `around` without time specification
- **T-CONSTRAINT-006**: Reject `between` with only one time
- **T-CONSTRAINT-007**: Reject `daily at <time>` syntax

#### 9.2.4 Timezone Tests (Level 3)

- **T-TZ-001**: Parse `utc+9` offset
- **T-TZ-002**: Parse `utc-5` offset
- **T-TZ-003**: Parse `utc+05:30` offset format
- **T-TZ-004**: Convert `14:00 utc+9` to `05:00` UTC
- **T-TZ-005**: Convert `3pm utc-5` to `20:00` UTC
- **T-TZ-006**: Handle negative UTC conversion (wrap to previous day)
- **T-TZ-007**: Handle >24:00 UTC conversion (wrap to next day)
- **T-TZ-008**: Reject invalid offsets (e.g., `utc+25`)

#### 9.2.5 Hourly and Interval Tests (Level 2/3)

- **T-HOURLY-001**: Parse `hourly` to `FUZZY:HOURLY * * *`
- **T-HOURLY-002**: Parse `every 2h` to `FUZZY:HOURLY:2 * * *`
- **T-HOURLY-003**: Parse `every 6 hours` to `FUZZY:HOURLY:6 * * *`
- **T-INTERVAL-001**: Parse `every 5 minutes` to `*/5 * * * *`
- **T-INTERVAL-002**: Parse `every 2 days` to `0 0 */2 * *`
- **T-INTERVAL-003**: Reject `every 3 minutes` (below 5-minute minimum)
- **T-INTERVAL-004**: Parse `bi-weekly` to `FUZZY:BI-WEEKLY * * *`
- **T-INTERVAL-005**: Parse `tri-weekly` to `FUZZY:TRI-WEEKLY * * *`

#### 9.2.6 Scattering Algorithm Tests (Level 1-3)

- **T-SCATTER-001**: Hash produces same output for same input
- **T-SCATTER-002**: Different inputs produce different outputs
- **T-SCATTER-003**: Hash value is within modulo range (0 to modulo-1)
- **T-SCATTER-004**: Daily schedule scatters across full 24-hour period
- **T-SCATTER-005**: Around schedule stays within ±60 minute window
- **T-SCATTER-006**: Between schedule stays within specified range
- **T-SCATTER-007**: Midnight-crossing range handles day wrap correctly
- **T-SCATTER-008**: Hourly schedule produces minute 0-59
- **T-SCATTER-009**: Weekly schedule selects valid day 0-6
- **T-SCATTER-010**: Same workflow gets same time across compilations

#### 9.2.7 Cron Generation Tests (Level 1-3)

- **T-CRON-001**: Generated cron has exactly 5 fields
- **T-CRON-002**: Minute field is in range 0-59
- **T-CRON-003**: Hour field is in range 0-23
- **T-CRON-004**: Day-of-week field is in range 0-6 or `*`
- **T-CRON-005**: Month and day-of-month are valid
- **T-CRON-006**: Interval expressions use valid `*/N` syntax

### 9.3 Compliance Checklist

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
| Parse hourly schedules | T-HOURLY-001, 002, 003 | 2 | Required |
| Parse interval schedules | T-INTERVAL-001, 002 | 3 | Required |
| Hash determinism | T-SCATTER-001, 002 | 1 | Required |
| Scattering distribution | T-SCATTER-004-009 | 1-3 | Required |
| Generate valid cron | T-CRON-001-006 | 1-3 | Required |

### 9.4 Test Execution

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
