//go:build !integration

package workflow

import (
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGoRepositoryProfileSinglePublicationTarget(t *testing.T) {
	const current = "${{ github.repository }}"
	for _, test := range []struct {
		name    string
		target  string
		allowed []string
		wantErr bool
	}{
		{name: "omitted"},
		{name: "omitted with empty allowlist", allowed: []string{}},
		{name: "current expression", target: current},
		{name: "omitted with current allowlist", allowed: []string{current}},
		{name: "current expression with matching allowlist", target: current, allowed: []string{current}},
		{name: "literal", target: "jpmicrosoft/fam"},
		{name: "matching literal allowlist", target: "jpmicrosoft/fam", allowed: []string{"jpmicrosoft/fam"}},
		{name: "case insensitive match", target: "JPMicrosoft/FAM", allowed: []string{"jpmicrosoft/fam"}},
		{name: "literal punctuation", target: "owner-name/repo_name.v2"},
		{name: "wildcard target", target: "*", wantErr: true},
		{name: "wildcard owner", target: "*/fam", wantErr: true},
		{name: "wildcard repository", target: "jpmicrosoft/*", wantErr: true},
		{name: "dynamic target", target: "${{ inputs.repository }}", wantErr: true},
		{name: "missing owner", target: "/fam", wantErr: true},
		{name: "missing repository", target: "jpmicrosoft/", wantErr: true},
		{name: "missing slash", target: "jpmicrosoft", wantErr: true},
		{name: "extra path", target: "github.com/jpmicrosoft/fam", wantErr: true},
		{name: "URL", target: "https://github.com/jpmicrosoft/fam", wantErr: true},
		{name: "ref qualified", target: "jpmicrosoft/fam@main", wantErr: true},
		{name: "leading whitespace", target: " jpmicrosoft/fam", wantErr: true},
		{name: "trailing newline", target: "jpmicrosoft/fam\n", wantErr: true},
		{name: "dot owner", target: "../fam", wantErr: true},
		{name: "dot repository", target: "jpmicrosoft/..", wantErr: true},
		{name: "different allowed repository", target: "jpmicrosoft/fam", allowed: []string{"other/repo"}, wantErr: true},
		{name: "wildcard allowlist", target: "jpmicrosoft/fam", allowed: []string{"*"}, wantErr: true},
		{name: "nonliteral case fold", target: "Key/repo", allowed: []string{"\u212aey/repo"}, wantErr: true},
		{name: "multiple repositories", target: "jpmicrosoft/fam", allowed: []string{"jpmicrosoft/fam", "other/repo"}, wantErr: true},
		{name: "duplicate allowlist entries", target: "jpmicrosoft/fam", allowed: []string{"jpmicrosoft/fam", "JPMICROSOFT/FAM"}, wantErr: true},
		{name: "literal target with expression allowlist", target: "jpmicrosoft/fam", allowed: []string{current}, wantErr: true},
		{name: "current expression with literal allowlist", target: current, allowed: []string{"jpmicrosoft/fam"}, wantErr: true},
		{name: "omitted with literal allowlist", allowed: []string{"jpmicrosoft/fam"}, wantErr: true},
		{name: "omitted with empty entry", allowed: []string{""}, wantErr: true},
		{name: "current expression with multiple entries", target: current, allowed: []string{current, "other/repo"}, wantErr: true},
		{name: "dynamic allowlist", target: current, allowed: []string{"${{ inputs.repositories }}"}, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := parseGoRepositoryProfileTestWorkflow(t)
			pr := data.SafeOutputs.CreatePullRequests
			pr.TargetRepoSlug = test.target
			pr.AllowedRepos = slices.Clone(test.allowed)
			err := validateCopilotToolProfile(data)
			if test.wantErr {
				require.ErrorContains(t, err, "create-pull-request target")
			} else {
				require.NoError(t, err)
			}
			assert.Equal(t, test.target, pr.TargetRepoSlug)
			assert.Equal(t, test.allowed, pr.AllowedRepos, "validation must not rewrite publication restrictions")
		})
	}
}

