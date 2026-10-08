//go:build !integration

package workflow

import (
	"testing"

	"github.com/github/gh-aw/pkg/testutil"
	"github.com/stretchr/testify/require"
)

func TestFormal_CTR016_NilManifestSkipsEnforcement(t *testing.T) {
	err := EnforceSafeUpdate(SafeUpdateOptions{
		SecretNames: []string{"MY_SECRET"},
		ActionRefs:  []string{"evil-org/action@deadbeef # v1"},
	})
	require.NoError(t, err)
}

func TestFormal_CTR016_EmptyManifestRejectsNewSecret(t *testing.T) {
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
		SecretNames: []string{"MY_SECRET"},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "MY_SECRET")
}

func TestFormal_CTR016_GitHubTokenExempt_BareForm(t *testing.T) {
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
		SecretNames: []string{"GITHUB_TOKEN"},
	})
	require.NoError(t, err)
}

func TestFormal_CTR016_GitHubTokenExempt_PrefixedForm(t *testing.T) {
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
		SecretNames: []string{"secrets.GITHUB_TOKEN"},
	})
	require.NoError(t, err)
}

func TestFormal_CTR016_GhAwInternalSecretExempt(t *testing.T) {
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
		SecretNames: []string{"GH_AW_GITHUB_TOKEN"},
	})
	require.NoError(t, err)
}

func TestFormal_CTR016_SecretPrefixNormalization(t *testing.T) {
	manifest := &GHAWManifest{Version: currentGHAWManifestVersion, Secrets: []string{"MY_SECRET"}}
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:    manifest,
		SecretNames: []string{"secrets.MY_SECRET"},
	})
	require.NoError(t, err)
}

func TestFormal_CTR016_NewActionDriftRejected(t *testing.T) {
	manifest := &GHAWManifest{Version: currentGHAWManifestVersion, Actions: []GHAWManifestAction{{Repo: "actions/checkout", SHA: "abc1234", Version: "v4"}}}
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:   manifest,
		ActionRefs: []string{"actions/checkout@abc1234 # v4", "evil-org/steal@deadbeef # v1"},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "evil-org/steal")
}

func TestFormal_CTR016_RemovedActionDriftRejected(t *testing.T) {
	manifest := &GHAWManifest{Version: currentGHAWManifestVersion, Actions: []GHAWManifestAction{{Repo: "my-org/approved-action", SHA: "abc1234", Version: "v1"}}}
	err := EnforceSafeUpdate(SafeUpdateOptions{Manifest: manifest})
	require.Error(t, err)
	require.ErrorContains(t, err, "Previously-approved action")
	require.ErrorContains(t, err, "my-org/approved-action")
}

func TestFormal_CTR016_KnownActionPinUpdateAllowed(t *testing.T) {
	manifest := &GHAWManifest{Version: currentGHAWManifestVersion, Actions: []GHAWManifestAction{{Repo: "my-org/action", SHA: "abc1234", Version: "v1"}}}
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:   manifest,
		ActionRefs: []string{"my-org/action@def5678 # v2"},
	})
	require.NoError(t, err)
}

func TestFormal_CTR016_RedirectWhitespaceNormalization(t *testing.T) {
	manifest := &GHAWManifest{Version: currentGHAWManifestVersion, Redirect: "owner/repo/workflows/new.md@main"}
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:        manifest,
		CurrentRedirect: "  owner/repo/workflows/new.md@main  ",
	})
	require.NoError(t, err)
}

func TestFormal_CTR016_RedirectChangeRejected(t *testing.T) {
	manifest := &GHAWManifest{Version: currentGHAWManifestVersion, Redirect: "owner/repo/workflows/old.md@main"}
	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:        manifest,
		CurrentRedirect: "owner/repo/workflows/new.md@main",
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "New redirect configured")
	require.ErrorContains(t, err, "Previously-approved redirect removed")
}

func TestFormal_CTR001_WritePermissionsRejected(t *testing.T) {
	// PermissionIdToken: id-token:write is allowed for OIDC auth and does not grant repo write access.
	// PermissionMetadata: metadata is always implicitly read-only, so it is excluded from the write-rejection rule.
	for _, scope := range GetAllPermissionScopes() {
		if scope == PermissionIdToken || scope == PermissionMetadata {
			continue
		}
		t.Run(string(scope), func(t *testing.T) {
			perms := NewPermissions()
			perms.Set(scope, PermissionWrite)
			err := validateDangerousPermissions(&WorkflowData{Permissions: "permissions: {}"}, perms)
			require.Error(t, err)
			require.ErrorContains(t, err, "write permissions")
		})
	}
}

