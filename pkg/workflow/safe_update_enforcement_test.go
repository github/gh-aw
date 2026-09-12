//go:build !integration

package workflow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnforceSafeUpdate(t *testing.T) {
	tests := []struct {
		name               string
		manifest           *GHAWManifest
		secretNames        []string
		actionRefs         []string
		redirect           string
		oldHasPR           bool
		oldHasPRTarget     bool
		currentHasPR       bool
		currentHasPRTarget bool
		wantErr            bool
		wantErrMsgs        []string
	}{
		{
			name:        "nil manifest (lock file without manifest section) skips enforcement",
			manifest:    nil,
			secretNames: []string{"MY_SECRET"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "nil manifest (lock file without manifest section) skips enforcement for actions",
			manifest:    nil,
			secretNames: []string{},
			actionRefs:  []string{"my-org/my-action@abc1234 # v1"},
			wantErr:     false,
		},
		{
			name:        "nil manifest (lock file without manifest section) skips with GITHUB_TOKEN",
			manifest:    nil,
			secretNames: []string{"GITHUB_TOKEN"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "nil manifest (lock file without manifest section) skips with no secrets",
			manifest:    nil,
			secretNames: []string{},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "empty non-nil manifest (no lock file) enforces — new secret flagged",
			manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
			secretNames: []string{"MY_SECRET"},
			actionRefs:  []string{},
			wantErr:     true,
			wantErrMsgs: []string{"MY_SECRET", "safe update mode"},
		},
		{
			name:        "empty non-nil manifest (no lock file) enforces — custom action flagged",
			manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
			secretNames: []string{},
			actionRefs:  []string{"my-org/my-action@abc1234 # v1"},
			wantErr:     true,
			wantErrMsgs: []string{"my-org/my-action", "safe update mode"},
		},
		{
			name:        "empty non-nil manifest (no lock file) allows GITHUB_TOKEN",
			manifest:    &GHAWManifest{Version: currentGHAWManifestVersion},
			secretNames: []string{"GITHUB_TOKEN"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "empty secrets and actions with existing manifest passes",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{}, Actions: []GHAWManifestAction{}},
			secretNames: []string{},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "GITHUB_TOKEN always allowed even when not in manifest",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{}, Actions: []GHAWManifestAction{}},
			secretNames: []string{"GITHUB_TOKEN"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "compiler-injected OTLP default secrets are always allowed",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{}, Actions: []GHAWManifestAction{}},
			secretNames: []string{"GH_AW_DEFAULT_OTLP_ENDPOINT", "GH_AW_DEFAULT_OTLP_HEADERS"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "compiler-injected OTLP endpoint secret alone is allowed",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{}, Actions: []GHAWManifestAction{}},
			secretNames: []string{"GH_AW_DEFAULT_OTLP_ENDPOINT"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "compiler-injected OTLP endpoint secret is allowed when headers secret already exists in manifest",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{"GH_AW_DEFAULT_OTLP_HEADERS"}, Actions: []GHAWManifestAction{}},
			secretNames: []string{"GH_AW_DEFAULT_OTLP_ENDPOINT"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "compiler-injected OTLP headers secret alone is allowed",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{}, Actions: []GHAWManifestAction{}},
			secretNames: []string{"GH_AW_DEFAULT_OTLP_HEADERS"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "compiler-injected OTLP headers secret is allowed when endpoint secret already exists in manifest",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{"GH_AW_DEFAULT_OTLP_ENDPOINT"}, Actions: []GHAWManifestAction{}},
			secretNames: []string{"GH_AW_DEFAULT_OTLP_HEADERS"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name:        "GITHUB_TOKEN with secrets. prefix always allowed",
			manifest:    &GHAWManifest{Version: 1, Secrets: []string{}, Actions: []GHAWManifestAction{}},
			secretNames: []string{"secrets.GITHUB_TOKEN"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name: "known secret passes",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{"MY_SECRET"},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{"MY_SECRET"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name: "new restricted secret causes failure",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{"EXISTING_SECRET"},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{"EXISTING_SECRET", "NEW_SECRET"},
			actionRefs:  []string{},
			wantErr:     true,
			wantErrMsgs: []string{"NEW_SECRET", "safe update mode"},
		},
		{
			name: "multiple new secrets listed in error",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{"SECRET_A", "SECRET_B"},
			actionRefs:  []string{},
			wantErr:     true,
			wantErrMsgs: []string{"SECRET_A", "SECRET_B"},
		},
		{
			name: "GITHUB_TOKEN plus known secret passes",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{"MY_SECRET"},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{"GITHUB_TOKEN", "MY_SECRET"},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name: "empty manifest blocks any new secret except GITHUB_TOKEN",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{"SOME_SECRET"},
			actionRefs:  []string{},
			wantErr:     true,
			wantErrMsgs: []string{"SOME_SECRET"},
		},
		// Action enforcement tests.
		{
			name: "known action passes",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{{Repo: "my-org/my-action", SHA: "abc1234", Version: "v1"}},
			},
			secretNames: []string{},
			actionRefs:  []string{"my-org/my-action@abc1234 # v1"},
			wantErr:     false,
		},
		{
			name: "known action with different SHA (pin update) passes",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{{Repo: "my-org/my-action", SHA: "abc1234", Version: "v1"}},
			},
			secretNames: []string{},
			actionRefs:  []string{"my-org/my-action@def5678 # v2"},
			wantErr:     false,
		},
		{
			name: "new unapproved action causes failure",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{{Repo: "actions/checkout", SHA: "abc1234", Version: "v4"}},
			},
			secretNames: []string{},
			actionRefs:  []string{"actions/checkout@abc1234 # v4", "evil-org/steal-secrets@deadbeef # v1"},
			wantErr:     true,
			wantErrMsgs: []string{"evil-org/steal-secrets", "safe update mode", "New unapproved action"},
		},
		{
			name: "removed previously-approved action causes failure",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{
					{Repo: "actions/checkout", SHA: "abc1234", Version: "v4"},
					{Repo: "my-org/setup-tool", SHA: "def5678", Version: "v3"},
				},
			},
			secretNames: []string{},
			actionRefs:  []string{"actions/checkout@abc1234 # v4"},
			wantErr:     true,
			wantErrMsgs: []string{"my-org/setup-tool", "Previously-approved action"},
		},
		{
			name: "both added and removed actions reported together",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{{Repo: "my-org/approved-action", SHA: "abc1234", Version: "v4"}},
			},
			secretNames: []string{},
			actionRefs:  []string{"evil-org/bad-action@deadbeef # v1"},
			wantErr:     true,
			wantErrMsgs: []string{"evil-org/bad-action", "my-org/approved-action"},
		},
		{
			name: "new secret and new action both reported in one error",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{"NEW_SECRET"},
			actionRefs:  []string{"new-org/new-action@abc # v1"},
			wantErr:     true,
			wantErrMsgs: []string{"NEW_SECRET", "new-org/new-action"},
		},
		// actions/ org exemption tests.
		{
			name:        "nil manifest: new actions/checkout is allowed on first compile",
			manifest:    nil,
			secretNames: []string{},
			actionRefs:  []string{"actions/checkout@abc1234 # v4"},
			wantErr:     false,
		},
		{
			name: "new actions/ action (not in manifest) is always allowed",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{},
			actionRefs:  []string{"actions/setup-node@abc1234 # v4"},
			wantErr:     false,
		},
		{
			name: "removal of actions/ action from manifest is not flagged",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{{Repo: "actions/checkout", SHA: "abc1234", Version: "v4"}},
			},
			secretNames: []string{},
			actionRefs:  []string{},
			wantErr:     false,
		},
		{
			name: "actions/ action allowed alongside non-actions/ violation",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{},
			},
			secretNames: []string{},
			actionRefs:  []string{"actions/checkout@abc1234 # v4", "evil-org/bad-action@deadbeef # v1"},
			wantErr:     true,
			wantErrMsgs: []string{"evil-org/bad-action"},
		},
		// gh-aw infrastructure action exemption tests.
		{
			name: "gh aw upgrade: gh-aw-actions/setup added after manifest had gh-aw/actions/setup",
			manifest: &GHAWManifest{
				Version: 1,
				Secrets: []string{},
				Actions: []GHAWManifestAction{
					{Repo: "github/gh-aw/actions/setup", SHA: "abc1234", Version: "v0.66.1"},
				},
			},
			secretNames: []string{},
			actionRefs:  []string{"github/gh-aw-actions/setup@def5678 # v0.68.1"},
			wantErr:     false,
		},
		{
			name:        "gh-aw-actions allowed on first compile with nil manifest",
			manifest:    nil,
			secretNames: []string{},
			actionRefs:  []string{"github/gh-aw-actions/setup@abc1234 # v0.68.1"},
			wantErr:     false,
		},
		{
			name:        "new redirect causes failure",
			manifest:    &GHAWManifest{Version: 1},
			secretNames: []string{},
			actionRefs:  []string{},
			redirect:    "owner/repo/workflows/new.md@main",
			wantErr:     true,
			wantErrMsgs: []string{"New redirect configured", "owner/repo/workflows/new.md@main"},
		},
		{
			name: "removed redirect causes failure",
			manifest: &GHAWManifest{
				Version:  1,
				Redirect: "owner/repo/workflows/old.md@main",
			},
			secretNames: []string{},
			actionRefs:  []string{},
			redirect:    "",
			wantErr:     true,
			wantErrMsgs: []string{"Previously-approved redirect removed", "owner/repo/workflows/old.md@main"},
		},
		{
			name: "changed redirect reports add and remove",
			manifest: &GHAWManifest{
				Version:  1,
				Redirect: "owner/repo/workflows/old.md@main",
			},
			secretNames: []string{},
			actionRefs:  []string{},
			redirect:    "owner/repo/workflows/new.md@main",
			wantErr:     true,
			wantErrMsgs: []string{
				"New redirect configured",
				"Previously-approved redirect removed",
				"owner/repo/workflows/new.md@main",
				"owner/repo/workflows/old.md@main",
			},
		},
		{
			name: "redirect whitespace differences are normalized",
			manifest: &GHAWManifest{
				Version:  1,
				Redirect: "owner/repo/workflows/new.md@main",
			},
			secretNames: []string{},
			actionRefs:  []string{},
			redirect:    "  owner/repo/workflows/new.md@main  ",
			wantErr:     false,
		},
		{
			name:               "pull_request converted to pull_request_target is flagged",
			manifest:           &GHAWManifest{Version: 1},
			secretNames:        []string{},
			actionRefs:         []string{},
			oldHasPR:           true,
			currentHasPRTarget: true,
			wantErr:            true,
			wantErrMsgs:        []string{"Event trigger security escalation", "pull_request was converted to pull_request_target"},
		},
		{
			name:               "pull_request_target added without removing pull_request is not conversion",
			manifest:           &GHAWManifest{Version: 1},
			secretNames:        []string{},
			actionRefs:         []string{},
			oldHasPR:           true,
			currentHasPR:       true,
			currentHasPRTarget: true,
			wantErr:            false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prTransition := PullRequestEventTransition{
				OldHasPullRequest:           tt.oldHasPR,
				OldHasPullRequestTarget:     tt.oldHasPRTarget,
				CurrentHasPullRequest:       tt.currentHasPR,
				CurrentHasPullRequestTarget: tt.currentHasPRTarget,
			}
			err := EnforceSafeUpdate(SafeUpdateOptions{
				Manifest:              tt.manifest,
				SecretNames:           tt.secretNames,
				ActionRefs:            tt.actionRefs,
				CurrentRedirect:       tt.redirect,
				PullRequestTransition: prTransition,
			})
			if tt.wantErr {
				require.Error(t, err, "expected safe update enforcement error")
				for _, msg := range tt.wantErrMsgs {
					require.ErrorContains(t, err, msg, "error message should contain %q", msg)
				}
			} else {
				assert.NoError(t, err, "unexpected safe update enforcement error")
			}
		})
	}
}

