# Console Rendering Package

The `console` package provides utilities for rendering Go structs and data structures to formatted console output, as well as progress bar and spinner components for long-running operations.

## Design Philosophy

This package follows Charmbracelet best practices for terminal UI:

- **Adaptive Colors**: All styling uses `lipgloss.AdaptiveColor` for light/dark theme support
- **Rounded Borders**: Tables and boxes use rounded corners (╭╮╰╯) for a polished appearance
- **Consistent Padding**: All rendered elements include proper spacing (horizontal and vertical)
- **TTY Detection**: Automatically adapts output for terminals vs pipes/redirects
- **Visual Hierarchy**: Clear separation between sections using borders and spacing
- **Zebra Striping**: Tables use alternating row colors for improved readability

### Border Usage Guidelines

- **RoundedBorder** (primary): Use for tables, boxes, and panels
  - Creates a polished, modern appearance
  - Consistent with Charmbracelet design language
- **NormalBorder** (subtle): Use for left-side emphasis on info sections
  - Provides gentle visual guidance without overwhelming
- **ThickBorder** (reserved): Available for special cases requiring extra emphasis
  - Use sparingly - rounded borders with bold text usually suffice

### Padding Guidelines

- **Table cells**: 1 character horizontal padding (left/right)
- **Boxes**: 2 character horizontal padding, 0-1 vertical padding
- **Info sections**: 2 character left padding for consistent indentation

## Spinner Component

The `Spinner` component provides animated visual feedback during long-running operations with automatic TTY detection and accessibility support.

### Features

- **MiniDot animation**: Minimal dot spinner (⣾ ⣽ ⣻ ⢿ ⡿ ⣟ ⣯ ⣷)
- **TTY detection**: Automatically disabled in pipes/redirects
- **Accessibility support**: Respects `ACCESSIBLE` environment variable
- **Color adaptation**: Uses adaptive colors for light/dark themes
- **Idiomatic Bubble Tea**: Uses `tea.NewProgram()` with proper message passing
- **Thread-safe**: Safe for concurrent use via Bubble Tea's message handling

### Usage

```go
import "github.com/github/gh-aw/pkg/console"

// Create and use a spinner
spinner := console.NewSpinner("Loading...")
spinner.Start()
// Long-running operation
spinner.Stop()

// Stop with a message
spinner := console.NewSpinner("Processing...")
spinner.Start()
// Long-running operation
spinner.StopWithMessage("✓ Done!")

// Update message while running
spinner := console.NewSpinner("Starting...")
spinner.Start()
spinner.UpdateMessage("Still working...")
// Long-running operation
spinner.Stop()
```

### Accessibility

The spinner respects the `ACCESSIBLE` environment variable. When set to any value, spinner animations are disabled to support screen readers and accessibility tools:

```bash
export ACCESSIBLE=1
gh aw compile workflow.md  # Spinners will be disabled
```

### TTY Detection

Spinners only animate in terminal environments. When output is piped or redirected, the spinner is automatically disabled:

```bash
gh aw compile workflow.md           # Spinner animates
gh aw compile workflow.md > log.txt # Spinner disabled
```

## ProgressBar Component

The `ProgressBar` component provides a reusable progress bar with TTY detection and graceful fallback for non-TTY environments.

### Features

- **Scaled gradient effect**: Smooth color transition from purple to cyan as progress advances
- **TTY detection**: Automatically adapts to terminal environment
- **Byte formatting**: Converts byte counts to human-readable sizes (KB, MB, GB)
- **Thread-safe updates**: Safe for concurrent use with atomic operations

### Visual Styling

The progress bar uses bubbles v0.21.0+ gradient capabilities for enhanced visual appeal:
- **Start (0%)**: #BD93F9 (purple) - vibrant, attention-grabbing
- **End (100%)**: #8BE9FD (cyan) - cool, completion feeling
- **Empty portion**: #6272A4 (muted purple-gray)
- **Gradient scaling**: WithScaledGradient ensures gradient scales with filled portion

### Usage

#### Determinate Mode (known total)
Use when the total size or count is known:

```go
import "github.com/github/gh-aw/pkg/console"

// Create a progress bar for 1GB total
totalBytes := int64(1024 * 1024 * 1024)
bar := console.NewProgressBar(totalBytes)

// Update progress (returns formatted string)
output := bar.Update(currentBytes)
fmt.Fprintf(os.Stderr, "\r%s", output)
```

#### Indeterminate Mode (unknown total)
Use when the total size or count is unknown:

