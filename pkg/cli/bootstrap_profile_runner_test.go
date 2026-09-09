//go:build !integration

package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestBootstrapActionNeedsMutation(t *testing.T) {
	originalLabelNeedsMutation := bootstrapLabelNeedsMutation
	t.Cleanup(func() {
		bootstrapLabelNeedsMutation = originalLabelNeedsMutation
	})
	bootstrapLabelNeedsMutation = func(_ context.Context, _ string, name, description, color string) (bool, error) {
		return name != "automation" || description != "Managed by automation" || color != "1f6feb", nil
	}

	state := &bootstrapProfileExistingState{
		variables: map[string]struct{}{"EXISTING_VAR": {}, "APP_ID": {}},
		secrets:   map[string]struct{}{"EXISTING_SECRET": {}},
	}

	tests := []struct {
		name             string
		action           repositoryPackageBootstrapAction
		usesActionsToken bool
		want             bool
	}{
		{name: "repo variable missing", action: repositoryPackageBootstrapAction{Type: "repo-variable", Name: "NEW_VAR"}, want: true},
		{name: "repo variable existing", action: repositoryPackageBootstrapAction{Type: "repo-variable", Name: "EXISTING_VAR"}, want: false},
		{name: "repo secret missing", action: repositoryPackageBootstrapAction{Type: "repo-secret", Name: "NEW_SECRET"}, want: true},
		{name: "repo secret existing", action: repositoryPackageBootstrapAction{Type: "repo-secret", Name: "EXISTING_SECRET"}, want: false},
		{name: "repo label reconciled", action: repositoryPackageBootstrapAction{Type: "repo-label", Name: "automation", Description: "Managed by automation", Color: "1f6feb"}, want: false},
		{name: "repo label pending", action: repositoryPackageBootstrapAction{Type: "repo-label", Name: "automation", Description: "Old description", Color: "1f6feb"}, want: true},
		{name: "github app partial", action: repositoryPackageBootstrapAction{Type: "github-app", AppIDVariable: "APP_ID", PrivateKeySecret: "APP_PRIVATE_KEY"}, want: true},
		{name: "copilot auth with actions token", action: repositoryPackageBootstrapAction{Type: "copilot-auth", Secret: "COPILOT_TOKEN"}, usesActionsToken: true, want: false},
		{name: "commit push always pending", action: repositoryPackageBootstrapAction{Type: "commit-and-push"}, want: true},
		{name: "handoff never pending", action: repositoryPackageBootstrapAction{Type: "handoff"}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := bootstrapActionNeedsMutation(context.Background(), "octo/platform-ops", tt.action, state, tt.usesActionsToken)
			if err != nil {
				t.Fatalf("bootstrapActionNeedsMutation returned error: %v", err)
			}
			if got != tt.want {
				t.Fatalf("bootstrapActionNeedsMutation returned %t, want %t", got, tt.want)
			}
		})
	}
}

func TestBootstrapProfileState(t *testing.T) {
	originalRunGH := runBootstrapGHContext
	t.Cleanup(func() {
		runBootstrapGHContext = originalRunGH
	})

	runBootstrapGHContext = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		if len(args) > 1 && args[1] == "/repos/octo/platform-ops/actions/variables?per_page=100" {
			return []byte("BETA\nALPHA\n"), nil
		}
		return []byte("SECRET_ONE\n"), nil
	}

	state, err := bootstrapProfileState(context.Background(), "octo/platform-ops")
	if err != nil {
		t.Fatalf("bootstrapProfileState returned error: %v", err)
	}
	if _, ok := state.variables["ALPHA"]; !ok {
		t.Fatal("expected ALPHA variable")
	}
	if _, ok := state.variables["BETA"]; !ok {
		t.Fatal("expected BETA variable")
	}
	if _, ok := state.secrets["SECRET_ONE"]; !ok {
		t.Fatal("expected SECRET_ONE secret")
	}
}

