package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"path"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
)

type privateCloneManifest struct {
	Version     int      `json:"version"`
	Source      string   `json:"source_repository"`
	Destination string   `json:"private_repository"`
	SourceSHA   string   `json:"source_sha"`
	Downloaded  string   `json:"downloaded_at"`
	Files       []string `json:"context_files"`
	Secrets     []string `json:"required_secrets"`
	Bootstrap   string   `json:"bootstrap_ref"`
}

type privateCloneSnapshot struct {
	privateCloneManifest
	Data map[string][]byte
}

func privateCloneGH(ctx context.Context, args ...string) ([]byte, error) {
	cmd := workflow.ExecGHContext(ctx, args...)
	data, err := cmd.Output()
	if err != nil {
		// gh errors can contain confidential API response bodies.
		return nil, fmt.Errorf("private-clone GitHub operation failed: %w; verify repository access and required permissions", err)
	}
	return data, nil
}

func downloadPrivateCloneContext(ctx context.Context, opts privateCloneOptions) (*privateCloneSnapshot, error) {
	snapshot := &privateCloneSnapshot{
		privateCloneManifest: privateCloneManifest{
			Version: 1, Source: opts.Source, Destination: opts.Destination,
			Downloaded: time.Now().UTC().Format(time.RFC3339),
		},
		Data: make(map[string][]byte),
	}
	data, err := privateCloneGH(ctx, "api", path.Join("repos", opts.Source, "commits", url.PathEscape(opts.Ref)), "--jq", ".sha")
	if err != nil {
		return nil, fmt.Errorf("resolve source revision: %w", err)
	}
	snapshot.SourceSHA = strings.TrimSpace(string(data))
	if !privateCloneSHAPattern.MatchString(snapshot.SourceSHA) {
		return nil, errors.New("resolve source revision: GitHub did not return a full commit SHA")
	}
	for _, number := range slices.Compact(slices.Sorted(slices.Values(opts.Alerts))) {
		id := strconv.Itoa(number)
		endpoint := path.Join("repos", opts.Source, "code-scanning/alerts", id)
		if err := snapshot.download(ctx, "code-scanning/"+id+".json", endpoint, false); err != nil {
			return nil, err
		}
		var alert struct {
			Number int `json:"number"`
		}
		if err := json.Unmarshal(snapshot.Data[path.Join(privateCloneContextDir, "code-scanning/"+id+".json")], &alert); err != nil || alert.Number != number {
			return nil, fmt.Errorf("download code scanning alert %s: response does not identify the selected alert", id)
		}
		if err := snapshot.download(ctx, "code-scanning/"+id+".instances.json", path.Join(endpoint, "instances"), true); err != nil {
			return nil, err
		}
	}
	for _, id := range slices.Compact(slices.Sorted(slices.Values(opts.Advisories))) {
		if err := snapshot.download(ctx, "advisories/"+id+".json", path.Join("repos", opts.Source, "security-advisories", id), false); err != nil {
			return nil, err
		}
		var advisory struct {
			ID string `json:"ghsa_id"`
		}
		if err := json.Unmarshal(snapshot.Data[path.Join(privateCloneContextDir, "advisories/"+id+".json")], &advisory); err != nil || advisory.ID != id {
			return nil, fmt.Errorf("download advisory %s: response does not identify the selected advisory", id)
		}
	}
	slices.Sort(snapshot.Files)
	return snapshot, nil
}

func (s *privateCloneSnapshot) download(ctx context.Context, name, endpoint string, paginate bool) error {
	args := []string{"api", endpoint}
	if paginate {
		args = append(args, "--paginate", "--slurp")
	}
	data, err := privateCloneGH(ctx, args...)
	if err != nil {
		return fmt.Errorf("download %s: %w", name, err)
	}
	value, err := decodePrivateCloneJSON(data)
	if err != nil {
		return fmt.Errorf("download %s: GitHub returned invalid JSON", name)
	}
	if paginate {
		pages, ok := value.([]any)
		if !ok {
			return fmt.Errorf("download %s: expected paginated JSON arrays", name)
		}
		items := make([]any, 0)
		for _, page := range pages {
			entries, ok := page.([]any)
			if !ok {
				return fmt.Errorf("download %s: expected paginated JSON arrays", name)
			}
			items = append(items, entries...)
		}
		value = items
	}
	// Store re-encoded JSON only so snapshot text always remains quoted data.
	data, err = json.MarshalIndent(sanitizePrivateCloneJSON(value), "", "  ")
	if err != nil {
		return err
	}
	name = path.Join(privateCloneContextDir, name)
	s.Data[name] = data
	s.Files = append(s.Files, name)
	return nil
}

func decodePrivateCloneJSON(data []byte) (any, error) {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return nil, err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return nil, errors.New("unexpected data after JSON value")
	}
	return value, nil
}

// sanitizePrivateCloneJSON drops API links and dismissal comments, which are
// not remediation evidence and only widen the untrusted text and link surface.
func sanitizePrivateCloneJSON(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		for key, child := range typed {
			if key == "url" || strings.HasSuffix(key, "_url") || key == "dismissed_comment" {
				delete(typed, key)
				continue
			}
			typed[key] = sanitizePrivateCloneJSON(child)
		}
	case []any:
		for i, child := range typed {
			typed[i] = sanitizePrivateCloneJSON(child)
		}
	}
	return value
}
