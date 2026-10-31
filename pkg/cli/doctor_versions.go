package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/semverutil"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/aw"
)

type DoctorReleaseCheck struct {
	Current string `json:"current"`
	Latest  string `json:"latest,omitempty"`
	Status  string `json:"status"`
	Message string `json:"message"`
}

type DoctorCompiledWorkflowCheck struct {
	Workflow        string `json:"workflow"`
	LockFile        string `json:"lock_file"`
	CompilerVersion string `json:"compiler_version,omitempty"`
	SourceStatus    string `json:"source_status"`
	CompilerStatus  string `json:"compiler_status"`
	Message         string `json:"message"`
}

func checkDoctorRelease(ctx context.Context) *DoctorReleaseCheck {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	latest, err := getLatestRelease(ctx, false)
	return diagnoseDoctorRelease(GetVersion(), latest, err)
}

func checkDoctorVersions(ctx context.Context, dir string, scanCheckout bool, runtime setupRepositoryRuntime) (*DoctorReleaseCheck, []DoctorCompiledWorkflowCheck, error) {
	if runtime.checkRelease == nil {
		return nil, nil, nil
	}
	release := runtime.checkRelease(ctx)
	if !scanCheckout || runtime.checkCompiledWorkflows == nil {
		return release, nil, nil
	}
	checks, err := runtime.checkCompiledWorkflows(ctx, dir, release.Latest)
	return release, checks, err
}

func diagnoseDoctorRelease(current, latest string, lookupErr error) *DoctorReleaseCheck {
	check := &DoctorReleaseCheck{Current: current, Latest: latest, Status: "unknown"}
	if lookupErr != nil {
		check.Message = "Cannot query the current gh-aw stable release: " + lookupErr.Error()
		return check
	}
	latestVersion := semverutil.EnsureVPrefix(latest)
	if !semverutil.IsValid(latestVersion) {
		check.Message = "The latest release did not provide a valid semantic version."
		return check
	}
	currentVersion := semverutil.EnsureVPrefix(current)
	if !semverutil.IsValid(currentVersion) {
		check.Status = "development"
		check.Message = "Local gh-aw is a development build; its version cannot be ordered against the latest stable release."
		return check
	}
	if semverutil.Compare(currentVersion, latestVersion) < 0 {
		check.Status = "outdated"
		check.Message = "A newer stable gh-aw release is available. Update with gh extension upgrade github/gh-aw, then recompile workflows with gh aw compile."
	} else {
		check.Status = "current"
		check.Message = "Local gh-aw is at least as recent as the latest stable release."
	}
	return check
}

