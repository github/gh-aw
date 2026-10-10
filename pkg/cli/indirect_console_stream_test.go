//go:build !js && !wasm && !integration

package cli

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"github.com/creack/pty"
	"github.com/github/gh-aw/pkg/actionpins"
	"github.com/github/gh-aw/pkg/scanfindings"
	"github.com/stretchr/testify/require"
)

const indirectProbeMarker = "__GH_AW_INDIRECT_CONSOLE_CASE__"

func TestIndirectDiagnosticStreamContracts(t *testing.T) {
	if os.Getenv("GH_AW_INDIRECT_CONSOLE_PROBE") == "1" {
		for _, probe := range indirectDiagnosticProbes() {
			fmt.Fprintln(os.Stderr, indirectProbeMarker+probe.name)
			probe.render(t)
		}
		return
	}
	for _, stdoutTTY := range []bool{false, true} {
		for _, stderrTTY := range []bool{false, true} {
			for _, noColor := range []bool{false, true} {
				t.Run(fmt.Sprintf("stdout=%v/stderr=%v/no_color=%v", stdoutTTY, stderrTTY, noColor), func(t *testing.T) {
					output := captureIndirectDiagnosticProbe(t, stdoutTTY, stderrTTY, noColor)
					sections := strings.Split(strings.ReplaceAll(output, "\r\n", "\n"), indirectProbeMarker)
					probes := indirectDiagnosticProbes()
					require.Len(t, sections, len(probes)+1)
					for i, probe := range probes {
						name, text, found := strings.Cut(sections[i+1], "\n")
						require.True(t, found)
						require.Equal(t, probe.name, name)
						var matching []string
						for line := range strings.SplitSeq(text, "\n") {
							if strings.Contains(line, probe.message) {
								matching = append(matching, line)
							}
						}
						require.Len(t, matching, 1, "%s: %q", name, text)
						require.Equal(t, stderrTTY && !noColor, strings.Contains(matching[0], "\x1b["), "%s: %q", name, matching[0])
					}
				})
			}
		}
	}
}

type indirectDiagnosticProbe struct {
	name    string
	message string
	render  func(*testing.T)
}

func indirectDiagnosticProbes() []indirectDiagnosticProbe {
	digest := strings.Repeat("a", 64)
	sha := strings.Repeat("b", 40)
	bootstrap := func(*testing.T) {
		printBootstrapConfigTODO(os.Stderr, &resolvedBootstrapProfile{
			PackageID: "owner/repo",
			Profile: &repositoryPackageBootstrap{
				Config: []repositoryPackageBootstrapAction{{Type: "handoff", Message: "handoff message"}},
			},
		})
	}
	return []indirectDiagnosticProbe{
		{"scanner", "scanner message", func(*testing.T) {
			scanfindings.Render(os.Stderr, []scanfindings.Finding{{Severity: scanfindings.SeverityHigh, Message: "scanner message"}})
		}},
		{"pin-prefix", "Action pin mapping applied", func(t *testing.T) {
			got := actionpins.ApplyResolvedActionPinPrefix("actions/checkout", "actions/checkout@"+sha, &actionpins.PinContext{
				PrefixMappings: map[string]string{"actions/": "mirror/"},
			})
			require.Equal(t, "mirror/checkout@"+sha, got)
		}},
		{"pin-mapping", "Action pin mapping applied", func(t *testing.T) {
			got, err := actionpins.ResolveActionPin("actions/checkout", "v1", &actionpins.PinContext{
				Mappings: map[string]string{"actions/checkout@v1": "mirror/checkout@" + sha},
			})
			require.NoError(t, err)
			require.Equal(t, "mirror/checkout@"+sha, got)
		}},
		{"container-info", "Container pin mapping applied", func(t *testing.T) {
			ctx := &actionpins.PinContext{ContainerMappings: map[string]string{"image:v1": "image@sha256:" + digest}}
			require.Equal(t, "image@sha256:"+digest, actionpins.ApplyContainerPinMapping("image:v1", ctx))
			require.Equal(t, "image@sha256:"+digest, actionpins.ApplyContainerPinMapping("image:v1", ctx))
		}},
		{"container-warning", "container_pins: invalid replacement", func(t *testing.T) {
			ctx := &actionpins.PinContext{ContainerMappings: map[string]string{"image:v1": "image:latest"}}
			require.Equal(t, "image:v1", actionpins.ApplyContainerPinMapping("image:v1", ctx))
			require.Equal(t, "image:v1", actionpins.ApplyContainerPinMapping("image:v1", ctx))
		}},
		{"unresolved-pin", "Unable to pin action", func(t *testing.T) {
			got, err := actionpins.ResolveActionPin("missing/action", "v0.0.0", nil)
			require.NoError(t, err)
			require.Empty(t, got)
		}},
		{"fallback-pin", "using hardcoded pin", func(t *testing.T) {
			got, err := actionpins.ResolveActionPin("actions/checkout", "v999999", nil)
			require.NoError(t, err)
			require.NotEmpty(t, got)
		}},
		{"bootstrap-header", "Post-installation steps from owner/repo", bootstrap},
		{"bootstrap-handoff", "handoff message", bootstrap},
	}
}

func captureIndirectDiagnosticProbe(t *testing.T, stdoutTTY, stderrTTY, noColor bool) string {
	t.Helper()
	stdoutMaster, stdoutSlave, err := pty.Open()
	require.NoError(t, err)
	defer stdoutMaster.Close()
	defer stdoutSlave.Close()
	stderrMaster, stderrSlave, err := pty.Open()
	require.NoError(t, err)
	defer stderrMaster.Close()
	defer stderrSlave.Close()
	cmd := exec.Command(os.Args[0], "-test.run=^TestIndirectDiagnosticStreamContracts$")
	cmd.Env = indirectDiagnosticEnviron(noColor)
	cmd.Stdout = io.Discard
	if stdoutTTY {
		cmd.Stdout = stdoutSlave
	}
	var output bytes.Buffer
	cmd.Stderr = &output
	if !stderrTTY {
		require.NoError(t, cmd.Run())
		return output.String()
	}
	cmd.Stderr = stderrSlave
	require.NoError(t, cmd.Start())
	readDone := make(chan error, 1)
	go func() {
		_, err := io.Copy(&output, stderrMaster)
		readDone <- err
	}()
	runErr := cmd.Wait()
	require.NoError(t, stderrSlave.Close())
	readErr := <-readDone
	require.NoError(t, runErr)
	if !errors.Is(readErr, syscall.EIO) {
		require.NoError(t, readErr)
	}
	return output.String()
}

func indirectDiagnosticEnviron(noColor bool) []string {
	var environ []string
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		switch key {
		case "NO_COLOR", "TERM", "COLORTERM", "CLICOLOR", "CLICOLOR_FORCE", "DEBUG", "GH_AW_INDIRECT_CONSOLE_PROBE":
			continue
		}
		environ = append(environ, entry)
	}
	environ = append(environ, "TERM=xterm-256color", "COLORTERM=truecolor", "DEBUG=", "GH_AW_INDIRECT_CONSOLE_PROBE=1")
	if noColor {
		environ = append(environ, "NO_COLOR=1")
	}
	return environ
}
