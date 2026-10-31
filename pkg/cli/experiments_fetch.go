package cli

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/errorutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow"
)

type evalResultRecord struct {
	ID        string `json:"id"`
	Answer    string `json:"answer"`
	RunID     string `json:"runid"`
	Timestamp string `json:"timestamp"`
}

func loadLocalMetricEvalResults(workflowID string) map[string]MetricEvalResults {
	return summarizeMetricEvalResults(parseEvalResultRecords(loadLocalEvalResultsData(workflowID)))
}

func loadRemoteMetricEvalResults(repoOverride, workflowID string) map[string]MetricEvalResults {
	return summarizeMetricEvalResults(parseEvalResultRecords(loadRemoteEvalResultsData(repoOverride, workflowID)))
}

// loadLocalEvalResultRecords returns the raw per-run eval answer records for workflowID,
// used to attribute eval-backed experiment metrics to variants by run ID.
func loadLocalEvalResultRecords(workflowID string) []evalResultRecord {
	return parseEvalResultRecords(loadLocalEvalResultsData(workflowID))
}

// loadRemoteEvalResultRecords returns the raw per-run eval answer records for workflowID
// from repoOverride, used to attribute eval-backed experiment metrics to variants by run ID.
func loadRemoteEvalResultRecords(repoOverride, workflowID string) []evalResultRecord {
	return parseEvalResultRecords(loadRemoteEvalResultsData(repoOverride, workflowID))
}

func loadLocalEvalResultsData(workflowID string) []byte {
	branchName := workflow.WorkflowStateBranchName(constants.EvalsBranchPrefix, workflowID)
	ref := "origin/" + branchName
	if !gitRefExists(ref) {
		if !gitRefExists(branchName) {
			return nil
		}
		ref = branchName
	}
	if !isSafeExperimentStateRef(ref) {
		experimentsLog.Printf("Rejecting unsafe git ref: %q", ref)
		return nil
	}
	objectArg, err := buildSafeGitShowObjectArg(ref, constants.EvalsResultFilename.String())
	if err != nil {
		experimentsLog.Printf("Rejecting unsafe git show argument (ref=%q file=%q): %v", ref, constants.EvalsResultFilename, err)
		return nil
	}
	cmd := exec.Command("git", "show", objectArg)
	out, err := cmd.Output()
	if err != nil {
		return nil
	}
	return out
}

func loadRemoteEvalResultsData(repoOverride, workflowID string) []byte {
	branchName := workflow.WorkflowStateBranchName(constants.EvalsBranchPrefix, workflowID)
	decoded, err := readRemoteRepoBranchFile(repoOverride, branchName, constants.EvalsResultFilename.String(), "")
	if err != nil {
		return nil
	}
	return decoded
}

// parseEvalResultRecords parses evals.jsonl content into individual per-run eval answer records.
func parseEvalResultRecords(data []byte) []evalResultRecord {
	if len(data) == 0 {
		return nil
	}
	scanner := bufio.NewScanner(bytes.NewReader(data))
	var records []evalResultRecord
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var record evalResultRecord
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			continue
		}
		if record.ID == "" {
			continue
		}
		records = append(records, record)
	}
	return records
}

func summarizeMetricEvalResults(records []evalResultRecord) map[string]MetricEvalResults {
	results := map[string]MetricEvalResults{}
	for _, record := range records {
		summary := results[record.ID]
		summary.Total++
		switch strings.ToUpper(strings.TrimSpace(record.Answer)) {
		case "YES":
			summary.Yes++
		case "NO":
			summary.No++
		default:
			summary.Unknown++
		}
		summary.LatestAnswer = strings.ToUpper(strings.TrimSpace(record.Answer))
		summary.LatestRunID = record.RunID
		results[record.ID] = summary
	}
	if len(results) == 0 {
		return nil
	}
	return results
}

// experimentFrontmatterResult holds the experiment, eval, and grader configs parsed
// from a workflow's frontmatter.
type experimentFrontmatterResult struct {
	ExperimentConfigs map[string]*workflow.ExperimentConfig
	Evals             *workflow.EvalsConfig
	Graders           *workflow.GradersConfig
}