// TestPrintBootstrapConfigTODO_PreservesManifestOrder verifies that all declared config
// actions are included in the TODO output in their exact manifest order. This guards
// against regressions that split actions into pre-install and post-install phases and
// thereby reorder or omit steps.
func TestPrintBootstrapConfigTODO_PreservesManifestOrder(t *testing.T) {
	t.Parallel()
	// Declare a profile with an interleaved mix of action types to ensure ordering
	// cannot be accidentally satisfied by any implicit categorisation.
	profile := &resolvedBootstrapProfile{
		PackageID: "owner/repo",
		Profile: &repositoryPackageBootstrap{
			Config: []repositoryPackageBootstrapAction{
				{Type: "require-owner-type", Value: "Organization"},
				{Type: "copilot-auth", Secret: "COPILOT_TOKEN"},
				{Type: "repo-variable", Name: "MY_VAR"},
				{Type: "github-app", AppIDVariable: "APP_ID", PrivateKeySecret: "APP_KEY"},
				{Type: "repo-secret", Name: "MY_SECRET"},
				{Type: "repo-label", Name: "automation", Color: "1f6feb"},
				{Type: "commit-and-push", Message: "commit changes"},
				{Type: "handoff", Message: "all done"},
			},
		},
	}

	var buf bytes.Buffer
	printBootstrapConfigTODO(&buf, profile)
	output := buf.String()

	// Every action type must appear in the output.
	for _, want := range []string{
		"Organization",   // require-owner-type
		"COPILOT_TOKEN",  // copilot-auth
		"MY_VAR",         // repo-variable
		"APP_ID",         // github-app
		"MY_SECRET",      // repo-secret
		"automation",     // repo-label
		"commit changes", // commit-and-push
		"all done",       // handoff
	} {
		if !strings.Contains(output, want) {
			t.Errorf("expected %q in output, got:\n%s", want, output)
		}
	}

	// Verify declared order is preserved in the output.
	positions := []int{
		strings.Index(output, "Organization"),
		strings.Index(output, "COPILOT_TOKEN"),
		strings.Index(output, "MY_VAR"),
		strings.Index(output, "APP_ID"),
		strings.Index(output, "MY_SECRET"),
		strings.Index(output, "automation"),
		strings.Index(output, "commit changes"),
		strings.Index(output, "all done"),
	}
	for i := 1; i < len(positions); i++ {
		if positions[i-1] < 0 || positions[i] < 0 {
			continue // already reported above
		}
		if positions[i-1] >= positions[i] {
			t.Errorf("action at position %d appears after action at position %d (ordering regression)", i-1, i)
		}
	}
}

// TestExecuteBootstrapProfile_DisableGitHubAppPermissionInferenceSkipsResolution verifies
// that setting DisableGitHubAppPermissionInference on the run config skips inferring
// GitHub App requirements from the package's resolved workflows entirely. The inference
// function is stubbed to fail so any invocation surfaces as an error.
func TestExecuteBootstrapProfile_DisableGitHubAppPermissionInferenceSkipsResolution(t *testing.T) {
	originalRunGH := runBootstrapGHContext
	originalInfer := bootstrapInferGitHubAppRequires
	t.Cleanup(func() {
		runBootstrapGHContext = originalRunGH
		bootstrapInferGitHubAppRequires = originalInfer
	})
	runBootstrapGHContext = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte("APP_ID\nAPP_PRIVATE_KEY\n"), nil
	}
	inferCalled := false
	bootstrapInferGitHubAppRequires = func(_ context.Context, _ []string) (map[string]string, []string, error) {
		inferCalled = true
		return nil, nil, errors.New("inference must not run when disabled")
	}

	profile := &resolvedBootstrapProfile{
		PackageID: "owner/repo",
		Profile: &repositoryPackageBootstrap{
			Config: []repositoryPackageBootstrapAction{
				{Type: "github-app", AppIDVariable: "APP_ID", PrivateKeySecret: "APP_PRIVATE_KEY"},
			},
		},
	}

	err := executeBootstrapProfile(context.Background(), bootstrapProfileRunConfig{
		Repo:                                "octo/platform-ops",
		Sources:                             nil,
		Profile:                             profile,
		DisableGitHubAppPermissionInference: true,
	})
	if err != nil {
		t.Fatalf("executeBootstrapProfile returned error with inference disabled: %v", err)
	}
	if inferCalled {
		t.Fatal("expected inference function not to be called when DisableGitHubAppPermissionInference is true")
	}
}

