package cli

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workqueue"
)

type workAPIRoundTripper func(*http.Request) (*http.Response, error)

func (f workAPIRoundTripper) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestWorkCommandEndToEndWithoutCheckout(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	t.Setenv("GH_HOST", "github.com")
	t.Setenv("GH_TOKEN", "test-token")
	const remote = "owner/repo"
	var log, pending string
	head := 0
	originalTransport := http.DefaultTransport
	t.Cleanup(func() { http.DefaultTransport = originalTransport })
	http.DefaultTransport = workAPIRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Header.Get("Authorization") != "token test-token" {
			t.Error("queue API request was not authenticated")
		}
		path := strings.TrimPrefix(r.URL.Path, "/repos/"+remote+"/")
		status := http.StatusOK
		var result any
		switch {
		case r.Method == http.MethodGet && path == "git/ref/heads/"+workqueue.DefaultBranch:
			if head == 0 {
				status = http.StatusNotFound
				result = map[string]string{"message": "Not Found"}
			} else {
				result = map[string]any{"ref": "refs/heads/" + workqueue.DefaultBranch, "object": map[string]string{"sha": strconv.Itoa(head)}}
			}
		case r.Method == http.MethodGet && r.URL.Path == "/repos/"+remote:
			result = map[string]string{"full_name": remote}
		case r.Method == http.MethodGet && strings.HasPrefix(path, "git/commits/"):
			result = map[string]any{"tree": map[string]string{"sha": "tree"}}
		case r.Method == http.MethodGet && path == "git/trees/tree":
			result = map[string]any{"tree": []map[string]string{
				{"path": workqueue.FileName, "mode": "100644", "type": "blob", "sha": "blob"},
			}}
		case r.Method == http.MethodGet && path == "git/blobs/blob":
			result = map[string]string{"encoding": "base64", "content": base64.StdEncoding.EncodeToString([]byte(log))}
		case r.Method == http.MethodPost && path == "git/trees":
			var body struct {
				Tree []struct{ Content string }
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				return nil, err
			}
			for _, entry := range body.Tree {
				pending = entry.Content
			}
			result = map[string]string{"sha": "tree"}
		case r.Method == http.MethodPost && path == "git/commits":
			result = map[string]string{"sha": strconv.Itoa(head + 1)}
		case (r.Method == http.MethodPost && path == "git/refs") ||
			(r.Method == http.MethodPatch && path == "git/refs/heads/"+workqueue.DefaultBranch):
			head++
			log = pending
			result = map[string]string{"ref": "refs/heads/" + workqueue.DefaultBranch}
		default:
			t.Errorf("unexpected queue API request: %s %s", r.Method, r.URL.Path)
			status = http.StatusNotFound
			result = map[string]string{"message": "Not Found"}
		}
		data, err := json.Marshal(result)
		if err != nil {
			return nil, err
		}
		return &http.Response{
			StatusCode: status, Header: http.Header{"Content-Type": {"application/json"}},
			Body: io.NopCloser(bytes.NewReader(data)), Request: r,
		}, nil
	})
	payload := filepath.Join(t.TempDir(), "work.json")
	if err := os.WriteFile(payload, []byte(`{"task":"review"}`), constants.FilePermSensitive); err != nil {
		t.Fatal(err)
	}
	run := func(args ...string) map[string]any {
		t.Helper()
		command := NewWorkCommand()
		var output bytes.Buffer
		command.SetOut(&output)
		command.SetErr(&output)
		command.SetArgs(append([]string{"--repo", remote, "--json"}, args...))
		if err := command.Execute(); err != nil {
			t.Fatalf("work %v: %v (%s)", args, err, output.String())
		}
		var result map[string]any
		if err := json.Unmarshal(output.Bytes(), &result); err != nil {
			t.Fatalf("invalid JSON output %q: %v", output.String(), err)
		}
		return result
	}
	submitted := run("submit-work", "--file", payload)
	workID := submitted["work_id"].(string)
	if !submitted["created"].(bool) || len(workID) != 64 {
		t.Fatalf("unexpected submit: %v", submitted)
	}
	if run("submit-work", "--file", payload)["created"] != false {
		t.Fatal("duplicate submission was not idempotent")
	}
	claimed := run("claim", "--work-id", workID, "--run-id", "run-1")
	claimID := claimed["claim_id"].(string)
	if claimID == "" {
		t.Fatal("claim ID missing")
	}
	run("finish", "--claim-id", claimID, "--attempt-id", "attempt-1")
	state := run("replay")
	works := state["works"].([]any)
	if works[0].(map[string]any)["state"] != "completed" {
		t.Fatalf("completion did not persist: %v", state)
	}
	if run("stats")["completed"] != float64(1) {
		t.Fatal("stats did not reflect completion")
	}
	run("compact")
	if run("compact")["changed"] != false {
		t.Fatal("second compaction should not change the canonical log")
	}
	second := NewWorkCommand()
	second.SetIn(strings.NewReader(`{"task":"another"}`))
	second.SetArgs([]string{"--repo", remote, "--json", "submit-work", "--file", "-"})
	var secondOutput bytes.Buffer
	second.SetOut(&secondOutput)
	if err := second.Execute(); err != nil {
		t.Fatal(err)
	}
	var submittedSecond map[string]any
	if err := json.Unmarshal(secondOutput.Bytes(), &submittedSecond); err != nil {
		t.Fatal(err)
	}
	secondID := submittedSecond["work_id"].(string)
	secondClaim := run("claim", "--work-id", secondID, "--run-id", "run-2")["claim_id"].(string)
	if run("cancel-claim", "--claim-id", secondClaim)["cancelled"] != true {
		t.Fatal("claim cancellation failed")
	}
	if run("cancel-work", "--work-id", secondID)["cancelled"] != true {
		t.Fatal("work cancellation failed")
	}
	if run("stats")["cancelled"] != float64(1) {
		t.Fatal("stats did not reflect cancellation")
	}
	command := NewWorkCommand()
	command.SetArgs([]string{"--repo", remote, "cancel-work", "--work-id", workID})
	if err := command.Execute(); err == nil || !strings.Contains(err.Error(), "terminal") {
		t.Fatalf("terminal work accepted cancellation: %v", err)
	}
}
