//go:build !js && !wasm && !integration

package console

import (
	"strings"
	"testing"

	lipgloss "charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/stretchr/testify/require"
)

func TestRenderTableWidthBudget(t *testing.T) {
	for _, width := range []int{1, 2, 12, 40, 80, 100} {
		config := TableConfig{
			Title:     strings.Repeat("q", 120),
			Headers:   []string{"Path", "Unicode"},
			Rows:      [][]string{{strings.Repeat("x", 180), strings.Repeat("界", 60) + "e\u0301🙂"}},
			ShowTotal: true, TotalRow: []string{"TOTAL", "complete"}, MaxWidth: width,
		}
		output := renderTableWithTTY(config, func() bool { return false }, nil, true)
		for line := range strings.SplitSeq(output, "\n") {
			require.LessOrEqual(t, lipgloss.Width(line), max(2, width), "width %d: %q", width, line)
		}
		require.Equal(t, 180, strings.Count(output, "x"), "long paths must not be truncated")
		require.Equal(t, 60, strings.Count(output, "界"), "wide text must not be truncated")
		require.Equal(t, 120, strings.Count(output, "q"))
		require.NotContains(t, output, "\x1b[")
		require.Contains(t, strings.ReplaceAll(output, "\n", ""), "e\u0301")
	}
}

func TestRenderTableUnboundedContract(t *testing.T) {
	config := TableConfig{Headers: []string{"Path"}, Rows: [][]string{{strings.Repeat("x", 180)}}}
	output := renderTableWithTTY(config, func() bool { return false }, nil, true)
	require.Contains(t, output, config.Rows[0][0])
	require.Greater(t, lipgloss.Width(output), DefaultTableWidth)
	require.Empty(t, RenderTableStderr(TableConfig{}))
}

func TestRenderStructStderrWidthBudget(t *testing.T) {
	data := struct {
		Rows []struct{ Path string }
	}{Rows: []struct{ Path string }{{strings.Repeat("x", 180)}}}
	output := RenderStructWithOptions(data, RenderOptions{Stderr: true, MaxWidth: DefaultTableWidth})
	for line := range strings.SplitSeq(output, "\n") {
		require.LessOrEqual(t, ansi.StringWidth(line), DefaultTableWidth)
	}
	require.Equal(t, 180, strings.Count(output, "x"))
	require.Contains(t, RenderStructStderr(data), data.Rows[0].Path, "stderr rendering is unbounded unless explicitly configured")
}
