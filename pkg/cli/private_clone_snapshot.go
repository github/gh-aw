package cli

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/workflow"
)

// privateCloneGitHubInstructionPaths are .github entries that agent engines
// load automatically as instructions, skills, agents, hooks, or MCP config.
var privateCloneGitHubInstructionPaths = []string{
	"copilot-instructions.md", "instructions", "agents", "skills",
	"hooks", "prompts", "chatmodes", "copilot", "mcp.json",
}

const privateCloneAWDir = constants.GithubDir + "aw"

var privateCloneRootInstructionPaths = []string{".mcp.json", ".vscode/mcp.json"}

// validatePrivateCloneSourceTree rejects snapshots whose content cannot be
// published without upstream access: submodules and Git LFS objects.
func validatePrivateCloneSourceTree(ctx context.Context, dir, sha string) error {
	data, err := privateCloneGit(ctx, dir, "ls-tree", "-r", "-z", "--full-tree", sha)
	if err != nil {
		return fmt.Errorf("inspect upstream snapshot: %w", err)
	}
	for entry := range bytes.SplitSeq(data, []byte{0}) {
		meta, name, ok := strings.Cut(string(entry), "\t")
		if !ok {
			continue
		}
		mode, rest, _ := strings.Cut(meta, " ")
		kind, oid, _ := strings.Cut(rest, " ")
		if mode == "160000" || name == ".gitmodules" {
			return fmt.Errorf("upstream snapshot contains a Git submodule (%s); private-clone cannot publish submodule contents without upstream access. Choose a source revision without submodules; no repository was created", name)
		}
		if path.Base(name) != ".gitattributes" || kind != "blob" {
			continue
		}
		blob, err := privateCloneGit(ctx, dir, "cat-file", "blob", oid)
		if err != nil {
			return fmt.Errorf("inspect upstream %s: %w", name, err)
		}
		if privateCloneUsesLFS(blob) {
			return fmt.Errorf("upstream snapshot uses Git LFS (%s); private-clone does not transfer LFS objects, so the clone would contain only pointer files. Choose a source revision without LFS-tracked files; no repository was created", name)
		}
	}
	return nil
}

func privateCloneUsesLFS(attributes []byte) bool {
	for line := range strings.SplitSeq(string(attributes), "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "#") && strings.Contains(line, "filter=lfs") {
			return true
		}
	}
	return false
}

// sanitizePrivateCloneSnapshot prepares an upstream checkout for operator
// workflows: it rejects symlinks at compiler-written paths and removes
// inherited workflows, action pin caches, and agent instruction files.
func sanitizePrivateCloneSnapshot(dir string) error {
	for _, check := range []struct {
		name string
		dir  bool
	}{{".github", true}, {privateCloneAWDir, true}, {".gitattributes", false}} {
		if err := requirePrivateCloneRealPath(dir, check.name, check.dir); err != nil {
			return err
		}
	}
	if _, err := os.Lstat(filepath.Join(dir, privateCloneContextDir)); err == nil {
		return errors.New("upstream already contains the reserved .github/private-clone directory")
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	removals := []string{constants.WorkflowsDir, privateCloneAWDir + "/" + workflow.CacheFileName}
	removals = append(removals, privateCloneRootInstructionPaths...)
	for _, name := range privateCloneGitHubInstructionPaths {
		removals = append(removals, constants.GithubDir+name)
	}
	for _, folder := range workflow.GetGlobalEngineRegistry().GetAllAgentManifestFolders() {
		if folder != ".github" {
			removals = append(removals, folder)
		}
	}
	for _, name := range removals {
		if err := requirePrivateCloneRealPath(dir, path.Dir(name), true); err != nil {
			return err
		}
		if err := os.RemoveAll(filepath.Join(dir, filepath.FromSlash(name))); err != nil {
			return err
		}
	}
	return removePrivateCloneManifestFiles(dir)
}

// requirePrivateCloneRealPath fails when name exists as a symlink or the wrong
// file type, so later writes and removals cannot escape the checkout.
func requirePrivateCloneRealPath(dir, name string, wantDir bool) error {
	if name == "." {
		return nil
	}
	info, err := os.Lstat(filepath.Join(dir, filepath.FromSlash(name)))
	if errors.Is(err, os.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	if wantDir && !info.IsDir() {
		return fmt.Errorf("upstream %s must be a real directory, not a file or symlink", name)
	}
	if !wantDir && !info.Mode().IsRegular() {
		return fmt.Errorf("upstream %s must be a regular file, not a symlink", name)
	}
	return nil
}

// removePrivateCloneManifestFiles removes engine instruction files such as
// AGENTS.md and CLAUDE.md at any depth, because engines load nested copies.
func removePrivateCloneManifestFiles(dir string) error {
	names := make(map[string]struct{})
	registry := workflow.GetGlobalEngineRegistry()
	for _, id := range registry.GetSupportedEngines() {
		engine, err := registry.GetEngine(id)
		if err != nil {
			return err
		}
		if provider, ok := engine.(workflow.AgentFileProvider); ok {
			for _, name := range provider.GetAgentManifestFiles() {
				names[strings.ToLower(name)] = struct{}{}
			}
		}
	}
	return filepath.WalkDir(dir, func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() && entry.Name() == ".git" {
			return filepath.SkipDir
		}
		if _, ok := names[strings.ToLower(entry.Name())]; ok && !entry.IsDir() {
			return os.Remove(name)
		}
		return nil
	})
}