func TestFormal_CTR001_IDTokenWriteIsInformational(t *testing.T) {
	for name, strict := range map[string]bool{"strict": true, "non-strict": false} {
		t.Run(name, func(t *testing.T) {
			compiler := NewCompiler()
			compiler.SetStrictMode(strict)
			stderr := testutil.CaptureStderr(t, func() {
				perms, err := compiler.validatePermissions(&WorkflowData{
					Permissions: "permissions:\n  contents: read\n  id-token: write\n",
				}, "workflow.md")
				require.NoError(t, err)
				level, exists := perms.Get(PermissionIdToken)
				require.True(t, exists)
				require.Equal(t, PermissionWrite, level)
			})
			require.Contains(t, stderr, "info: This workflow grants id-token: write permission")
			require.Contains(t, stderr, "Ensure proper audience validation and trust policies are configured.")
			require.NotContains(t, stderr, "warning: This workflow grants id-token: write permission")
			require.Zero(t, compiler.GetWarningCount())
		})
	}
}

func TestFormal_CTR001_ReadOnlyPermissionsAllowed(t *testing.T) {
	perms := NewPermissions()
	for _, scope := range GetAllPermissionScopes() {
		// PermissionIdToken is intentionally omitted because GitHub Actions treats it as write-or-absent, not read-or-write.
		if scope == PermissionIdToken {
			continue
		}
		perms.Set(scope, PermissionRead)
	}
	err := validateDangerousPermissions(&WorkflowData{Permissions: "permissions: {}"}, perms)
	require.NoError(t, err)
}

func TestFormal_CTR001_EmptyPermissionsAllowed(t *testing.T) {
	err := validateDangerousPermissions(&WorkflowData{Permissions: ""}, NewPermissions())
	require.NoError(t, err)
}

func TestFormal_CTR011_AllowURLsRequiresSSLBump(t *testing.T) {
	err := validateNetworkFirewallConfig(&NetworkPermissions{
		Firewall: &FirewallConfig{
			AllowURLs: []string{"https://github.com/githubnext/*"},
			SSLBump:   false,
		},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "allow-urls requires ssl-bump: true")
}

func TestFormal_CTR011_AllowURLsWithSSLBumpAllowed(t *testing.T) {
	err := validateNetworkFirewallConfig(&NetworkPermissions{
		Firewall: &FirewallConfig{
			AllowURLs: []string{"https://github.com/githubnext/*"},
			SSLBump:   true,
		},
	})
	require.NoError(t, err)
}

func TestFormal_CTR011_WildcardOnlyDomainRejected(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateStrictNetwork(&NetworkPermissions{Allowed: []string{"*"}})
	require.Error(t, err)
	require.ErrorContains(t, err, "wildcard '*' is not allowed")
}

func TestFormal_CTR015_WildcardLabelRejected(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateSafeOutputsAllowedLabelsGlobScope(&SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{SafeOutputAllowedLabelsConfig: SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"*"}}},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "CTR-015")
}

func TestFormal_CTR015_WildcardLabelRejected_CreateDiscussion(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateSafeOutputsAllowedLabelsGlobScope(&SafeOutputsConfig{
		CreateDiscussions: &CreateDiscussionsConfig{SafeOutputAllowedLabelsConfig: SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"*"}}},
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "CTR-015")
}

func TestFormal_CTR015_SpecificLabelsAllowed(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateSafeOutputsAllowedLabelsGlobScope(&SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{SafeOutputAllowedLabelsConfig: SafeOutputAllowedLabelsConfig{AllowedLabels: []string{"bug", "team-*"}}},
	})
	require.NoError(t, err)
}

func TestFormal_CTR015_NilConfigAllowed(t *testing.T) {
	compiler := NewCompiler()
	err := compiler.validateSafeOutputsAllowedLabelsGlobScope(nil)
	require.NoError(t, err)
}

func TestFormal_CTR014_StrictModeEnabledRejected(t *testing.T) {
	compiler := NewCompiler()
	compiler.SetStrictMode(true)
	err := compiler.validateRunInstallScripts(&WorkflowData{RunInstallScripts: true})
	require.Error(t, err)
	require.ErrorContains(t, err, "strict mode")
}

func TestFormal_CTR014_DisabledAlwaysAllowed(t *testing.T) {
	t.Run("strict mode", func(t *testing.T) {
		compiler := NewCompiler()
		compiler.SetStrictMode(true)
		err := compiler.validateRunInstallScripts(&WorkflowData{RunInstallScripts: false})
		require.NoError(t, err)
	})

	t.Run("non-strict mode", func(t *testing.T) {
		compiler := NewCompiler()
		err := compiler.validateRunInstallScripts(&WorkflowData{RunInstallScripts: false})
		require.NoError(t, err)
	})
}
