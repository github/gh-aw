//go:build !integration

package cli

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/stretchr/testify/require"
)

func TestConnectorSetupRepositoryRequirements(t *testing.T) {
	original := runBootstrapGHContext
	t.Cleanup(func() { runBootstrapGHContext = original })
	root := t.TempDir()
	content := strings.Replace(developmentConnectorFixture,
		"token:\n          secret: ${{ github.aw.import-inputs.reader-secret }}",
		"github-app:\n          client-id: ${{ vars.DEMO_APP_ID }}\n          private-key: ${{ secrets.DEMO_APP_KEY }}\n          owner: owner\n          repositories: [repo]\n          permissions:\n            issues: read", 1)
	writeDevelopmentConnector(t, root, content)
	for _, test := range []struct {
		name       string
		flags      []string
		secrets    string
		variables  string
		queries    int
		missing    string
		notMissing string
	}{
		{name: "missing app key", secrets: "DEV_WRITE_TOKEN\n", variables: "DEMO_APP_ID\n", queries: 2,
			missing: "1 of 3 declared repository settings are missing.", notMissing: "Missing variable"},
		{name: "all present", secrets: "DEV_WRITE_TOKEN\nDEMO_APP_KEY\n", variables: "DEMO_APP_ID\n", queries: 2,
			missing: "0 of 3 declared repository settings are missing.", notMissing: "Missing secret"},
		{name: "reader phase", flags: []string{"--phase", "agent"}, queries: 2,
			missing: "2 of 2 declared repository settings are missing.", notMissing: "Missing secret DEV_WRITE_TOKEN"},
		{name: "writer phase", flags: []string{"--phase", "safe-output"}, queries: 1,
			missing: "1 of 1 declared repository settings are missing.", notMissing: "Missing variable"},
	} {
		t.Run(test.name, func(t *testing.T) {
			var calls []string
			runBootstrapGHContext = func(ctx context.Context, _ string, args ...string) ([]byte, error) {
				_, bounded := ctx.Deadline()
				require.True(t, bounded)
				require.Equal(t, "api", args[0])
				require.Equal(t, []string{"--paginate", "--jq"}, args[2:4])
				calls = append(calls, strings.Join(args, " "))
				switch args[1] {
				case "/repos/owner/repo/actions/secrets?per_page=100":
					require.Equal(t, ".secrets[].name", args[4], "never request or display values")
					return []byte(test.secrets), nil
				case "/repos/owner/repo/actions/variables?per_page=100":
					require.Equal(t, ".variables[].name", args[4], "never request or display values")
					return []byte(test.variables), nil
				default:
					t.Fatalf("unexpected GitHub operation: %v", args)
					return nil, errors.New("unexpected operation")
				}
			}
			args := append([]string{"demo", "setup", "--root", root, "--repo", "owner/repo"}, test.flags...)
			output, err := executeDevelopmentCommand(args...)
			require.NoError(t, err)
			require.Len(t, calls, test.queries, "query each required storage kind once")
			require.Contains(t, output, test.missing)
			require.NotContains(t, output, test.notMissing)
			require.Contains(t, output, "Name presence does not verify token validity, app installation/permissions, OIDC trust, or provider access")
			require.Contains(t, output, "No values were displayed and no settings were changed")
			if test.name == "missing app key" {
				require.Contains(t, output, "Missing secret DEMO_APP_KEY: gh secret set DEMO_APP_KEY --repo owner/repo")
				require.Contains(t, output, "Declared installation owner: owner")
				require.Contains(t, output, "Declared installation repositories: repo")
				require.Contains(t, output, "Required GitHub App permission issues: read")
				require.NotContains(t, output, "Missing secret GH_AW_CONNECTOR_APP_")
			}
			if test.name == "reader phase" {
				require.Contains(t, output, "Missing variable DEMO_APP_ID: gh variable set DEMO_APP_ID --repo owner/repo")
			}
		})
	}
}

