package workqueue

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/githubapi"
)

const maxRetries = 5

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Branch operates through GitHub Git APIs without a local checkout or Git executable.
type Branch struct {
	Remote string
	Name   string
	client *api.RESTClient
}

func (b Branch) validate() error {
	if !repoPattern.MatchString(b.Remote) {
		return errors.New("repo must be owner/repo")
	}
	for part := range strings.SplitSeq(b.Remote, "/") {
		if part == "." || part == ".." {
			return errors.New("repo must be owner/repo")
		}
	}
	if !branchPattern.MatchString(b.Name) || strings.Contains(b.Name, "..") ||
		strings.Contains(b.Name, "//") || strings.HasSuffix(b.Name, ".") || strings.HasSuffix(b.Name, "/") {
		return errors.New("invalid queue branch")
	}
	for part := range strings.SplitSeq(b.Name, "/") {
		if strings.HasPrefix(part, ".") || strings.HasSuffix(part, ".lock") {
			return errors.New("invalid queue branch")
		}
	}
	return nil
}

func (b Branch) withClient() (Branch, error) {
	if err := b.validate(); err != nil {
		return b, err
	}
	if b.client == nil {
		client, err := api.NewRESTClient(githubapi.ClientOptions("", ""))
		if err != nil {
			return b, err
		}
		b.client = client
	}
	return b, nil
}

func (b Branch) request(ctx context.Context, method, endpoint string, body, response any) error {
	var data []byte
	if body != nil {
		var err error
		data, err = json.Marshal(body)
		if err != nil {
			return err
		}
	}
	return b.client.DoWithContext(ctx, method, path.Join("repos", b.Remote, endpoint), bytes.NewReader(data), response)
}

func hasStatus(err error, status int) bool {
	var apiError *api.HTTPError
	return errors.As(err, &apiError) && apiError.StatusCode == status
}

type branchSnapshot struct {
	head         string
	tree         string
	legacy       bool
	needsUpgrade bool
}

func (b Branch) read(ctx context.Context) ([]Transaction, branchSnapshot, error) {
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := b.request(ctx, http.MethodGet, "git/ref/heads/"+b.Name, nil, &ref); err != nil {
		if hasStatus(err, http.StatusNotFound) {
			if b.Name == DefaultBranch {
				legacyBranch := b
				legacyBranch.Name = LegacyBranch
				transactions, snapshot, err := legacyBranch.read(ctx)
				if snapshot.head != "" {
					snapshot.legacy = true
					snapshot.needsUpgrade = true
				}
				return transactions, snapshot, err
			}
			// A missing repository or missing read permission is not an empty queue.
			var repository any
			err = b.client.DoWithContext(ctx, http.MethodGet, "repos/"+b.Remote, nil, &repository)
			return nil, branchSnapshot{}, err
		}
		return nil, branchSnapshot{}, err
	}
	if ref.Ref != "refs/heads/"+b.Name || ref.Object.SHA == "" {
		return nil, branchSnapshot{}, errors.New("invalid queue branch reference")
	}
	snapshot := branchSnapshot{head: ref.Object.SHA}
	var commit struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := b.request(ctx, http.MethodGet, "git/commits/"+snapshot.head, nil, &commit); err != nil {
		return nil, snapshot, err
	}
	snapshot.tree = commit.Tree.SHA
	if snapshot.tree == "" {
		return nil, snapshot, errors.New("queue commit has no tree")
	}
	transactions, err := b.readLog(ctx, &snapshot)
	return transactions, snapshot, err
}

