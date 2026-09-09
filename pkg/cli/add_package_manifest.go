// Package cli: repository-package manifest support is split across several files that
// each own a distinct responsibility:
//   - add_package_manifest.go (this file): shared types, error sentinels, and
//     test-stub indirection vars used by the other files below.
//   - add_package_manifest_resolve.go: top-level orchestration of resolving a remote
//     package (manifest + files) given a repo spec.
//   - add_package_manifest_parse.go: parsing raw YAML into a repositoryPackageManifest
//     and validating manifest-level invariants.
//   - add_package_manifest_includes.go: parsing/validating/normalizing the
//     includes/files manifest fields into typed installable values.
//   - add_package_manifest_skills.go: discovering and resolving skill directories and
//     agent files from a remote package.
//   - add_package_manifest_remote.go: thin GitHub remote-fetch wrappers and repo-spec
//     parsing utilities.

package cli

import (
	"errors"
	"path"
	"path/filepath"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/logger"
)

var (
	errRepositoryPackageFileNotFound     = errors.New("repository package file not found")
	errRepositoryPackageManifestNotFound = errors.New("repository package manifest not found")
)

var downloadPackageFileFromGitHubForHost = downloadRepositoryPackageFileFromGitHubForHost
var listPackageWorkflowFilesForHost = listRepositoryPackageWorkflowFilesForHost
var listPackageDirFilesForHost = listRepositoryPackageDirFilesForHost
var listPackageDirFilesRecursivelyForHost = listRepositoryPackageDirFilesRecursivelyForHost
var listPackageDirSubdirsForHost = listRepositoryPackageDirSubdirsForHost
var getRepositoryPackageDefaultBranch = resolveRepositoryPackageDefaultBranch
var getRepositoryPackageLatestRelease = resolveRepositoryPackageLatestRelease
var addPackageManifestLog = logger.New("cli:add_package_manifest")

var packageSourceDirectories = []string{"workflows", constants.WorkflowsDir}

const repositoryPackageManifestFileName = "aw.yml"
const repositoryPackageManifestVersion = "1"
const ghAwRepositorySlug = "github/gh-aw"
const packageSkillsDirectory = "skills"
const packageAgentsDirectory = "agents"
const packageSkillMarkerFile = "SKILL.md"

type resolvedRepositoryPackage struct {
	ManifestPath       string
	ResolvedRef        string
	Name               string
	Emoji              string
	Icon               string
	Description        string
	License            string
	Private            bool
	Experimental       bool
	DocsPath           string
	InstallationSource []resolvedPackageInstallable
	ResourceFiles      []resolvedPackageResource
	ProjectFile        *resolvedPackageResource
	Bootstrap          *repositoryPackageBootstrap
	SkillFiles         []resolvedPackageSkillFile
	AgentFiles         []string
	Warnings           []string
}

// resolvedPackageInstallable pairs the location a workflow file is read from with the
// path it is installed to in the consuming repository.
type resolvedPackageInstallable struct {
	// SourcePath is where the file is fetched from. For remote packages it is a
	// repository-relative path (e.g. "factory/payload/workflows/reviewer.md"); for local
	// packages it is an absolute filesystem path.
	SourcePath string
	// DestinationPath is the repository-root-relative install path in the consuming
	// repository (e.g. ".github/workflows/reviewer.md").
	DestinationPath string
}

type resolvedPackageResource struct {
	SourcePath      string
	DestinationPath string
}

// packageInstallableSourcePaths returns the source paths of the given installables.
func packageInstallableSourcePaths(installables []resolvedPackageInstallable) []string {
	paths := make([]string, 0, len(installables))
	for _, installable := range installables {
		paths = append(paths, installable.SourcePath)
	}
	return paths
}

// defaultPackageInstallDestination returns the repository-root-relative install path used
// for entries that do not declare an explicit destination.
func defaultPackageInstallDestination(sourcePath string) string {
	return constants.WorkflowsDirSlash + path.Base(filepath.ToSlash(sourcePath))
}

// packageInstallablesFromSourcePaths converts plain source paths into installables using
// the default destination for each entry.
func packageInstallablesFromSourcePaths(sourcePaths []string) []resolvedPackageInstallable {
	installables := make([]resolvedPackageInstallable, 0, len(sourcePaths))
	for _, sourcePath := range sourcePaths {
		installables = append(installables, resolvedPackageInstallable{
			SourcePath:      sourcePath,
			DestinationPath: defaultPackageInstallDestination(sourcePath),
		})
	}
	return installables
}

// resolvedPackageSkillFile represents a single file within a skill directory that
// should be installed to the agentic engine skill folder.
type resolvedPackageSkillFile struct {
	// SourcePath is the file's path in the remote repository (e.g. "skills/my-skill/SKILL.md").
	SourcePath string
	// SkillName is the name of the skill directory (e.g. "my-skill").
	SkillName string
}

type packageRemoteNotFoundError struct {
	cause error
}

func (e packageRemoteNotFoundError) Error() string {
	return e.cause.Error()
}

func (e packageRemoteNotFoundError) Unwrap() []error {
	return []error{errRepositoryPackageFileNotFound, e.cause}
}
