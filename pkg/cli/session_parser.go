package cli

import (
	"bytes"
	"context"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/github/gh-aw/actions/setup"
	"github.com/github/gh-aw/pkg/constants"
)

func runSessionParser(ctx context.Context, args ...string) ([]byte, error) {
	tempDir, err := os.MkdirTemp("", "gh-aw-session-parser-*")
	if err != nil {
		return nil, fmt.Errorf("failed to create session parser directory: %w", err)
	}
	defer os.RemoveAll(tempDir)

	if err := fs.WalkDir(setup.SessionParserSources, "js", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := setup.SessionParserSources.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(filepath.Join(tempDir, entry.Name()), content, constants.FilePermSensitive)
	}); err != nil {
		return nil, fmt.Errorf("failed to prepare session parser sources: %w", err)
	}

	cmd := exec.CommandContext(ctx, "node", append([]string{filepath.Join(tempDir, "session_cli.cjs")}, args...)...)
	cmd.Dir = tempDir
	var diagnostics bytes.Buffer
	cmd.Stderr = &diagnostics
	output, err := cmd.Output()
	if diagnostics.Len() > 0 {
		fmt.Fprint(os.Stderr, diagnostics.String())
	}
	if err != nil {
		return nil, fmt.Errorf("failed to execute session parser (Node.js is required): %w: %s", err, diagnostics.String())
	}
	return output, nil
}