func TestConnectorSetupRepositoryChecksAreExplicitAndFailClosed(t *testing.T) {
	original := runBootstrapGHContext
	t.Cleanup(func() { runBootstrapGHContext = original })
	root := t.TempDir()
	writeDevelopmentConnector(t, root, developmentConnectorFixture)
	calls := 0
	runBootstrapGHContext = func(context.Context, string, ...string) ([]byte, error) {
		calls++
		return nil, errors.New("HTTP 403: repository metadata denied")
	}
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root)
	require.NoError(t, err)
	require.Zero(t, calls, "default setup must remain offline")
	require.NotContains(t, output, "declared repository settings are missing")
	for _, repo := range []string{"", "owner", "host/owner/repo", "owner/../repo", "owner/repo?x", "owner/repo\n"} {
		_, err := executeDevelopmentCommand("demo", "setup", "--root", root, "--repo", repo)
		require.ErrorContains(t, err, "--repo must name")
	}
	_, err = executeDevelopmentCommand("demo", "test", "--root", root, "--repo", "owner/repo")
	require.ErrorContains(t, err, "--repo applies only to connector setup")
	require.Zero(t, calls, "invalid target or command must not access GitHub")

	output, err = executeDevelopmentCommand("demo", "setup", "--root", root, "--repo", "owner/repo")
	require.ErrorContains(t, err, "missing requirements are unknown")
	require.ErrorContains(t, err, "HTTP 403")
	require.Equal(t, 1, calls, "authorization failure must not retry or fall back")
	require.NotContains(t, output, "Missing secret")
	require.NotContains(t, output, "declared repository settings are missing")
}

func TestConnectorSetupRepositoryBoundRequirements(t *testing.T) {
	original := runBootstrapGHContext
	t.Cleanup(func() { runBootstrapGHContext = original })
	for _, test := range []struct {
		name, route, present, variables, missing string
		flags                                    []string
	}{
		{name: "bound token", flags: []string{"--input", `reader-secret="CUSTOM_READER_TOKEN"`},
			missing: "Missing secret CUSTOM_READER_TOKEN: gh secret set CUSTOM_READER_TOKEN --repo owner/repo"},
		{name: "literal app ID", route: "github-app:\n          client-id: Iv1.example\n          private-key: ${{ secrets.APP_KEY }}\n          permissions:\n            issues: read",
			missing: "Missing secret APP_KEY: gh secret set APP_KEY --repo owner/repo"},
		{name: "secret app ID", route: "github-app:\n          client-id: ${{ secrets.APP_ID }}\n          private-key: ${{ secrets.APP_KEY }}\n          permissions:\n            issues: read", present: "APP_KEY\n",
			missing: "Missing secret APP_ID: gh secret set APP_ID --repo owner/repo"},
		{name: "case-insensitive app reference", route: "github-app:\n          client-id: ${{ vars.app_id }}\n          private-key: ${{ secrets.APP_KEY }}\n          permissions:\n            issues: read", present: "APP_KEY\n", variables: "APP_ID\n",
			missing: "0 of 2 declared repository settings are missing"},
	} {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			content := developmentConnectorFixture
			if test.route != "" {
				content = strings.Replace(content, "token:\n          secret: ${{ github.aw.import-inputs.reader-secret }}", test.route, 1)
			}
			writeDevelopmentConnector(t, root, content)
			calls := 0
			runBootstrapGHContext = func(_ context.Context, _ string, args ...string) ([]byte, error) {
				calls++
				if test.variables != "" && strings.Contains(args[1], "/variables?") {
					require.Equal(t, []string{"api", "/repos/owner/repo/actions/variables?per_page=100", "--paginate", "--jq", ".variables[].name"}, args)
					return []byte(test.variables), nil
				}
				require.Equal(t, []string{"api", "/repos/owner/repo/actions/secrets?per_page=100", "--paginate", "--jq", ".secrets[].name"}, args)
				return []byte(test.present), nil
			}
			args := append([]string{"demo", "setup", "--root", root, "--repo", "owner/repo", "--phase", "agent"}, test.flags...)
			output, err := executeDevelopmentCommand(args...)
			require.NoError(t, err)
			expectedCalls := 1
			if test.variables != "" {
				expectedCalls++
			}
			require.Equal(t, expectedCalls, calls, "query only required storage kinds")
			require.Contains(t, output, test.missing)
			require.NotContains(t, output, "Missing secret DEV_READ_TOKEN")
		})
	}
}

