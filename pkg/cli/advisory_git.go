package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

func (o advisoryHandoff) pushPatch(ctx context.Context, fork advisoryFork, patch []byte) error {
	dir, err := os.MkdirTemp("", "gh-aw-advisory-")
	if err != nil {
		return fmt.Errorf("create temporary advisory repository: %w", err)
	}
	defer os.RemoveAll(dir)
	remote := "https://github.com/" + fork.FullName + ".git"
	verify := func() error {
		current, err := o.resolveFork(ctx)
		if err != nil {
			return err
		}
		if current != fork {
			return errors.New("the advisory fork changed; run advisory inspect again")
		}
		return nil
	}
	return o.applyAndPush(ctx, dir, remote, patch, verify)
}

func (o advisoryHandoff) applyAndPush(ctx context.Context, dir, remote string, patch []byte, verify func() error) error {
	git := advisoryGit{ctx: ctx, dir: dir, remote: remote}
	baseRef, destRef := "refs/heads/"+o.baseBranch, "refs/heads/"+o.branch
	for _, ref := range []string{baseRef, destRef} {
		if _, err := git.run(nil, "check-ref-format", ref); err != nil {
			return errors.New("invalid advisory branch name; use a Git branch name such as advisory-fix")
		}
	}
	if _, err := git.run(nil, "init", "--quiet", "--template="); err != nil {
		return fmt.Errorf("could not initialize isolated advisory workspace: %w", err)
	}
	base, err := git.fetch(baseRef)
	if err != nil {
		return errors.New("could not fetch the base branch from the advisory fork; verify maintainer Git access and the branch name")
	}
	if base != o.base {
		return errors.New("the advisory fork base moved; rebase and retest the fix before transferring")
	}
	tree, err := git.patchTree(base, patch)
	if err != nil {
		return err
	}
	head, err := git.run(nil, "commit-tree", tree, "-p", base, "-m", "Apply reviewed advisory fix")
	if err != nil {
		return fmt.Errorf("could not create the advisory fix commit: %w", err)
	}
	if err := verify(); err != nil {
		return err
	}
	tip, err := git.run(nil, "ls-remote", "--refs", remote, baseRef)
	if err != nil || tip != base+"\t"+baseRef {
		return errors.New("the fork base could not be reconfirmed; verify access and rerun inspect against the current base")
	}
	// An empty lease means the destination must not exist, even if a concurrent
	// creator's branch could otherwise be updated with a fast-forward push.
	if _, err := git.run(nil, "push", "--quiet", "--force-with-lease="+destRef+":", remote, head+":"+destRef); err != nil {
		if !git.alreadyTransferred(destRef, base, tree) {
			return errors.New("could not create the private fix branch; verify Git access and choose an unused branch if it already exists")
		}
	}
	tip, err = git.run(nil, "ls-remote", "--refs", remote, baseRef)
	if err != nil || tip != base+"\t"+baseRef {
		return errors.New("the advisory fork base moved or could not be reconfirmed after transfer; the private fix branch may already exist, so check it before rebasing, retesting, and running inspect again")
	}
	return nil
}

type advisoryGit struct {
	ctx         context.Context
	dir, remote string
}

func (g advisoryGit) fetch(ref string) (string, error) {
	if _, err := g.run(nil, "fetch", "--quiet", "--depth=1", "--no-tags", g.remote, ref); err != nil {
		return "", err
	}
	return g.run(nil, "rev-parse", "--verify", "FETCH_HEAD^{commit}")
}

func (g advisoryGit) alreadyTransferred(ref, base, tree string) bool {
	// A shallow fetch hides parents in revision walks, but the raw commit still
	// records them. Check both the exact tree and the single expected parent.
	head, err := g.fetch(ref)
	if err != nil {
		return false
	}
	commit, err := g.run(nil, "cat-file", "commit", head)
	return err == nil && strings.HasPrefix(commit, "tree "+tree+"\nparent "+base+"\nauthor ")
}

func (g advisoryGit) patchTree(base string, patch []byte) (string, error) {
	if _, err := g.run(nil, "read-tree", base); err != nil {
		return "", fmt.Errorf("could not load the advisory fork base: %w", err)
	}
	if _, err := g.run(patch, "apply", "--cached", "--whitespace=nowarn", "-"); err != nil {
		return "", errors.New("patch does not apply cleanly to the exact tested base; regenerate it without rebasing implicitly")
	}
	changes, err := g.run(nil, "diff", "--cached", "--raw", "-z", "--no-renames", base)
	if err != nil {
		return "", fmt.Errorf("could not verify the staged patch: %w", err)
	}
	if changes == "" {
		return "", errors.New("patch produced no supported changes")
	}
	for changes != "" {
		status, rest, statusOK := strings.Cut(changes, "\x00")
		filename, remaining, pathOK := strings.Cut(rest, "\x00")
		oldMode, modes, _ := strings.Cut(strings.TrimPrefix(status, ":"), " ")
		newMode, _, _ := strings.Cut(modes, " ")
		if !statusOK || !pathOK || filename == "" {
			return "", errors.New("could not verify paths in the staged patch")
		}
		lower := strings.ToLower(filename)
		if lower == ".github" || strings.HasPrefix(lower, ".github/") {
			return "", errors.New("patch changes a workflow or another path under .github; use a manual handoff for this patch")
		}
		if !advisoryFileMode(oldMode) || !advisoryFileMode(newMode) {
			return "", errors.New("patch changes a non-regular file; use a manual handoff for this patch")
		}
		changes = remaining
	}
	return g.run(nil, "write-tree")
}

func advisoryFileMode(mode string) bool {
	return mode == "000000" || mode == "100644" || mode == "100755"
}

func (g advisoryGit) run(input []byte, args ...string) (string, error) {
	prefix := []string{"--no-pager", "-c", "core.hooksPath=/dev/null", "-c", "commit.gpgSign=false",
		"-c", "push.gpgSign=false", "-c", "http.followRedirects=false",
		"-c", "credential.helper=", "-c", "credential.helper=!gh auth git-credential",
		"-c", "user.name=gh-aw advisory handoff", "-c", "user.email=gh-aw-advisory@users.noreply.github.com"}
	cmd := exec.CommandContext(g.ctx, "git", append(prefix, args...)...)
	cmd.Dir = g.dir
	cmd.Stdin = bytes.NewReader(input)
	for _, entry := range os.Environ() {
		key, _, _ := strings.Cut(entry, "=")
		upper := strings.ToUpper(key)
		if !strings.HasPrefix(upper, "GIT_") && !strings.EqualFold(key, "GH_HOST") &&
			!strings.EqualFold(key, "GH_PROMPT_DISABLED") && !strings.EqualFold(key, "GH_DEBUG") {
			cmd.Env = append(cmd.Env, entry)
		}
	}
	cmd.Env = append(cmd.Env, "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_TERMINAL_PROMPT=0",
		"GH_HOST=github.com", "GH_PROMPT_DISABLED=1")
	out, err := cmd.Output()
	// Keep raw output for the NUL-delimited diff. It must not be written to logs.
	return strings.TrimRight(string(out), "\n"), err
}
