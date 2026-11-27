package workqueue

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/auth"
)

// gitLedgerStore reads and writes the ledger through Git plumbing in an isolated,
// temporary bare repository; it never touches the caller's checkout.
type gitLedgerStore struct {
	dir, remote, ref string
	env              []string
}

const gitCommandTimeout = 120 * time.Second
const gitMetadataBytes = 1 << 20

var errGitOutputLimit = errors.New("queue Git output exceeds size limit")

type limitedOutput struct {
	buffer   bytes.Buffer
	limit    int
	cancel   context.CancelFunc
	exceeded bool
}

func (output *limitedOutput) Write(data []byte) (int, error) {
	if len(data) > output.limit-output.buffer.Len() {
		output.exceeded = true
		output.cancel()
		return 0, errGitOutputLimit
	}
	return output.buffer.Write(data)
}

func newGitLedgerStore(ctx context.Context, host, repository, branch string) (*gitLedgerStore, error) {
	if host == "" {
		host, _ = auth.DefaultHost()
	}
	host = strings.ToLower(host)
	if !resourceHostPattern.MatchString(host) {
		return nil, errors.New("invalid queue Git host")
	}
	token, _ := auth.TokenForHost(host)
	if token == "" {
		return nil, errors.New("queue Git transport requires credentials for the selected GitHub host")
	}
	dir, err := os.MkdirTemp("", "gh-aw-workqueue-")
	if err != nil {
		return nil, err
	}
	if dir, err = filepath.Abs(dir); err != nil {
		return nil, err
	}
	remote := (&url.URL{Scheme: "https", Host: host, Path: repository + ".git"}).String()
	store := &gitLedgerStore{dir: dir, remote: remote, ref: "refs/heads/" + branch}
	for _, value := range os.Environ() {
		key, _, _ := strings.Cut(value, "=")
		if (!strings.HasPrefix(key, "GIT_") || key == "GIT_EXEC_PATH") && key != "SSH_ASKPASS" {
			store.env = append(store.env, value)
		}
	}
	// The credential exists only in this child environment, never argv, a URL,
	// repository config, or an error containing Git's stderr.
	header := base64.StdEncoding.EncodeToString([]byte("x-access-token:" + token))
	store.env = append(store.env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull,
		"GIT_TERMINAL_PROMPT=0", "GIT_NO_REPLACE_OBJECTS=1",
		"GIT_AUTHOR_NAME=GitHub Work Queue", "GIT_AUTHOR_EMAIL=work-queue@github.com",
		"GIT_COMMITTER_NAME=GitHub Work Queue", "GIT_COMMITTER_EMAIL=work-queue@github.com",
		"GIT_CONFIG_COUNT=1", "GIT_CONFIG_KEY_0=http."+remote+".extraheader", "GIT_CONFIG_VALUE_0=AUTHORIZATION: basic "+header)
	if _, err := store.run(ctx, nil, "init", "--bare", "--template=", dir); err != nil {
		store.close()
		return nil, err
	}
	return store, nil
}

func (s *gitLedgerStore) close() {
	if err := os.RemoveAll(s.dir); err != nil {
		branchLog.Printf("failed to remove temporary queue repository %s: %v", s.dir, err)
	}
}

func (s *gitLedgerStore) run(ctx context.Context, input []byte, operation string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, gitCommandTimeout)
	defer cancel()
	limit := gitMetadataBytes
	if operation == "cat-file" {
		limit = LedgerBytes + 1
	}
	output := &limitedOutput{limit: limit, cancel: cancel}
	base := []string{"--no-replace-objects", "-c", "core.hooksPath=" + os.DevNull,
		"-c", "credential.helper=", "-c", "core.askPass=", "-c", "http.followRedirects=false",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"-c", "maintenance.auto=false", "-c", "gc.auto=0", operation}
	command := exec.CommandContext(ctx, "git", append(base, args...)...)
	command.Dir, command.Env = s.dir, s.env
	command.Stdin = bytes.NewReader(input)
	command.Stdout, command.Stderr = output, io.Discard
	command.WaitDelay = time.Second
	err := command.Run()
	if output.exceeded {
		if operation == "cat-file" {
			return nil, queueError("resource_limit", "queue blob exceeds the ledger bound")
		}
		return nil, errGitOutputLimit
	}
	if err != nil {
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return output.buffer.Bytes(), fmt.Errorf("queue Git %s failed: %w", operation, err)
	}
	return output.buffer.Bytes(), nil
}