func TestGoRepositoryProfileParsesCurrentRepositoryAllowlist(t *testing.T) {
	for _, target := range []string{"", "    target-repo: ${{ github.repository }}\n"} {
		markdown := strings.Replace(goRepositoryProfileTestMarkdown, "  create-pull-request:\n",
			"  create-pull-request:\n"+target+"    allowed-repos:\n      - ${{ github.repository }}\n", 1)
		data := parseGoRepositoryProfileTestSource(t, markdown)
		require.NoError(t, validateCopilotToolProfile(data))
		assert.Equal(t, []string{"${{ github.repository }}"}, data.SafeOutputs.CreatePullRequests.AllowedRepos)
	}
}

func TestGoRepositoryProfileLiteralIdentityIsDeferredToRuntime(t *testing.T) {
	t.Setenv("GITHUB_REPOSITORY", "different/repository")
	markdown := strings.Replace(goRepositoryProfileTestMarkdown, "  create-pull-request:\n", `  create-pull-request:
    target-repo: jpmicrosoft/fam
    allowed-repos: [JPMICROSOFT/FAM]
`, 1)
	data := parseGoRepositoryProfileTestSource(t, markdown)
	require.NoError(t, validateCopilotToolProfile(data), "the compiler must not infer repository identity from its environment")
	pr := data.SafeOutputs.CreatePullRequests
	assert.Equal(t, "jpmicrosoft/fam", pr.TargetRepoSlug)
	assert.Equal(t, []string{"JPMICROSOFT/FAM"}, pr.AllowedRepos)

	_, args := NewCopilotEngine().buildCopilotArgs(data)
	config := buildCopilotSDKToolConfig(data, args)
	require.NotNil(t, config.Profile)
	assert.Equal(t, "jpmicrosoft/fam", config.Profile.Policy["target-repo"], "retain the literal for the runtime's fail-closed identity check")
	assert.NotContains(t, config.Profile.Policy, "allowed_repos", "the SDK policy whitelist must not expand")
	handlers := map[string]any{}
	addStandardHandlerConfigs(handlers, data)
	handler, ok := handlers["create_pull_request"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "jpmicrosoft/fam", handler["target-repo"])
	assert.Equal(t, []string{"JPMICROSOFT/FAM"}, handler["allowed_repos"], "publication must retain its original allowlist")
}

func TestGoRepositoryProfileRequiresParsedNoop(t *testing.T) {
	for _, test := range []struct {
		name    string
		present bool
		value   any
		wantErr bool
	}{
		{name: "implicit noop"},
		{name: "explicit noop", present: true},
		{name: "enabled noop", present: true, value: true},
		{name: "configured noop", present: true, value: map[string]any{"report-as-issue": false}},
		{name: "disabled noop", present: true, value: false, wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			data := parseGoRepositoryProfileTestWorkflow(t)
			outputs := map[string]any{"create-pull-request": nil}
			if test.present {
				outputs["noop"] = test.value
			}
			data.SafeOutputs = NewCompiler().extractSafeOutputsConfig(map[string]any{"safe-outputs": outputs})
			require.NotNil(t, data.SafeOutputs)
			err := validateCopilotToolProfile(data)
			if test.wantErr {
				require.Nil(t, data.SafeOutputs.NoOp)
				require.ErrorContains(t, err, "safe-outputs.noop")
				data.EngineConfig.ToolProfile = ""
				require.NoError(t, validateCopilotToolProfile(data), "legacy noop opt-out must remain supported")
				assert.Nil(t, data.SafeOutputs.NoOp)
			} else {
				require.NoError(t, err)
				require.NotNil(t, data.SafeOutputs.NoOp)
				assert.Equal(t, !test.present, data.SafeOutputs.NoOp.Implicit)
			}
		})
	}
}
