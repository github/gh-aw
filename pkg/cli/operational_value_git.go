package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/gitutil"
)

type valueGit struct {
	ctx    context.Context
	dir    string
	budget *valueEvidenceBudget
}

func (g valueGit) run(args ...string) (string, error) {
	output, err := g.output(args...)
	return strings.TrimRight(string(output), "\r\n"), err
}

func (g valueGit) output(args ...string) ([]byte, error) {
	cmd := exec.CommandContext(g.ctx, "git", append([]string{"--no-pager"}, args...)...)
	cmd.Dir = g.dir
	cmd.Env = append(os.Environ(), "GIT_NO_LAZY_FETCH=1", "GIT_NO_REPLACE_OBJECTS=1")
	output, err := valueCommandOutput(cmd, valueCommandLimit)
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) {
			return nil, fmt.Errorf("git: %w: %s", err, valueDisplayText(strings.TrimSpace(string(exit.Stderr))))
		}
		return nil, err
	}
	return output, nil
}

func (g valueGit) prepare(definition valueMeasurementDefinition, ref string, end time.Time) (string, error) {
	if err := gitutil.ValidateGitRef(ref); err != nil {
		return "", err
	}
	remote, err := g.run("remote", "get-url", "origin")
	if err != nil {
		return "", err
	}
	repository := path.Join(definition.Host, definition.Repository)
	if remote != "https://"+repository && remote != "https://"+repository+".git" &&
		remote != "git@"+definition.Host+":"+definition.Repository+".git" && remote != "ssh://git@"+repository+".git" {
		return "", fmt.Errorf("value requires a checkout with origin pointing to %s", repository)
	}
	shallow, err := g.run("rev-parse", "--is-shallow-repository")
	if err != nil {
		return "", err
	}
	if shallow != "false" {
		return "", errors.New("value requires full Git history; use a full checkout (the command never fetches history)")
	}
	tip, err := g.run("rev-parse", "--verify", "--end-of-options", ref+"^{commit}")
	if err != nil {
		return "", fmt.Errorf("resolve --ref: %w", err)
	}
	if !gitutil.IsValidFullSHA(tip) {
		return "", errors.New("--ref did not resolve to a full commit SHA")
	}
	clock, err := g.run("show", "-s", "--format=%cI", tip)
	if err != nil {
		return "", err
	}
	at, err := time.Parse(time.RFC3339, clock)
	if err != nil || at.Before(end) {
		return "", errors.New("--ref does not cover --end; select an up-to-date main-history ref")
	}
	return tip, nil
}

func (g valueGit) snapshot(measurement valueMeasurement, tip string, at time.Time) (valueSnapshot, error) {
	sha, err := g.run("rev-list", "--first-parent", "-1", "--before="+at.Format(time.RFC3339), tip)
	if err != nil {
		return valueSnapshot{}, err
	}
	if sha == "" {
		return valueSnapshot{}, fmt.Errorf("no main-history snapshot at %s; full history is required", at.Format(time.RFC3339))
	}
	snapshot, err := g.measure(measurement, sha)
	snapshot.At = at
	return snapshot, err
}

type valueSurvival struct {
	Through time.Time `json:"through"`
	Commits int       `json:"commits"`
	Files   int       `json:"files"`
	Touched int       `json:"touched"`
}

func (g valueGit) changedPaths(base, sha string) ([]string, error) {
	files, err := g.run("diff-tree", "--no-commit-id", "--name-only", "-r", "-z", base, sha)
	if err != nil || files == "" {
		return nil, err
	}
	return strings.Split(strings.TrimSuffix(files, "\x00"), "\x00"), nil
}

func (g valueGit) survival(tip, base, sha string, end time.Time) (valueSurvival, error) {
	result := valueSurvival{Through: end}
	endSHA, err := g.run("rev-list", "--first-parent", "-1", "--before="+end.Format(time.RFC3339), tip)
	if err != nil {
		return result, err
	}
	if endSHA == "" {
		return result, errors.New("survival window has no end commit")
	}
	commits, err := g.run("rev-list", "--first-parent", sha+".."+endSHA)
	if err != nil {
		return result, err
	}
	paths, err := g.changedPaths(base, sha)
	if err != nil {
		return result, err
	}
	if len(paths) == 0 {
		return result, errors.New("merge has no changed files; survival evidence is unavailable")
	}
	// Literal pathspecs keep filenames containing glob syntax from widening the check.
	args := []string{"log", "--first-parent", "--format=%H", sha + ".." + endSHA, "--"}
	for _, path := range paths {
		args = append(args, ":(literal)"+path)
	}
	touched, err := g.run(args...)
	if err != nil {
		return result, err
	}
	result.Commits, result.Files, result.Touched = valueLineCount(commits), len(paths), valueLineCount(touched)
	return result, nil
}

func valueLineCount(text string) int {
	if text == "" {
		return 0
	}
	return strings.Count(text, "\n") + 1
}
