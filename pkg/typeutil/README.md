# typeutil Package

> General-purpose type conversion utilities for heterogeneous `any` values, particularly those arising from JSON and YAML parsing.

## Overview

JSON and YAML parsers produce `any` values whose concrete type varies at runtime (`int`, `float64`, `string`, etc.). This package provides safe, well-documented conversion functions that handle the common cases without requiring callers to write their own type switches.

Functions are grouped into four categories: **strict conversions** (return a `(value, ok)` pair to distinguish zero from missing/invalid), **safe overflow conversions** (clamp to zero on overflow instead of panicking), **lenient conversions** (also handle string inputs, returning zero on failure), and **map-extraction helpers** (pull typed values from `map[string]any` by key or path). A separate group handles token-limit strings with optional `K`/`M` multiplier suffixes.

Choose the right category based on the source: use strict conversions for YAML config fields where the YAML library has already typed the value; use lenient conversions for heterogeneous sources such as JSON metrics or log-parsed data where a zero default on failure is acceptable; use map-extraction helpers when working with JSON/YAML-decoded `map[string]any` structures.

**File layout**: numeric conversion functions live in `convert.go`; map-extraction helpers (`ParseBool`, `LookupMap`, `LookupString`, `LookupStringPath`) live in `lookup.go`.

## Public API

### Exported Functions

| Function | Description |
|----------|-------------|
| `ParseIntValue` | Strict numeric parsing to `int` with `(value, ok)` result |
| `ParseBool` | Boolean extraction from `map[string]any` |
| `ParseInt64KMSuffix` | Parse a positive integer string with optional `K`/`M` suffix to `int64` |
| `NormalizeInt64KMSuffix` | Normalize a positive integer string with optional `K`/`M` suffix to a canonical base-10 string |
| `SafeUint64ToInt` | Overflow-safe conversion from `uint64` to `int` |
| `SafeUintToInt` | Overflow-safe conversion from `uint` to `int` |
| `ConvertToInt` | Lenient conversion of mixed inputs to `int` |
| `ConvertToFloat` | Lenient conversion of mixed inputs to `float64` |
| `LookupMap` | Safe map extraction from `map[string]any` by key |
| `LookupString` | Safe string extraction from `map[string]any` by key |
| `LookupStringPath` | Safe nested string extraction by key path |
| `SafeAllocationCapacity` | Overflow-safe sum of capacity hints for slice/map preallocation |

### Strict Conversions

#### `ParseIntValue(value any) (int, bool)`

Strictly parses numeric types (`int`, `int64`, `uint64`, `float64`) to `int`. Returns `(value, true)` on success and `(0, false)` for any unrecognized or non-numeric type.

Use this when the caller **must distinguish** a missing or invalid value from a legitimate zero (e.g. YAML config field parsing where the YAML library has already produced a typed numeric value).

```go
v, ok := typeutil.ParseIntValue(someYAMLField)
if !ok {
    return errors.New("field is missing or not an integer")
}
```

### K/M Suffix Parsing

#### `ParseInt64KMSuffix(raw string) (int64, bool)`

Parses a positive base-10 integer string with an optional `K`/`k` (×1,000) or `M`/`m` (×1,000,000) suffix. Returns `(value, true)` on success and `(0, false)` for empty input, non-positive values, non-numeric strings, or overflow.

```go
v, ok := typeutil.ParseInt64KMSuffix("128K")
// v == 128000, ok == true

v, ok = typeutil.ParseInt64KMSuffix("2M")
// v == 2000000, ok == true

v, ok = typeutil.ParseInt64KMSuffix("512")
// v == 512, ok == true

_, ok = typeutil.ParseInt64KMSuffix("0")
// ok == false  (must be positive)
```

#### `NormalizeInt64KMSuffix(raw string) (string, bool)`

Returns a canonical base-10 string for a positive integer string with an optional `K`/`k` or `M`/`m` suffix. Delegates to `ParseInt64KMSuffix` and formats the result with `strconv.FormatInt`.

```go
s, ok := typeutil.NormalizeInt64KMSuffix("128K")
// s == "128000", ok == true

s, ok = typeutil.NormalizeInt64KMSuffix("2m")
// s == "2000000", ok == true
```

### Safe Overflow Conversions

#### `SafeUint64ToInt(u uint64) int`

Converts `uint64` to `int`, returning `0` if the value would overflow `int`.

#### `SafeUintToInt(u uint) int`

Converts `uint` to `int`, returning `0` if the value would overflow `int`. Thin wrapper around `SafeUint64ToInt`.

### Lenient Conversions

#### `ConvertToInt(val any) int`

