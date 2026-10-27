package cli

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/ctxutil"
	"github.com/github/gh-aw/pkg/repoutil"
)

type healthLocalGit struct {
	dir string
	ctx context.Context
}

func runHealthGit(dir string, args ...string) ([]byte, error) {
	return runHealthGitContext(context.Background(), dir, args...)
}

func runHealthGitContext(ctx context.Context, dir string, args ...string) ([]byte, error) {
	ctx = ctxutil.OrBackground(ctx)
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "git", append([]string{"-c", "protocol.allow=never"}, args...)...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1", "GIT_TERMINAL_PROMPT=0")
	return cmd.Output()
}

func enableHealthLocalGit(run WorkflowRun, collection *HealthCollection) {
	if checkHealthTimeBudget(collection) != nil {
		return
	}
	if collection.localGitChecked {
		return
	}
	collection.localGitChecked = true
	remote, err := runHealthGitContext(collection.context(), "", "remote", "get-url", "origin")
	if err != nil {
		healthLog.Printf("Local workflow objects unavailable: %v; using GitHub API", err)
		return
	}
	target, host := repoutil.NormalizeRepoForAPI(collection.repoOverride)
	if target == "" {
		target = run.Repository
	}
	if host == "" {
		host = collection.Host
	}
	if target == "" || !healthRemoteMatches(string(remote), host, target) {
		healthLog.Print("Local checkout does not match health target; using GitHub API")
		return
	}
	root, err := runHealthGitContext(collection.context(), "", "rev-parse", "--show-toplevel")
	if err != nil {
		healthLog.Printf("Local workflow root unavailable: %v; using GitHub API", err)
		return
	}
	collection.localGit = &healthLocalGit{dir: strings.TrimSpace(string(root)), ctx: collection.context()}
}

func healthRemoteMatches(remote, host, repository string) bool {
	remote = strings.TrimSpace(remote)
	if !strings.Contains(remote, "://") {
		_, after, found := strings.Cut(remote, "@")
		if found {
			remote = after
		}
		remoteHost, slug, found := strings.Cut(remote, ":")
		return found && strings.EqualFold(remoteHost, host) && strings.EqualFold(strings.TrimSuffix(slug, ".git"), repository)
	}
	parsed, err := url.Parse(remote)
	return err == nil && parsed.Host != "" && strings.EqualFold(parsed.Host, host) &&
		strings.EqualFold(strings.TrimSuffix(strings.TrimPrefix(parsed.Path, "/"), ".git"), repository)
}

func (local *healthLocalGit) workflowFiles(sha string) (map[string]string, error) {
	if !validHealthBlobSHA(sha) {
		return nil, errors.New("local workflow lookup requires an immutable commit SHA")
	}
	out, err := runHealthGitContext(local.ctx, local.dir, "ls-tree", "-z", sha+":.github/workflows")
	if err != nil {
		return nil, fmt.Errorf("local workflow tree unavailable: %w", err)
	}
	files := make(map[string]string)
	for entry := range strings.SplitSeq(string(out), "\x00") {
		if entry == "" {
			continue
		}
		header, name, found := strings.Cut(entry, "\t")
		mode, object, hasMode := strings.Cut(header, " ")
		objectType, blobSHA, hasSHA := strings.Cut(object, " ")
		if !found || !hasMode || !hasSHA || path.Base(name) != name {
			return nil, errors.New("invalid local workflow tree entry")
		}
		if objectType == "blob" && (mode == "100644" || mode == "100755") && validHealthBlobSHA(blobSHA) {
			files[path.Join(constants.WorkflowsDir, name)] = blobSHA
		}
	}
	return files, nil
}

func (local *healthLocalGit) blob(sha string) ([]byte, error) {
	if !validHealthBlobSHA(sha) {
		return nil, errors.New("local workflow lookup requires a valid blob SHA")
	}
	return runHealthGitContext(local.ctx, local.dir, "cat-file", "blob", sha)
}