// TestExecuteBootstrapProfile_InfersRequirementsFromRealWorkflows is an end-to-end
// integration test that exercises the real (unstubbed) inference pipeline: it resolves
// actual workflow files from disk and verifies the merged permissions/events reach
// GitHub App creation. The merge happens on a per-iteration local copy of the action
// (not written back into the profile), so this test drives the flow far enough to
// observe the merged values on the action passed to bootstrapCreateGitHubApp.
func TestExecuteBootstrapProfile_InfersRequirementsFromRealWorkflows(t *testing.T) {
	restore := stubBootstrapGitHubAppCreationForInferenceTest(t)

	var capturedAction repositoryPackageBootstrapAction
	bootstrapCreateGitHubApp = func(_ context.Context, _, _, _, _ string, action repositoryPackageBootstrapAction, _ bootstrapGitHubAppOverrides) (*bootstrapCreatedGitHubApp, error) {
		capturedAction = action
		return &bootstrapCreatedGitHubApp{ClientID: "client-id", PEM: "pem"}, nil
	}
	t.Cleanup(restore)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.md")
	second := filepath.Join(dir, "second.md")
	if err := os.WriteFile(first, []byte("---\non: issues\npermissions:\n  issues: write\n---\n\n# First\n"), 0o644); err != nil {
		t.Fatalf("failed to write first workflow: %v", err)
	}
	if err := os.WriteFile(second, []byte("---\non:\n  pull_request:\n    types: [opened]\n  schedule:\n    - cron: \"0 0 * * *\"\npermissions:\n  contents: write\n---\n\n# Second\n"), 0o644); err != nil {
		t.Fatalf("failed to write second workflow: %v", err)
	}

	profile := &resolvedBootstrapProfile{
		PackageID: "owner/repo",
		Profile: &repositoryPackageBootstrap{
			Config: []repositoryPackageBootstrapAction{
				{Type: "github-app", AppIDVariable: "APP_ID", PrivateKeySecret: "APP_PRIVATE_KEY"},
			},
		},
	}

	err := executeBootstrapProfile(context.Background(), bootstrapProfileRunConfig{
		Repo:    "octo/platform-ops",
		Sources: []string{first, second},
		Profile: profile,
	})
	if err != nil {
		t.Fatalf("executeBootstrapProfile returned error: %v", err)
	}

	wantPermissions := map[string]string{"issues": "write", "contents": "write"}
	if !reflect.DeepEqual(capturedAction.Permissions, wantPermissions) {
		t.Fatalf("merged permissions = %v, want %v", capturedAction.Permissions, wantPermissions)
	}
	// mergeBootstrapGitHubAppRequirements always returns events sorted alphabetically,
	// so this exact-order comparison is deterministic.
	wantEvents := []string{"issues", "pull_request"}
	if !reflect.DeepEqual(capturedAction.Events, wantEvents) {
		t.Fatalf("merged events = %v, want %v (schedule must be excluded)", capturedAction.Events, wantEvents)
	}
}

// TestExecuteBootstrapProfile_InferredRequirementsMergeWithDeclaredManifestValues
// verifies that permissions/events declared explicitly in the aw.yml manifest's
// github-app action survive and are combined with (not replaced by) the values
// inferred from the package's resolved workflows.
func TestExecuteBootstrapProfile_InferredRequirementsMergeWithDeclaredManifestValues(t *testing.T) {
	restore := stubBootstrapGitHubAppCreationForInferenceTest(t)

	var capturedAction repositoryPackageBootstrapAction
	bootstrapCreateGitHubApp = func(_ context.Context, _, _, _, _ string, action repositoryPackageBootstrapAction, _ bootstrapGitHubAppOverrides) (*bootstrapCreatedGitHubApp, error) {
		capturedAction = action
		return &bootstrapCreatedGitHubApp{ClientID: "client-id", PEM: "pem"}, nil
	}
	t.Cleanup(restore)

	dir := t.TempDir()
	wf := filepath.Join(dir, "wf.md")
	if err := os.WriteFile(wf, []byte("---\non: issues\npermissions:\n  issues: read\n---\n\n# Workflow\n"), 0o644); err != nil {
		t.Fatalf("failed to write workflow: %v", err)
	}

	profile := &resolvedBootstrapProfile{
		PackageID: "owner/repo",
		Profile: &repositoryPackageBootstrap{
			Config: []repositoryPackageBootstrapAction{
				{
					Type:             "github-app",
					AppIDVariable:    "APP_ID",
					PrivateKeySecret: "APP_PRIVATE_KEY",
					Permissions:      map[string]string{"contents": "read"},
					Events:           []string{"push"},
				},
			},
		},
	}

	err := executeBootstrapProfile(context.Background(), bootstrapProfileRunConfig{
		Repo:    "octo/platform-ops",
		Sources: []string{wf},
		Profile: profile,
	})
	if err != nil {
		t.Fatalf("executeBootstrapProfile returned error: %v", err)
	}

	// The manifest-declared contents:read is preserved, and the inferred issues:read
	// (from the workflow's own "issues: read" permission) is added alongside it.
	wantPermissions := map[string]string{"contents": "read", "issues": "read"}
	if !reflect.DeepEqual(capturedAction.Permissions, wantPermissions) {
		t.Fatalf("merged permissions = %v, want %v", capturedAction.Permissions, wantPermissions)
	}
	// mergeBootstrapGitHubAppRequirements always returns events sorted alphabetically,
	// so this exact-order comparison is deterministic.
	wantEvents := []string{"issues", "push"}
	if !reflect.DeepEqual(capturedAction.Events, wantEvents) {
		t.Fatalf("merged events = %v, want %v", capturedAction.Events, wantEvents)
	}
}

