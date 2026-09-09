//go:build !integration

package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"slices"
	"strings"
	"testing"

	"github.com/cli/go-gh/v2/pkg/api"
)

type bootstrapLabelRESTClientStub struct {
	existing   *bootstrapRepositoryLabel
	getErr     error
	getPaths   []string
	postPaths  []string
	patchPaths []string
	posts      [][]byte
	patches    [][]byte
}

func (s *bootstrapLabelRESTClientStub) Get(path string, response any) error {
	s.getPaths = append(s.getPaths, path)
	if s.getErr != nil {
		return s.getErr
	}
	*response.(*bootstrapRepositoryLabel) = *s.existing
	return nil
}

func (s *bootstrapLabelRESTClientStub) Post(path string, body io.Reader, _ any) error {
	content, _ := io.ReadAll(body)
	s.postPaths = append(s.postPaths, path)
	s.posts = append(s.posts, content)
	return nil
}

func (s *bootstrapLabelRESTClientStub) Patch(path string, body io.Reader, _ any) error {
	content, _ := io.ReadAll(body)
	s.patchPaths = append(s.patchPaths, path)
	s.patches = append(s.patches, content)
	return nil
}

type bootstrapLabelStatusError struct {
	status int
}

func (e bootstrapLabelStatusError) Error() string {
	return "label lookup failed"
}

func (e bootstrapLabelStatusError) StatusCode() int {
	return e.status
}

func TestListBootstrapRepoNamesPaginate(t *testing.T) {
	originalRunGH := runBootstrapGHContext
	t.Cleanup(func() {
		runBootstrapGHContext = originalRunGH
	})

	calls := []string{}
	runBootstrapGHContext = func(_ context.Context, _ string, args ...string) ([]byte, error) {
		calls = append(calls, strings.Join(args, " "))
		if strings.Contains(args[1], "/variables") {
			return []byte("ALPHA\nOMEGA\n"), nil
		}
		return []byte("FIRST\nSECOND\n"), nil
	}

	variables, err := listBootstrapRepoVariableNames(context.Background(), "octo/platform-ops")
	if err != nil {
		t.Fatalf("listBootstrapRepoVariableNames returned error: %v", err)
	}
	if !slices.Equal(variables, []string{"ALPHA", "OMEGA"}) {
		t.Fatalf("unexpected variables: %#v", variables)
	}

	secrets, err := listBootstrapRepoSecretNames(context.Background(), "octo/platform-ops")
	if err != nil {
		t.Fatalf("listBootstrapRepoSecretNames returned error: %v", err)
	}
	if !slices.Equal(secrets, []string{"FIRST", "SECOND"}) {
		t.Fatalf("unexpected secrets: %#v", secrets)
	}

	if len(calls) != 2 {
		t.Fatalf("expected 2 gh api calls, got %d", len(calls))
	}
	for _, call := range calls {
		if !strings.Contains(call, "--paginate") {
			t.Fatalf("expected paginated gh api call, got %q", call)
		}
	}
}

func TestRunBootstrapRepoVariableAction(t *testing.T) {
	originalUpsertVariable := bootstrapUpsertVariable
	t.Cleanup(func() {
		bootstrapUpsertVariable = originalUpsertVariable
	})

	t.Setenv(bootstrapRepositoryVariableEnvName("MY_VAR"), "configured")
	var gotName, gotValue string
	bootstrapUpsertVariable = func(_ context.Context, _ string, name, value string) error {
		gotName = name
		gotValue = value
		return nil
	}

	applied, err := runBootstrapRepoVariableAction(context.Background(), "octo/platform-ops", repositoryPackageBootstrapAction{
		Name: "MY_VAR",
	}, &bootstrapProfileExistingState{variables: map[string]struct{}{}, secrets: map[string]struct{}{}})
	if err != nil {
		t.Fatalf("runBootstrapRepoVariableAction returned error: %v", err)
	}
	if !applied {
		t.Fatal("expected variable action to apply")
	}
	if gotName != "MY_VAR" || gotValue != "configured" {
		t.Fatalf("unexpected variable write: %s=%s", gotName, gotValue)
	}
}