// loadLocalExperimentConfigs reads the workflow .md file for the given experiment name
// and returns the ExperimentConfig map and EvalsConfig from its frontmatter.
// experimentName is the sanitized workflow ID (the part after "experiments/" in the branch name).
// Returns a zero-value result when the workflow file cannot be found or parsed.
func loadLocalExperimentConfigs(experimentName string) experimentFrontmatterResult {
	experimentsLog.Printf("Loading local experiment configs for %s", experimentName)

	filePath := findWorkflowFileForExperiment(experimentName)
	if filePath == "" {
		experimentsLog.Printf("No workflow file found for experiment %s", experimentName)
		return experimentFrontmatterResult{}
	}

	// Verify that the resolved path is within .github/workflows/ to prevent path traversal.
	// findWorkflowFileForExperiment returns paths from filepath.Glob with a relative base dir,
	// so convert both sides to absolute paths before the prefix check.
	absFilePath, err := filepath.Abs(filePath)
	if err != nil {
		experimentsLog.Printf("Failed to resolve absolute path for %s: %v", filePath, err)
		return experimentFrontmatterResult{}
	}
	workflowsDir, err := filepath.Abs(getWorkflowsDir())
	if err != nil {
		experimentsLog.Printf("Failed to resolve workflows dir: %v", err)
		return experimentFrontmatterResult{}
	}
	if !strings.HasPrefix(absFilePath, workflowsDir+string(filepath.Separator)) {
		experimentsLog.Printf("Refusing to read workflow file outside .github/workflows/: %s", absFilePath)
		return experimentFrontmatterResult{}
	}

	content, err := os.ReadFile(absFilePath) // #nosec G304 -- path confirmed within .github/workflows/
	if err != nil {
		experimentsLog.Printf("Failed to read workflow file %s: %v", absFilePath, err)
		return experimentFrontmatterResult{}
	}

	result, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil {
		experimentsLog.Printf("Failed to parse frontmatter from %s: %v", filePath, err)
		return experimentFrontmatterResult{}
	}

	cfg, err := workflow.ParseFrontmatterConfig(result.Frontmatter)
	if err != nil {
		experimentsLog.Printf("Failed to parse frontmatter config from %s: %v", filePath, err)
		return experimentFrontmatterResult{}
	}

	evals, graders := parseExperimentMetricConfigs(result.Frontmatter, filePath)

	return experimentFrontmatterResult{
		ExperimentConfigs: cfg.ExperimentConfigs,
		Evals:             evals,
		Graders:           graders,
	}
}

// loadRemoteExperimentConfigs fetches the workflow .md file from the repository default branch
// via the GitHub API and returns the ExperimentConfig map and EvalsConfig from its frontmatter.
// Returns a zero-value result when the file cannot be fetched or parsed.
func loadRemoteExperimentConfigs(repoOverride, experimentName string) experimentFrontmatterResult {
	experimentsLog.Printf("Loading remote experiment configs for %s from %s", experimentName, repoOverride)
	if !isSafeRemoteExperimentName(experimentName) {
		return experimentFrontmatterResult{}
	}

	// Build the candidate list. First, use the directory listing to find the exact filename
	// whose sanitized basename matches experimentName (e.g. "ci-coach" for "cicoach").
	// Fall back to the bare experiment name if the listing is unavailable.
	candidates := workflowFileCandidates(experimentName)
	if resolved := findRemoteWorkflowFilenameForExperiment(repoOverride, experimentName); resolved != "" && resolved != experimentName {
		// Prepend the resolved name so it is tried before the bare sanitized form.
		candidates = append([]string{resolved}, candidates...)
	}

	for _, candidate := range candidates {
		apiPath := constants.WorkflowsDirSlash + candidate + ".md"
		args, err := repositoryAPIArgs(repoOverride, "", "contents/"+escapeEndpoint(apiPath),
			"--jq", ".content",
		)
		if err != nil {
			experimentsLog.Printf("Rejecting remote workflow path %q: %v", apiPath, err)
			continue
		}
		cmd := workflow.ExecGH(args...)
		out, err := cmd.Output()
		if err != nil {
			continue
		}

		decoded, err := decodeBase64FileContent(string(out))
		if err != nil {
			experimentsLog.Printf("Failed to base64-decode workflow file %s: %v", candidate, err)
			continue
		}

		result, err := parser.ExtractFrontmatterFromContent(string(decoded))
		if err != nil {
			continue
		}

		cfg, err := workflow.ParseFrontmatterConfig(result.Frontmatter)
		if err != nil {
			continue
		}

		evals, graders := parseExperimentMetricConfigs(result.Frontmatter, apiPath)

		if len(cfg.ExperimentConfigs) > 0 {
			experimentsLog.Printf("Loaded remote configs from %s", apiPath)
			return experimentFrontmatterResult{
				ExperimentConfigs: cfg.ExperimentConfigs,
				Evals:             evals,
				Graders:           graders,
			}
		}
	}

	experimentsLog.Printf("No remote workflow file found for experiment %s", experimentName)
	return experimentFrontmatterResult{}
}

