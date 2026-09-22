//go:build !integration

package workflow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestNewCheckoutManager verifies that a CheckoutManager can be created with user configs.
func TestNewCheckoutManager(t *testing.T) {
	t.Run("empty configs produces empty manager", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		// HasUserCheckouts removed (dead code)
		assert.Nil(t, cm.GetDefaultCheckoutOverride(), "empty manager should have no default override")
	})

	t.Run("single default override", func(t *testing.T) {
		depth := 0
		cm := NewCheckoutManager([]*CheckoutConfig{
			{FetchDepth: &depth},
		})
		// HasUserCheckouts removed (dead code)
		override := cm.GetDefaultCheckoutOverride()
		require.NotNil(t, override, "should have default override")
		require.NotNil(t, override.fetchDepth, "fetch depth should be set")
		assert.Equal(t, 0, *override.fetchDepth, "fetch depth should be 0")
	})

	t.Run("custom github-token on default checkout", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_TOKEN }}"},
		})
		override := cm.GetDefaultCheckoutOverride()
		require.NotNil(t, override, "should have default override")
		assert.Equal(t, "${{ secrets.MY_TOKEN }}", override.token, "github-token should be set")
	})
}

// TestCheckoutManagerMerging verifies that duplicate checkout configs are merged.
func TestCheckoutManagerMerging(t *testing.T) {
	t.Run("duplicate default checkout takes deepest fetch-depth", func(t *testing.T) {
		depth1 := 1
		depth10 := 10
		cm := NewCheckoutManager([]*CheckoutConfig{
			{FetchDepth: &depth1},
			{FetchDepth: &depth10},
		})
		assert.Len(t, cm.ordered, 1, "should have merged into a single entry")
		override := cm.GetDefaultCheckoutOverride()
		require.NotNil(t, override.fetchDepth, "fetch depth should be set after merge")
		assert.Equal(t, 10, *override.fetchDepth, "should use deeper fetch-depth (10 > 1)")
	})

	t.Run("zero fetch-depth wins over any positive value", func(t *testing.T) {
		depth0 := 0
		depth5 := 5
		cm := NewCheckoutManager([]*CheckoutConfig{
			{FetchDepth: &depth5},
			{FetchDepth: &depth0},
		})
		override := cm.GetDefaultCheckoutOverride()
		require.NotNil(t, override.fetchDepth, "fetch depth should be set")
		assert.Equal(t, 0, *override.fetchDepth, "0 (full history) should win")
	})

	t.Run("sparse-checkout patterns are merged", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./workspace", SparseCheckout: ".github/"},
			{Path: "./workspace", SparseCheckout: "src/"},
		})
		assert.Len(t, cm.ordered, 1, "should have merged into a single entry")
		additional := cm.GenerateAdditionalCheckoutSteps(func(s string) string { return s })
		combined := strings.Join(additional, "")
		assert.Contains(t, combined, ".github/", "should contain first sparse pattern")
		assert.Contains(t, combined, "src/", "should contain second sparse pattern")
	})

	t.Run("different paths produce separate checkouts", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./workspace1"},
			{Path: "./workspace2"},
		})
		assert.Len(t, cm.ordered, 2, "different paths should not be merged")
	})

	t.Run("different repos produce separate checkouts", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo1", Path: "./r1"},
			{Repository: "owner/repo2", Path: "./r2"},
		})
		assert.Len(t, cm.ordered, 2, "different repos should not be merged")
	})

	t.Run("same path with different refs merges to first ref", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./workspace", Ref: "main"},
			{Path: "./workspace", Ref: "develop"},
		})
		assert.Len(t, cm.ordered, 1, "same path should be merged")
		assert.Equal(t, "main", cm.ordered[0].ref, "first-seen ref should win")
	})

	t.Run("path dot and empty path are normalized to the same root checkout", func(t *testing.T) {
		depth0 := 0
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: ".", FetchDepth: nil},
			{Path: "", FetchDepth: &depth0},
		})
		assert.Len(t, cm.ordered, 1, "path '.' and '' should merge as the same root checkout")
		assert.Empty(t, cm.ordered[0].key.path, "normalized path should be empty string")
		require.NotNil(t, cm.ordered[0].fetchDepth, "fetch depth should be set from second config")
		assert.Equal(t, 0, *cm.ordered[0].fetchDepth, "fetch depth 0 should win")
	})
}

// TestGenerateDefaultCheckoutStep verifies the default checkout step output.
func TestGenerateDefaultCheckoutStep(t *testing.T) {
	getPin := func(action string) string { return action + "@v4" }

	t.Run("default checkout has persist-credentials false", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "persist-credentials: false", "must always have persist-credentials: false")
		assert.Contains(t, combined, "Checkout repository", "should have default step name")
		assert.Contains(t, combined, "actions/checkout@v4", "should use pinned checkout action")
	})

	t.Run("user github-token is included in default checkout", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_TOKEN }}"},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "token: ${{ secrets.MY_TOKEN }}", "should include custom token in actions/checkout 'token' input")
		assert.Contains(t, combined, "persist-credentials: false", "must always have persist-credentials: false even with custom token")
	})

	t.Run("fetch-depth override is included", func(t *testing.T) {
		depth := 0
		cm := NewCheckoutManager([]*CheckoutConfig{
			{FetchDepth: &depth},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "fetch-depth: 0", "should include fetch-depth override")
	})

	t.Run("ref override is included", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Ref: "develop"},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "ref: develop", "should include ref override")
	})

	t.Run("trial mode overrides user config", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_TOKEN }}"},
		})
		lines := cm.GenerateDefaultCheckoutStep(true, "owner/trial-repo", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "repository: owner/trial-repo", "trial repo should be in output")
		// In trial mode, user token should NOT be emitted (trial uses its own token)
		assert.NotContains(t, combined, "secrets.MY_TOKEN", "user token should not appear in trial mode")
	})

	t.Run("sparse-checkout override is included", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{SparseCheckout: ".github/\nsrc/"},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "sparse-checkout: |", "should include sparse-checkout header")
		assert.Contains(t, combined, ".github/", "should include first pattern")
		assert.Contains(t, combined, "src/", "should include second pattern")
		assert.Contains(t, combined, "filter: 'blob:limit=1073741824'", "sparse-checkout should emit blob:limit filter to ensure blobs are fetched")
		assert.Contains(t, combined, "Clear partial clone markers after sparse checkout", "sparse-checkout should repair partial clone state after checkout")
		assert.Contains(t, combined, "git config --local --unset-all remote.origin.promisor || true", "default checkout repair should target workspace root")
		assert.Contains(t, combined, "git config --local --unset-all remote.origin.partialclonefilter || true", "default checkout repair should clear partial clone filter")
	})

	t.Run("sparse-checkout without fetch refs still ensures blobs present", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{SparseCheckout: ".github/\nsrc/"},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		// blob:limit filter ensures all blobs are fetched during checkout even without additional refs
		assert.Contains(t, combined, "filter: 'blob:limit=1073741824'", "sparse-checkout should use blob:limit to fetch all blobs")
		assert.Contains(t, combined, "Clear partial clone markers after sparse checkout", "repair step should run even without fetch refs")
		assert.NotContains(t, combined, "Fetch additional refs", "should not emit fetch step when no refs configured")
	})

	t.Run("no filter emitted without sparse-checkout", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Ref: "develop"},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.NotContains(t, combined, "filter:", "should not emit filter when no sparse-checkout")
	})

	t.Run("sparse-checkout repair runs before additional ref fetch", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{SparseCheckout: ".github/\nsrc/", Fetch: []string{"main"}},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		repairIndex := strings.Index(combined, "Clear partial clone markers after sparse checkout")
		fetchIndex := strings.Index(combined, "Fetch additional refs")
		require.NotEqual(t, -1, repairIndex, "should emit sparse-checkout repair step")
		require.NotEqual(t, -1, fetchIndex, "should emit fetch step")
		assert.Less(t, repairIndex, fetchIndex, "repair step should run before additional fetches")
	})

	t.Run("force-clean-git-credentials enables persist true and cleanup step", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{CleanGitCredentials: true},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "persist-credentials: true", "force-clean-git-credentials should switch persist-credentials to true")
		assert.Contains(t, combined, "Clean git credentials after checkout", "should inject post-checkout clean step")
		assert.Contains(t, combined, "${RUNNER_TEMP}/gh-aw/actions/clean_git_credentials_checkout.sh", "cleanup should call orchestrator helper")
		assert.NotContains(t, combined, "${GITHUB_WORKSPACE}/actions/setup/sh/clean_git_credentials_pre_setup.sh", "cleanup must not execute helper from workspace")
		assert.NotContains(t, combined, "WARNING: Checkout cleanup helper missing. Running inline fallback.", "cleanup should not include inline fallback path")
		assert.NotContains(t, combined, "cleaned_configs=0", "cleanup should not include inline fallback logic")
	})
}

