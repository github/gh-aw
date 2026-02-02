# Logger Package

A simple, debug-style logging framework for Go that follows the pattern matching syntax of the [debug npm package](https://www.npmjs.com/package/debug).

## Features

- **Namespace-based logging**: Each logger has a namespace (e.g., `workflow:compiler`, `cli:audit`)
- **Pattern matching**: Enable/disable loggers using wildcards and exclusions via the `DEBUG` environment variable
- **Printf interface**: Standard printf-style formatting
- **Time diff display**: Shows time elapsed since last log call (like debug npm package)
- **Automatic color coding**: Each namespace gets a unique color when stderr is a TTY
- **Zero overhead**: Logger enabled state is computed once at construction time
- **Thread-safe**: Safe for concurrent use

## Usage

### Basic Usage

```go
package main

import "github.com/github/gh-aw/pkg/logger"

var log = logger.New("myapp:feature")

func main() {
    log.Printf("Starting application with config: %s", config)
    log.Print("Multiple", " ", "arguments")
}
```

Output shows namespace, message, and time diff:
```
myapp:feature Starting application with config: production +0ns
myapp:feature Multiple arguments +125ms
```

### Avoiding Expensive Operations

Check if a logger is enabled before performing expensive operations:

```go
if log.Enabled() {
    // Do expensive work only if logging is enabled
    result := expensiveOperation()
    log.Printf("Result: %v", result)
}
```

### Time Diff Display

Like the debug npm package, each log shows the time elapsed since the last log call:

```go
log.Printf("Starting task")
// ... do some work ...
log.Printf("Task completed")  // Shows +2.5s (or +500ms, +100µs, etc.)
```

## DEBUG Environment Variable

Control which loggers are enabled using the `DEBUG` environment variable with patterns:

### Examples

```bash
# Enable all loggers
DEBUG=*

# Enable all loggers in the 'workflow' namespace
DEBUG=workflow:*

# Enable specific loggers
DEBUG=workflow:compiler,cli:audit

# Enable all except specific loggers
DEBUG=*,-workflow:compiler

# Enable namespace but exclude specific patterns
DEBUG=workflow:*,-workflow:compiler:cache

# Multiple patterns with exclusions
DEBUG=workflow:*,cli:*,-workflow:test
```

## Color Support

Colors are automatically assigned to each namespace when:
- Stderr is a TTY (terminal)
- `DEBUG_COLORS` is not set to `0`

Each namespace gets a consistent color based on a hash of its name. This makes it easy to visually distinguish between different loggers.

### Disabling Colors

```bash
# Disable colors
DEBUG_COLORS=0 DEBUG=* gh aw compile workflow.md

# Colors are automatically disabled when piping output
DEBUG=* gh aw compile workflow.md 2>&1 | tee output.log
```

### Pattern Syntax

- `*` - Matches all loggers
- `namespace:*` - Matches all loggers with the given prefix
- `*:suffix` - Matches all loggers with the given suffix
- `prefix:*:suffix` - Matches loggers with both prefix and suffix
- `-pattern` - Excludes loggers matching the pattern (takes precedence)
- `pattern1,pattern2` - Multiple patterns separated by commas

## Design Decisions

### Logger Enabled State

The enabled state is computed **once at logger construction time** based on the `DEBUG` environment variable. This means:

- Zero overhead for disabled loggers (simple boolean check)
- `DEBUG` changes after the process starts won't affect existing loggers

### Time Diff Tracking

Each logger tracks the time of its last log call to display elapsed time, similar to the debug npm package. This helps identify performance bottlenecks and understand timing relationships between log messages.

### Output Destination

All log output goes to **stderr** to avoid interfering with stdout data (JSON, command output, etc.).

### Printf Interface

The logger provides a familiar printf-style interface that Go developers expect:

- `Printf(format, args...)` - Formatted output (always adds newline)
- `Print(args...)` - Simple concatenation (always adds newline)

## Example Patterns

### File-based Namespaces

```go
// In pkg/workflow/compiler.go
var log = logger.New("workflow:compiler")

// In pkg/cli/audit.go  
var log = logger.New("cli:audit")

// In pkg/parser/frontmatter.go
var log = logger.New("parser:frontmatter")
```

Enable with:
```bash
DEBUG=workflow:* go run main.go      # Only workflow package
DEBUG=cli:*,parser:* go run main.go  # CLI and parser packages
DEBUG=* go run main.go                # Everything
```

### Feature-based Namespaces

```go
var compileLog = logger.New("compile")
var parseLog = logger.New("parse")
var validateLog = logger.New("validate")
```

## Implementation Notes

- The `DEBUG` environment variable is read once when the package is initialized
- Thread-safe using `sync.Mutex` for time tracking
- Simple pattern matching without regex (prefix, suffix, and middle wildcards only)
- Exclusion patterns (prefixed with `-`) take precedence over inclusion patterns
- Time diff formatted like debug npm package (ns, µs, ms, s, m, h)
- Colors assigned using FNV-1a hash for consistent namespace-to-color mapping
- Color palette chosen for readability on both light and dark terminals
- Uses ANSI 256-color codes for better compatibility
