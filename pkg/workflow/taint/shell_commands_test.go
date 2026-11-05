package taint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellInvocationTraces(t *testing.T) {
	for _, tt := range []struct {
		name  string
		shell string
		trace bool
	}{
		{name: "short option enables trace", shell: "bash -x", trace: true},
		{name: "set option enables trace", shell: "bash -o xtrace", trace: true},
		{name: "short option disables trace", shell: "bash +x"},
		{name: "set option disables trace", shell: "bash +o xtrace"},
		{name: "unrelated option", shell: "bash -o pipefail"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.trace, shellInvocationTraces(tt.shell))
		})
	}
}