// TestCheckoutPushTokenFallback verifies the safe_outputs push-token fallback that
// persists the resolved PR push token into the checkout when keepCredentialsForPush is
// enabled and no explicit checkout token (or app auth) already governs the checkout.
func TestCheckoutPushTokenFallback(t *testing.T) {
	getPin := func(action string) string { return action + "@v4" }
	const pushToken = "${{ secrets.PUSH_TOKEN }}"

	t.Run("default checkout with no explicit token emits pushToken once", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetKeepCredentialsForPush(true)
		cm.SetPushToken(pushToken)
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "persist-credentials: true", "keepCredentialsForPush should retain credentials")
		assert.Contains(t, combined, "token: "+pushToken, "should persist the push token")
		assert.Equal(t, 1, strings.Count(combined, "token: "), "token must be emitted exactly once")
	})

	t.Run("default checkout with explicit token does not override with pushToken", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_TOKEN }}"},
		})
		cm.SetKeepCredentialsForPush(true)
		cm.SetPushToken(pushToken)
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "token: ${{ secrets.MY_TOKEN }}", "explicit checkout token should win")
		assert.NotContains(t, combined, pushToken, "pushToken must not override an explicit checkout token")
		assert.Equal(t, 1, strings.Count(combined, "token: "), "token must be emitted exactly once")
	})

	t.Run("default checkout with app auth does not override with pushToken", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubApp: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.APP_KEY }}"}},
		})
		cm.SetKeepCredentialsForPush(true)
		cm.SetPushToken(pushToken)
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "checkout-app-token-0.outputs.token", "app-minted token should govern the checkout")
		assert.NotContains(t, combined, pushToken, "pushToken must not override an app-minted token")
	})

	t.Run("default checkout does not emit pushToken when keepCredentialsForPush is false", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetPushToken(pushToken)
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "persist-credentials: false", "agent-style checkout strips credentials")
		assert.NotContains(t, combined, pushToken, "pushToken must not be persisted when credentials are not retained")
	})

	t.Run("additional checkout with no token uses pushToken", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/libs", Path: "./libs"},
		})
		cm.SetKeepCredentialsForPush(true)
		cm.SetPushToken(pushToken)
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "persist-credentials: true", "keepCredentialsForPush should retain credentials")
		assert.Contains(t, combined, "token: "+pushToken, "additional checkout should fall back to the push token")
		assert.Equal(t, 1, strings.Count(combined, "token: "), "token must be emitted exactly once")
	})

	t.Run("additional checkout with explicit token does not override with pushToken", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/libs", Path: "./libs", GitHubToken: "${{ secrets.MY_TOKEN }}"},
		})
		cm.SetKeepCredentialsForPush(true)
		cm.SetPushToken(pushToken)
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "token: ${{ secrets.MY_TOKEN }}", "explicit checkout token should win")
		assert.NotContains(t, combined, pushToken, "pushToken must not override an explicit checkout token")
		assert.Equal(t, 1, strings.Count(combined, "token: "), "token must be emitted exactly once")
	})
}

// TestGenerateAdditionalCheckoutSteps verifies that non-default checkouts are emitted correctly.
func TestGenerateAdditionalCheckoutSteps(t *testing.T) {
	getPin := func(action string) string { return action + "@v4" }

	t.Run("no additional checkouts when only default configured", func(t *testing.T) {
		depth := 0
		cm := NewCheckoutManager([]*CheckoutConfig{
			{FetchDepth: &depth},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		assert.Empty(t, lines, "should produce no additional checkout steps")
	})

	t.Run("additional checkout for different path", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/libs", Path: "./libs/owner-libs", Ref: "main"},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "repository: owner/libs", "should include repo")
		assert.Contains(t, combined, "path: ./libs/owner-libs", "should include path")
		assert.Contains(t, combined, "ref: main", "should include ref")
		assert.Contains(t, combined, "persist-credentials: false", "must always have persist-credentials: false")
	})

	t.Run("additional checkout with LFS enabled", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./lfs-repo", LFS: true},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "lfs: true", "should include LFS option")
	})

	t.Run("additional checkout with recursive submodules", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./with-submodules", Submodules: "recursive"},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "submodules: recursive", "should include submodules option")
	})

	t.Run("additional checkout emits actions/checkout token input from github-token config", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./libs", Repository: "owner/libs", GitHubToken: "${{ secrets.MY_TOKEN }}"},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "token: ${{ secrets.MY_TOKEN }}", "actions/checkout input must be 'token' even when frontmatter uses 'github-token'")
		assert.NotContains(t, combined, "github-token:", "must not emit 'github-token' as actions/checkout input")
	})

	t.Run("additional checkout supports force-clean-git-credentials", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./libs", Repository: "owner/libs", CleanGitCredentials: true},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "persist-credentials: true", "force-clean-git-credentials should switch persist-credentials to true")
		assert.Contains(t, combined, "Clean git credentials after checkout", "should inject post-checkout clean step")
	})

	t.Run("additional checkout with sparse-checkout emits filter empty", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./libs", Repository: "owner/libs", SparseCheckout: "src/\nlib/"},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "sparse-checkout: |", "should include sparse-checkout header")
		assert.Contains(t, combined, "filter: 'blob:limit=1073741824'", "sparse-checkout should emit blob:limit filter to ensure blobs are fetched")
		assert.Contains(t, combined, "Clear partial clone markers after sparse checkout", "sparse-checkout should repair partial clone state after checkout")
		assert.Contains(t, combined, `git -C "${{ github.workspace }}/./libs" config --local --unset-all remote.origin.promisor || true`, "additional checkout repair should target checkout path")
		assert.Contains(t, combined, `git -C "${{ github.workspace }}/./libs" config --local --unset-all remote.origin.partialclonefilter || true`, "additional checkout repair should clear partial clone filter")
	})

	t.Run("additional checkout without sparse-checkout does not emit filter", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./libs", Repository: "owner/libs"},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.NotContains(t, combined, "filter:", "should not emit filter when no sparse-checkout")
	})
}