func TestRunBootstrapRepoSecretAction(t *testing.T) {
	originalSetSecret := bootstrapSetSecret
	t.Cleanup(func() {
		bootstrapSetSecret = originalSetSecret
	})

	t.Setenv(bootstrapRepositorySecretEnvName("MY_SECRET"), "top-secret")
	var gotName, gotValue string
	bootstrapSetSecret = func(_ context.Context, _ string, name, value string) error {
		gotName = name
		gotValue = value
		return nil
	}

	applied, err := runBootstrapRepoSecretAction(context.Background(), "octo/platform-ops", repositoryPackageBootstrapAction{
		Name: "MY_SECRET",
	}, &bootstrapProfileExistingState{variables: map[string]struct{}{}, secrets: map[string]struct{}{}})
	if err != nil {
		t.Fatalf("runBootstrapRepoSecretAction returned error: %v", err)
	}
	if !applied {
		t.Fatal("expected secret action to apply")
	}
	if gotName != "MY_SECRET" || gotValue != "top-secret" {
		t.Fatalf("unexpected secret write: %s=%s", gotName, gotValue)
	}
}

func TestRunBootstrapCopilotAuthAction(t *testing.T) {
	t.Run("skips actions token auth", func(t *testing.T) {
		applied, err := runBootstrapCopilotAuthAction(context.Background(), "octo/platform-ops", repositoryPackageBootstrapAction{
			Secret: "COPILOT_TOKEN",
		}, &bootstrapProfileExistingState{variables: map[string]struct{}{}, secrets: map[string]struct{}{}}, true)
		if err != nil {
			t.Fatalf("runBootstrapCopilotAuthAction returned error: %v", err)
		}
		if applied {
			t.Fatal("expected action to skip when Actions token auth is enabled")
		}
	})

	t.Run("stores valid pat", func(t *testing.T) {
		originalSetSecret := bootstrapSetSecret
		t.Cleanup(func() {
			bootstrapSetSecret = originalSetSecret
		})

		t.Setenv("COPILOT_TOKEN", "github_pat_abc123xyz")
		var wrote string
		bootstrapSetSecret = func(_ context.Context, _ string, name, value string) error {
			wrote = name + "=" + value
			return nil
		}

		applied, err := runBootstrapCopilotAuthAction(context.Background(), "octo/platform-ops", repositoryPackageBootstrapAction{
			Secret: "COPILOT_TOKEN",
		}, &bootstrapProfileExistingState{variables: map[string]struct{}{}, secrets: map[string]struct{}{}}, false)
		if err != nil {
			t.Fatalf("runBootstrapCopilotAuthAction returned error: %v", err)
		}
		if !applied {
			t.Fatal("expected action to apply")
		}
		if wrote != "COPILOT_TOKEN=github_pat_abc123xyz" {
			t.Fatalf("unexpected secret write: %s", wrote)
		}
	})
}

