// Package gradershell loads repository Bash graders and checks a finite set of
// direct file-write patterns. It is not a sandbox: only trusted repository
// scripts should run until execution is isolated.
package gradershell

import (
	"bytes"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/github/gh-aw/pkg/fileutil"
	"github.com/github/gh-aw/pkg/gitutil"
)

const (
	maxSourceBytes       = 64 * 1024
	envShebangPath       = "/usr/bin/env"
	legacyEnvShebangPath = "/bin/env"
)

// ValidRunPath accepts literal .sh paths, without traversal or shell expressions.
// A leading ./ selects the declaring workflow's directory; other paths start at
// the repository root.
func ValidRunPath(path string) bool {
	if !strings.HasSuffix(path, ".sh") {
		return false
	}
	for _, c := range path {
		allowed := (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') ||
			(c >= '0' && c <= '9') || strings.ContainsRune("._/-", c)
		if !allowed {
			return false
		}
	}
	return fs.ValidPath(strings.TrimPrefix(path, "./"))
}

// Load reads a regular UTF-8 Bash source file of at most 64 KiB, without
// executing it. Both the declaring workflow and resolved source must remain
// inside the repository, and the source may not be a symlink.
func Load(workflowPath, runPath string) (string, error) {
	if !ValidRunPath(runPath) {
		return "", errors.New("shell grader run must be a literal relative .sh path without traversal or expressions")
	}
	if workflowPath == "" {
		return "", errors.New("shell grader requires the declaring workflow source path")
	}
	workflow, err := filepath.Abs(workflowPath)
	if err != nil {
		return "", errors.New("cannot resolve shell grader workflow source path")
	}
	root, err := gitutil.FindGitRootFrom(filepath.Dir(workflow))
	if err != nil {
		return "", errors.New("cannot locate shell grader repository root from the declaring workflow")
	}
	if err := fileutil.ValidatePathWithinBase(root, workflow); err != nil {
		return "", errors.New("shell grader declaring workflow escapes the repository")
	}
	base := root
	if strings.HasPrefix(runPath, "./") {
		base = filepath.Dir(workflow)
	}
	file := filepath.Join(base, filepath.FromSlash(runPath))
	if err := fileutil.ValidatePathWithinBase(root, file); err != nil {
		return "", errors.New("shell grader source escapes the repository")
	}
	if err := fileutil.ValidatePathWithinBase(base, file); err != nil {
		return "", errors.New("shell grader source escapes its path base")
	}
	info, err := os.Lstat(file)
	if err != nil {
		return "", errors.New("cannot stat shell grader source; provide an existing regular Bash file")
	}
	if !info.Mode().IsRegular() || info.Size() > maxSourceBytes {
		return "", errors.New("shell grader source must be a regular non-symlink file of at most 64 KiB")
	}
	return readSource(file, info)
}

func readSource(file string, info os.FileInfo) (source string, err error) {
	handle, err := os.Open(file)
	if err != nil {
		return "", errors.New("cannot read shell grader source")
	}
	defer func() {
		if closeErr := handle.Close(); closeErr != nil && err == nil {
			source = ""
			err = errors.New("cannot close shell grader source")
		}
	}()
	opened, err := handle.Stat()
	if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
		return "", errors.New("shell grader source changed while loading")
	}
	content, err := io.ReadAll(io.LimitReader(handle, maxSourceBytes+1))
	if err != nil {
		return "", errors.New("cannot read shell grader source")
	}
	if len(content) > maxSourceBytes || !utf8.Valid(content) || bytes.ContainsRune(content, '\x00') {
		return "", errors.New("shell grader source must be UTF-8 Bash text of at most 64 KiB without NUL bytes")
	}
	source = string(content)
	line, _, _ := strings.Cut(source, "\n")
	if !bashShebang(line) {
		return "", errors.New("shell grader source must start with a Bash shebang")
	}
	return source, nil
}

func bashShebang(line string) bool {
	if !strings.HasPrefix(line, "#!") {
		return false
	}
	fields := strings.Fields(strings.TrimPrefix(line, "#!"))
	if len(fields) == 0 {
		return false
	}
	interpreter := fields[0] //nolint:uncheckedsliceindex // The preceding empty-fields guard ensures index 0 exists.
	if filepath.IsAbs(interpreter) && filepath.Base(interpreter) == "bash" {
		return true
	}
	if interpreter != envShebangPath && interpreter != legacyEnvShebangPath {
		return false
	}
	if len(fields) == 2 {
		return fields[1] == "bash" //nolint:uncheckedsliceindex // Exactly two fields guarantees index 1.
	}
	if len(fields) >= 3 {
		return fields[1] == "-S" && fields[2] == "bash" //nolint:uncheckedsliceindex // At least three fields guarantees indexes 1 and 2.
	}
	return false
}