// TestParseCheckoutConfigs verifies parsing of raw frontmatter values.
func TestParseCheckoutConfigs(t *testing.T) {
	t.Run("nil returns nil", func(t *testing.T) {
		configs, err := ParseCheckoutConfigs(nil)
		require.NoError(t, err, "nil should not error")
		assert.Nil(t, configs, "nil input should return nil configs")
	})

	t.Run("single object with github-token", func(t *testing.T) {
		raw := map[string]any{
			"fetch-depth":  float64(0),
			"github-token": "${{ secrets.MY_TOKEN }}",
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "single object should parse without error")
		require.Len(t, configs, 1, "should produce one config")
		assert.Equal(t, "${{ secrets.MY_TOKEN }}", configs[0].GitHubToken, "github-token should be set")
		require.NotNil(t, configs[0].FetchDepth, "fetch-depth should be set")
		assert.Equal(t, 0, *configs[0].FetchDepth, "fetch-depth should be 0")
	})

	t.Run("negative fetch-depth returns error", func(t *testing.T) {
		for _, depth := range []float64{-1, -999999} {
			raw := map[string]any{
				"fetch-depth": depth,
			}
			_, err := ParseCheckoutConfigs(raw)
			require.Error(t, err)
			require.ErrorContains(t, err, "checkout.fetch-depth must be >= 0")
		}
	})

	t.Run("backward compat: token key still works", func(t *testing.T) {
		raw := map[string]any{
			"token": "${{ secrets.MY_TOKEN }}",
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "legacy token key should parse without error")
		require.Len(t, configs, 1, "should produce one config")
		assert.Equal(t, "${{ secrets.MY_TOKEN }}", configs[0].GitHubToken, "legacy token should populate GitHubToken")
	})

	t.Run("github-app config is parsed", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/target-repo",
			"github-app": map[string]any{
				"app-id":      "${{ vars.APP_ID }}",
				"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
			},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "github-app config should parse without error")
		require.Len(t, configs, 1)
		require.NotNil(t, configs[0].GitHubApp, "github-app config should be set")
		assert.Equal(t, "${{ vars.APP_ID }}", configs[0].GitHubApp.AppID, "app-id should be set")
		assert.Equal(t, "${{ secrets.APP_PRIVATE_KEY }}", configs[0].GitHubApp.PrivateKey, "private-key should be set")
	})

	t.Run("github-app config with owner and repositories", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/target-repo",
			"github-app": map[string]any{
				"app-id":       "${{ vars.APP_ID }}",
				"private-key":  "${{ secrets.APP_PRIVATE_KEY }}",
				"owner":        "my-org",
				"repositories": []any{"repo-a", "repo-b"},
			},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "github-app config with owner should parse without error")
		require.Len(t, configs, 1)
		require.NotNil(t, configs[0].GitHubApp)
		assert.Equal(t, "my-org", configs[0].GitHubApp.Owner)
		assert.Equal(t, []string{"repo-a", "repo-b"}, configs[0].GitHubApp.Repositories)
	})

	t.Run("github-app config accepts client-id", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/target-repo",
			"github-app": map[string]any{
				"client-id":   "${{ vars.CLIENT_ID }}",
				"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
			},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "github-app config with client-id should parse without error")
		require.Len(t, configs, 1)
		require.NotNil(t, configs[0].GitHubApp, "github-app config should be set")
		assert.Equal(t, "${{ vars.CLIENT_ID }}", configs[0].GitHubApp.AppID, "client-id should populate AppID")
	})

	t.Run("safe-outputs-github-app config is parsed", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/target-repo",
			"safe-outputs-github-app": map[string]any{
				"client-id":   "${{ vars.SO_CLIENT_ID }}",
				"private-key": "${{ secrets.SO_APP_PRIVATE_KEY }}",
			},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "safe-outputs-github-app config should parse without error")
		require.Len(t, configs, 1)
		require.NotNil(t, configs[0].SafeOutputGitHubApp, "safe-outputs-github-app config should be set")
		assert.Equal(t, "${{ vars.SO_CLIENT_ID }}", configs[0].SafeOutputGitHubApp.AppID, "client-id should populate AppID")
		assert.Equal(t, "${{ secrets.SO_APP_PRIVATE_KEY }}", configs[0].SafeOutputGitHubApp.PrivateKey, "private-key should be set")
	})

	t.Run("safe-output-github-app is rejected", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/target-repo",
			"safe-output-github-app": map[string]any{
				"app-id":      "${{ vars.SO_APP_ID }}",
				"private-key": "${{ secrets.SO_APP_PRIVATE_KEY }}",
			},
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "safe-output-github-app should be rejected")
		require.ErrorContains(t, err, "checkout.safe-output-github-app is not supported; use checkout.safe-outputs-github-app")
	})

	t.Run("github-token and github-app are mutually exclusive", func(t *testing.T) {
		raw := map[string]any{
			"github-token": "${{ secrets.MY_TOKEN }}",
			"github-app": map[string]any{
				"app-id":      "${{ vars.APP_ID }}",
				"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
			},
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "github-token and github-app together should return error")
		require.ErrorContains(t, err, "mutually exclusive", "error should mention mutual exclusivity")
	})

	t.Run("github-app config missing app-id returns error", func(t *testing.T) {
		raw := map[string]any{
			"github-app": map[string]any{
				"private-key": "${{ secrets.APP_PRIVATE_KEY }}",
			},
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "github-app without app-id should return error")
		require.ErrorContains(t, err, "client-id (or app-id) and private-key")
	})

	t.Run("github-app config missing private-key returns error", func(t *testing.T) {
		raw := map[string]any{
			"github-app": map[string]any{
				"app-id": "${{ vars.APP_ID }}",
			},
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "github-app without private-key should return error")
		require.ErrorContains(t, err, "client-id (or app-id) and private-key")
	})

	t.Run("github-app must be an object", func(t *testing.T) {
		raw := map[string]any{
			"github-app": "not-an-object",
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "non-object github-app should return error")
		require.ErrorContains(t, err, "checkout.github-app must be an object")
	})

	t.Run("array of objects", func(t *testing.T) {
		raw := []any{
			map[string]any{"path": "."},
			map[string]any{"repository": "owner/repo", "path": "./libs"},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "array should parse without error")
		require.Len(t, configs, 2, "should produce two configs")
		assert.Empty(t, configs[0].Path, "first path should be normalized from '.' to empty")
		assert.Equal(t, "owner/repo", configs[1].Repository, "second repo should be set")
	})

	t.Run("invalid type returns error", func(t *testing.T) {
		_, err := ParseCheckoutConfigs("invalid")
		assert.Error(t, err, "string value should return an error")
	})

	t.Run("array with non-object entry returns error", func(t *testing.T) {
		raw := []any{"not-an-object"}
		_, err := ParseCheckoutConfigs(raw)
		assert.Error(t, err, "array with non-object entry should return error")
	})

	t.Run("submodules as bool true", func(t *testing.T) {
		raw := map[string]any{"submodules": true}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Equal(t, "true", configs[0].Submodules, "bool true should convert to string 'true'")
	})

	t.Run("submodules as bool false", func(t *testing.T) {
		raw := map[string]any{"submodules": false}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Equal(t, "false", configs[0].Submodules, "bool false should convert to string 'false'")
	})

	t.Run("submodules as string recursive", func(t *testing.T) {
		raw := map[string]any{"submodules": "recursive"}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Equal(t, "recursive", configs[0].Submodules, "string should be preserved")
	})
}

// TestDeeperFetchDepth tests the fetch-depth comparison logic.
func TestDeeperFetchDepth(t *testing.T) {
	ptr := func(n int) *int { return &n }

	tests := []struct {
		name     string
		a, b     *int
		expected *int
	}{
		{"both nil returns nil", nil, nil, nil},
		{"a nil returns b", nil, ptr(5), ptr(5)},
		{"b nil returns a", ptr(5), nil, ptr(5)},
		{"0 beats positive", ptr(0), ptr(5), ptr(0)},
		{"positive beats 0 (reversed)", ptr(5), ptr(0), ptr(0)},
		{"larger positive wins", ptr(3), ptr(10), ptr(10)},
		{"smaller positive loses", ptr(10), ptr(3), ptr(10)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := deeperFetchDepth(tt.a, tt.b)
			if tt.expected == nil {
				assert.Nil(t, result, "should be nil")
			} else {
				require.NotNil(t, result, "should not be nil")
				assert.Equal(t, *tt.expected, *result, "should return correct value")
			}
		})
	}
}

// TestMergeSparsePatterns tests pattern deduplication and merging.
func TestMergeSparsePatterns(t *testing.T) {
	t.Run("merges unique patterns", func(t *testing.T) {
		result := mergeSparsePatterns([]string{".github/"}, "src/\ndocs/")
		assert.Equal(t, []string{".github/", "src/", "docs/"}, result, "should contain all unique patterns")
	})

	t.Run("deduplicates patterns", func(t *testing.T) {
		result := mergeSparsePatterns([]string{".github/"}, ".github/\nsrc/")
		assert.Equal(t, []string{".github/", "src/"}, result, "should deduplicate .github/")
	})

	t.Run("nil existing with new patterns", func(t *testing.T) {
		result := mergeSparsePatterns(nil, "src/\ndocs/")
		assert.Equal(t, []string{"src/", "docs/"}, result, "should return new patterns")
	})

	t.Run("empty new patterns preserves existing", func(t *testing.T) {
		result := mergeSparsePatterns([]string{"src/"}, "")
		assert.Equal(t, []string{"src/"}, result, "should preserve existing patterns")
	})
}

// TestCheckoutCurrentFlag verifies the current: true checkout flag behavior.
func TestCheckoutCurrentFlag(t *testing.T) {
	t.Run("parse current: true from single object", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/target-repo",
			"current":    true,
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "should parse without error")
		require.Len(t, configs, 1, "should produce one config")
		assert.True(t, configs[0].Current, "current flag should be true")
		assert.Equal(t, "owner/target-repo", configs[0].Repository, "repository should be set")
	})

	t.Run("parse current: false from map", func(t *testing.T) {
		raw := map[string]any{"current": false}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "should parse without error")
		require.Len(t, configs, 1)
		assert.False(t, configs[0].Current, "current flag should be false")
	})

	t.Run("invalid current type returns error", func(t *testing.T) {
		raw := map[string]any{"current": "yes"}
		_, err := ParseCheckoutConfigs(raw)
		assert.Error(t, err, "non-boolean current should return error")
	})

	t.Run("multiple current: true in array returns error", func(t *testing.T) {
		raw := []any{
			map[string]any{"repository": "owner/repo1", "path": "./r1", "current": true},
			map[string]any{"repository": "owner/repo2", "path": "./r2", "current": true},
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "multiple current: true should return error")
		require.ErrorContains(t, err, "only one checkout target may have current: true", "error should mention the constraint")
	})

	t.Run("single current: true in array is valid", func(t *testing.T) {
		raw := []any{
			map[string]any{"path": "."},
			map[string]any{"repository": "owner/target", "path": "./target", "current": true},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "single current: true in array should be valid")
		require.Len(t, configs, 2)
		assert.False(t, configs[0].Current, "first checkout should not be current")
		assert.True(t, configs[1].Current, "second checkout should be current")
	})
}

