package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestConnectorCommandNamespaces(t *testing.T) {
	for _, id := range []string{"plan", "schema", "validate", "serve", "invoke"} {
		command, remaining, err := rootCmd.Find([]string{"connector", id, "setup"})
		require.NoError(t, err)
		require.Equal(t, "connector", command.Name())
		require.Equal(t, []string{id, "setup"}, remaining)
	}
	command, _, err := rootCmd.Find([]string{"connectors", "schema", "ConnectorSpec"})
	require.NoError(t, err)
	require.Equal(t, "schema", command.Name())
	require.Equal(t, "connectors", command.Parent().Name())
}
