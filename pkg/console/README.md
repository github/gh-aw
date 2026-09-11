# Console Package

> Terminal UI formatting, rendering, and interactive console helpers for `gh-aw`.

## Overview

The `console` package defines the terminal-facing presentation layer used by `gh-aw`. It formats diagnostic messages, compiler-style errors, banners, tables, sections, and reflected struct output, and it provides convenience print helpers that route human-readable output to stderr. The package distinguishes between `Format*` functions, which produce formatted strings, and `Render*` functions, which build structured output such as tables, boxes, or composed sections.

The package is designed to adapt to the execution environment. Native builds detect TTY availability, honor accessibility-oriented environment variables, and degrade ANSI output through the color-profile writer so output respects terminal capabilities and settings such as `NO_COLOR`. For interactive workflows it also provides spinners, progress bars, confirmation dialogs, secret prompts, and themed `huh` form constructors, while WASM builds expose simpler fallback implementations or explicit “not available” errors where interactivity is unsupported.

## Public API

### Types

| Type | Kind | Description |
|------|------|-------------|
| `CompilerError` | struct | Structured diagnostic with source position, severity-like type string, message, optional source context, and optional hint text. |
| `ErrorPosition` | struct | Source location with file, line, and column fields. |
| `FormField` | struct | Declarative form field description used by WASM-only `RunForm`, including type, labels, bound value, options, and validation callback. |
| `ListItem` | struct | Interactive list item value created by `NewListItem`; its fields are intentionally unexported. |
| `ProgressBar` | struct | Progress-bar controller returned by `NewProgressBar` and `NewIndeterminateProgressBar`, with `Update` for rendering determinate or indeterminate progress. |
| `PromptForm` | struct | Wraps a `huh.Form` (embedded) so completed questions are cleared from the terminal before the caller prints the decision result; created by `NewForm`, `NewInputForm`, `NewSelectForm`, and `NewConfirmForm`. Native builds only. |
| `SelectOption` | struct | Label/value pair used by select-oriented APIs. |
| `SpinnerWrapper` | struct | Spinner controller with lifecycle methods `Start`, `Stop`, `StopWithMessage`, and `UpdateMessage`. |
| `TableConfig` | struct | Table-rendering configuration including headers, rows, optional title/total row, and optional TTY override. |
| `TreeNode` | struct | Tree node with a display value and child nodes; rendered by `RenderTree` in WASM builds. |

### Functions