func TestBuildSafeUpdateError(t *testing.T) {
	t.Run("secrets only", func(t *testing.T) {
		violations := []string{"NEW_SECRET", "ANOTHER_SECRET"}
		err := buildSafeUpdateError(violations, nil, nil, "", "", false, nil)
		require.Error(t, err, "should return an error")

		msg := err.Error()
		assert.Contains(t, msg, "safe update mode", "error message")
		assert.Contains(t, msg, "NEW_SECRET", "violation in message")
		assert.Contains(t, msg, "ANOTHER_SECRET", "violation in message")
		assert.Contains(t, msg, "--approve", "remediation guidance")
	})

	t.Run("added actions only", func(t *testing.T) {
		err := buildSafeUpdateError(nil, []string{"evil-org/bad-action"}, nil, "", "", false, nil)
		require.Error(t, err, "should return an error")
		msg := err.Error()
		assert.Contains(t, msg, "evil-org/bad-action", "action in message")
		assert.Contains(t, msg, "New unapproved action", "section header in message")
	})

	t.Run("removed actions only", func(t *testing.T) {
		err := buildSafeUpdateError(nil, nil, []string{"actions/setup-node"}, "", "", false, nil)
		require.Error(t, err, "should return an error")
		msg := err.Error()
		assert.Contains(t, msg, "actions/setup-node", "action in message")
		assert.Contains(t, msg, "Previously-approved action", "section header in message")
	})

	t.Run("added redirect only", func(t *testing.T) {
		err := buildSafeUpdateError(nil, nil, nil, "owner/repo/workflows/new.md@main", "", false, nil)
		require.Error(t, err, "should return an error")
		msg := err.Error()
		assert.Contains(t, msg, "New redirect configured", "added redirect section header in message")
		assert.Contains(t, msg, "owner/repo/workflows/new.md@main", "added redirect in message")
	})

	t.Run("removed redirect only", func(t *testing.T) {
		err := buildSafeUpdateError(nil, nil, nil, "", "owner/repo/workflows/old.md@main", false, nil)
		require.Error(t, err, "should return an error")
		msg := err.Error()
		assert.Contains(t, msg, "Previously-approved redirect removed", "removed redirect section header in message")
		assert.Contains(t, msg, "owner/repo/workflows/old.md@main", "removed redirect in message")
	})

	t.Run("mixed violations", func(t *testing.T) {
		err := buildSafeUpdateError(
			[]string{"MY_SECRET"},
			[]string{"evil-org/bad-action"},
			[]string{"actions/checkout"},
			"owner/repo/workflows/new.md@main",
			"owner/repo/workflows/old.md@main",
			false,
			nil,
		)
		require.Error(t, err, "should return an error")
		msg := err.Error()
		assert.Contains(t, msg, "MY_SECRET", "secret in message")
		assert.Contains(t, msg, "evil-org/bad-action", "added action in message")
		assert.Contains(t, msg, "actions/checkout", "removed action in message")
		assert.Contains(t, msg, "New redirect configured", "added redirect section in message")
		assert.Contains(t, msg, "Previously-approved redirect removed", "removed redirect section in message")
	})

	t.Run("pull_request to pull_request_target escalation", func(t *testing.T) {
		err := buildSafeUpdateError(nil, nil, nil, "", "", true, nil)
		require.Error(t, err, "should return an error")
		msg := err.Error()
		assert.Contains(t, msg, "Event trigger security escalation", "event escalation section in message")
		assert.Contains(t, msg, "pull_request was converted to pull_request_target", "event escalation details in message")
	})
}