func checkDoctorCompiledWorkflows(ctx context.Context, dir, latest string) ([]DoctorCompiledWorkflowCheck, error) {
	root, err := gitutil.FindGitRootFrom(dir)
	if errors.Is(err, gitutil.ErrNotGitRepository) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	workflowDir := getWorkflowsDir()
	if !filepath.IsAbs(workflowDir) {
		workflowDir = filepath.Join(root, workflowDir)
	}
	if _, err := os.Stat(workflowDir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	files, err := getMarkdownWorkflowFiles(workflowDir)
	if err != nil {
		return nil, err
	}
	cache := parser.NewImportCache(root)
	cache.SetReadOnly(true)
	var checks []DoctorCompiledWorkflowCheck
	for _, file := range files {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		check, err := inspectDoctorCompiledWorkflow(file, root, latest, cache)
		if err != nil {
			return nil, err
		}
		if check != nil {
			checks = append(checks, *check)
		}
	}
	return checks, nil
}

func inspectDoctorCompiledWorkflow(file, root, latest string, cache *parser.ImportCache) (*DoctorCompiledWorkflowCheck, error) {
	rel, err := filepath.Rel(root, file)
	if err != nil {
		return nil, err
	}
	check := &DoctorCompiledWorkflowCheck{
		Workflow: filepath.ToSlash(rel), LockFile: stringutil.MarkdownToLockFile(filepath.ToSlash(rel)),
		SourceStatus: "unknown", CompilerStatus: "unknown",
	}
	content, err := readWorkflowFileContent(file)
	if err != nil {
		check.Message = "Cannot inspect workflow source: " + err.Error()
		return check, nil
	}
	document, err := aw.Parse(content)
	if err != nil {
		check.Message = "Cannot parse workflow source: " + err.Error()
		return check, nil
	}
	on, exists := document.Frontmatter["on"]
	if !exists || parser.IsImportSafeSharedWorkflowOn(on) {
		return nil, nil
	}
	inspectDoctorCompiledMetadata(check, file, latest, document, cache)
	return check, nil
}

func inspectDoctorCompiledMetadata(check *DoctorCompiledWorkflowCheck, file, latest string, document *aw.Document, cache *parser.ImportCache) {
	metadata, err := readLockMetadataFromHeader(stringutil.MarkdownToLockFile(file))
	if errors.Is(err, os.ErrNotExist) {
		check.SourceStatus = "missing"
		check.Message = "Compiled workflow is missing. Run gh aw compile to generate it."
		return
	}
	if err != nil {
		check.Message = "Cannot inspect compiled workflow metadata: " + err.Error()
		return
	}
	if metadata == nil {
		check.Message = "Compiled workflow lacks current metadata; source freshness and compiler version are unknown. Recompile with gh aw compile."
		return
	}
	check.CompilerVersion = metadata.CompilerVersion
	check.CompilerStatus = doctorCompiledVersionStatus(metadata.CompilerVersion, latest)
	check.SourceStatus, err = doctorCompiledSourceStatus(file, document, metadata, cache)
	if err != nil {
		check.Message = "Cannot verify compiled workflow freshness: " + err.Error()
	} else {
		check.Message = "Source freshness and compiler version are checked separately. Recompile stale or outdated workflows with gh aw compile after updating gh-aw; intentional newer/prerelease versions are not downgraded."
	}
	if check.CompilerStatus == "unknown" {
		check.Message += " Compiler version comparison is unavailable: the recorded compiler version or latest release is missing or is not a semantic version."
	}
}

func doctorCompiledVersionStatus(current, latest string) string {
	currentVersion, latestVersion := semverutil.EnsureVPrefix(current), semverutil.EnsureVPrefix(latest)
	if !semverutil.IsValid(currentVersion) || !semverutil.IsValid(latestVersion) {
		return "unknown"
	}
	switch comparison := semverutil.Compare(currentVersion, latestVersion); {
	case comparison < 0:
		return "outdated"
	case comparison > 0:
		return "newer"
	default:
		return "current"
	}
}

func doctorCompiledSourceStatus(file string, document *aw.Document, metadata *workflow.LockMetadata, cache *parser.ImportCache) (string, error) {
	if metadata.FrontmatterHash == "" {
		return "unknown", errors.New("compiled workflow has no frontmatter hash")
	}
	if _, err := parser.ProcessImportsFromFrontmatterWithSource(document.Frontmatter, filepath.Dir(file), cache, file, strings.Join(document.Location.FrontmatterLines, "\n")); err != nil {
		return "unknown", fmt.Errorf("cannot resolve workflow imports: %w", err)
	}
	hash, err := parser.ComputeFrontmatterHashFromFile(file, cache)
	if err != nil {
		return "unknown", err
	}
	if hash != metadata.FrontmatterHash {
		return "stale", nil
	}
	_, full := workflow.ParseStaleCheckMode(document.Frontmatter)
	if full {
		if metadata.BodyHash == "" {
			return "unknown", errors.New("full stale-check requires a compiled body hash")
		}
		bodyHash, err := parser.ComputeBodyHashFromFile(file)
		if err != nil {
			return "unknown", err
		}
		if bodyHash != metadata.BodyHash {
			return "stale", nil
		}
	}
	return "current", nil
}

func doctorVersionCheckError(release *DoctorReleaseCheck, checks []DoctorCompiledWorkflowCheck) error {
	if release != nil && (release.Status == "outdated" || release.Status == "unknown") {
		return errors.New("gh-aw release diagnostics need attention; see update guidance or release lookup error")
	}
	for _, check := range checks {
		if check.SourceStatus != "current" || check.CompilerStatus == "outdated" || check.CompilerStatus == "unknown" {
			return errors.New("compiled workflows need attention; see source freshness and compiler version diagnostics")
		}
	}
	return nil
}

func printDoctorVersionChecks(release *DoctorReleaseCheck, checks []DoctorCompiledWorkflowCheck) error {
	if release != nil {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(fmt.Sprintf("gh-aw release: installed %s, latest stable %s (%s). %s", release.Current, release.Latest, release.Status, release.Message)))
	}
	for _, check := range checks {
		if check.SourceStatus == "current" && (check.CompilerStatus == "current" || check.CompilerStatus == "newer") {
			continue
		}
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%s: source %s, compiler %s (%s). %s", check.Workflow, check.SourceStatus, check.CompilerVersion, check.CompilerStatus, check.Message)))
	}
	return doctorVersionCheckError(release, checks)
}