| Function | Signature | Description |
|----------|-----------|-------------|
| `ClearLine` | `func ClearLine()` | Clears the current stderr terminal line when stderr is a TTY. |
| `ClearScreen` | `func ClearScreen()` | Clears the stderr terminal screen when stderr is a TTY. |
| `ConfirmAction` | `func ConfirmAction(title, affirmative, negative string) (bool, error)` | Presents a confirmation prompt; native builds use `huh`, non-TTY mode falls back to text input, and WASM reports unsupported behavior. |
| `FormatBanner` | `func FormatBanner() string` | Returns the embedded `gh-aw` ASCII banner, styled in native TTY mode and empty in WASM. |
| `FormatCommandMessage` | `func FormatCommandMessage(command string) string` | Formats a command-prefixed message (`$ ...`). |
| `FormatCommandMessageStderr` | `func FormatCommandMessageStderr(command string) string` | Formats a command-prefixed message for stderr styling. |
| `FormatCountMessage` | `func FormatCountMessage(message string) string` | Formats a count-style message (`# ...`) in WASM builds. |
| `FormatError` | `func FormatError(err CompilerError) string` | Formats a `CompilerError` including location, severity prefix, context lines, and hint text. |
| `FormatErrorChain` | `func FormatErrorChain(err error) string` | Formats an error and unwrap chain into a readable multi-line diagnostic. |
| `FormatErrorMessage` | `func FormatErrorMessage(message string) string` | Formats a simple error message with an error prefix. |
| `FormatErrorStderr` | `func FormatErrorStderr(err CompilerError) string` | Formats a `CompilerError` using stderr-aware styling rules. |
| `FormatErrorTextStderr` | `func FormatErrorTextStderr(text string) string` | Applies error styling to plain stderr text. |
| `FormatErrorWithSuggestions` | `func FormatErrorWithSuggestions(message string, suggestions []string) string` | Formats an error message followed by actionable suggestions. |
| `FormatFileSize` | `func FormatFileSize(size int64) string` | Formats byte counts into human-readable sizes. |
| `FormatInfoMessage` | `func FormatInfoMessage(message string) string` | Formats an informational message with an `i` prefix. |
| `FormatInfoMessageStderr` | `func FormatInfoMessageStderr(message string) string` | Formats an informational message for stderr styling. |
| `FormatListHeader` | `func FormatListHeader(header string) string` | Formats a list header in WASM builds. |
| `FormatListItem` | `func FormatListItem(item string) string` | Formats a bullet list item. |
| `FormatListItemStderr` | `func FormatListItemStderr(item string) string` | Formats a bullet list item for stderr styling. |
| `FormatLocationMessage` | `func FormatLocationMessage(message string) string` | Formats a location-style message (`~ ...`) in WASM builds. |
| `FormatNumber` | `func FormatNumber(n int) string` | Formats integers with grouping for display. |
| `FormatProgressMessage` | `func FormatProgressMessage(message string) string` | Formats a progress/activity message with a `▸` prefix. |
| `FormatProgressMessageStderr` | `func FormatProgressMessageStderr(message string) string` | Formats a progress/activity message for stderr styling. |
| `FormatPromptMessage` | `func FormatPromptMessage(message string) string` | Formats a prompt message with a `?` prefix. |
| `FormatSectionHeader` | `func FormatSectionHeader(header string) string` | Formats a section header. |
| `FormatSectionHeaderStderr` | `func FormatSectionHeaderStderr(header string) string` | Formats a section header for stderr styling. |
| `FormatSuccessMessage` | `func FormatSuccessMessage(message string) string` | Formats a success message with a checkmark prefix. |
| `FormatSuccessMessageStderr` | `func FormatSuccessMessageStderr(message string) string` | Formats a success message for stderr styling. |
| `FormatTableHeaderStderr` | `func FormatTableHeaderStderr(text string) string` | Formats table-header text for stderr output. |
| `FormatTokens` | `func FormatTokens(tokens int) string` | Formats token counts into readable grouped text. |
| `FormatVerboseMessage` | `func FormatVerboseMessage(message string) string` | Formats verbose output with a `»` prefix. |
| `FormatWarningMessage` | `func FormatWarningMessage(message string) string` | Formats a warning message with a warning prefix. |
| `FormatWarningMessageStderr` | `func FormatWarningMessageStderr(message string) string` | Formats a warning message for stderr styling. |
| `(*SpinnerWrapper).IsEnabled` | `func (s *SpinnerWrapper) IsEnabled() bool` | WASM-only helper that reports spinner availability; always returns `false` in WASM builds. |
| `IsAccessibleMode` | `func IsAccessibleMode() bool` | Returns whether accessibility mode should be enabled based on environment variables. |
| `IsCancelled` | `func IsCancelled(err error) bool` | Reports whether an error represents user cancellation from a `huh` form. |
| `LayoutEmphasisBox` | `func LayoutEmphasisBox(content string, color any) string` | Returns a simple emphasized block layout in WASM builds. |
| `LayoutInfoSection` | `func LayoutInfoSection(label, value string) string` | Returns a simple labeled info line in WASM builds. |
| `LayoutJoinVertical` | `func LayoutJoinVertical(sections ...string) string` | Joins multiple sections vertically in WASM builds. |
| `LayoutTitleBox` | `func LayoutTitleBox(title string, width int) string` | Returns a simple title-box layout in WASM builds. |
| `LogVerbose` | `func LogVerbose(verbose bool, message string)` | Prints a verbose message to stderr only when verbose mode is enabled. |
| `NewConfirmForm` | `func NewConfirmForm(confirm *huh.Confirm) *PromptForm` | Wraps a confirm field in a themed, accessibility-aware form that clears after completion. |
| `NewForm` | `func NewForm(groups ...*huh.Group) *PromptForm` | Creates a themed, accessibility-aware form that clears after completion. |
| `NewIndeterminateProgressBar` | `func NewIndeterminateProgressBar() *ProgressBar` | Creates an indeterminate progress bar; available in WASM builds. |
| `NewInputForm` | `func NewInputForm(input *huh.Input) *PromptForm` | Wraps an input field in a themed, accessibility-aware form that clears after completion. |
| `NewListItem` | `func NewListItem(title, description, value string) ListItem` | Constructs a `ListItem` for interactive list APIs. |
| `NewProgressBar` | `func NewProgressBar(total int64) *ProgressBar` | Creates a progress bar for a known total amount of work. |
| `NewSelectForm` | `func NewSelectForm[T comparable](selectField *huh.Select[T]) *PromptForm` | Wraps a select field in a themed, accessibility-aware form that clears after completion. |
| `NewSpinner` | `func NewSpinner(message string) *SpinnerWrapper` | Creates a spinner configured for stderr TTY and accessibility conditions. |
| `PrintBanner` | `func PrintBanner()` | Prints the banner to stderr in native builds; no-op in WASM. |
| `PrintCommandMessage` | `func PrintCommandMessage(command string)` | Prints a formatted command message to stderr. |
| `PrintErrorMessage` | `func PrintErrorMessage(message string)` | Prints a formatted error message to stderr. |
| `PrintInfoMessage` | `func PrintInfoMessage(message string)` | Prints a formatted info message to stderr. |
| `PrintSectionHeader` | `func PrintSectionHeader(header string)` | Prints a formatted section header to stderr. |
| `PrintSuccessMessage` | `func PrintSuccessMessage(message string)` | Prints a formatted success message to stderr. |
| `PrintWarningMessage` | `func PrintWarningMessage(message string)` | Prints a formatted warning message to stderr. |
| `PromptInput` | `func PromptInput(title, description, placeholder string) (string, error)` | Requests plain-text input in WASM builds, where it currently reports unsupported interactivity. |
| `PromptInputWithValidation` | `func PromptInputWithValidation(title, description, placeholder string, validate func(string) error) (string, error)` | Requests validated plain-text input in WASM builds, where it currently reports unsupported interactivity. |
| `PromptMultiSelect` | `func PromptMultiSelect(title, description string, options []SelectOption, limit int) ([]string, error)` | Requests multiple selections in WASM builds, where it currently reports unsupported interactivity. |
| `PromptSecretInput` | `func PromptSecretInput(title, description string) (string, error)` | Requests masked secret input in native TTY mode; unavailable in non-TTY and WASM environments. |
| `PromptSelect` | `func PromptSelect(title, description string, options []SelectOption) (string, error)` | Requests a single selection in WASM builds, where it currently reports unsupported interactivity. |
| `(*PromptForm).Run` | `func (f *PromptForm) Run() error` | Runs the wrapped form and clears its rendered question from the terminal when it exits (native builds only). |
| `(*PromptForm).RunWithContext` | `func (f *PromptForm) RunWithContext(ctx context.Context) error` | Runs the wrapped form with a context and clears its rendered question from the terminal when it exits (native builds only). |
| `RenderComposedSections` | `func RenderComposedSections(sections []string)` | Writes multiple rendered sections to stderr with spacing and terminal-aware composition. |
| `RenderErrorBox` | `func RenderErrorBox(title string) []string` | Renders an error-emphasis box, with TTY and plain-text variants. |
| `RenderInfoSection` | `func RenderInfoSection(content string) []string` | Renders an informational section with left-border emphasis or plain indentation. |
| `RenderStruct` | `func RenderStruct(v any) string` | Reflectively renders structs, slices, arrays, and maps into structured console output. |
| `RenderTable` | `func RenderTable(config TableConfig) string` | Renders a table from `TableConfig`, optionally including a title and total row. |
| `RenderTitleBox` | `func RenderTitleBox(title string, width int) []string` | Renders a titled box suitable for section headings. |
| `RenderTree` | `func RenderTree(root TreeNode) string` | Renders a `TreeNode` hierarchy in WASM builds. |
| `ResetTimeLocation` | `func ResetTimeLocation()` | Clears the configured `time.Time` display location override. |
| `RunForm` | `func RunForm(fields []FormField) error` | Executes declarative forms in WASM builds, where it currently reports unsupported interactivity. |
| `SetTimeLocation` | `func SetTimeLocation(location *time.Location)` | Sets the location used when rendering `time.Time` values. |
| `ShowInteractiveList` | `func ShowInteractiveList(title string, items []ListItem) (string, error)` | Shows a single-selection interactive list; native builds use `huh` and non-TTY mode falls back to numbered text input. |
| `ShowWelcomeBanner` | `func ShowWelcomeBanner(description string)` | Clears the screen and prints the interactive welcome banner and description to stderr. |
| `ToRelativePath` | `func ToRelativePath(path string) string` | Converts an absolute path to a cwd-relative display path when possible. |