// TestExecuteBootstrapProfile_InferenceScopedToProfileSourceOnly verifies that when the
// resolved bootstrap profile carries a Source (the package that produced it), inference
// resolves only that source's workflows rather than every source config.Sources may
// contain. This prevents an unrelated standalone workflow/package installed alongside the
// bootstrap-profile package from leaking its permissions/events into this package's App.
func TestExecuteBootstrapProfile_InferenceScopedToProfileSourceOnly(t *testing.T) {
	restore := stubBootstrapGitHubAppCreationForInferenceTest(t)

	var capturedAction repositoryPackageBootstrapAction
	bootstrapCreateGitHubApp = func(_ context.Context, _, _, _, _ string, action repositoryPackageBootstrapAction, _ bootstrapGitHubAppOverrides) (*bootstrapCreatedGitHubApp, error) {
		capturedAction = action
		return &bootstrapCreatedGitHubApp{ClientID: "client-id", PEM: "pem"}, nil
	}
	t.Cleanup(restore)

	dir := t.TempDir()
	packageWorkflow := filepath.Join(dir, "package.md")
	unrelatedWorkflow := filepath.Join(dir, "unrelated.md")
	if err := os.WriteFile(packageWorkflow, []byte("---\non: issues\npermissions:\n  issues: write\n---\n\n# Package\n"), 0o644); err != nil {
		t.Fatalf("failed to write package workflow: %v", err)
	}
	if err := os.WriteFile(unrelatedWorkflow, []byte("---\non: pull_request\npermissions:\n  contents: write\n---\n\n# Unrelated\n"), 0o644); err != nil {
		t.Fatalf("failed to write unrelated workflow: %v", err)
	}

	profile := &resolvedBootstrapProfile{
		PackageID: "owner/repo",
		Source:    packageWorkflow,
		Profile: &repositoryPackageBootstrap{
			Config: []repositoryPackageBootstrapAction{
				{Type: "github-app", AppIDVariable: "APP_ID", PrivateKeySecret: "APP_PRIVATE_KEY"},
			},
		},
	}

	// config.Sources includes the unrelated standalone workflow installed in the same
	// run; only profile.Source's workflows should influence the inferred App scopes.
	err := executeBootstrapProfile(context.Background(), bootstrapProfileRunConfig{
		Repo:    "octo/platform-ops",
		Sources: []string{packageWorkflow, unrelatedWorkflow},
		Profile: profile,
	})
	if err != nil {
		t.Fatalf("executeBootstrapProfile returned error: %v", err)
	}

	wantPermissions := map[string]string{"issues": "write"}
	if !reflect.DeepEqual(capturedAction.Permissions, wantPermissions) {
		t.Fatalf("merged permissions = %v, want %v (unrelated workflow's contents:write must not leak in)", capturedAction.Permissions, wantPermissions)
	}
	wantEvents := []string{"issues"}
	if !reflect.DeepEqual(capturedAction.Events, wantEvents) {
		t.Fatalf("merged events = %v, want %v (unrelated workflow's pull_request must not leak in)", capturedAction.Events, wantEvents)
	}
}

