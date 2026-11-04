package taint

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestShellEnvUnsetVariable(t *testing.T) {
	for _, option := range []string{"-u", "--unset"} {
		t.Run(option, func(t *testing.T) {
			result := checkSecretScript(t, "env "+option+" TOKEN printenv TOKEN")
			require.Empty(t, result.Gaps)
			require.Empty(t, result.Findings)
		})
	}
}
