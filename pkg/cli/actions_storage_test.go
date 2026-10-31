//go:build !integration

package cli

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestActionsStorageRejectsOAuthBeforeUpload(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"gho_not_a_real_token", " \ngho_not_a_real_token\n", `{"token":"gho_not_a_real_token"}`} {
		for name, store := range map[string]func() error{
			"REST secret": func() error { return setRepoSecret(nil, "octo", "repo", "CUSTOM", value) },
			"engine secret": func() error {
				return uploadSecretToRepo(context.Background(), "CUSTOM", value, "octo/repo", false, true)
			},
			"wizard secret":         func() error { return (&AddInteractiveConfig{}).addRepositorySecret("CUSTOM", value) },
			"repository variable":   func() error { return upsertDefaultsVariable(defaultsTarget{scope: defaultsScopeRepo}, "CUSTOM", value) },
			"organization variable": func() error { return upsertDefaultsVariable(defaultsTarget{scope: defaultsScopeOrg}, "CUSTOM", value) },
			"enterprise variable":   func() error { return upsertDefaultsVariable(defaultsTarget{scope: defaultsScopeEnt}, "CUSTOM", value) },
		} {
			t.Run(name+value, func(t *testing.T) {
				err := store()
				require.Error(t, err)
				assert.Contains(t, err.Error(), "refusing to store a GitHub OAuth token")
				assert.Contains(t, err.Error(), "elicit the user")
				assert.NotContains(t, err.Error(), value)
				assert.NotContains(t, err.Error(), "not_a_real_token")
			})
		}
	}
}

func TestActionsStorageAllowsOtherValues(t *testing.T) {
	t.Parallel()
	for _, value := range []string{"github_pat_not_a_real_token", "ghp_not_a_real_token", "api-key", "42", ""} {
		require.NoError(t, validateActionsStorageValue(value))
	}
}
