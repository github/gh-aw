# GitHub Agentic Workflows - Styles Guide

> Visual style guide for terminal output colors and formatting
> Last updated: 2025-12-27

## Overview

This document provides a complete visual guide to the adaptive color palette and styling system used in GitHub Agentic Workflows. The system uses [lipgloss](https://github.com/charmbracelet/lipgloss) with adaptive colors that automatically adjust based on the user's terminal theme (light or dark).

**Implementation**: `pkg/styles/theme.go`

## Design Philosophy

### Adaptive Color Strategy

GitHub Agentic Workflows uses an adaptive color system that provides readability in both light and dark terminals:

**Light Mode**:
- Darker, more saturated colors for visibility on light backgrounds
- High contrast ratios for accessibility
- Muted tones to reduce visual fatigue during extended use

**Dark Mode**:
- Inspired by the [Dracula color theme](https://draculatheme.com/)
- Bright, vibrant colors optimized for dark backgrounds
- Maintains consistency with popular dark terminal themes

### Automatic Adaptation

The lipgloss library automatically detects the terminal's background and selects the appropriate color variant:
- No configuration required by end users
- Consistent experience across different terminal emulators
- Graceful fallback for terminals with limited color support

## Color Palette

### Status Colors

These colors communicate the status or severity of information.

| Color | Light Value | Dark Value | Semantic Usage | Style Name |
|-------|-------------|------------|----------------|------------|
| **Error** | `#D73737` | `#FF5555` | Error messages, critical issues, failures | `Error` |
| **Warning** | `#E67E22` | `#FFB86C` | Warning messages, cautionary information | `Warning` |
| **Success** | `#27AE60` | `#50FA7B` | Success messages, confirmations, completed operations | `Success` |
| **Info** | `#2980B9` | `#8BE9FD` | Informational messages, help text, tips | `Info` |

#### Visual Examples

```
Error:   [Light: darker red]  [Dark: bright red]
         "Error: File not found"

Warning: [Light: darker orange] [Dark: bright orange]
         "Warning: Deprecation notice"

Success: [Light: darker green]  [Dark: bright green]
         "✓ Operation completed successfully"

Info:    [Light: darker cyan]   [Dark: bright cyan]
         "ℹ Use --help for more information"
```

### Highlight Colors

These colors draw attention to specific elements like commands, file paths, and progress indicators.

| Color | Light Value | Dark Value | Semantic Usage | Style Names |
|-------|-------------|------------|----------------|-------------|
| **Purple** | `#8E44AD` | `#BD93F9` | File paths, commands, server names | `FilePath`, `Command`, `ServerName` |
| **Yellow** | `#B7950B` | `#F1FA8C` | Progress messages, attention-grabbing content | `Progress` |

#### Visual Examples

```
Purple:  [Light: darker purple] [Dark: bright purple]
         "Command: gh aw compile"
         "File: /path/to/workflow.md"

Yellow:  [Light: darker gold]   [Dark: bright yellow]
         "⏳ Processing workflow..."
```

### Structural Colors

These colors provide structure and visual hierarchy in terminal output.

| Color | Light Value | Dark Value | Semantic Usage | Style Names |
|-------|-------------|------------|----------------|-------------|
| **Comment** | `#6C7A89` | `#6272A4` | Secondary information, line numbers, muted text | `LineNumber`, `Verbose`, `TableHeader` |
| **Foreground** | `#2C3E50` | `#F8F8F2` | Primary text content | `ContextLine`, `ListItem`, `TableCell` |
| **Background** | `#ECF0F1` | `#282A36` | Highlighted backgrounds | `Highlight` (background) |
| **Border** | `#BDC3C7` | `#44475A` | Table borders, dividers, box outlines | `TableBorder`, `ErrorBox` (border) |

#### Visual Examples

```
Comment:    [Light: gray-blue]     [Dark: muted purple-gray]
            "  12 │ func main() {"

Foreground: [Light: dark gray]     [Dark: light gray/white]
            "Regular text content"

Border:     [Light: light gray]    [Dark: dark purple]
            ┌─────────────┐
            │   Content   │
            └─────────────┘
```

## Pre-Configured Styles

The styles package provides pre-configured styles for common use cases. These combine colors with formatting (bold, italic, etc.).

### Message Styles

| Style | Color | Formatting | Usage |
|-------|-------|------------|-------|
| `Error` | ColorError | Bold | Error messages |
| `Warning` | ColorWarning | Bold | Warning messages |
| `Success` | ColorSuccess | Bold | Success messages |
| `Info` | ColorInfo | Bold | Informational messages |
| `Prompt` | ColorSuccess | Bold | User prompts |
| `Progress` | ColorYellow | Normal | Progress indicators |
| `Verbose` | ColorComment | Italic | Debug/verbose output |

### Location and Metadata Styles

| Style | Color | Formatting | Usage |
|-------|-------|------------|-------|
| `FilePath` | ColorPurple | Bold | File paths and names |
| `Location` | ColorWarning | Bold | Directory/location messages |
| `Command` | ColorPurple | Bold | Command execution messages |
| `LineNumber` | ColorComment | Normal | Source code line numbers |
| `Count` | ColorInfo | Bold | Count/numeric status |

### List and Table Styles

| Style | Color | Formatting | Usage |
|-------|-------|------------|-------|
| `ListHeader` | ColorSuccess | Bold + Underline | Section headers in lists |
| `ListItem` | ColorForeground | Normal | Items in lists |
| `TableHeader` | ColorComment | Bold | Table column headers |
| `TableCell` | ColorForeground | Normal | Regular table cells |
| `TableTotal` | ColorSuccess | Bold | Total/summary rows |
| `TableTitle` | ColorSuccess | Bold | Table titles |
| `TableBorder` | ColorBorder | Normal | Table borders |

### MCP Inspection Styles

| Style | Color | Formatting | Usage |
|-------|-------|------------|-------|
| `ServerName` | ColorPurple | Bold | MCP server names |
| `ServerType` | ColorInfo | Normal | MCP server type info |

### Special Styles

| Style | Colors | Formatting | Usage |
|-------|--------|------------|-------|
| `Highlight` | Background: ColorError<br>Foreground: ColorBackground | Normal | Error highlighting (inverted colors) |
| `ContextLine` | ColorForeground | Normal | Source code context lines |
| `ErrorBox` | Border: ColorError | Border: RoundedBorder<br>Padding: 1<br>Margin: 1 | Boxed error messages |
| `Header` | ColorSuccess | Bold<br>Margin-bottom: 1 | Section headers |

## Border Styles

Three centralized border styles are available for consistent visual design:

| Border | Style | Usage | Example |
|--------|-------|-------|---------|
| `RoundedBorder` | Curved corners (╭╮╰╯) | Error boxes, emphasis boxes, informational panels | `ErrorBox` |
| `NormalBorder` | Straight corners (┌┐└┘) | Standard tables, section dividers | Tables |
| `ThickBorder` | Heavy lines (━) | High-emphasis boxes, critical information | Important notices |

## Usage Guidelines

### Choosing Colors

1. **Status Communication**: Use Error/Warning/Success/Info colors based on message severity
   - Error: System failures, blocking issues
   - Warning: Non-blocking issues, deprecations
   - Success: Confirmations, completed operations
   - Info: Helpful information, tips

2. **Highlighting Elements**: Use Purple/Yellow for non-status highlighting
   - Purple: Technical elements (commands, file paths, identifiers)
   - Yellow: Temporary states (progress, loading)

3. **Structure**: Use Comment/Foreground/Border for layout
   - Comment: De-emphasize secondary information
   - Foreground: Standard readable text
   - Border: Visual separation

### Adding New Styles

When adding new styles to the system:

1. **Determine if you need a new color**:
   - Check if existing colors can serve the purpose
   - New colors should have distinct semantic meaning
   - Ensure both light and dark variants are defined

2. **Add color constant in `pkg/styles/theme.go`**:
   ```go
   // ColorName is used for [semantic usage description].
   ColorName = lipgloss.AdaptiveColor{
       Light: "#RRGGBB", // Description for light backgrounds
       Dark:  "#RRGGBB", // Description for dark backgrounds (Dracula)
   }
   ```

3. **Create pre-configured style** (if needed):
   ```go
   // StyleName style for [usage description]
   var StyleName = lipgloss.NewStyle().
       Bold(true).
       Foreground(ColorName)
   ```

4. **Add tests**:
   - Color format validation
   - Light/Dark variant existence
   - Style rendering tests

5. **Update this guide**:
   - Add color to appropriate table
   - Include visual examples
   - Document semantic usage

### Recommended Practices

**DO**:
- ✅ Use semantic colors consistently across the codebase
- ✅ Prefer pre-configured styles over creating new ones inline
- ✅ Test output in both light and dark terminals
- ✅ Use borders consistently (RoundedBorder for emphasis, NormalBorder for data)
- ✅ Keep color palette minimal - reuse existing colors when possible

**DON'T**:
- ❌ Mix semantic meanings (e.g., using Error color for success)
- ❌ Create inline styles that duplicate existing pre-configured styles
- ❌ Use raw hex colors directly - use adaptive color constants
- ❌ Add new colors without both light and dark variants
- ❌ Use color alone to convey critical information (ensure text is also descriptive)

## Color Contrast and Accessibility

The color palette is designed with accessibility in mind:

- **Light Mode**: All colors have been chosen to provide sufficient contrast (WCAG AA minimum) on light backgrounds
- **Dark Mode**: Dracula theme colors are well-tested for readability on dark backgrounds
- **Fallback**: The system gracefully degrades in terminals with limited color support

### Testing Your Output

When developing new features that use colors:

1. **Test in multiple terminals**:
   - iTerm2, Terminal.app (macOS)
   - Windows Terminal, PowerShell (Windows)
   - GNOME Terminal, Konsole (Linux)

2. **Test both themes**:
   - Configure your terminal for light mode
   - Configure your terminal for dark mode
   - Verify readability in both

3. **Test color support**:
   - 256-color terminals (most common)
   - True color terminals (24-bit)
   - Basic 16-color terminals (fallback)

## Related Documentation

- [Code Organization Patterns](./code-organization.md) - File structure guidelines
- [Capitalization Guidelines](./capitalization.md) - Text formatting conventions
- [GitHub Actions Security Best Practices](./github-actions-security-best-practices.md) - Security considerations

## Implementation Details

### File Locations

- **Color Definitions**: `pkg/styles/theme.go`
- **Tests**: `pkg/styles/theme_test.go`
- **Usage**: Throughout `pkg/` and `cmd/` packages

### Dependencies

- [lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling library
- [Dracula Theme](https://draculatheme.com/) - Dark mode color inspiration

### Testing

The color system includes tests:
- `TestAdaptiveColorsHaveBothVariants` - Ensures all colors have light and dark variants
- `TestColorFormats` - Validates hex color format
- `TestDarkColorsAreOriginalDracula` - Ensures dark mode maintains Dracula theme colors
- `TestStylesExist` - Verifies all styles are properly initialized
- `TestBordersExist` - Validates border definitions

Run tests:
```bash
cd pkg/styles
go test -v
```

---

**Status**: ✅ Documented
**Last Updated**: 2025-12-27