func (b Branch) readLog(ctx context.Context, snapshot *branchSnapshot) ([]Transaction, error) {
	var tree struct {
		Truncated bool `json:"truncated"`
		Tree      []struct {
			Path string `json:"path"`
			Mode string `json:"mode"`
			Type string `json:"type"`
			SHA  string `json:"sha"`
		} `json:"tree"`
	}
	if err := b.request(ctx, http.MethodGet, "git/trees/"+snapshot.tree, nil, &tree); err != nil {
		return nil, err
	}
	if tree.Truncated {
		return nil, errors.New("queue tree is truncated")
	}
	for _, entry := range tree.Tree {
		if entry.Path != FileName {
			continue
		}
		if entry.Type != "blob" || (entry.Mode != "100644" && entry.Mode != "100755") || entry.SHA == "" {
			return nil, errors.New("queue log must be a regular file")
		}
		var blob struct {
			Encoding string `json:"encoding"`
			Content  string `json:"content"`
		}
		if err := b.request(ctx, http.MethodGet, "git/blobs/"+entry.SHA, nil, &blob); err != nil {
			return nil, err
		}
		if blob.Encoding != "base64" {
			return nil, errors.New("unsupported queue blob encoding")
		}
		data, err := base64.StdEncoding.DecodeString(blob.Content)
		if err != nil {
			return nil, err
		}
		transactions, err := Parse(data)
		if err != nil {
			return nil, err
		}
		_, err = Replay(transactions)
		if err == nil {
			canonical, serializeErr := Serialize(transactions)
			if serializeErr != nil {
				return nil, serializeErr
			}
			snapshot.needsUpgrade = !bytes.Equal(data, canonical)
		}
		return transactions, err
	}
	return nil, errors.New("queue branch is missing " + FileName)
}

func (b Branch) Read(ctx context.Context) ([]Transaction, error) {
	b, err := b.withClient()
	if err != nil {
		return nil, err
	}
	transactions, _, err := b.read(ctx)
	return transactions, err
}

func (b Branch) publish(ctx context.Context, snapshot branchSnapshot, next []Transaction) (bool, error) {
	if _, err := Replay(next); err != nil {
		return false, err
	}
	data, err := Serialize(next)
	if err != nil {
		return false, err
	}
	treeBody := map[string]any{
		"tree": []map[string]string{{"path": FileName, "mode": "100644", "type": "blob", "content": string(data)}},
	}
	parents := []string{}
	if snapshot.head != "" {
		treeBody["base_tree"] = snapshot.tree
		parents = append(parents, snapshot.head)
	}
	var tree struct {
		SHA string `json:"sha"`
	}
	if err := b.request(ctx, http.MethodPost, "git/trees", treeBody, &tree); err != nil {
		return false, err
	}
	var commit struct {
		SHA string `json:"sha"`
	}
	if err := b.request(ctx, http.MethodPost, "git/commits", map[string]any{
		"message": "Update work queue", "tree": tree.SHA, "parents": parents,
	}, &commit); err != nil {
		return false, err
	}
	if snapshot.head == "" || snapshot.legacy {
		err = b.request(ctx, http.MethodPost, "git/refs", map[string]any{
			"ref": "refs/heads/" + b.Name, "sha": commit.SHA,
		}, new(any))
	} else {
		err = b.request(ctx, http.MethodPatch, "git/refs/heads/"+b.Name, map[string]any{
			"sha": commit.SHA, "force": false,
		}, new(any))
	}
	return hasStatus(err, http.StatusConflict) || hasStatus(err, http.StatusUnprocessableEntity), err
}

// Update retries rejected non-force reference updates from a fresh branch snapshot.
func (b Branch) Update(ctx context.Context, change func([]Transaction) ([]Transaction, bool, error)) ([]Transaction, bool, error) {
	b, err := b.withClient()
	if err != nil {
		return nil, false, err
	}
	for attempt := range maxRetries {
		transactions, snapshot, err := b.read(ctx)
		if err != nil {
			return nil, false, err
		}
		next, changed, err := change(transactions)
		if err != nil {
			return nil, false, err
		}
		if !changed && !snapshot.needsUpgrade {
			return next, false, nil
		}
		conflict, err := b.publish(ctx, snapshot, next)
		if err == nil {
			return next, true, nil
		}
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if !conflict {
			return nil, false, err
		}
		if attempt == maxRetries-1 {
			return nil, false, fmt.Errorf("queue publication failed after %d attempts: %w", maxRetries, err)
		}
		delay := time.Duration(50*(1<<attempt)+rand.Intn(50)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, false, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, false, errors.New("queue publication exhausted retries")
}