// stubBootstrapGitHubAppCreationForInferenceTest stubs the collaborators needed to drive
// executeBootstrapProfile all the way to bootstrapCreateGitHubApp without any network
// access or interactive prompts, returning a restore func to reset all stubbed globals.
func stubBootstrapGitHubAppCreationForInferenceTest(t *testing.T) func() {
	t.Helper()
	originalRunGH := runBootstrapGHContext
	originalCheckOwnerType := bootstrapCheckOwnerType
	originalCreateApp := bootstrapCreateGitHubApp
	originalUpsertVariable := bootstrapUpsertVariable
	originalSetSecret := bootstrapSetSecret
	t.Setenv(bootstrapGitHubAppModeEnv, "create")

	runBootstrapGHContext = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(""), nil
	}
	bootstrapCheckOwnerType = func(_ context.Context, _ string) (string, error) {
		return "Organization", nil
	}
	bootstrapUpsertVariable = func(_ context.Context, _, _, _ string) error { return nil }
	bootstrapSetSecret = func(_ context.Context, _, _, _ string) error { return nil }
	// Safe default: callers are expected to override this immediately with a capturing
	// stub, but default to a no-op (no network access) in case they don't.
	bootstrapCreateGitHubApp = func(_ context.Context, _, _, _, _ string, _ repositoryPackageBootstrapAction, _ bootstrapGitHubAppOverrides) (*bootstrapCreatedGitHubApp, error) {
		return &bootstrapCreatedGitHubApp{ClientID: "client-id", PEM: "pem"}, nil
	}

	return func() {
		runBootstrapGHContext = originalRunGH
		bootstrapCheckOwnerType = originalCheckOwnerType
		bootstrapCreateGitHubApp = originalCreateApp
		bootstrapUpsertVariable = originalUpsertVariable
		bootstrapSetSecret = originalSetSecret
	}
}

// TestExecuteBootstrapProfile_NoGitHubAppActionSkipsInferenceEntirely verifies that
// when the resolved bootstrap profile has no github-app action at all, inference is
// never invoked, even though valid, resolvable sources were provided.
func TestExecuteBootstrapProfile_NoGitHubAppActionSkipsInferenceEntirely(t *testing.T) {
	originalRunGH := runBootstrapGHContext
	originalInfer := bootstrapInferGitHubAppRequires
	originalUpsertVariable := bootstrapUpsertVariable
	t.Cleanup(func() {
		runBootstrapGHContext = originalRunGH
		bootstrapInferGitHubAppRequires = originalInfer
		bootstrapUpsertVariable = originalUpsertVariable
	})
	runBootstrapGHContext = func(_ context.Context, _ string, _ ...string) ([]byte, error) {
		return []byte(""), nil
	}
	bootstrapUpsertVariable = func(_ context.Context, _, _, _ string) error { return nil }
	inferCalled := false
	bootstrapInferGitHubAppRequires = func(_ context.Context, _ []string) (map[string]string, []string, error) {
		inferCalled = true
		return nil, nil, errors.New("inference must not run without a github-app action")
	}

	dir := t.TempDir()
	wf := filepath.Join(dir, "wf.md")
	if err := os.WriteFile(wf, []byte("---\non: issues\npermissions:\n  issues: read\n---\n\n# Workflow\n"), 0o644); err != nil {
		t.Fatalf("failed to write workflow: %v", err)
	}

	profile := &resolvedBootstrapProfile{
		PackageID: "owner/repo",
		Profile: &repositoryPackageBootstrap{
			Config: []repositoryPackageBootstrapAction{
				{Type: "repo-variable", Name: "MY_VAR", Default: "value"},
			},
		},
	}

	err := executeBootstrapProfile(context.Background(), bootstrapProfileRunConfig{
		Repo:    "octo/platform-ops",
		Sources: []string{wf},
		Profile: profile,
	})
	if err != nil {
		t.Fatalf("executeBootstrapProfile returned error even though no github-app action requires inference: %v", err)
	}
	if inferCalled {
		t.Fatal("expected inference function not to be called when no github-app action is present")
	}
}

// TestExecuteBootstrapProfile_InferenceResolutionErrorPropagates verifies that when a
// github-app action is present and inference is enabled, a resolution failure for one
// of the package's sources aborts the whole bootstrap run with an error.
func TestExecuteBootstrapProfile_InferenceResolutionErrorPropagates(t *testing.T) {
	profile := &resolvedBootstrapProfile{
		PackageID: "owner/repo",
		Profile: &repositoryPackageBootstrap{
			Config: []repositoryPackageBootstrapAction{
				{Type: "github-app", AppIDVariable: "APP_ID", PrivateKeySecret: "APP_PRIVATE_KEY"},
			},
		},
	}

	err := executeBootstrapProfile(context.Background(), bootstrapProfileRunConfig{
		Repo:    "octo/platform-ops",
		Sources: []string{"/nonexistent/does-not-exist.md"},
		Profile: profile,
	})
	if err == nil {
		t.Fatal("expected an error when the package's workflow sources cannot be resolved")
	}
}