// TestBuildCheckoutsPromptContent verifies the prompt content generation for the checkout list.
func TestBuildCheckoutsPromptContent(t *testing.T) {
	t.Run("nil slice returns empty string", func(t *testing.T) {
		assert.Empty(t, buildCheckoutsPromptContent(nil), "nil should return empty string")
	})

	t.Run("empty slice returns empty string", func(t *testing.T) {
		assert.Empty(t, buildCheckoutsPromptContent([]*CheckoutConfig{}), "empty slice should return empty string")
	})

	t.Run("default checkout with no repo uses github.repository expression and cwd", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{},
		})
		assert.Contains(t, content, "${{ github.workspace }}", "should resolve the full workspace path for root checkout at runtime")
		assert.Contains(t, content, "(cwd)", "root checkout should be marked as cwd")
		assert.Contains(t, content, "${{ github.repository }}", "should reference github.repository expression for default checkout")
	})

	t.Run("nested checkout of host repo shows runtime-resolved absolute path", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "myorg/myrepo", Path: "myrepo"},
		})
		assert.Contains(t, content, "repo `myorg/myrepo` → `${{ github.workspace }}/myrepo`", "should present the nested self-repository checkout with a runtime-resolved absolute path")
		assert.Contains(t, content, "myorg/myrepo", "should show the configured repo")
		assert.NotContains(t, content, "github.repository", "should not include github.repository expression for explicit repo")
		assert.NotContains(t, content, "(cwd)", "non-root checkout should not be marked as cwd")
	})

	t.Run("current checkout is marked", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/target", Path: "./target", Current: true},
		})
		assert.Contains(t, content, "**current**", "current checkout should be marked")
		assert.Contains(t, content, "this is the repository you are working on", "current checkout should have instructions")
	})

	t.Run("non-current checkout is not marked", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/libs", Path: "./libs"},
		})
		assert.NotContains(t, content, "**current**", "non-current checkout should not be marked")
	})

	t.Run("multiple checkouts all listed", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Path: ""},
			{Repository: "owner/target", Path: "./target", Current: true},
			{Repository: "owner/libs", Path: "./libs"},
		})
		assert.Contains(t, content, "${{ github.workspace }}", "should include workspace root for root checkout")
		assert.Contains(t, content, "(cwd)", "root checkout should be marked as cwd")
		assert.Contains(t, content, "${{ github.workspace }}/target", "should include full path for target checkout")
		assert.Contains(t, content, "owner/target", "should include target repo")
		assert.Contains(t, content, "${{ github.workspace }}/libs", "should include full path for libs checkout")
		assert.Contains(t, content, "owner/libs", "should include libs repo")
		assert.Contains(t, content, "**current**", "current checkout should be marked")
	})

	t.Run("default fetch-depth annotation shows shallow clone", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo"},
		})
		assert.Contains(t, content, "shallow clone, fetch-depth=1 (default)", "should show default shallow clone annotation")
	})

	t.Run("fetch-depth 0 annotation shows full history", func(t *testing.T) {
		depth := 0
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo", FetchDepth: &depth},
		})
		assert.Contains(t, content, "full history", "should show full history annotation")
	})

	t.Run("non-zero fetch-depth annotation shows value", func(t *testing.T) {
		depth := 50
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo", FetchDepth: &depth},
		})
		assert.Contains(t, content, "fetch-depth=50", "should show configured fetch-depth")
	})

	t.Run("fetch refs are listed in prompt", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo", Fetch: []string{"refs/pulls/open/*", "main"}},
		})
		assert.Contains(t, content, "additional refs fetched", "should mention additional refs")
		assert.Contains(t, content, "refs/pulls/open/*", "should list the refs/pulls/open/* pattern")
		assert.Contains(t, content, "main", "should list the main branch")
	})

	t.Run("sparse checkout is annotated in prompt notes", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo", SparseCheckout: ".github/\nsrc/"},
		})
		assert.Contains(t, content, "sparse checkout enabled", "should indicate sparse checkout when configured")
	})

	t.Run("unavailable branch note is always present", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo"},
		})
		assert.Contains(t, content, "separate shallow, credential-free checkout", "should distinguish the workspace root from configured checkouts")
		assert.Contains(t, content, "confirm your working directory matches that path", "should require checking the working directory first")
		assert.Contains(t, content, "has NOT been checked out", "should mention branches that are not checked out")
		assert.Contains(t, content, "fetch:", "should mention the fetch option for resolution")
	})

	t.Run("no credentials warning is always present", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo"},
		})
		assert.Contains(t, content, "No git credentials are available", "should warn that git credentials are absent")
		assert.Contains(t, content, "git fetch", "should mention that git fetch will fail")
		assert.Contains(t, content, "authentication will not succeed", "should explain that authentication attempts will fail")
	})

	t.Run("no credentials warning present for sparse checkout", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/repo", SparseCheckout: ".github/\nsrc/"},
		})
		assert.Contains(t, content, "No git credentials are available", "should warn about credentials even for sparse checkouts")
		assert.Contains(t, content, "partial/blobless clones", "should mention partial clone risk for sparse checkouts")
	})
}

// TestParseFetchField verifies parsing of the fetch field in checkout configuration.
func TestParseFetchField(t *testing.T) {
	t.Run("fetch as array of strings", func(t *testing.T) {
		raw := map[string]any{
			"fetch": []any{"*", "refs/pulls/open/*"},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "should parse without error")
		require.Len(t, configs, 1)
		assert.Equal(t, []string{"*", "refs/pulls/open/*"}, configs[0].Fetch, "fetch should be set")
	})

	t.Run("fetch as single string", func(t *testing.T) {
		raw := map[string]any{
			"fetch": "*",
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "single string fetch should parse without error")
		require.Len(t, configs, 1)
		assert.Equal(t, []string{"*"}, configs[0].Fetch, "single string should become a one-element slice")
	})

	t.Run("fetch with specific branch names", func(t *testing.T) {
		raw := map[string]any{
			"fetch": []any{"main", "feature/my-branch"},
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Equal(t, []string{"main", "feature/my-branch"}, configs[0].Fetch)
	})

	t.Run("invalid fetch type returns error", func(t *testing.T) {
		raw := map[string]any{
			"fetch": 42,
		}
		_, err := ParseCheckoutConfigs(raw)
		assert.Error(t, err, "integer fetch should return error")
	})

	t.Run("fetch array with non-string element returns error", func(t *testing.T) {
		raw := map[string]any{
			"fetch": []any{"main", 123},
		}
		_, err := ParseCheckoutConfigs(raw)
		assert.Error(t, err, "array with non-string entry should return error")
	})

	t.Run("fetch absent means no fetch refs", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/repo",
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err)
		require.Len(t, configs, 1)
		assert.Empty(t, configs[0].Fetch, "absent fetch should produce empty slice")
	})
}

// TestFetchRefToRefspec verifies the refspec expansion logic.
func TestFetchRefToRefspec(t *testing.T) {
	tests := []struct {
		pattern  string
		expected string
	}{
		{"*", "+refs/heads/*:refs/remotes/origin/*"},
		{"refs/pulls/open/*", "+refs/pull/*/head:refs/remotes/origin/pull/*/head"},
		{"main", "+refs/heads/main:refs/remotes/origin/main"},
		{"feature/my-branch", "+refs/heads/feature/my-branch:refs/remotes/origin/feature/my-branch"},
		{"feature/*", "+refs/heads/feature/*:refs/remotes/origin/feature/*"},
	}
	for _, tt := range tests {
		t.Run(tt.pattern, func(t *testing.T) {
			got := fetchRefToRefspec(tt.pattern)
			assert.Equal(t, tt.expected, got, "refspec should match expected value")
		})
	}
}

// TestMergeFetchRefs verifies that fetch ref lists are properly unioned.
func TestMergeFetchRefs(t *testing.T) {
	t.Run("union of two disjoint sets", func(t *testing.T) {
		result := mergeFetchRefs([]string{"*"}, []string{"refs/pulls/open/*"})
		assert.Equal(t, []string{"*", "refs/pulls/open/*"}, result)
	})

	t.Run("removes duplicate refs", func(t *testing.T) {
		result := mergeFetchRefs([]string{"main"}, []string{"main", "develop"})
		assert.Equal(t, []string{"main", "develop"}, result)
	})

	t.Run("nil existing returns new refs", func(t *testing.T) {
		result := mergeFetchRefs(nil, []string{"*"})
		assert.Equal(t, []string{"*"}, result)
	})
}

// TestGenerateFetchStep verifies that the git fetch YAML step is generated correctly.
func TestGenerateFetchStep(t *testing.T) {
	t.Run("no fetch refs returns empty string", func(t *testing.T) {
		entry := &resolvedCheckout{}
		got := generateFetchStepLines(entry, 0)
		assert.Empty(t, got, "empty fetchRefs should produce no step")
	})

	t.Run("fetch all branches uses star refspec", func(t *testing.T) {
		entry := &resolvedCheckout{
			fetchRefs: []string{"*"},
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "Fetch additional refs", "should include step name")
		assert.Contains(t, got, "+refs/heads/*:refs/remotes/origin/*", "should include correct refspec")
		assert.Contains(t, got, "GH_AW_FETCH_TOKEN", "should set fetch token env var")
		assert.Contains(t, got, "http.extraheader=Authorization:", "should configure credentials via http.extraheader")
		// When no custom token set, falls back to the effective GitHub token chain
		assert.Contains(t, got, "GH_AW_GITHUB_TOKEN", "should fall back to GH_AW token chain when no checkout token set")
		// base64 must use -w 0 to prevent line wrapping with long tokens (e.g. fine-grained PATs)
		assert.Contains(t, got, "base64 -w 0", "should use base64 -w 0 to disable line wrapping")
	})

	t.Run("fetch refs/pulls/open/* uses PR refspec", func(t *testing.T) {
		entry := &resolvedCheckout{
			fetchRefs: []string{"refs/pulls/open/*"},
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "+refs/pull/*/head:refs/remotes/origin/pull/*/head", "should include PR refspec")
	})

	t.Run("custom token is used in fetch step", func(t *testing.T) {
		entry := &resolvedCheckout{
			fetchRefs: []string{"main"},
			token:     "${{ secrets.MY_PAT }}",
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "${{ secrets.MY_PAT }}", "should use custom token from checkout config")
		assert.NotContains(t, got, "github.token", "should not fall back to github.token when custom token set")
	})

	t.Run("repository name used in step name", func(t *testing.T) {
		entry := &resolvedCheckout{
			key:       checkoutKey{repository: "owner/side-repo"},
			fetchRefs: []string{"main"},
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "Fetch additional refs for owner/side-repo", "should include repo in step name")
	})

	t.Run("non-root path uses -C flag", func(t *testing.T) {
		entry := &resolvedCheckout{
			key:       checkoutKey{path: "libs/other"},
			fetchRefs: []string{"main"},
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, `-C "${{ github.workspace }}/libs/other"`, "should use -C flag for non-root path")
	})

	t.Run("root path does not add -C flag", func(t *testing.T) {
		entry := &resolvedCheckout{
			fetchRefs: []string{"main"},
		}
		got := generateFetchStepLines(entry, 0)
		assert.NotContains(t, got, "-C ", "root checkout should not use -C flag")
	})

	t.Run("multiple refspecs in single command", func(t *testing.T) {
		entry := &resolvedCheckout{
			fetchRefs: []string{"*", "refs/pulls/open/*"},
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "+refs/heads/*:refs/remotes/origin/*", "should include branches refspec")
		assert.Contains(t, got, "+refs/pull/*/head:refs/remotes/origin/pull/*/head", "should include PR refspec")
	})

	t.Run("default depth adds --depth=1 to prevent full-history fetch", func(t *testing.T) {
		// nil fetchDepth = default shallow clone (depth 1); fetch must not expand history
		entry := &resolvedCheckout{
			fetchRefs: []string{"main"},
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "--depth=1", "default shallow checkout should pass --depth=1 to git fetch")
	})

	t.Run("explicit depth=1 adds --depth=1", func(t *testing.T) {
		depth := 1
		entry := &resolvedCheckout{
			fetchRefs:  []string{"main"},
			fetchDepth: &depth,
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "--depth=1", "explicit fetch-depth: 1 should pass --depth=1 to git fetch")
	})

	t.Run("explicit depth=0 (full history) omits --depth flag", func(t *testing.T) {
		depth := 0
		entry := &resolvedCheckout{
			fetchRefs:  []string{"main"},
			fetchDepth: &depth,
		}
		got := generateFetchStepLines(entry, 0)
		assert.NotContains(t, got, "--depth", "fetch-depth: 0 (full history) should not add --depth flag to git fetch")
	})

	t.Run("explicit depth=N adds --depth=N", func(t *testing.T) {
		depth := 10
		entry := &resolvedCheckout{
			fetchRefs:  []string{"main"},
			fetchDepth: &depth,
		}
		got := generateFetchStepLines(entry, 0)
		assert.Contains(t, got, "--depth=10", "fetch-depth: 10 should pass --depth=10 to git fetch")
	})
}

// TestCheckoutManagerFetchMerging verifies that fetch refs are merged correctly.
func TestCheckoutManagerFetchMerging(t *testing.T) {
	t.Run("fetch refs are merged for same checkout", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "./r", Fetch: []string{"main"}},
			{Repository: "owner/repo", Path: "./r", Fetch: []string{"develop"}},
		})
		require.Len(t, cm.ordered, 1, "same (repo, path) should merge")
		assert.Equal(t, []string{"main", "develop"}, cm.ordered[0].fetchRefs, "fetch refs should be unioned")
	})

	t.Run("fetch refs preserved when no merge", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "./r", Fetch: []string{"*", "refs/pulls/open/*"}},
		})
		assert.Equal(t, []string{"*", "refs/pulls/open/*"}, cm.ordered[0].fetchRefs)
	})
}

// TestGenerateAdditionalCheckoutStepsWithFetch verifies that fetch steps are
// appended after additional checkout steps when fetch refs are configured.
func TestGenerateAdditionalCheckoutStepsWithFetch(t *testing.T) {
	getPin := func(action string) string { return action + "@v4" }

	t.Run("fetch step appended after checkout step", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/side-repo", Path: "./side", Fetch: []string{"refs/pulls/open/*"}},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "repository: owner/side-repo", "should include checkout step")
		assert.Contains(t, combined, "Fetch additional refs for owner/side-repo", "should include fetch step")
		assert.Contains(t, combined, "+refs/pull/*/head:refs/remotes/origin/pull/*/head", "should include PR refspec")
	})

	t.Run("no fetch step when fetch not configured", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/side-repo", Path: "./side"},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.NotContains(t, combined, "Fetch additional refs", "should not include fetch step without fetch config")
	})
}

// TestGenerateDefaultCheckoutStepWithFetch verifies that fetch steps are appended
// after the default checkout step when fetch refs are configured.
func TestGenerateDefaultCheckoutStepWithFetch(t *testing.T) {
	getPin := func(action string) string { return action + "@v4" }

	t.Run("fetch step appended after default checkout", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Fetch: []string{"*"}},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "Checkout repository", "should include default checkout step")
		assert.Contains(t, combined, "Fetch additional refs", "should include fetch step")
		assert.Contains(t, combined, "+refs/heads/*:refs/remotes/origin/*", "should include all-branches refspec")
	})

	t.Run("no fetch step when fetch not configured on default checkout", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.NotContains(t, combined, "Fetch additional refs", "should not include fetch step without config")
	})

	t.Run("fetch with custom github-token uses that token", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_PAT }}", Fetch: []string{"refs/pulls/open/*"}},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		// Token should appear both in the checkout step and the fetch env var
		assert.Contains(t, combined, "${{ secrets.MY_PAT }}", "custom token should be in output")
		assert.Contains(t, combined, "+refs/pull/*/head:refs/remotes/origin/pull/*/head", "PR refspec should be present")
	})
}

func TestHasAppAuth(t *testing.T) {
	t.Run("returns false when no app configured", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_PAT }}"},
		})
		assert.False(t, cm.HasAppAuth(), "should be false when no app is configured")
	})

	t.Run("returns false for nil configs", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		assert.False(t, cm.HasAppAuth(), "should be false for nil configs")
	})

	t.Run("returns true when default checkout has app", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubApp: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.KEY }}"}},
		})
		assert.True(t, cm.HasAppAuth(), "should be true when default checkout has app")
	})

	t.Run("returns true when additional checkout has app", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_PAT }}"},
			{Repository: "other/repo", Path: "deps", GitHubApp: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.KEY }}"}},
		})
		assert.True(t, cm.HasAppAuth(), "should be true when any checkout has app")
	})
}

func TestHasSafeOutputAppAuth(t *testing.T) {
	t.Run("returns false when no safe-output app configured", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_PAT }}"},
		})
		assert.False(t, cm.HasSafeOutputAppAuth(), "should be false when no safe-output app is configured")
	})

	t.Run("returns true when checkout has safe-output app", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{
				Repository: "owner/target-repo",
				SafeOutputGitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.APP_ID }}",
					PrivateKey: "${{ secrets.KEY }}",
				},
			},
		})
		assert.True(t, cm.HasSafeOutputAppAuth(), "should be true when any checkout has safe-output app")
	})
}

func TestResolveSafeOutputCheckoutTokenExpression(t *testing.T) {
	t.Run("uses target-repo matching checkout safe-output app", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/a", Path: "./a"},
			{
				Repository: "owner/b",
				Path:       "./b",
				SafeOutputGitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.APP_ID }}",
					PrivateKey: "${{ secrets.KEY }}",
				},
			},
		})
		token, ok := cm.ResolveSafeOutputCheckoutTokenExpression("owner/b")
		require.True(t, ok)
		assert.Equal(t, "${{ steps.checkout-safe-output-app-token-1.outputs.token }}", token)
	})

	t.Run("falls back to current checkout when target repo not provided", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{
				Repository: "owner/current",
				Current:    true,
				SafeOutputGitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.APP_ID }}",
					PrivateKey: "${{ secrets.KEY }}",
				},
			},
		})
		token, ok := cm.ResolveSafeOutputCheckoutTokenExpression("")
		require.True(t, ok)
		assert.Equal(t, "${{ steps.checkout-safe-output-app-token-0.outputs.token }}", token)
	})

	t.Run("ignore-if-missing uses default safe output token fallback", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{
				Repository: "owner/current",
				Current:    true,
				SafeOutputGitHubApp: &GitHubAppConfig{
					AppID:           "${{ vars.APP_ID }}",
					PrivateKey:      "${{ secrets.KEY }}",
					IgnoreIfMissing: true,
				},
			},
		})
		token, ok := cm.ResolveSafeOutputCheckoutTokenExpression("")
		require.True(t, ok)
		assert.Equal(
			t,
			"${{ steps.checkout-safe-output-app-token-0.outputs.token || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}",
			token,
		)
	})
}

func TestGenerateSafeOutputCheckoutAppTokenSteps(t *testing.T) {
	compiler := NewCompiler()
	permissions := NewPermissions()

	cm := NewCheckoutManager([]*CheckoutConfig{
		{
			Repository: "owner/target",
			SafeOutputGitHubApp: &GitHubAppConfig{
				AppID:      "${{ vars.APP_ID }}",
				PrivateKey: "${{ secrets.APP_KEY }}",
			},
		},
	})

	steps := cm.GenerateSafeOutputCheckoutAppTokenSteps(compiler, permissions)
	require.NotEmpty(t, steps)
	combined := strings.Join(steps, "")
	assert.Contains(t, combined, "id: checkout-safe-output-app-token-0")
	assert.Contains(t, combined, "Generate safe_outputs GitHub App token for checkout (0)")
}

func TestDefaultCheckoutWithAppAuth(t *testing.T) {
	getPin := func(ref string) string { return ref }

	t.Run("checkout step uses app token reference", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubApp: &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.KEY }}"}},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		// Token is now minted in the agent job itself (same-job step reference)
		assert.Contains(t, combined, "steps.checkout-app-token-0.outputs.token", "checkout should reference step output in same job")
		assert.NotContains(t, combined, "needs.activation.outputs.checkout_app_token_0", "checkout must not reference activation job outputs (masked values are dropped by runner)")
	})
}

func TestAdditionalCheckoutWithAppAuth(t *testing.T) {
	getPin := func(ref string) string { return ref }

	t.Run("additional checkout uses app token reference", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_PAT }}"}, // default checkout
			{
				Repository: "other/repo",
				Path:       "deps",
				GitHubApp:  &GitHubAppConfig{AppID: "${{ vars.APP_ID }}", PrivateKey: "${{ secrets.KEY }}"},
			},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		// Token is now minted in the agent job itself (same-job step reference)
		assert.Contains(t, combined, "steps.checkout-app-token-1.outputs.token", "additional checkout should reference step output at index 1")
		assert.NotContains(t, combined, "needs.activation.outputs.checkout_app_token_1", "checkout must not reference activation job outputs (masked values are dropped by runner)")
		assert.Contains(t, combined, "other/repo", "should reference the additional repo")
	})
}

// TestCrossRepoTargetRepo verifies the SetCrossRepoTargetRepo/GetCrossRepoTargetRepo lifecycle.
func TestCrossRepoTargetRepo(t *testing.T) {
	t.Run("default is empty string (same-repo)", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		assert.Empty(t, cm.GetCrossRepoTargetRepo(), "new checkout manager should have no cross-repo target")
	})

	t.Run("activation job expression is stored and retrievable", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetCrossRepoTargetRepo("${{ steps.resolve-host-repo.outputs.target_repo }}")
		assert.Equal(t, "${{ steps.resolve-host-repo.outputs.target_repo }}", cm.GetCrossRepoTargetRepo())
	})

	t.Run("downstream job expression (needs.activation.outputs) is stored and retrievable", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetCrossRepoTargetRepo("${{ needs.activation.outputs.target_repo }}")
		assert.Equal(t, "${{ needs.activation.outputs.target_repo }}", cm.GetCrossRepoTargetRepo())
	})

	t.Run("GenerateGitHubFolderCheckoutStep uses stored value", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetCrossRepoTargetRepo("${{ needs.activation.outputs.target_repo }}")

		lines := cm.GenerateGitHubFolderCheckoutStep(cm.GetCrossRepoTargetRepo(), "", "", getActionPin)
		combined := strings.Join(lines, "")

		assert.Contains(t, combined, "repository: ${{ needs.activation.outputs.target_repo }}",
			"checkout step should use the cross-repo target")
	})
}

// TestCrossRepoTargetRef verifies the SetCrossRepoTargetRef/GetCrossRepoTargetRef lifecycle
// and that GenerateGitHubFolderCheckoutStep emits a ref: field when a ref is provided.
func TestCrossRepoTargetRef(t *testing.T) {
	t.Run("default is empty string", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		assert.Empty(t, cm.GetCrossRepoTargetRef(), "new checkout manager should have no cross-repo ref")
	})

	t.Run("activation job ref expression is stored and retrievable", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetCrossRepoTargetRef("${{ steps.resolve-host-repo.outputs.target_ref }}")
		assert.Equal(t, "${{ steps.resolve-host-repo.outputs.target_ref }}", cm.GetCrossRepoTargetRef())
	})

	t.Run("downstream job ref expression (needs.activation.outputs) is stored and retrievable", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetCrossRepoTargetRef("${{ needs.activation.outputs.target_ref }}")
		assert.Equal(t, "${{ needs.activation.outputs.target_ref }}", cm.GetCrossRepoTargetRef())
	})

	t.Run("GenerateGitHubFolderCheckoutStep emits ref: when ref is provided", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		cm.SetCrossRepoTargetRepo("${{ steps.resolve-host-repo.outputs.target_repo }}")
		cm.SetCrossRepoTargetRef("${{ steps.resolve-host-repo.outputs.target_ref }}")

		lines := cm.GenerateGitHubFolderCheckoutStep(cm.GetCrossRepoTargetRepo(), cm.GetCrossRepoTargetRef(), "", getActionPin)
		combined := strings.Join(lines, "")

		assert.Contains(t, combined, "repository: ${{ steps.resolve-host-repo.outputs.target_repo }}",
			"checkout step should include repository field")
		assert.Contains(t, combined, "ref: ${{ steps.resolve-host-repo.outputs.target_ref }}",
			"checkout step should include ref field")
	})

	t.Run("GenerateGitHubFolderCheckoutStep omits ref: when ref is empty", func(t *testing.T) {
		cm := NewCheckoutManager(nil)

		lines := cm.GenerateGitHubFolderCheckoutStep("org/repo", "", "", getActionPin)
		combined := strings.Join(lines, "")

		assert.NotContains(t, combined, "ref:", "checkout step should not include ref field when empty")
	})
}

// TestHasExternalRootCheckout verifies detection of external checkouts targeting the workspace root.
func TestHasExternalRootCheckout(t *testing.T) {
	t.Run("returns false for nil configs", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		assert.False(t, cm.HasExternalRootCheckout(), "should be false for nil configs")
	})

	t.Run("returns false for empty configs", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{})
		assert.False(t, cm.HasExternalRootCheckout(), "should be false for empty configs")
	})

	t.Run("returns false for default checkout only (no repository)", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{GitHubToken: "${{ secrets.MY_PAT }}"},
		})
		assert.False(t, cm.HasExternalRootCheckout(), "should be false when only default checkout is configured")
	})

	t.Run("returns false for external checkout with non-root path", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "other/repo", Path: "libs/other"},
		})
		assert.False(t, cm.HasExternalRootCheckout(), "should be false when external repo uses a subdirectory path")
	})

	t.Run("returns true for external checkout without path (workspace root)", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "githubnext/gh-aw-side-repo", GitHubToken: "${{ secrets.SIDE_REPO_PAT }}"},
		})
		assert.True(t, cm.HasExternalRootCheckout(), "should be true when external repo checks out to workspace root")
	})

	t.Run("returns true for external checkout with explicit dot path", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "other/repo", Path: "."},
		})
		assert.True(t, cm.HasExternalRootCheckout(), "should be true when external repo uses '.' as path (workspace root)")
	})

	t.Run("returns true when one of multiple checkouts targets external root", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "other/repo", Path: "libs/other"},
			{Repository: "githubnext/gh-aw-side-repo"},
		})
		assert.True(t, cm.HasExternalRootCheckout(), "should be true when any checkout targets external root")
	})
}

func TestGetCurrentCheckoutPath(t *testing.T) {
	t.Run("returns empty when no current checkout is configured", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "repo"},
		})
		assert.Empty(t, cm.GetCurrentCheckoutPath())
	})

	t.Run("returns empty when current checkout is workspace root", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Current: true, Path: "."},
		})
		assert.Empty(t, cm.GetCurrentCheckoutPath())
	})

	t.Run("returns normalized subdirectory when current checkout is non-root", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "caido/proxy-frontend", Current: true, Path: "./proxy-frontend"},
		})
		assert.Equal(t, "proxy-frontend", cm.GetCurrentCheckoutPath())
	})
}

// TestWikiCheckout verifies wiki: true support across parsing, deduplication, and step generation.
func TestWikiCheckout(t *testing.T) {
	getPin := func(action string) string { return action + "@v4" }

	t.Run("parse wiki true", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/repo",
			"wiki":       true,
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "should parse wiki: true without error")
		require.Len(t, configs, 1, "should produce one config")
		assert.True(t, configs[0].Wiki, "wiki field should be true")
	})

	t.Run("parse wiki false", func(t *testing.T) {
		raw := map[string]any{
			"repository": "owner/repo",
			"wiki":       false,
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "should parse wiki: false without error")
		require.Len(t, configs, 1, "should produce one config")
		assert.False(t, configs[0].Wiki, "wiki field should be false")
	})

	t.Run("wiki must be boolean", func(t *testing.T) {
		raw := map[string]any{
			"wiki": "yes",
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "non-boolean wiki should return error")
		require.ErrorContains(t, err, "checkout.wiki must be a boolean", "error message should mention wiki")
	})

	t.Run("parse force-clean-git-credentials true", func(t *testing.T) {
		raw := map[string]any{
			"force-clean-git-credentials": true,
		}
		configs, err := ParseCheckoutConfigs(raw)
		require.NoError(t, err, "should parse force-clean-git-credentials: true without error")
		require.Len(t, configs, 1, "should produce one config")
		assert.True(t, configs[0].CleanGitCredentials, "force-clean-git-credentials should be true")
	})

	t.Run("force-clean-git-credentials must be boolean", func(t *testing.T) {
		raw := map[string]any{
			"force-clean-git-credentials": "true",
		}
		_, err := ParseCheckoutConfigs(raw)
		require.Error(t, err, "non-boolean force-clean-git-credentials should return error")
		require.ErrorContains(t, err, "checkout.force-clean-git-credentials must be a boolean", "error message should mention force-clean-git-credentials")
	})

	t.Run("wiki and non-wiki checkouts of same repo and path are not merged", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "./wiki", Wiki: true},
			{Repository: "owner/repo", Path: "./wiki", Wiki: false},
		})
		assert.Len(t, cm.ordered, 2, "wiki and non-wiki checkouts must remain separate even with same repo and path")
	})

	t.Run("two wiki checkouts of same repo and path are merged", func(t *testing.T) {
		depth0 := 0
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "./wiki", Wiki: true},
			{Repository: "owner/repo", Path: "./wiki", Wiki: true, FetchDepth: &depth0},
		})
		assert.Len(t, cm.ordered, 1, "two wiki checkouts with same key should be merged")
		require.NotNil(t, cm.ordered[0].fetchDepth, "fetch depth should be merged")
		assert.Equal(t, 0, *cm.ordered[0].fetchDepth, "deeper fetch depth should win")
	})

	t.Run("additional checkout step uses .wiki repository", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "./wiki-content", Wiki: true},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "repository: owner/repo.wiki", "wiki checkout must use .wiki repository suffix")
		assert.NotContains(t, combined, "repository: owner/repo\n", "must not emit plain repo name")
	})

	t.Run("default checkout step uses .wiki repository when wiki true", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Wiki: true},
		})
		lines := cm.GenerateDefaultCheckoutStep(false, "", getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "repository: ${{ github.repository }}.wiki", "default wiki checkout must use github.repository.wiki")
	})

	t.Run("additional checkout step uses explicit .wiki repository when repo set", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/docs", Path: "./docs-wiki", Wiki: true},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "repository: owner/docs.wiki", "wiki checkout must append .wiki to explicit repo")
	})

	t.Run("wiki false does not affect repository name", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "./other"},
		})
		lines := cm.GenerateAdditionalCheckoutSteps(getPin)
		combined := strings.Join(lines, "")
		assert.Contains(t, combined, "repository: owner/repo\n", "non-wiki checkout must not have .wiki suffix")
		assert.NotContains(t, combined, "owner/repo.wiki", "non-wiki checkout must not have .wiki suffix")
	})

	t.Run("wiki checkout with explicit .wiki suffix merges with wiki checkout without suffix", func(t *testing.T) {
		depth0 := 0
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/repo", Path: "./wiki", Wiki: true},
			{Repository: "owner/repo.wiki", Path: "./wiki", Wiki: true, FetchDepth: &depth0},
		})
		assert.Len(t, cm.ordered, 1, "explicit .wiki suffix should be normalized and merged with the non-suffix wiki checkout")
		require.NotNil(t, cm.ordered[0].fetchDepth, "fetch depth should be merged from second config")
		assert.Equal(t, 0, *cm.ordered[0].fetchDepth, "deeper fetch depth should win")
		// The stored key should use the normalized (no-suffix) repo name
		assert.Equal(t, "owner/repo", cm.ordered[0].key.repository, "key should store normalized repo without .wiki suffix")
		assert.True(t, cm.ordered[0].key.wiki, "key should have wiki=true")
	})

	t.Run("wiki prompt content includes .wiki suffix annotation", func(t *testing.T) {
		content := buildCheckoutsPromptContent([]*CheckoutConfig{
			{Repository: "owner/docs", Path: "./wiki", Wiki: true},
		})
		assert.Contains(t, content, "owner/docs.wiki", "prompt must show .wiki suffix for wiki checkout")
		assert.Contains(t, content, "(wiki)", "prompt must annotate wiki checkout")
	})
}

// TestGenerateCheckoutManifestStep verifies the cross-repo manifest emitter.
// The manifest step is consumed by the safe-outputs MCP server to resolve a
// per-repo default branch without making any network calls at request time.
func TestGenerateCheckoutManifestStep(t *testing.T) {
	getActionPin := func(action string) string {
		return action + "@pin"
	}

	t.Run("no configs emits nothing", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		assert.Empty(t, cm.GenerateCheckoutManifestStep(getActionPin), "empty manager should not emit a manifest step")
	})

	t.Run("default-only checkout emits nothing", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "."},
		})
		assert.Empty(t, cm.GenerateCheckoutManifestStep(getActionPin), "manifest is for cross-repo entries only; default checkout should not produce one")
	})

	t.Run("path-only additional checkout (no repository) emits nothing", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./workspace"},
		})
		assert.Empty(t, cm.GenerateCheckoutManifestStep(getActionPin), "additional checkout without repository should not be in manifest")
	})

	t.Run("wiki cross-repo checkout is excluded", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/docs", Path: "./wiki", Wiki: true},
		})
		assert.Empty(t, cm.GenerateCheckoutManifestStep(getActionPin), "wiki checkouts must be excluded from manifest")
	})

	t.Run("cross-repo additional checkout emits entry", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/other", Path: "./other", GitHubToken: "${{ secrets.CROSS_REPO_PAT }}"},
		})
		steps := cm.GenerateCheckoutManifestStep(getActionPin)
		require.Len(t, steps, 1, "should emit one manifest step")
		out := steps[0]
		assert.Contains(t, out, "name: Build checkout manifest for safe-outputs handlers")
		assert.Contains(t, out, "uses: actions/github-script@pin")
		assert.Contains(t, out, "GH_TOKEN: ${{ secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}")
		assert.Contains(t, out, `GH_AW_CHECKOUT_MANIFEST_COUNT: "1"`)
		assert.Contains(t, out, `GH_AW_CHECKOUT_REPO_0: "owner/other"`)
		assert.Contains(t, out, `GH_AW_CHECKOUT_PATH_0: "./other"`)
		assert.Contains(t, out, "GH_AW_CHECKOUT_TOKEN_0: ${{ secrets.CROSS_REPO_PAT }}")
		assert.Contains(t, out, "build_checkout_manifest.cjs")
		assert.NotContains(t, out, "run: |", "manifest step should use github-script instead of shell run block")
	})

	t.Run("cross-repo root checkout (empty path) is included", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/other"},
		})
		steps := cm.GenerateCheckoutManifestStep(getActionPin)
		require.Len(t, steps, 1, "cross-repo root checkout must be in manifest")
		out := steps[0]
		assert.Contains(t, out, `GH_AW_CHECKOUT_REPO_0: "owner/other"`)
		assert.Contains(t, out, `GH_AW_CHECKOUT_PATH_0: ""`, "empty path should still be emitted (manifest consumer expects the key)")
	})

	t.Run("multiple cross-repo entries each get a jq update", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "owner/a", Path: "./a"},
			{Repository: "owner/b", Path: "./b"},
			{Path: "./local-only"},               // no repo → skipped
			{Repository: "owner/c", Path: "./c"}, // included
		})
		steps := cm.GenerateCheckoutManifestStep(getActionPin)
		require.Len(t, steps, 1)
		out := steps[0]
		assert.Contains(t, out, `GH_AW_CHECKOUT_MANIFEST_COUNT: "3"`)
		assert.Contains(t, out, `GH_AW_CHECKOUT_REPO_0: "owner/a"`)
		assert.Contains(t, out, `GH_AW_CHECKOUT_REPO_1: "owner/b"`)
		assert.Contains(t, out, `GH_AW_CHECKOUT_REPO_2: "owner/c"`)
		assert.NotContains(t, out, "./local-only", "path-only entries must not be in the manifest step")
	})

	t.Run("repository names containing single quotes are yaml-escaped", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "weird'owner/repo", Path: "./x"},
		})
		steps := cm.GenerateCheckoutManifestStep(getActionPin)
		require.Len(t, steps, 1)
		assert.Contains(t, steps[0], `GH_AW_CHECKOUT_REPO_0: "weird'owner/repo"`)
	})

	t.Run("dynamic repository expressions are emitted as raw env expressions", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "${{ github.event.inputs.trigger_ref }}", Path: "./target"},
		})
		steps := cm.GenerateCheckoutManifestStep(getActionPin)
		require.Len(t, steps, 1)
		assert.Contains(t, steps[0], "GH_AW_CHECKOUT_REPO_0: ${{ github.event.inputs.trigger_ref }}")
	})

	t.Run("github-app token expression preserves checkout index in manifest env", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Path: "./local"},
			{
				Repository: "owner/private",
				Path:       "./private",
				GitHubApp: &GitHubAppConfig{
					AppID:      "${{ vars.APP_ID }}",
					PrivateKey: "${{ secrets.APP_PRIVATE_KEY }}",
				},
			},
		})
		steps := cm.GenerateCheckoutManifestStep(getActionPin)
		require.Len(t, steps, 1)
		assert.Contains(t, steps[0], "GH_AW_CHECKOUT_TOKEN_0: ${{ steps.checkout-app-token-1.outputs.token }}")
	})
}

// TestGenerateConfigureGitCredentialsSteps verifies that the "Configure Git credentials"
// step never inlines GitHub Actions expressions directly into the shell run: block.
// Regression test for: compiler inlines workflow_dispatch input into generated step,
// tripping the template-injection scanner for target-repo workflows.
func TestGenerateConfigureGitCredentialsSteps(t *testing.T) {
	alwaysTrue := BuildBooleanLiteral(true)
	token := "${{ steps.safe-outputs-app-token.outputs.token }}"

	t.Run("single root repo emits simple script call (no multi-repo env vars)", func(t *testing.T) {
		cm := NewCheckoutManager(nil)
		steps := cm.GenerateConfigureGitCredentialsSteps(token, alwaysTrue)
		combined := strings.Join(steps, "")

		assert.Contains(t, combined, "name: Configure Git credentials")
		assert.Contains(t, combined, "GITHUB_REPOSITORY: ${{ github.repository }}")
		assert.Contains(t, combined, "GIT_TOKEN: "+token)
		assert.Contains(t, combined, `run: bash "${RUNNER_TEMP}/gh-aw/actions/configure_git_credentials.sh"`)
		assert.NotContains(t, combined, "GH_AW_SUBREPO_", "single-repo case must not emit subrepo env vars")
	})

	t.Run("multi-repo with literal repo name places it in env var", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "org/other-repo", Path: "./target-repo"},
		})
		steps := cm.GenerateConfigureGitCredentialsSteps(token, alwaysTrue)
		combined := strings.Join(steps, "")

		assert.Contains(t, combined, `GH_AW_SUBREPO_0: "org/other-repo"`, "literal repo must be in env var (quoted)")
		assert.Contains(t, combined, "${GH_AW_SUBREPO_0}.git", "shell command must reference the env var")
		assert.NotContains(t, combined, "org/other-repo.git", "literal repo must not be inlined in shell command")
		assert.Contains(t, combined, "GITHUB_REPOSITORY:", "root repo env var must be present")
		assert.Contains(t, combined, "GIT_TOKEN: "+token, "token env var must be present")
	})

	t.Run("multi-repo with expression-based repo does not inline expression in shell command (regression)", func(t *testing.T) {
		// Regression: v0.80.6 inlined the expression directly into the git remote set-url
		// command, which the template-injection scanner correctly rejected.
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "github/${{ github.event.inputs.target_repo }}", Path: "./target-repo"},
		})
		steps := cm.GenerateConfigureGitCredentialsSteps(token, alwaysTrue)
		combined := strings.Join(steps, "")

		// Expression must be in the env: block, not inlined in the shell command.
		assert.Contains(t, combined, "GH_AW_SUBREPO_0: github/${{ github.event.inputs.target_repo }}",
			"expression-based repo must be assigned to an env var")
		assert.Contains(t, combined, "${GH_AW_SUBREPO_0}.git",
			"shell command must reference env var, not the raw expression")
		assert.NotContains(t, combined, "github/${{ github.event.inputs.target_repo }}.git",
			"expression must not be inlined in the git remote set-url command (template injection risk)")
		// The bash comment must use the path, not the expression-based repo name.
		assert.Contains(t, combined, "# Re-authenticate git for ./target-repo",
			"comment must reference checkout path, never the raw expression")
		assert.NotContains(t, combined, "# Re-authenticate git for github/${{",
			"expression must not appear in bash comment (template injection risk)")
		assert.Contains(t, combined, "GITHUB_REPOSITORY:", "root repo env var must be present")
		assert.Contains(t, combined, "GIT_TOKEN: "+token, "token env var must be present")
		// No raw Actions expression may appear in the run: block.
		runIdx := strings.Index(combined, "run: |")
		if runIdx >= 0 {
			assert.NotContains(t, combined[runIdx:], "${{",
				"run: block must not contain any raw GitHub Actions expression")
		}
	})

	t.Run("multi-repo with expression-based path routes path through env var", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "org/other-repo", Path: "${{ github.event.inputs.target_path }}"},
		})
		steps := cm.GenerateConfigureGitCredentialsSteps(token, alwaysTrue)
		combined := strings.Join(steps, "")

		assert.Contains(t, combined, "GH_AW_SUBREPO_PATH_0: ${{ github.event.inputs.target_path }}",
			"expression-based path must be assigned to an env var")
		assert.Contains(t, combined, "${GH_AW_SUBREPO_PATH_0}",
			"git -C argument must reference the path env var, not the raw expression")
		assert.NotContains(t, combined, `git -C "${{ github.event.inputs.target_path }}"`,
			"expression path must not be inlined in git -C argument (template injection risk)")
		// No raw Actions expression may appear in the run: block.
		runIdx := strings.Index(combined, "run: |")
		if runIdx >= 0 {
			assert.NotContains(t, combined[runIdx:], "${{",
				"run: block must not contain any raw GitHub Actions expression")
		}
	})

	t.Run("multiple sub-repos each get a unique env var", func(t *testing.T) {
		cm := NewCheckoutManager([]*CheckoutConfig{
			{Repository: "org/repo-a", Path: "./repo-a"},
			{Repository: "org/repo-b", Path: "./repo-b"},
		})
		steps := cm.GenerateConfigureGitCredentialsSteps(token, alwaysTrue)
		combined := strings.Join(steps, "")

		assert.Contains(t, combined, `GH_AW_SUBREPO_0: "org/repo-a"`)
		assert.Contains(t, combined, `GH_AW_SUBREPO_1: "org/repo-b"`)
		assert.Contains(t, combined, `${GH_AW_SUBREPO_0}.git`)
		assert.Contains(t, combined, `${GH_AW_SUBREPO_1}.git`)
	})
}