func TestMemoryValidationScriptChangesRequireSafeUpdateReview(t *testing.T) {
	manifest := &GHAWManifest{
		MemoryValidationScripts: []GHAWManifestMemoryValidationScript{
			{Memory: "cache-memory:unchanged", SHA256: "same"},
			{Memory: "repo-memory:modified", SHA256: "before"},
			{Memory: "repo-memory:removed", SHA256: "removed"},
		},
	}
	current := []GHAWManifestMemoryValidationScript{
		{Memory: "cache-memory:added", SHA256: "added"},
		{Memory: "cache-memory:unchanged", SHA256: "same"},
		{Memory: "repo-memory:modified", SHA256: "after"},
	}

	changes := collectMemoryValidationScriptChanges(manifest, current)
	assert.Equal(t, []string{
		"cache-memory:added (added)",
		"repo-memory:modified (modified)",
		"repo-memory:removed (removed)",
	}, changes)

	err := EnforceSafeUpdate(SafeUpdateOptions{
		Manifest:                manifest,
		PullRequestTransition:   PullRequestEventTransition{},
		MemoryValidationScripts: current,
	})
	require.Error(t, err)
	require.ErrorContains(t, err, "Memory validation script changes")
	require.ErrorContains(t, err, "cache-memory:added (added)")
	require.ErrorContains(t, err, "repo-memory:modified (modified)")
	require.ErrorContains(t, err, "repo-memory:removed (removed)")
}

