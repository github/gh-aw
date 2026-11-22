package cli

import (
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"path"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

type healthFileSnapshot struct {
	files map[string]string
	err   error
}

type healthLockSnapshot struct {
	metadata *workflow.LockMetadata
	err      error
	content  []byte
}

func useHealthFileSnapshots(collection *HealthCollection) bool {
	return collection.localGit != nil || len(collection.workflows) > 1 || len(collection.fileSnapshots) > 0
}

// A directory response supplies source and lock blob identities for every workflow.
func healthWorkflowFiles(ref string, collection *HealthCollection) healthFileSnapshot {
	if collection.fileSnapshots == nil {
		collection.fileSnapshots = make(map[string]healthFileSnapshot)
	}
	if snapshot, checked := collection.fileSnapshots[ref]; checked {
		return snapshot
	}
	snapshotRef := ref
	if ref == collection.scanBranch && collection.pinnedBranchSHA != "" {
		snapshotRef = collection.pinnedBranchSHA
	}
	if snapshot, checked := collection.fileSnapshots[snapshotRef]; checked {
		collection.fileSnapshots[ref] = snapshot
		return snapshot
	}
	if collection.localGit != nil {
		sha := snapshotRef
		if !validHealthBlobSHA(sha) {
			endpoint := path.Join(healthRepositoryEndpoint(collection.repoOverride), "git/ref/heads", url.PathEscape(ref))
			out, err := healthAPI(collection, endpoint, "--jq", ".object.sha")
			if err == nil {
				sha = strings.TrimSpace(string(out))
			} else {
				healthLog.Printf("Pushed workflow branch unavailable locally: %v; using API directory snapshot", err)
			}
		}
		if validHealthBlobSHA(sha) {
			snapshotRef = sha
			if snapshot, checked := collection.fileSnapshots[sha]; checked {
				collection.fileSnapshots[ref] = snapshot
				return snapshot
			}
			files, err := collection.localGit.workflowFiles(sha)
			if err == nil {
				collection.LocalRevisionReads++
				snapshot := healthFileSnapshot{files: files}
				collection.fileSnapshots[ref] = snapshot
				collection.fileSnapshots[sha] = snapshot
				return snapshot
			}
			healthLog.Printf("%v; using GitHub API directory snapshot", err)
		}
	}
	endpoint := path.Join(healthRepositoryEndpoint(collection.repoOverride), "contents", constants.WorkflowsDir) +
		"?ref=" + url.QueryEscape(snapshotRef)
	out, err := healthAPI(collection, endpoint, "--jq", `map({path,sha,type})`)
	collection.RevisionLookups++
	snapshot := healthFileSnapshot{err: err}
	if err == nil {
		snapshot.files, snapshot.err = parseHealthWorkflowFiles(out)
	}
	collection.fileSnapshots[ref] = snapshot
	collection.fileSnapshots[snapshotRef] = snapshot
	return snapshot
}

func parseHealthWorkflowFiles(out []byte) (map[string]string, error) {
	var entries []struct {
		Path string `json:"path"`
		SHA  string `json:"sha"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(out, &entries); err != nil {
		return nil, fmt.Errorf("failed to parse workflow directory snapshot: %w", err)
	}
	if entries == nil {
		return nil, errors.New("workflow directory snapshot is missing its entries")
	}
	files := make(map[string]string)
	for _, entry := range entries {
		if entry.Type != "file" {
			continue
		}
		if !validHealthBlobSHA(entry.SHA) || path.Dir(entry.Path) != constants.WorkflowsDir {
			return nil, errors.New("workflow directory snapshot contains an invalid file path or blob SHA")
		}
		files[entry.Path] = entry.SHA
	}
	return files, nil
}

func validHealthBlobSHA(sha string) bool {
	_, err := hex.DecodeString(sha)
	return err == nil && (len(sha) == 40 || len(sha) == 64)
}

func healthWorkflowFileSHA(filePath, ref string, collection *HealthCollection) (string, error) {
	snapshot := healthWorkflowFiles(ref, collection)
	if snapshot.err != nil {
		return "", snapshot.err
	}
	sha := snapshot.files[filePath]
	if sha == "" {
		return "", fmt.Errorf("workflow file %s is unavailable in the directory snapshot at %s", filePath, ref)
	}
	return sha, nil
}

func readHealthLockMetadata(run WorkflowRun, ref string, collection *HealthCollection) (*workflow.LockMetadata, error) {
	endpoint := path.Join(healthRepositoryEndpoint(collection.repoOverride), "contents", run.WorkflowPath) +
		"?ref=" + url.QueryEscape(ref)
	var blobSHA string
	if useHealthFileSnapshots(collection) {
		var err error
		blobSHA, err = healthWorkflowFileSHA(run.WorkflowPath, ref, collection)
		if err != nil {
			return nil, err
		}
		if cached, checked := collection.lockSnapshots[blobSHA]; checked {
			return cached.metadata, cached.err
		}
		endpoint = path.Join(healthRepositoryEndpoint(collection.repoOverride), "git/blobs", blobSHA)
	}
	out, err := readHealthLockContent(endpoint, blobSHA, collection)
	if err != nil {
		if blobSHA != "" {
			cacheHealthLockSnapshot(blobSHA, nil, err, collection)
		}
		return nil, err
	}
	snapshot := cacheHealthLockSnapshot(blobSHA, out, nil, collection)
	return snapshot.metadata, snapshot.err
}

func cacheHealthLockSnapshot(blobSHA string, content []byte, readErr error, collection *HealthCollection) healthLockSnapshot {
	snapshot := healthLockSnapshot{content: content, err: readErr}
	if readErr == nil {
		// Metadata belongs to the header, never to a matching string in a job's script.
		header, _, _ := strings.Cut(string(content), "\n")
		snapshot.metadata, _, snapshot.err = workflow.ExtractMetadataFromLockFile(header)
		if snapshot.err == nil && (snapshot.metadata == nil || snapshot.metadata.FrontmatterHash == "" || snapshot.metadata.BodyHash == "") {
			snapshot.err = errors.New("workflow revision hashes unavailable; recompile workflows to include gh-aw-metadata body_hash")
		}
	}
	if blobSHA != "" {
		if collection.lockSnapshots == nil {
			collection.lockSnapshots = make(map[string]healthLockSnapshot)
		}
		collection.lockSnapshots[blobSHA] = snapshot
	}
	return snapshot
}

func readHealthLockContent(endpoint, blobSHA string, collection *HealthCollection) ([]byte, error) {
	if collection.localGit != nil && blobSHA != "" {
		out, err := collection.localGit.blob(blobSHA)
		if err == nil {
			collection.LocalRevisionReads++
			return out, nil
		}
		healthLog.Printf("Local lock blob unavailable: %v; using GitHub API", err)
	}
	filter := `.content | @base64d | split("\n") | .[0]`
	if blobSHA != "" && collection.pinnedBranchSHA == "" {
		filter = ".content | @base64d"
	}
	out, err := healthAPI(collection, endpoint, "--jq", filter)
	collection.RevisionLookups++
	return out, err
}