Leniently converts any value to `int`, returning `0` on failure. Unlike `ParseIntValue`, this function also handles string inputs via `strconv.Atoi`, making it suitable for heterogeneous sources such as JSON metrics, log-parsed data, or user-provided configuration where a zero default on failure is acceptable.

```go
// Works with int, int64, float64, and string inputs
count := typeutil.ConvertToInt(jsonData["count"])
```

#### `ConvertToFloat(val any) float64`

Safely converts any value (`float64`, `int`, `int64`, `string`) to `float64`, returning `0` on failure.

```go
ratio := typeutil.ConvertToFloat(jsonData["ratio"])
```

### Map Extraction

> Defined in `lookup.go`.

#### `ParseBool(m map[string]any, key string) bool`

Extracts a boolean value from a `map[string]any` by key. Returns `false` if the map is `nil`, the key is absent, or the value is not a `bool`.

```go
enabled := typeutil.ParseBool(config, "enabled")
```

#### `LookupMap(m map[string]any, key string) (map[string]any, bool)`

Extracts a `map[string]any` value from `m` by key. Returns `(nil, false)` if the map is `nil`, the key is absent, or the value is not a `map[string]any`.

#### `LookupString(m map[string]any, key string) (string, bool)`

Extracts a `string` value from `m` by key. Returns `("", false)` if the map is `nil`, the key is absent, or the value is not a `string`.

#### `LookupStringPath(m map[string]any, path ...string) (string, bool)`

Extracts a nested string value by following a sequence of keys through `map[string]any` layers. Returns `("", false)` if any step in the path is missing or has an invalid type, or if the path is empty.

```go
cmd, ok := typeutil.LookupStringPath(event, "input", "command")
```

### Allocation Sizing

#### `SafeAllocationCapacity(parts ...int) int`

Returns the summed capacity hint from `parts` when the total fits in `int`. When the sum would overflow, or any part is negative, it returns `0` so callers can safely skip preallocation (e.g. `make([]T, 0, n)`) rather than change correctness or panic. Defined in `allocation.go`; intentionally side-effect free so it does not pull in a logging dependency.

```go
n := typeutil.SafeAllocationCapacity(len(a), len(b))
result := make([]Item, 0, n)
```

## Choosing the Right Function

| Situation | Function to use |
|-----------|----------------|
| YAML/Go-typed numeric field; must detect missing vs zero | `ParseIntValue` |
| JSON / log-parsed metric; zero default on failure is fine | `ConvertToInt` |
| Boolean flag in a `map[string]any` | `ParseBool` |
| Nested `map[string]any` value by key | `LookupMap` |
| String value from `map[string]any` by key | `LookupString` |
| String value from nested `map[string]any` by key path | `LookupStringPath` |
| Casting `uint64` counter to `int` | `SafeUint64ToInt` |
| Numeric value from any source as float | `ConvertToFloat` |
| Token/limit string with optional `K`/`M` suffix | `ParseInt64KMSuffix` |
| Canonicalize a `K`/`M`-suffixed string to base-10 | `NormalizeInt64KMSuffix` |
| Summing capacity hints for slice/map preallocation | `SafeAllocationCapacity` |

## Usage Examples

```go
import "github.com/github/gh-aw/pkg/typeutil"

// Parse a YAML integer value
v, ok := typeutil.ParseIntValue(someYAMLField)
if !ok {
    return errors.New("field is missing or not an integer")
}

// Parse a boolean from a map
enabled := typeutil.ParseBool(config, "enabled")

// Convert any value to int (lenient, zero on failure)
count := typeutil.ConvertToInt(jsonData["count"])

// Safe uint64 to int conversion
n := typeutil.SafeUint64ToInt(uint64Value)

// Parse an AI Credit limit string with optional K/M suffix
limit, ok := typeutil.ParseInt64KMSuffix("128K")
if !ok {
    return errors.New("invalid token limit value")
}

// Safely size a preallocated slice from multiple length hints
cap := typeutil.SafeAllocationCapacity(len(existing), len(incoming))
merged := make([]Item, 0, cap)
```

## Dependencies

**Internal**:
- `github.com/github/gh-aw/pkg/logger` — debug logging

## Design Notes

- Numeric conversion debug output uses `logger.New("typeutil:convert")` and is only emitted when `DEBUG=typeutil:*`.
- `float64 → int` truncation is logged at debug level when the fractional part is lost.
- `uint64 → int` overflow returns `0` rather than panicking, following the defensive convention used elsewhere in the codebase.
- Map-extraction helpers (`ParseBool`, `LookupMap`, `LookupString`, `LookupStringPath`) are defined in `lookup.go` to keep `convert.go` focused on single-value numeric conversion.
- `SafeAllocationCapacity` (`allocation.go`) returns `0` on overflow or negative inputs rather than panicking, matching the defensive-zero convention used by the other overflow-safe conversions in this package.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