func TestExtractPullRequestEventPresence(t *testing.T) {
	t.Run("from on field map", func(t *testing.T) {
		hasPR, hasPRTarget := extractPullRequestEventPresenceFromOnField(map[string]any{
			"pull_request": map[string]any{"types": []any{"opened"}},
		})
		assert.True(t, hasPR)
		assert.False(t, hasPRTarget)
	})

	t.Run("from compiled workflow yaml", func(t *testing.T) {
		content := `
name: test
on:
  pull_request_target:
    types: [opened]
jobs: {}
`
		hasPR, hasPRTarget := extractPullRequestEventPresenceFromCompiledWorkflow(content)
		assert.False(t, hasPR)
		assert.True(t, hasPRTarget)
	})

	t.Run("invalid yaml returns false", func(t *testing.T) {
		hasPR, hasPRTarget := extractPullRequestEventPresenceFromCompiledWorkflow("on: [")
		assert.False(t, hasPR)
		assert.False(t, hasPRTarget)
	})
}

func TestCollectActionViolations(t *testing.T) {
	tests := []struct {
		name        string
		manifest    *GHAWManifest
		actionRefs  []string
		wantAdded   []string
		wantRemoved []string
	}{
		{
			name:        "no changes passes",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{{Repo: "actions/checkout", SHA: "abc"}}},
			actionRefs:  []string{"actions/checkout@abc # v4"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "SHA change on same repo passes",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{{Repo: "actions/checkout", SHA: "abc"}}},
			actionRefs:  []string{"actions/checkout@def # v5"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "new repo is an addition",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{{Repo: "actions/checkout", SHA: "abc"}}},
			actionRefs:  []string{"actions/checkout@abc", "new-org/new-action@xyz"},
			wantAdded:   []string{"new-org/new-action"},
			wantRemoved: nil,
		},
		{
			name:        "missing repo is a removal",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{{Repo: "my-org/custom-action", SHA: "abc"}, {Repo: "my-org/setup-tool", SHA: "def"}}},
			actionRefs:  []string{"my-org/custom-action@abc"},
			wantAdded:   nil,
			wantRemoved: []string{"my-org/setup-tool"},
		},
		{
			name:        "empty manifest with no new actions passes",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{}},
			actionRefs:  []string{},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "new actions/ action is not an addition violation",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{}},
			actionRefs:  []string{"actions/checkout@abc1234 # v4"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "removal of actions/ action from manifest is not a removal violation",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{{Repo: "actions/checkout", SHA: "abc1234", Version: "v4"}}},
			actionRefs:  []string{},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name: "actions/ action not flagged, non-actions/ action is flagged",
			manifest: &GHAWManifest{Actions: []GHAWManifestAction{
				{Repo: "actions/checkout", SHA: "abc1234", Version: "v4"},
			}},
			actionRefs:  []string{"actions/setup-node@def5678 # v4", "evil-org/bad-action@deadbeef # v1"},
			wantAdded:   []string{"evil-org/bad-action"},
			wantRemoved: nil,
		},
		// gh-aw infrastructure action exemption tests.
		{
			name:        "new github/gh-aw-actions/ action is not an addition violation",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{}},
			actionRefs:  []string{"github/gh-aw-actions/setup@abc1234 # v0.68.1"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "new github/gh-aw/actions/ action is not an addition violation",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{}},
			actionRefs:  []string{"github/gh-aw/actions/setup@abc1234 # v0.68.1"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name:        "removal of github/gh-aw-actions/ action from manifest is not a removal violation",
			manifest:    &GHAWManifest{Actions: []GHAWManifestAction{{Repo: "github/gh-aw-actions/setup", SHA: "abc1234", Version: "v0.66.1"}}},
			actionRefs:  []string{},
			wantAdded:   nil,
			wantRemoved: nil,
		},
		{
			name: "gh-aw-actions replacement of gh-aw/actions is not a violation",
			manifest: &GHAWManifest{Actions: []GHAWManifestAction{
				{Repo: "github/gh-aw/actions/setup", SHA: "abc1234", Version: "v0.66.1"},
			}},
			actionRefs:  []string{"github/gh-aw-actions/setup@def5678 # v0.68.1"},
			wantAdded:   nil,
			wantRemoved: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			added, removed := collectActionViolations(tt.manifest, tt.actionRefs)
			assert.Equal(t, tt.wantAdded, added, "added actions")
			assert.Equal(t, tt.wantRemoved, removed, "removed actions")
		})
	}
}