func isSafeRemoteExperimentName(experimentName string) bool {
	if err := validateAPIEndpoint(experimentName); err != nil {
		experimentsLog.Printf("Rejecting unsafe experiment name %q: %v", experimentName, err)
		return false
	}
	return true
}

func parseExperimentMetricConfigs(
	frontmatter map[string]any,
	source string,
) (*workflow.EvalsConfig, *workflow.GradersConfig) {
	evals, err := workflow.ParseEvalsFromFrontmatter(frontmatter)
	if err != nil {
		experimentsLog.Printf("Failed to parse evals config from %s: %v", source, err)
	}
	graders, err := workflow.ParseGradersFromFrontmatter(frontmatter)
	if err != nil {
		experimentsLog.Printf("Failed to parse graders config from %s: %v", source, err)
	}
	return evals, graders
}

// findRemoteWorkflowFilenameForExperiment lists .md files in .github/workflows/ via the
// GitHub API and returns the basename (without .md) of the first file whose sanitized name
// matches experimentName. This mirrors findWorkflowFileForExperiment for remote repos.
// Returns "" when the directory cannot be listed or no match is found.
func findRemoteWorkflowFilenameForExperiment(repoOverride, experimentName string) string {
	args, err := repositoryAPIArgs(repoOverride, "", "contents/.github/workflows",
		"--jq", `[.[] | select(.name | endswith(".md")) | .name]`,
	)
	if err != nil {
		experimentsLog.Printf("Rejecting remote workflow listing endpoint: %v", err)
		return ""
	}
	cmd := workflow.ExecGH(args...)
	out, err := cmd.Output()
	if err != nil {
		experimentsLog.Printf("Failed to list remote workflow files from %s: %v", repoOverride, err)
		return ""
	}

	var filenames []string
	if err := json.Unmarshal(out, &filenames); err != nil {
		experimentsLog.Printf("Failed to parse remote workflow file listing: %v", err)
		return ""
	}

	return matchWorkflowFilenameByExperiment(filenames, experimentName)
}

// matchWorkflowFilenameByExperiment returns the basename (without .md) of the first file in
// filenames whose sanitized name matches experimentName. Returns "" when no match is found.
// Logs a warning when more than one file maps to the same sanitized name.
//
// Note: normalizeWorkflowID calls filepath.Base internally, so any path prefix in filenames
// is stripped before matching. Callers that supply bare filenames (e.g. "my-flow.md") are
// unaffected; callers supplying full paths (e.g. ".github/workflows/my-flow.md") will have
// the directory component removed — only the basename is returned and compared.
func matchWorkflowFilenameByExperiment(filenames []string, experimentName string) string {
	var matches []string
	for _, filename := range filenames {
		base := normalizeWorkflowID(filename)
		if workflow.SanitizeWorkflowIDForCacheKey(base) == experimentName {
			matches = append(matches, base)
		}
	}
	if len(matches) == 0 {
		return ""
	}
	if len(matches) > 1 {
		experimentsLog.Printf("Ambiguous experiment name %q: multiple workflow files match (%s); using first", experimentName, strings.Join(matches, ", "))
	}
	return matches[0]
}

// findWorkflowFileForExperiment scans .github/workflows/ for a .md file whose sanitized
// basename (lowercase, hyphens removed) matches the given experiment name.
// Returns the file path or "" when no match is found.
func findWorkflowFileForExperiment(experimentName string) string {
	mdFiles, err := getMarkdownWorkflowFiles("")
	if err != nil {
		return ""
	}
	for _, f := range mdFiles {
		base := normalizeWorkflowID(f)
		if workflow.SanitizeWorkflowIDForCacheKey(base) == experimentName {
			return f
		}
	}
	return ""
}

// workflowFileCandidates returns a fallback list of candidate workflow file basenames (without .md)
// for remote lookups when the directory listing is unavailable. The sanitized form
// (hyphens removed, lowercased) is irreversible, so only the experiment name itself is
// returned here. The caller should prefer findRemoteWorkflowFilenameForExperiment which
// resolves the real filename by scanning the remote directory.
func workflowFileCandidates(experimentName string) []string {
	return []string{experimentName}
}