```go
import "github.com/github/gh-aw/pkg/console"

// Create an indeterminate progress bar
bar := console.NewIndeterminateProgressBar()

// Update with current progress (shows activity without percentage)
output := bar.Update(currentBytes)
fmt.Fprintf(os.Stderr, "\r%s", output)
```

### Output Examples

**Determinate Mode - TTY**:
```
████████████████████░░░░░░░░░░░░░░░░░  50%
```
*(Displays with gradient from purple to cyan)*

**Determinate Mode - Non-TTY**:
```
50% (512.0MB/1.00GB)
```

**Indeterminate Mode - TTY**:
```
████████████████░░░░░░░░░░░░░░░░░░░░  (pulsing animation)
```
*(Shows pulsing progress indicator)*

**Indeterminate Mode - Non-TTY**:
```
Processing... (512.0MB)
```


## RenderStruct Function

The `RenderStruct` function uses reflection to automatically render Go structs based on struct tags.

### Struct Tags

Use the `console` struct tag to control rendering behavior:

#### Available Tags

- **`header:"Column Name"`** - Sets the display name for the field (used in both structs and tables)
- **`title:"Section Title"`** - Sets the title for nested structs, slices, or maps
- **`omitempty`** - Skips the field if it has a zero value
- **`"-"`** - Always skips the field

#### Tag Examples

```go
type Overview struct {
    RunID      int64  `console:"header:Run ID"`
    Workflow   string `console:"header:Workflow"`
    Status     string `console:"header:Status"`
    Duration   string `console:"header:Duration,omitempty"`
    Internal   string `console:"-"` // Never displayed
}
```

### Rendering Behavior

#### Structs
Structs are rendered as key-value pairs with proper alignment:

```
  Run ID    : 12345
  Workflow  : my-workflow
  Status    : completed
  Duration  : 5m30s
```

#### Slices
Slices of structs are automatically rendered as tables using the console table renderer:

```go
type Job struct {
    Name       string `console:"header:Name"`
    Status     string `console:"header:Status"`
    Conclusion string `console:"header:Conclusion,omitempty"`
}

jobs := []Job{
    {Name: "build", Status: "completed", Conclusion: "success"},
    {Name: "test", Status: "in_progress", Conclusion: ""},
}

fmt.Print(console.RenderStruct(jobs))
```

Renders as:

```
Name  | Status      | Conclusion
----- | ----------- | ----------
build | completed   | success
test  | in_progress | -
```

#### Maps
Maps are rendered as markdown-style headers with key-value pairs:

```go
data := map[string]string{
    "Repository": "github/gh-aw",
    "Author":     "test-user",
}

fmt.Print(console.RenderStruct(data))
```

Renders as:

```
  Repository: github/gh-aw
  Author    : test-user
```

### Special Type Handling

#### time.Time
`time.Time` fields are automatically formatted as `"2006-01-02 15:04:05"`. Zero time values are considered empty when used with `omitempty`.

#### Unexported Fields
The rendering system safely handles unexported struct fields by checking `CanInterface()` before attempting to access field values.

### Usage in Audit Command

The audit command uses the new rendering system for structured output:

```go
// Render overview section
renderOverview(data.Overview)

// Render metrics with custom formatting
renderMetrics(data.Metrics)

// Render jobs as a table
renderJobsTable(data.Jobs)
```

This provides:
- Consistent formatting across all audit sections
- Automatic table generation for slice data
- Proper handling of optional/empty fields
- Type-safe reflection-based rendering

### Migration Guide

To migrate existing rendering code to use the new system:

1. **Add struct tags** to your data types:
   ```go
   type MyData struct {
       Field1 string `console:"header:Field 1"`
       Field2 int    `console:"header:Field 2,omitempty"`
   }
   ```

2. **Use RenderStruct** for simple structs:
   ```go
   fmt.Print(console.RenderStruct(myData))
   ```

3. **Use custom rendering** for special formatting needs:
   ```go
   func renderMyData(data MyData) {
       fmt.Printf("  %-15s %s\n", "Field 1:", formatCustom(data.Field1))
       // ... custom formatting logic
   }
   ```

4. **Use console.RenderTable** for tables with custom formatting:
   ```go
   config := console.TableConfig{
       Headers: []string{"Name", "Value"},
       Rows: [][]string{
           {truncateString(item.Name, 40), formatNumber(item.Value)},
       },
   }
   fmt.Print(console.RenderTable(config))
   ```