func (s *gitLedgerStore) head(ctx context.Context) (string, error) {
	output, err := s.run(ctx, nil, "ls-remote", "--refs", s.remote, s.ref)
	if err != nil {
		return "", err
	}
	sha := ""
	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		value, ref, ok := strings.Cut(line, "\t")
		if !ok || ref != s.ref {
			continue
		}
		if sha != "" || !revisionPattern.MatchString(value) {
			return "", errors.New("invalid queue branch reference")
		}
		sha = value
	}
	if sha == "" {
		return "", nil
	}
	if _, err := s.run(ctx, nil, "fetch", "--depth=1", "--no-tags", "--no-recurse-submodules", "--no-write-fetch-head", s.remote, "+"+s.ref+":refs/heads/ledger"); err != nil {
		return "", err
	}
	fetched, err := s.run(ctx, nil, "rev-parse", "--verify", "refs/heads/ledger^{commit}")
	if err != nil {
		return "", err
	}
	// A head that moved between ls-remote and fetch is simply the newer head.
	return strings.TrimSpace(string(fetched)), nil
}

func (s *gitLedgerStore) read(ctx context.Context, head string) ([]byte, string, error) {
	if !revisionPattern.MatchString(head) {
		return nil, "", errors.New("invalid queue Git revision")
	}
	tree, err := s.run(ctx, nil, "rev-parse", "--verify", head+"^{tree}")
	if err != nil {
		return nil, "", err
	}
	treeSHA := strings.TrimSpace(string(tree))
	entries, err := s.run(ctx, nil, "ls-tree", "-z", treeSHA)
	if err != nil {
		return nil, "", err
	}
	for entry := range strings.SplitSeq(strings.TrimSuffix(string(entries), "\x00"), "\x00") {
		meta, name, ok := strings.Cut(entry, "\t")
		if !ok || name != FileName {
			continue
		}
		// ls-tree entries are "<mode> SP <type> SP <object>".
		mode, rest, _ := strings.Cut(meta, " ")
		kind, blob, _ := strings.Cut(rest, " ")
		if mode != "100644" || kind != "blob" || !revisionPattern.MatchString(blob) {
			return nil, "", queueError("ledger_invalid", "existing queue must contain exactly one regular log")
		}
		data, err := s.run(ctx, nil, "cat-file", "blob", blob)
		if err != nil {
			return nil, "", err
		}
		if len(data) > LedgerBytes {
			return nil, "", queueError("resource_limit", "queue blob exceeds the ledger bound")
		}
		return data, treeSHA, nil
	}
	return nil, "", queueError("ledger_invalid", "existing queue must contain exactly one regular log")
}

func (s *gitLedgerStore) write(ctx context.Context, parent, baseTree string, content []byte, message string) (string, error) {
	var entries []byte
	if baseTree != "" {
		raw, err := s.run(ctx, nil, "ls-tree", "-z", baseTree)
		if err != nil {
			return "", err
		}
		for entry := range strings.SplitSeq(string(raw), "\x00") {
			if _, name, ok := strings.Cut(entry, "\t"); ok && name != FileName {
				entries = append(entries, (entry + "\x00")...)
			}
		}
	}
	blob, err := s.run(ctx, content, "hash-object", "-w", "--stdin")
	if err != nil {
		return "", err
	}
	entries = append(entries, ("100644 blob " + strings.TrimSpace(string(blob)) + "\t" + FileName + "\x00")...)
	tree, err := s.run(ctx, entries, "mktree", "-z")
	if err != nil {
		return "", err
	}
	args := []string{strings.TrimSpace(string(tree))}
	if parent != "" {
		args = append(args, "-p", parent)
	}
	commit, err := s.run(ctx, []byte(message+"\n"), "commit-tree", args...)
	if err != nil {
		return "", err
	}
	sha := strings.TrimSpace(string(commit))
	output, err := s.run(ctx, nil, "push", "--porcelain", "--no-verify", "--recurse-submodules=no",
		"--force-with-lease="+s.ref+":"+parent, s.remote, sha+":"+s.ref)
	if err != nil {
		if strings.Contains(string(output), "[rejected]") {
			return "", errLedgerConflict
		}
		return "", err
	}
	return sha, nil
}
