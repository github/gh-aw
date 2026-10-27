//go:build !integration

package cli

import (
	"testing"

	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPullRequestTargetGuidedErrorCodemodMetadata(t *testing.T) {
	t.Parallel()
	mod := getPullRequestTargetGuidedErrorCodemod()
	assert.Equal(t, "pull-request-target-guided-error", mod.ID)
	assert.NotEmpty(t, mod.Name)
	assert.NotEmpty(t, mod.Description)
	assert.Equal(t, "1.5.0", mod.IntroducedIn)
	assert.True(t, mod.Guided)
	assert.NotNil(t, mod.Apply)
}

func TestPullRequestTargetGuidedErrorCodemod(t *testing.T) {
	t.Parallel()
	const trigger = "on: {pull_request_target: null}\n"
	const allow = "on: {pull_request_target: {allowed-checkouts: [{repository: Owner/Repo, ref: main}]}}\n"
	tests := []struct {
		name   string
		fields string
		want   []string
	}{
		{"default checkout", trigger, []string{"Checkout", "checkout: false", "base.sha"}},
		{"explicit unsafe checkout", trigger + "checkout: {ref: '${{ github.event.pull_request.head.sha }}'}", []string{"Checkout", "checkout: false"}},
		{"acknowledged unsafe checkout", "on: {pull_request_target: {acknowledge-risk: true}}", []string{"Checkout", "does not bypass"}},
		{"unsafe roles", "on: {pull_request_target: null, roles: read}\ncheckout: false", []string{"Untrusted contributors", "on.roles: [admin, maintainer, write]"}},
		{"both risks", "on: {pull_request_target: null, roles: [admin, all]}", []string{"Checkout", "Untrusted contributors"}},
		{"acknowledged unsafe roles", "on: {pull_request_target: {acknowledge-risk: true}, roles: all}\ncheckout: false", []string{"Untrusted contributors"}},
		{"unsafe member", trigger + "checkout: [{ref: '${{ github.event.pull_request.base.sha }}'}, {ref: '${{ github.event.pull_request.head.sha }}'}]", []string{"Checkout"}},
		{"untrusted external pair", trigger + "checkout: {repository: Owner/Repo, ref: main}", []string{"allowed-checkouts"}},
		{"disabled checkout", trigger + "checkout: false", nil},
		{"base sha", trigger + "checkout: {ref: '${{ github.event.pull_request.base.sha }}'}", nil},
		{"base ref", trigger + "checkout: {ref: '${{ github.event.pull_request.base.ref }}'}", nil},
		{"default base", trigger + "checkout: {}", nil},
		{"allowed pair", allow + "checkout: {repository: Owner/Repo, ref: main}", nil},
		{"mismatched pair", allow + "checkout: {repository: Owner/Repo, ref: other}", []string{"Checkout"}},
		{"extra fetch", trigger + "checkout: {fetch: refs/pull/1/head}", []string{"Checkout"}},
		{"expression allowlist", "on: {pull_request_target: {allowed-checkouts: [{repository: Owner/Repo, ref: '${{ inputs.ref }}'}]}}\ncheckout: {repository: Owner/Repo, ref: '${{ inputs.ref }}'}", []string{"Checkout"}},
		{"non target", "on: pull_request\ncheckout: true", nil},
		{"exact trigger", "on: {pull_request_target_staging: null}\ncheckout: true", nil},
		{"list trigger", "on: [push, pull_request_target]", []string{"Checkout"}},
		{"mixed triggers", "on: {pull_request_target: null, workflow_dispatch: null}", []string{"Checkout"}},
		{"pending imports", trigger + "imports: [shared.md]", nil},
		{"imports cannot hide explicit unsafe checkout", trigger + "imports: [shared.md]\ncheckout: {ref: '${{ github.event.pull_request.head.sha }}'}", []string{"Checkout"}},
		{"imports cannot hide unsafe roles", "imports: [shared.md]\non: {pull_request_target: null, roles: read}\ncheckout: false", []string{"Untrusted contributors"}},
		{"nonstrict", "strict: false\n" + trigger + "checkout: true", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content := "---\n" + tt.fields + "\n---\n\n# Prompt\n"
			document, err := aw.Parse(content)
			require.NoError(t, err)
			result, applied, err := getPullRequestTargetGuidedErrorCodemod().Apply(content, document.Frontmatter)
			assert.False(t, applied)
			assert.Equal(t, content, result)
			if tt.want == nil {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			for _, text := range append(tt.want, "Manual fix required", "switch to pull_request", "acknowledge-risk: true suppresses only", "never added automatically", "https://securitylab.github.com/resources/github-actions-preventing-pwn-requests/") {
				assert.ErrorContains(t, err, text)
			}
		})
	}
}

func TestPullRequestTargetGuidedErrorCodemodMalformedInput(t *testing.T) {
	t.Parallel()
	for _, fields := range []string{
		"strict: invalid\non: pull_request_target",
		"on: pull_request_target\ncheckout: invalid",
		"on: {pull_request_target: null, roles: [read, {key: PRIVATE_VALUE}]}\ncheckout: false",
		"on: pull_request_target\nimports: invalid",
	} {
		t.Run(fields, func(t *testing.T) {
			t.Parallel()
			content := "---\n" + fields + "\n---\n"
			document, err := aw.Parse(content)
			require.NoError(t, err)
			result, applied, err := getPullRequestTargetGuidedErrorCodemod().Apply(content, document.Frontmatter)
			require.ErrorContains(t, err, "checking pull_request_target configuration")
			assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
			assert.False(t, applied)
			assert.Equal(t, content, result)
		})
	}
}