func TestConnectorSetupRepositoryVariableInventoryFailure(t *testing.T) {
	original := runBootstrapGHContext
	t.Cleanup(func() { runBootstrapGHContext = original })
	root := t.TempDir()
	content := strings.Replace(developmentConnectorFixture,
		"token:\n          secret: ${{ github.aw.import-inputs.reader-secret }}",
		"github-app:\n          client-id: ${{ vars.APP_ID }}\n          private-key: ${{ secrets.APP_KEY }}\n          permissions:\n            issues: read", 1)
	writeDevelopmentConnector(t, root, content)
	calls := 0
	runBootstrapGHContext = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls++
		if strings.Contains(args[1], "/secrets?") {
			return []byte("DEV_WRITE_TOKEN\n"), nil
		}
		return nil, errors.New("HTTP 401: variables unavailable")
	}
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root, "--repo", "owner/repo")
	require.ErrorContains(t, err, "repository variable names; missing requirements are unknown")
	require.ErrorContains(t, err, "HTTP 401")
	require.Equal(t, 2, calls)
	require.NotContains(t, output, "Missing secret APP_KEY", "do not render a partial inventory as a complete report")
	require.NotContains(t, output, "declared repository settings are missing")
}

func TestConnectorSetupRequirementsDeduplicateAndDoNotInventOIDCSecrets(t *testing.T) {
	registration := workflow.ConnectorRegistration{
		Spec: &connectors.ConnectorSpec{
			Credentials: map[string]connectors.Credential{"first": {Phase: "agent"}, "second": {Phase: "safe-output"}},
			SafeOutputs: map[string]connectors.Operation{"publish": {TrustedInputs: map[string]connectors.TrustedInput{"project": {Secret: "PROJECT_ID"}}}},
		},
		Credentials: map[string]connectors.SecretRef{"first": {Secret: "SHARED_TOKEN"}, "second": {Secret: "SHARED_TOKEN"}},
	}
	require.Equal(t, []connectorSetupRequirement{{kind: "secret", name: "PROJECT_ID"}, {kind: "secret", name: "SHARED_TOKEN"}}, connectorSetupRequirements(registration, ""))
	require.Equal(t, []connectorSetupRequirement{{kind: "secret", name: "SHARED_TOKEN"}}, connectorSetupRequirements(registration, "agent"))
	root := t.TempDir()
	content := strings.Replace(developmentConnectorFixture,
		"token:\n          secret: ${{ github.aw.import-inputs.reader-secret }}",
		"github-oidc:\n          audience: https://identity.example.test", 1)
	writeDevelopmentConnector(t, root, content)
	original := runBootstrapGHContext
	t.Cleanup(func() { runBootstrapGHContext = original })
	runBootstrapGHContext = func(context.Context, string, ...string) ([]byte, error) {
		t.Fatal("OIDC trust must not be checked as a static token secret")
		return nil, errors.New("unexpected GitHub call")
	}
	output, err := executeDevelopmentCommand("demo", "setup", "--root", root, "--phase", "agent", "--repo", "owner/repo")
	require.NoError(t, err)
	require.Contains(t, output, "0 of 0 declared repository settings are missing")
	require.Contains(t, output, "Configure provider trust for GitHub Actions OIDC")
	require.Contains(t, output, "Name presence does not verify")
}
