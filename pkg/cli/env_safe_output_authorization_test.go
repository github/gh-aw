//go:build !integration

package cli

import (
	"slices"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/compilerenv"
	"github.com/stretchr/testify/require"
)

func TestSafeOutputAuthorizationManagedDefaults(t *testing.T) {
	file, err := defaultsParseFile("defaults.yml", []byte("default_safe_output_authorization_url: https://authority.example/api/safe-output-authorization\ndefault_safe_output_authorization_audience: https://authority.example/gh-aw\n"))
	require.NoError(t, err)
	require.NoError(t, defaultsValidateFile(&file))
	for _, binding := range []struct{ field, variable, value string }{
		{"default_safe_output_authorization_url", compilerenv.DefaultSafeOutputAuthorizationURL, "https://authority.example/api/safe-output-authorization"},
		{"default_safe_output_authorization_audience", compilerenv.DefaultSafeOutputAuthorizationAudience, "https://authority.example/gh-aw"},
	} {
		changes := defaultsBuildUpdateChanges(&file)
		index := slices.IndexFunc(changes, func(change defaultsUpdateChange) bool { return change.field == binding.field })
		require.NotEqual(t, -1, index)
		require.Equal(t, binding.variable, changes[index].envName)
		require.Equal(t, binding.value, changes[index].value)
		require.False(t, changes[index].delete)
	}
	for _, raw := range []string{"{}", "default_safe_output_authorization_url: null\n", "default_safe_output_authorization_url: ''\ndefault_safe_output_authorization_audience: ''\n"} {
		file, err := defaultsParseFile("defaults.yml", []byte(raw))
		require.NoError(t, err)
		require.NoError(t, defaultsValidateFile(&file))
		changes := defaultsBuildUpdateChanges(&file)
		index := slices.IndexFunc(changes, func(change defaultsUpdateChange) bool {
			return change.envName == compilerenv.DefaultSafeOutputAuthorizationURL
		})
		require.NotEqual(t, -1, index)
		require.Equal(t, file.DefaultSafeOutputAuthorizationURL == nil, changes[index].delete)
		require.Empty(t, changes[index].value)
	}
}

func TestSafeOutputAuthorizationDefaultsValidation(t *testing.T) {
	for _, endpoint := range []string{"http://insecure.example/a", "https://user:secret@example.test/a", "https://example.test/a?query", "https://example.test/a#", "${{ vars.ENDPOINT }}", " https://example.test/a", "https://example.test\\@attacker.test/a"} {
		require.ErrorContains(t, defaultsValidateFile(&defaultsFile{DefaultSafeOutputAuthorizationURL: &endpoint}), "default_safe_output_authorization_url")
	}
	for _, audience := range []string{" ", "bad\nvalue", "${{ vars.AUDIENCE }}"} {
		require.ErrorContains(t, defaultsValidateFile(&defaultsFile{DefaultSafeOutputAuthorizationAudience: &audience}), "default_safe_output_authorization_audience")
	}
}
