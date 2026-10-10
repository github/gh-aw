//go:build !js && !wasm && !integration

package console

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/creack/pty"
	"github.com/stretchr/testify/require"
)

func TestFormatterStreamContracts(t *testing.T) {
	if os.Getenv("GH_AW_CONSOLE_STREAM_TEST") == "1" {
		writeFormatterProbe()
		return
	}
	for _, stdoutTTY := range []bool{false, true} {
		for _, stderrTTY := range []bool{false, true} {
			for _, noColor := range []bool{false, true} {
				name := fmt.Sprintf("stdout=%v/stderr=%v/no_color=%v", stdoutTTY, stderrTTY, noColor)
				t.Run(name, func(t *testing.T) {
					result := probeFormatterStreams(t, stdoutTTY, stderrTTY, noColor)
					for name, text := range result {
						wantANSI := stderrTTY && !noColor
						if strings.HasPrefix(name, "stdout_") {
							wantANSI = stdoutTTY && !noColor
						}
						require.Equal(t, wantANSI, strings.Contains(text, "\x1b["), "%s: %q", name, text)
						require.Contains(t, text, "message")
					}
				})
			}
		}
	}
}

func writeFormatterProbe() {
	m := "message"
	result := map[string]string{
		"stdout_info":            FormatInfoMessageStdout(m),
		"stdout_success":         FormatSuccessMessageStdout(m),
		"stdout_warning":         FormatWarningMessageStdout(m),
		"stdout_command":         FormatCommandMessageStdout(m),
		"stdout_progress":        FormatProgressMessageStdout(m),
		"stdout_prompt":          FormatPromptMessageStdout(m),
		"stdout_verbose":         FormatVerboseMessageStdout(m),
		"stdout_list":            FormatListItemStdout(m),
		"stdout_section":         FormatSectionHeaderStdout(m),
		"stdout_error":           FormatErrorStdout(CompilerError{Message: m}),
		"stderr_info":            FormatInfoMessage(m),
		"stderr_success":         FormatSuccessMessage(m),
		"stderr_warning":         FormatWarningMessage(m),
		"stderr_command":         FormatCommandMessage(m),
		"stderr_progress":        FormatProgressMessage(m),
		"stderr_prompt":          FormatPromptMessage(m),
		"stderr_verbose":         FormatVerboseMessage(m),
		"stderr_list":            FormatListItem(m),
		"stderr_section":         FormatSectionHeader(m),
		"stderr_error":           FormatError(CompilerError{Message: m}),
		"stderr_simple_error":    FormatErrorMessage(m),
		"stderr_wrapped_error":   FormatErrorChain(fmt.Errorf("outer message: %w", errors.New(m))),
		"stderr_multiline_error": FormatErrorChain(errors.New("message\nsecond message")),
		"stdout_table":           RenderTableStdout(TableConfig{Headers: []string{"message"}, Rows: [][]string{{m}}}),
		"stderr_table":           RenderTable(TableConfig{Headers: []string{"message"}, Rows: [][]string{{m}}}),
		"stderr_nested_table": RenderStruct(struct{ Rows []struct{ Message string } }{
			Rows: []struct{ Message string }{{m}},
		}),
		"stdout_nested_table": RenderStructStdout(struct{ Rows []struct{ Message string } }{
			Rows: []struct{ Message string }{{m}},
		}),
		"stderr_options_table": RenderStructWithOptions([]struct{ Message string }{{m}}, RenderOptions{Stderr: true}),
		"stdout_options_table": RenderStructWithOptions([]struct{ Message string }{{m}}, RenderOptions{}),
		"stderr_alias_info":    FormatInfoMessageStderr(m),
		"stderr_alias_table":   RenderTableStderr(TableConfig{Headers: []string{"message"}, Rows: [][]string{{m}}}),
	}
	file := os.NewFile(3, "formatter-probe")
	if err := json.NewEncoder(file).Encode(result); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func probeFormatterStreams(t *testing.T, stdoutTTY, stderrTTY, noColor bool) map[string]string {
	t.Helper()
	master, slave, err := pty.Open()
	require.NoError(t, err)
	defer master.Close()
	defer slave.Close()
	reader, writer, err := os.Pipe()
	require.NoError(t, err)
	defer reader.Close()
	defer writer.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestFormatterStreamContracts$")
	cmd.Env = streamProbeEnviron(noColor)
	cmd.ExtraFiles = []*os.File{writer}
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	if stdoutTTY {
		cmd.Stdout = slave
	}
	if stderrTTY {
		cmd.Stderr = slave
	}
	require.NoError(t, cmd.Run())
	require.NoError(t, writer.Close())
	var result map[string]string
	require.NoError(t, json.NewDecoder(reader).Decode(&result))
	return result
}

func streamProbeEnviron(noColor bool) []string {
	var environ []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "NO_COLOR", "TERM", "COLORTERM", "CLICOLOR", "CLICOLOR_FORCE", "GH_AW_CONSOLE_STREAM_TEST":
			continue
		}
		environ = append(environ, entry)
	}
	environ = append(environ, "TERM=xterm-256color", "COLORTERM=truecolor", "GH_AW_CONSOLE_STREAM_TEST=1")
	if noColor {
		environ = append(environ, "NO_COLOR=1")
	}
	return environ
}