func TestEffectiveSafeUpdate(t *testing.T) {
	// effectiveSafeUpdate returns true when strict mode is active (default true),
	// UNLESS the compiler approve flag is set, which skips enforcement entirely.
	tests := []struct {
		name           string
		approveFlag    bool
		rawFrontmatter map[string]any
		want           bool
	}{
		{
			name:        "approve off, no frontmatter => true (strict default)",
			approveFlag: false,
			want:        true, // strict mode defaults to true, so safe update is enabled
		},
		{
			name:        "approve on => false (enforcement skipped)",
			approveFlag: true,
			want:        false,
		},
		{
			name:           "frontmatter strict: false, approve off => false",
			approveFlag:    false,
			rawFrontmatter: map[string]any{"strict": false},
			want:           false,
		},
		{
			name:           "frontmatter strict: false, approve on => false",
			approveFlag:    true,
			rawFrontmatter: map[string]any{"strict": false},
			want:           false,
		},
		{
			name:           "frontmatter strict: true, approve off => true",
			approveFlag:    false,
			rawFrontmatter: map[string]any{"strict": true},
			want:           true,
		},
		{
			name:           "frontmatter strict: true, approve on => false (flag overrides)",
			approveFlag:    true,
			rawFrontmatter: map[string]any{"strict": true},
			want:           false, // --approve overrides strict mode
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := &Compiler{approve: tt.approveFlag}
			data := &WorkflowData{RawFrontmatter: tt.rawFrontmatter}
			got := c.effectiveSafeUpdate(data)
			assert.Equal(t, tt.want, got, "effectiveSafeUpdate result")
		})
	}
}