func TestUpsertBootstrapRepoLabelWithClient(t *testing.T) {
	t.Run("skips matching label", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			existing: &bootstrapRepositoryLabel{Name: "Automation", Description: "Managed by automation", Color: "1F6FEB"},
		}
		err := upsertBootstrapRepoLabelWithClient(client, "octo", "platform-ops", "automation", "Managed by automation", "1f6feb")
		if err != nil {
			t.Fatalf("upsertBootstrapRepoLabelWithClient returned error: %v", err)
		}
		if len(client.posts) != 0 || len(client.patches) != 0 {
			t.Fatal("expected matching label not to be mutated")
		}
	})

	t.Run("updates differing label", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			existing: &bootstrapRepositoryLabel{Name: "automation", Description: "Old description", Color: "ffffff"},
		}
		err := upsertBootstrapRepoLabelWithClient(client, "octo", "platform-ops", "automation", "Managed by automation", "1f6feb")
		if err != nil {
			t.Fatalf("upsertBootstrapRepoLabelWithClient returned error: %v", err)
		}
		if len(client.patches) != 1 {
			t.Fatalf("expected one label update, got %d", len(client.patches))
		}
		var body map[string]string
		if err := json.NewDecoder(bytes.NewReader(client.patches[0])).Decode(&body); err != nil {
			t.Fatalf("failed to decode update body: %v", err)
		}
		if body["new_name"] != "automation" || body["description"] != "Managed by automation" || body["color"] != "1f6feb" {
			t.Fatalf("unexpected update body: %#v", body)
		}
	})

	t.Run("creates missing label", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			getErr: fmt.Errorf("lookup failed: %w", bootstrapLabelStatusError{status: 404}),
		}
		err := upsertBootstrapRepoLabelWithClient(client, "octo", "platform-ops", "automation", "Managed by automation", "1f6feb")
		if err != nil {
			t.Fatalf("upsertBootstrapRepoLabelWithClient returned error: %v", err)
		}
		if len(client.posts) != 1 {
			t.Fatalf("expected one label creation, got %d", len(client.posts))
		}
		var body bootstrapRepositoryLabel
		if err := json.NewDecoder(bytes.NewReader(client.posts[0])).Decode(&body); err != nil {
			t.Fatalf("failed to decode create body: %v", err)
		}
		if body.Name != "automation" || body.Description != "Managed by automation" || body.Color != "1f6feb" {
			t.Fatalf("unexpected create body: %#v", body)
		}
	})

	t.Run("escapes label name in lookup path", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			getErr: &api.HTTPError{StatusCode: 404, RequestURL: &url.URL{}},
		}
		err := upsertBootstrapRepoLabelWithClient(client, "octo", "platform-ops", "needs/review α", "Managed by automation", "1f6feb")
		if err != nil {
			t.Fatalf("upsertBootstrapRepoLabelWithClient returned error: %v", err)
		}
		if len(client.getPaths) != 1 || client.getPaths[0] != "repos/octo/platform-ops/labels/needs%2Freview%20%CE%B1" {
			t.Fatalf("unexpected lookup paths: %#v", client.getPaths)
		}
		if len(client.postPaths) != 1 || client.postPaths[0] != "repos/octo/platform-ops/labels" {
			t.Fatalf("unexpected create paths: %#v", client.postPaths)
		}
	})

	t.Run("returns non-not-found lookup errors", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			getErr: &api.HTTPError{StatusCode: 500, RequestURL: &url.URL{}},
		}
		err := upsertBootstrapRepoLabelWithClient(client, "octo", "platform-ops", "automation", "Managed by automation", "1f6feb")
		if err == nil || !strings.Contains(err.Error(), "failed to inspect repository label") {
			t.Fatalf("expected label lookup error, got %v", err)
		}
		if len(client.posts) != 0 || len(client.patches) != 0 {
			t.Fatal("expected lookup failure not to mutate the label")
		}
	})
}

func TestRepoLabelNeedsMutationWithClient(t *testing.T) {
	t.Run("matching label does not need mutation", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			existing: &bootstrapRepositoryLabel{Name: "Automation", Description: "Managed by automation", Color: "1F6FEB"},
		}
		got, err := repoLabelNeedsMutationWithClient(client, "octo", "platform-ops", "automation", "Managed by automation", "1f6feb")
		if err != nil {
			t.Fatalf("repoLabelNeedsMutationWithClient returned error: %v", err)
		}
		if got {
			t.Fatal("expected matching label not to need mutation")
		}
	})

	t.Run("missing label needs mutation", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			getErr: &api.HTTPError{StatusCode: 404, RequestURL: &url.URL{}},
		}
		got, err := repoLabelNeedsMutationWithClient(client, "octo", "platform-ops", "automation", "Managed by automation", "1f6feb")
		if err != nil {
			t.Fatalf("repoLabelNeedsMutationWithClient returned error: %v", err)
		}
		if !got {
			t.Fatal("expected missing label to need mutation")
		}
	})

	t.Run("differing label needs mutation", func(t *testing.T) {
		client := &bootstrapLabelRESTClientStub{
			existing: &bootstrapRepositoryLabel{Name: "automation", Description: "Old description", Color: "ffffff"},
		}
		got, err := repoLabelNeedsMutationWithClient(client, "octo", "platform-ops", "automation", "Managed by automation", "1f6feb")
		if err != nil {
			t.Fatalf("repoLabelNeedsMutationWithClient returned error: %v", err)
		}
		if !got {
			t.Fatal("expected differing label to need mutation")
		}
	})
}

func TestBootstrapRepoMutationHelpers_RejectInvalidRepo(t *testing.T) {
	if err := upsertBootstrapRepoVariable("not-a-repo", "NAME", "value"); err == nil {
		t.Fatal("expected invalid repo slug error for variable upsert")
	}
	if err := setBootstrapRepoSecret("not-a-repo", "NAME", "value"); err == nil {
		t.Fatal("expected invalid repo slug error for secret set")
	}
	if err := upsertBootstrapRepoLabel(context.Background(), "not-a-repo", "automation", "Managed by automation", "1f6feb"); err == nil {
		t.Fatal("expected invalid repo slug error for label upsert")
	}
}
