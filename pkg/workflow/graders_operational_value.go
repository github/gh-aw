package workflow

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
)

const maxOperationalValueEvaluatorSize = 64 * 1024

func (c *Compiler) prepareOperationalValueGrader(data *WorkflowData, markdownPath string) error { //nolint:largefunc
	if data == nil || data.Graders == nil {
		return nil
	}
	grader, ok := data.Graders.Graders["operational-value"]
	if !ok || (grader.Enabled != nil && !*grader.Enabled) {
		return nil
	}
	if grader.Run == "" && grader.Script == "" {
		return errors.New("graders.operational-value requires exactly one of 'run' or 'script'")
	}
	if grader.Run != "" && grader.Script != "" {
		return errors.New("graders.operational-value must specify exactly one of 'run' or 'script'")
	}

	repoRoot, err := gitutil.FindGitRootFrom(filepath.Dir(markdownPath))
	if err != nil {
		return errors.New("cannot prepare graders.operational-value: workflow is not inside a Git repository")
	}
	if grader.Script != "" {
		if err := validateOperationalValueEvaluatorContent(grader.Script); err != nil {
			return fmt.Errorf("graders.operational-value.script %w", err)
		}
		relativeWorkflowPath, err := filepath.Rel(repoRoot, markdownPath)
		if err != nil {
			return fmt.Errorf("cannot resolve inline operational-value workflow source: %w", err)
		}
		grader.evaluatorSourcePath = filepath.ToSlash(relativeWorkflowPath)
		grader.evaluatorContent = grader.Script
		return nil
	}
	evaluatorPath := ResolveOperationalValueEvaluatorPath(repoRoot, markdownPath, grader.Run)
	if err := fileutil.ValidatePathWithinBase(repoRoot, evaluatorPath); err != nil {
		return fmt.Errorf("graders.operational-value.run %q escapes the Git repository", grader.Run)
	}
	evaluatorInfo, err := os.Lstat(evaluatorPath)
	if err != nil {
		return fmt.Errorf("cannot inspect graders.operational-value.run %q: %w", grader.Run, err)
	}
	if evaluatorInfo.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("graders.operational-value.run %q must not be a symbolic link", grader.Run)
	}

	file, err := os.Open(evaluatorPath)
	if err != nil {
		return fmt.Errorf("cannot read graders.operational-value.run %q: %w", grader.Run, err)
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("cannot inspect graders.operational-value.run %q: %w", grader.Run, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("graders.operational-value.run %q must be a regular file", grader.Run)
	}
	if info.Size() > maxOperationalValueEvaluatorSize {
		return fmt.Errorf("graders.operational-value.run %q exceeds the %d-byte limit", grader.Run, maxOperationalValueEvaluatorSize)
	}

	content, err := io.ReadAll(io.LimitReader(file, maxOperationalValueEvaluatorSize+1))
	if err != nil {
		return fmt.Errorf("cannot read graders.operational-value.run %q: %w", grader.Run, err)
	}
	if len(content) > maxOperationalValueEvaluatorSize {
		return fmt.Errorf("graders.operational-value.run %q exceeds the %d-byte limit", grader.Run, maxOperationalValueEvaluatorSize)
	}
	if !utf8.Valid(content) {
		return fmt.Errorf("graders.operational-value.run %q must be valid UTF-8", grader.Run)
	}
	evaluatorContent := string(content)
	if err := validateOperationalValueEvaluatorContent(evaluatorContent); err != nil {
		return fmt.Errorf("graders.operational-value.run %q %w", grader.Run, err)
	}

	grader.evaluatorContent = evaluatorContent
	return nil
}

func validateOperationalValueEvaluatorContent(evaluatorContent string) error {
	if len(evaluatorContent) > maxOperationalValueEvaluatorSize {
		return fmt.Errorf("exceeds the %d-byte limit", maxOperationalValueEvaluatorSize)
	}
	if !strings.HasPrefix(evaluatorContent, "#!/usr/bin/env bash\n") && !strings.HasPrefix(evaluatorContent, "#!/bin/bash\n") {
		return errors.New("must start with a Bash shebang")
	}
	if err := validateOperationalValueEvaluatorBash(evaluatorContent); err != nil {
		return fmt.Errorf("has invalid Bash syntax: %w", err)
	}
	return nil
}

// ResolveOperationalValueEvaluatorPath resolves a validated operational-value
// evaluator run path. Paths starting with "./" are local to the workflow file's
// directory; all other paths are relative to the repository root.
func ResolveOperationalValueEvaluatorPath(repoRoot, markdownPath, runPath string) string {
	if localPath, ok := strings.CutPrefix(runPath, "./"); ok {
		return filepath.Join(filepath.Dir(markdownPath), filepath.FromSlash(localPath))
	}
	return filepath.Join(repoRoot, filepath.FromSlash(runPath))
}