### Constants

| Constant | Type | Value | Description |
|----------|------|-------|-------------|
| *(none)* | — | — | `pkg/console` exposes no exported constants in current source. |

## Usage Examples

### Formatting and printing diagnostic output

```go
fmt.Fprintln(os.Stderr, console.FormatErrorChain(err))
console.PrintSuccessMessage("Workflow compiled")
console.PrintCommandMessage("gh aw compile .github/workflows/example.md")
```

The formatting functions return strings, while the `Print*` helpers write directly to stderr.

### Rendering reflected structs and tables

```go
type Overview struct {
    Name   string `console:"header:Workflow"`
    Tokens int    `console:"header:Token Count"`
}

output := console.RenderStruct([]Overview{{
    Name:   "agentic-token-audit",
    Tokens: 1200,
}})
fmt.Print(output)
```

`RenderStruct` uses reflection and `console` struct tags such as `header`, `title`, `omitempty`, and `-` to choose headings, omit zero values, and build tables for slices of structs.

### Spinner lifecycle

```go
spinner := console.NewSpinner("Compiling workflow...")
spinner.Start()
// long-running work
spinner.StopWithMessage("✓ Workflow compiled")
```

Native spinners render on stderr only when stderr is a TTY and accessibility mode is not enabled.

### Progress bars

