package console

import (
	"strings"

	"charm.land/lipgloss/v2/table"
	"github.com/charmbracelet/x/ansi"
)

// DefaultTableWidth is the recommended width budget for human-facing tables.
const DefaultTableWidth = 80

func wrapConsoleText(text string, width int) string {
	if width <= 0 {
		return text
	}
	return ansi.Wrap(text, max(2, width), "")
}

func exceedsTableWidth(text string, width int) bool {
	for line := range strings.SplitSeq(text, "\n") {
		if ansi.StringWidth(line) > width {
			return true
		}
	}
	return false
}

func renderBoundedTable(t *table.Table, config TableConfig, rows [][]string, styled bool) string {
	if config.MaxWidth <= 0 {
		return t.String()
	}
	width := max(2, config.MaxWidth)
	cellWidth := (width - len(config.Headers) - 1) / len(config.Headers)
	if styled {
		cellWidth -= 2
	}
	if cellWidth < 2 {
		return renderNarrowTable(config.Headers, rows)
	}
	for _, header := range config.Headers {
		if exceedsTableWidth(header, cellWidth) {
			return renderNarrowTable(config.Headers, rows)
		}
	}
	wrappedRows := make([][]string, len(rows))
	for i, row := range rows {
		wrappedRows[i] = make([]string, len(row))
		for col, value := range row {
			wrappedRows[i][col] = wrapConsoleText(value, cellWidth)
		}
	}
	// Wrap graphemes before rendering: the table's own width contraction can
	// truncate headers and separate a combining mark from its base character.
	output := t.ClearRows().Rows(wrappedRows...).Wrap(true).String()
	if exceedsTableWidth(output, width) {
		return renderNarrowTable(config.Headers, rows)
	}
	return output
}

func renderNarrowTable(headers []string, rows [][]string) string {
	var output strings.Builder
	output.WriteString(strings.Join(headers, " | "))
	output.WriteString("\n")
	for i, row := range rows {
		if i > 0 {
			output.WriteString("\n")
		}
		for col, value := range row {
			if col < len(headers) {
				output.WriteString(headers[col])
				output.WriteString(": ")
			}
			output.WriteString(value)
			output.WriteString("\n")
		}
	}
	return strings.TrimSuffix(output.String(), "\n")
}
