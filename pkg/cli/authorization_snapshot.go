package cli

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var authorizationCommitPattern = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type authorizationSnapshotEntry struct{ name, oid string }

func authorizationGit(ctx context.Context, root string, args ...string) ([]byte, error) {
	command := exec.CommandContext(ctx, "git", append([]string{"--no-pager", "-C", root}, args...)...)
	command.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0", "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL=/dev/null")
	return command.Output()
}

// materializeAuthorizationSnapshot reads raw Git objects, not a checkout or
// archive: neither filters/hooks nor export-ignore attributes can alter scope.
func materializeAuthorizationSnapshot(ctx context.Context, root, commit, destination string) (string, error) {
	tree, err := authorizationGit(ctx, root, "ls-tree", "-r", "-z", "--full-tree", commit)
	if err != nil {
		return "", errors.New("cannot enumerate candidate commit")
	}
	if len(tree) == 0 {
		return authorizationDigest(tree), nil
	}
	entries := bytes.Split(bytes.TrimSuffix(tree, []byte{0}), []byte{0})
	if len(entries) > 50000 {
		return "", errors.New("candidate exceeds snapshot file limit")
	}
	files := make([]authorizationSnapshotEntry, 0, len(entries))
	var input strings.Builder
	for _, line := range entries {
		header, name, ok := bytes.Cut(line, []byte{'\t'})
		mode, rest, modeOK := strings.Cut(string(header), " ")
		kind, oid, kindOK := strings.Cut(rest, " ")
		file := string(name)
		firstComponent, _, _ := strings.Cut(file, "/")
		if !ok || !modeOK || !kindOK || (mode != "100644" && mode != "100755") ||
			kind != "blob" || !authorizationCommitPattern.MatchString(oid) ||
			!filepath.IsLocal(file) || path.Clean(file) != file || strings.ContainsAny(file, "\\\x00\r\n") ||
			strings.EqualFold(firstComponent, ".git") {
			return "", errors.New("candidate contains an unsupported path, symlink, or submodule")
		}
		files = append(files, authorizationSnapshotEntry{file, oid})
		input.WriteString(oid + "\n")
	}
	command := exec.CommandContext(ctx, "git", "--no-pager", "-C", root, "cat-file", "--batch")
	command.Env = append(os.Environ(), "GIT_NO_REPLACE_OBJECTS=1", "GIT_NO_LAZY_FETCH=1", "GIT_OPTIONAL_LOCKS=0", "GIT_TERMINAL_PROMPT=0")
	command.Stdin = strings.NewReader(input.String())
	output, err := command.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := command.Start(); err != nil {
		return "", err
	}
	readErr := writeAuthorizationSnapshot(bufio.NewReader(output), files, destination)
	if readErr != nil {
		// Drain no further candidate data after a resource-limit failure.
		_ = command.Process.Kill()
	}
	waitErr := command.Wait()
	if readErr != nil {
		return "", readErr
	}
	if waitErr != nil {
		return "", errors.New("cannot read candidate objects")
	}
	return authorizationDigest(tree), nil
}

func writeAuthorizationSnapshot(reader *bufio.Reader, files []authorizationSnapshotEntry, destination string) error {
	var total int64
	for _, file := range files {
		header, err := reader.ReadString('\n')
		if err != nil {
			return err
		}
		oid, rest, oidOK := strings.Cut(strings.TrimSuffix(header, "\n"), " ")
		kind, sizeText, kindOK := strings.Cut(rest, " ")
		if !oidOK || !kindOK || oid != file.oid || kind != "blob" {
			return errors.New("invalid candidate object")
		}
		size, err := strconv.ParseInt(sizeText, 10, 64)
		if err != nil || size < 0 || size > 16<<20 || total+size > 256<<20 {
			return errors.New("candidate exceeds snapshot byte limits")
		}
		total += size
		target := filepath.Join(destination, filepath.FromSlash(file.name))
		if err := os.MkdirAll(filepath.Dir(target), 0o700); err != nil {
			return err
		}
		if err := writeAuthorizationBlob(reader, target, size); err != nil {
			return err
		}
		separator, err := reader.ReadByte()
		if err != nil || separator != '\n' {
			return errors.New("invalid candidate object separator")
		}
	}
	return nil
}

func writeAuthorizationBlob(reader io.Reader, target string, size int64) (resultErr error) {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o400)
	if err != nil {
		return err
	}
	defer func() { resultErr = errors.Join(resultErr, file.Close()) }()
	_, copyErr := io.CopyN(file, reader, size)
	if copyErr != nil {
		return fmt.Errorf("reading candidate object: %w", copyErr)
	}
	return nil
}