```go
bar := console.NewProgressBar(totalBytes)
fmt.Fprintf(os.Stderr, "\r%s", bar.Update(currentBytes))
```

On native TTYs, `Update` returns a rendered progress bar. In non-TTY mode, it returns plain text such as `50% (512MB/1GB)`.

## Design Decisions

The package preserves a strong separation between formatting and rendering. `Format*` helpers are string-producing utilities for single messages, while `Render*` helpers handle multi-line layout and composition. This convention is documented in `doc.go` and is reflected throughout the exported API.

The native implementation is terminal-aware. Styling is applied only when appropriate for the destination stream, and stdout-oriented rendering is degraded through the color-profile writer so environment variables such as `NO_COLOR`, `COLORTERM`, and `TERM` are respected. Diagnostic output is intended for stderr, while structured machine-readable output is expected to remain on stdout.

Interactive helpers deliberately degrade or refuse operation outside native TTY contexts. `ConfirmAction` and `ShowInteractiveList` provide plain-text fallbacks for non-TTY native runs, while secret input remains unavailable without a TTY. WASM-specific files provide explicit fallback implementations so exported APIs remain available across build targets even when rich interactivity is unsupported.

Spinner coordination is intentionally global in native builds: only one spinner may actively render at a time. Additional concurrent spinners become suppressed instead of competing for the same stderr line, avoiding flicker and escape-sequence corruption.

## Dependencies

Internal dependencies include `pkg/styles` for shared visual styling, `pkg/tty` for terminal detection, `pkg/colorwriter` for stream-aware ANSI degradation, `pkg/logger` for debug logging, and `pkg/stringutil` for text formatting support during reflective rendering.

External dependencies include Charmbracelet libraries: `lipgloss` and `lipgloss/table` for styling and layout, `bubbletea` and `bubbles/progress`/`bubbles/spinner` for progress and spinner components, and `huh` for interactive forms and prompts.

## Thread Safety

`SetTimeLocation` and `ResetTimeLocation` are safe for concurrent use; the package protects the configured time location with an RW mutex. Native `SpinnerWrapper` lifecycle methods use mutexes and a wait group internally, and the implementation includes global coordination so only one spinner renders at a time.

`ProgressBar.Update` mutates the progress bar instance and should be treated as operating on shared mutable state. The implementation does not expose separate synchronization for callers, so concurrent access to the same instance should be externally coordinated.

`Render*`, `Format*`, and most constructor-style helpers are otherwise side-effect free apart from reading environment state or writing to stderr/stdout through explicit print-oriented APIs.

---

*This specification is automatically maintained by the [spec-extractor](../../.github/workflows/spec-extractor.md) workflow.*