// fetchLocalExperiments lists experiment branches and reads their state from the local git repo.
func fetchLocalExperiments() ([]ExperimentInfo, error) {
	experimentsLog.Print("Fetching local experiment branches via git for-each-ref")

	cmd := exec.Command("git", "for-each-ref",
		"--sort=-committerdate",
		"--format=%(refname:short)",
		"refs/remotes/origin/"+experimentsBranchPrefix+"*",
		"refs/heads/"+experimentsBranchPrefix+"*",
	)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && exitErr.ExitCode() == 128 {
			return []ExperimentInfo{}, nil
		}
		return nil, fmt.Errorf("failed to list experiment branches: %w", err)
	}

	seen := make(map[string]struct {
	})
	var experiments []ExperimentInfo

	for line := range strings.SplitSeq(strings.TrimSpace(string(output)), "\n") {
		if line == "" {
			continue
		}
		workflowID := extractExperimentName(line)
		if workflowID == "" || setutil.Contains(seen, workflowID) {
			continue
		}
		seen[workflowID] = struct {
		}{}

		branchName := experimentsBranchPrefix + workflowID
		// Prefer remote ref; fall back to local.
		ref := "origin/" + branchName
		if !gitRefExists(ref) {
			ref = branchName
		}
		state := readLocalExperimentState(ref)
		experiments = append(experiments, experimentInfoFromState(workflowID, branchName, state))
	}

	return experiments, nil
}

// fetchRemoteExperiments lists experiment branches and reads their state via the GitHub API.
func fetchRemoteExperiments(repoOverride string) ([]ExperimentInfo, error) {
	experimentsLog.Printf("Fetching remote experiment branches: repo=%s", repoOverride)

	args, err := repositoryAPIArgs(repoOverride, "", "branches",
		"--paginate",
		"--jq", `[.[] | select(.name | startswith("`+experimentsBranchPrefix+`")) | .name]`,
	)
	if err != nil {
		return nil, fmt.Errorf("invalid remote branches endpoint: %w", err)
	}
	cmd := workflow.ExecGH(args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			return nil, fmt.Errorf("failed to fetch branches (exit %d): %s", exitErr.ExitCode(), strings.TrimSpace(string(exitErr.Stderr)))
		}
		return nil, fmt.Errorf("failed to fetch branches: %w", err)
	}

	branchNames, err := parsePagedJSONArray[string](string(output))
	if err != nil {
		return nil, fmt.Errorf("failed to parse branch list: %w", err)
	}

	var experiments []ExperimentInfo
	for _, branchName := range branchNames {
		workflowID := strings.TrimPrefix(branchName, experimentsBranchPrefix)
		state := readRemoteExperimentState(repoOverride, branchName)
		experiments = append(experiments, experimentInfoFromState(workflowID, branchName, state))
	}

	return experiments, nil
}

// fetchLocalExperimentDetails reads experiment state from a local experiment branch.
func fetchLocalExperimentDetails(branchName, workflowID string) (*ExperimentDetails, error) {
	experimentsLog.Printf("Fetching local experiment details: branch=%s", branchName)

	ref := "origin/" + branchName
	if !gitRefExists(ref) {
		if !gitRefExists(branchName) {
			return nil, fmt.Errorf("experiment branch %q not found locally (tried origin/%s and %s)",
				branchName, branchName, branchName)
		}
		ref = branchName
	}

	state := readLocalExperimentState(ref)
	return experimentDetailsFromState(workflowID, branchName, state), nil
}

// fetchRemoteExperimentDetails reads experiment state from a remote experiment branch.
func fetchRemoteExperimentDetails(repoOverride, branchName, workflowID string) (*ExperimentDetails, error) {
	experimentsLog.Printf("Fetching remote experiment details: repo=%s, branch=%s", repoOverride, branchName)

	// Verify the branch exists.
	encodedBranch := url.PathEscape(branchName)
	checkArgs, err := repositoryAPIArgs(repoOverride, "", "branches/"+encodedBranch,
		"--jq", ".name",
	)
	if err != nil {
		return nil, fmt.Errorf("invalid experiment branch endpoint: %w", err)
	}
	checkCmd := workflow.ExecGH(checkArgs...)
	if _, err := checkCmd.Output(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if errorutil.IsNotFoundOutput(stderr) {
				return nil, fmt.Errorf("experiment %q not found in %s", workflowID, repoOverride)
			}
			return nil, fmt.Errorf("failed to fetch experiment branch (exit %d): %s", exitErr.ExitCode(), stderr)
		}
		return nil, fmt.Errorf("failed to fetch experiment branch: %w", err)
	}

	state := readRemoteExperimentState(repoOverride, branchName)
	return experimentDetailsFromState(workflowID, branchName, state), nil
}
