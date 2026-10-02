package workqueue

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

const maxRetries = 5

var branchPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)
var repoPattern = regexp.MustCompile(`^[A-Za-z0-9_.-]+/[A-Za-z0-9_.-]+$`)

// Branch operates on a temporary checkout. It never changes the caller's checkout.
type Branch struct {
	Remote string
	Name   string
}

func (b Branch) validate() error {
	if b.Remote == "" || !branchPattern.MatchString(b.Name) || strings.Contains(b.Name, "..") ||
		strings.Contains(b.Name, "//") || strings.HasSuffix(b.Name, ".lock") || strings.HasSuffix(b.Name, "/") {
		return errors.New("invalid coordinator remote or branch")
	}
	return nil
}

func remoteURL(remote string) (string, error) {
	if filepath.IsAbs(remote) {
		if _, err := os.Stat(remote); err != nil {
			return "", err
		}
		return remote, nil
	}
	if repoPattern.MatchString(remote) {
		return "https://github.com/" + remote + ".git", nil
	}
	return "", errors.New("repo must be owner/repo or an absolute local path")
}

func git(ctx context.Context, dir, remote string, args ...string) ([]byte, error) {
	if len(args) == 0 {
		return nil, errors.New("git requires an operation")
	}
	argv := []string{"-C", dir}
	if strings.HasPrefix(remote, "https://") {
		argv = append(argv, "-c", "credential.helper=!gh auth git-credential")
	}
	argv = append(argv, args...)
	command := exec.CommandContext(ctx, "git", argv...)
	command.Env = append(os.Environ(), "GIT_TERMINAL_PROMPT=0")
	out, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("git operation failed: %s: %w", strings.TrimSpace(string(out)), err)
	}
	return bytes.TrimSpace(out), nil
}

func (b Branch) checkout(ctx context.Context) (string, func(), bool, error) {
	if err := b.validate(); err != nil {
		return "", nil, false, err
	}
	remote, err := remoteURL(b.Remote)
	if err != nil {
		return "", nil, false, err
	}
	dir, err := os.MkdirTemp("", "gh-aw-work-*")
	if err != nil {
		return "", nil, false, err
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	if _, err := git(ctx, dir, remote, "init", "-q"); err != nil {
		cleanup()
		return "", nil, false, err
	}
	if _, err := git(ctx, dir, remote, "remote", "add", "origin", remote); err != nil {
		cleanup()
		return "", nil, false, err
	}
	out, err := git(ctx, dir, remote, "ls-remote", "--heads", "origin", "refs/heads/"+b.Name)
	exists := err == nil && len(out) > 0
	if err != nil {
		cleanup()
		return "", nil, false, err
	}
	if exists {
		if _, err := git(ctx, dir, remote, "fetch", "-q", "--depth=1", "origin", "refs/heads/"+b.Name); err != nil {
			cleanup()
			return "", nil, false, err
		}
		if _, err := git(ctx, dir, remote, "checkout", "-q", "-b", b.Name, "FETCH_HEAD"); err != nil {
			cleanup()
			return "", nil, false, err
		}
	} else {
		if _, err := git(ctx, dir, remote, "checkout", "-q", "--orphan", b.Name); err != nil {
			cleanup()
			return "", nil, false, err
		}
	}
	return dir, cleanup, exists, nil
}

func (b Branch) Read(ctx context.Context) ([]Transaction, error) {
	dir, cleanup, exists, err := b.checkout(ctx)
	if err != nil {
		return nil, err
	}
	defer cleanup()
	return readCheckout(dir, exists)
}

func readCheckout(dir string, exists bool) ([]Transaction, error) {
	if !exists {
		return nil, nil
	}
	data, err := os.ReadFile(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	transactions, err := Parse(data)
	if err != nil {
		return nil, err
	}
	_, err = Replay(transactions)
	return transactions, err
}

func (b Branch) publish(ctx context.Context, dir string, next []Transaction) error {
	if _, err := Replay(next); err != nil {
		return err
	}
	data, err := Serialize(next)
	if err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, FileName), data, 0600); err != nil {
		return err
	}
	if _, err := git(ctx, dir, b.Remote, "add", "--", FileName); err != nil {
		return err
	}
	if _, err := git(ctx, dir, b.Remote, "-c", "user.name=gh-aw", "-c", "user.email=gh-aw@users.noreply.github.com", "commit", "-qm", "Update dispatch work coordinator"); err != nil {
		return err
	}
	_, err = git(ctx, dir, b.Remote, "push", "-q", "origin", "HEAD:refs/heads/"+b.Name)
	return err
}

// Update retries rejected optimistic pushes from a fresh branch snapshot.
func (b Branch) Update(ctx context.Context, change func([]Transaction) ([]Transaction, bool, error)) ([]Transaction, bool, error) {
	for attempt := range maxRetries {
		dir, cleanup, exists, err := b.checkout(ctx)
		if err != nil {
			return nil, false, err
		}
		transactions, err := readCheckout(dir, exists)
		if err != nil {
			cleanup()
			return nil, false, err
		}
		next, changed, err := change(transactions)
		if err != nil || !changed {
			cleanup()
			return next, changed, err
		}
		err = b.publish(ctx, dir, next)
		cleanup()
		if err == nil {
			return next, true, nil
		}
		if ctx.Err() != nil {
			return nil, false, ctx.Err()
		}
		if attempt == maxRetries-1 {
			return nil, false, fmt.Errorf("coordinator publication failed after %d attempts: %w", maxRetries, err)
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
	return nil, false, errors.New("coordinator publication exhausted retries")
}
