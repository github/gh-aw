package cli

import (
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/goccy/go-yaml"
)

// DoctorTokenValidation checks availability and historical failures, never values.
type DoctorTokenValidation struct {
	Workflow     string                      `json:"workflow"`
	Secrets      []string                    `json:"secrets"`
	Environments []string                    `json:"environments,omitempty"`
	Available    []string                    `json:"available"`
	Missing      []string                    `json:"missing"`
	Status       string                      `json:"status"`
	Message      string                      `json:"message"`
	SecretsURL   string                      `json:"secrets_url"`
	RecentRuns   *DoctorAssignmentRunHistory `json:"recent_runs,omitempty"`
}

const doctorTokenReplacementGuidance = "The agent must elicit the user to create or replace the implicated workflow token through GitHub's secure Actions secrets UI. Never request token values in chat, retrieve stored secret values, or use the gh auth login OAuth token as a workflow credential. Never run gh auth token or copy CLI credentials into Actions secrets."

func checkDoctorTokens(ctx context.Context, repo, dir string) ([]DoctorTokenValidation, error) {
	root, err := gitutil.FindGitRootFrom(dir)
	if errors.Is(err, gitutil.ErrNotGitRepository) && repo == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to locate checkout for token validation: %w", err)
	}
	checks, err := discoverDoctorTokenValidations(root)
	if err != nil || len(checks) == 0 {
		return checks, err
	}
	hasSecrets := false
	for _, check := range checks {
		if len(check.Secrets) > 0 {
			hasSecrets = true
			break
		}
	}
	if !hasSecrets {
		return checks, nil
	}
	remote, _, err := resolveRemoteURL(root)
	if err != nil {
		return nil, err
	}
	host := getGitHubHost()
	if !isAnyGitHubHostEnvVarSet() {
		host = "https://" + extractHostFromRemoteURL(remote)
	}
	if repo == "" {
		repo = doctorAssignmentRepositoryFromRemote(remote)
	}
	if !isValidOwnerRepoSlug(repo) {
		return nil, errors.New("cannot resolve owner/repo for token validation")
	}
	names, lookupErr := listDoctorAssignmentSecrets(ctx, repo, host)
	environments := map[string]doctorTokenEnvironmentLookup{}
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	miner := doctorAssignmentRunMiner{
		ctx: ctx, repo: repo, host: host, now: time.Now().UTC(), runGH: workflow.RunGHContext,
		runGHLimited: runDoctorGHContextLimited, classify: classifyDoctorTokenLog,
		allJobs: true, limit: 1, maxLogBytes: doctorTokenRunLogLimitBytes,
	}
	var ordered []*DoctorTokenValidation
	for i := range checks {
		check := &checks[i]
		if len(check.Secrets) == 0 {
			continue
		}
		check.SecretsURL = fmt.Sprintf("%s/%s/settings/secrets/actions", host, repo)
		available, coverageErr := availableDoctorTokenSecrets(ctx, repo, host, names, check.Environments, environments)
		diagnoseDoctorTokenValidation(check, available, errors.Join(lookupErr, coverageErr))
		ordered = append(ordered, check)
	}
	collectDoctorTokenRunEvidence(ordered, &miner)
	return checks, nil
}

func prioritizeDoctorTokenChecks(checks []*DoctorTokenValidation) {
	known := constants.GetAllEngineSecretNames()
	priority := func(check *DoctorTokenValidation) int {
		for _, name := range check.Available {
			if !slices.Contains(known, name) {
				return 0
			}
		}
		if len(check.Available) > 0 {
			return 1
		}
		return 2
	}
	// Inspect custom credentials before optional built-in fallbacks. Sampling
	// one run per workflow avoids spending the shared budget on the first file.
	slices.SortStableFunc(checks, func(a, b *DoctorTokenValidation) int {
		return cmp.Compare(priority(a), priority(b))
	})
}

func discoverDoctorTokenValidations(root string) ([]DoctorTokenValidation, error) {
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
	return discoverDoctorTokenValidationsFromFiles(root, files, cache)
}

func discoverDoctorTokenValidationsFromFiles(root string, files []string, cache *parser.ImportCache) ([]DoctorTokenValidation, error) {
	var checks []DoctorTokenValidation
	for _, file := range files {
		rel, err := filepath.Rel(root, file)
		if err != nil {
			return nil, err
		}
		names, environments, err := discoverDoctorTokens(file, cache)
		if err != nil {
			checks = append(checks, DoctorTokenValidation{
				Workflow: filepath.ToSlash(rel),
				Status:   "unknown",
				Message:  fmt.Sprintf("Token discovery is incomplete: %v", err),
			})
			continue
		}
		if len(names) == 0 {
			continue
		}
		checks = append(checks, DoctorTokenValidation{Workflow: filepath.ToSlash(rel), Secrets: names, Environments: environments})
	}
	return checks, nil
}

type doctorTokenEnvironmentLookup struct {
	names map[string]struct{}
	err   error
}

func availableDoctorTokenSecrets(ctx context.Context, repo, host string, names map[string]struct{}, environments []string, cache map[string]doctorTokenEnvironmentLookup) (map[string]struct{}, error) {
	available := make(map[string]struct{}, len(names))
	for name := range names {
		available[name] = struct{}{}
	}
	var coverageErr error
	for _, environment := range environments {
		lookup, found := cache[environment]
		if !found {
			lookup.names, lookup.err = listDoctorAssignmentEnvironmentSecrets(ctx, repo, host, environment)
			cache[environment] = lookup
		}
		for name := range lookup.names {
			available[name] = struct{}{}
		}
		coverageErr = errors.Join(coverageErr, lookup.err)
	}
	return available, coverageErr
}

func diagnoseDoctorTokenValidation(check *DoctorTokenValidation, available map[string]struct{}, coverageErr error) {
	check.Available, check.Missing = []string{}, []string{}
	for _, name := range check.Secrets {
		if _, ok := available[name]; ok {
			check.Available = append(check.Available, name)
		} else {
			check.Missing = append(check.Missing, name)
		}
	}
	check.Status = "unverified"
	check.Message = "Secret metadata does not establish token validity, expiry, permissions, organization approval/SSO, or which fallback is selected. Availability in one scope does not prove availability in every consuming job. Absent fallback secrets are not necessarily required."
	if coverageErr != nil {
		check.Status = "unknown"
		check.Message += " Secret metadata coverage is incomplete: " + coverageErr.Error()
	}
	applyDoctorTokenRunEvidence(check)
	check.Message += " " + doctorTokenReplacementGuidance
}

func applyDoctorTokenRunEvidence(check *DoctorTokenValidation) {
	if check.RecentRuns != nil {
		if check.RecentRuns.Error != "" {
			check.Status = "unknown"
		}
		for _, run := range check.RecentRuns.Runs {
			if run.Outcome == "unknown" {
				check.Status = "unknown"
			}
		}
	}
	if doctorAssignmentRunFailure(check.RecentRuns) {
		check.Status = "credential_error"
		check.Message += " Existing runs report credential failures; this is historical workflow evidence, not proof that every listed token is invalid."
	}
}

func applyDoctorAssignmentTokenEvidence(checks []DoctorTokenValidation, assignments []DoctorAgentAssignmentCheck) {
	for i := range checks {
		check := &checks[i]
		for _, assignment := range assignments {
			if assignment.Workflow != check.Workflow || assignment.RecentRuns == nil || !slices.Contains(check.Secrets, assignment.Secret) {
				continue
			}
			for _, run := range assignment.RecentRuns.Runs {
				if run.Outcome == "credential_error" {
					check.Status = "credential_error"
					check.RecentRuns = assignment.RecentRuns
					var message strings.Builder
					message.WriteString(check.Message)
					message.WriteString(" The assignment handler's existing-run sample reports a credential failure; this sample covers assignment, not every operation in the workflow.")
					check.Message = message.String()
					break
				}
			}
		}
	}
}
func discoverDoctorTokens(file string, cache *parser.ImportCache) ([]string, []string, error) {
	content, err := readWorkflowFileContent(file)
	if err != nil {
		return nil, nil, err
	}
	document, err := aw.Parse(content)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to parse %s for token validation: %w", file, err)
	}
	on, exists := document.Frontmatter["on"]
	if !exists || parser.IsImportSafeSharedWorkflowOn(on) {
		return nil, nil, nil
	}
	names := map[string]struct{}{}
	environments := map[string]struct{}{}
	if err := scanDoctorTokenValues(document.Frontmatter, "workflow", names, environments); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", file, err)
	}
	if err := scanDoctorImportedTokens(document, file, cache, names, environments); err != nil {
		return nil, nil, err
	}
	result := make([]string, 0, len(names))
	for name := range names {
		result = append(result, name)
	}
	slices.Sort(result)
	envs := make([]string, 0, len(environments))
	for name := range environments {
		envs = append(envs, name)
	}
	slices.Sort(envs)
	return result, envs, nil
}

func scanDoctorImportedTokens(document *aw.Document, file string, cache *parser.ImportCache, names, environments map[string]struct{}) error {
	imports, err := parser.ProcessImportsFromFrontmatterWithSource(document.Frontmatter, filepath.Dir(file), cache, file, strings.Join(document.Location.FrontmatterLines, "\n"))
	if err != nil {
		return fmt.Errorf("failed to resolve imports for token validation in %s: %w", file, err)
	}
	included, err := parser.ExpandIncludesForSafeOutputs(document.Markdown, filepath.Dir(file), cache)
	if err != nil {
		return fmt.Errorf("failed to expand token validation includes in %s: %w", file, err)
	}
	// Scan compiler-resolved configuration rather than unrelated shared files.
	sections := append([]string{
		imports.MergedTools, imports.MergedMCPServers, imports.MergedEnv,
		imports.MergedJobs, imports.MergedSteps, imports.MergedPreSteps,
		imports.MergedPreAgentSteps, imports.MergedPostSteps,
		imports.CopilotSetupSteps, imports.MergedCheckout, imports.MergedServices,
		imports.MergedObservability, imports.MergedSecretMasking,
	}, imports.MergedEngines...)
	for _, section := range sections {
		if err := scanDoctorTokenSection(section, names, environments); err != nil {
			return fmt.Errorf("failed to inspect resolved token configuration in %s: %w", file, err)
		}
	}
	for _, section := range append(imports.MergedSafeOutputs, included...) {
		if err := scanDoctorTokenSectionWithScope(section, "safe-outputs", names, environments); err != nil {
			return fmt.Errorf("failed to inspect resolved safe-output token configuration in %s: %w", file, err)
		}
	}
	if imports.MergedActivationGitHubToken != "" {
		if err := scanDoctorTokenValues(imports.MergedActivationGitHubToken, "github-token", names, environments); err != nil {
			return err
		}
	}
	return nil
}

func scanDoctorTokenSection(section string, names, environments map[string]struct{}) error {
	return scanDoctorTokenSectionWithScope(section, "", names, environments)
}

func scanDoctorTokenSectionWithScope(section, scope string, names, environments map[string]struct{}) error {
	if strings.TrimSpace(section) == "" {
		return nil
	}
	var value any
	if err := yaml.Unmarshal([]byte(section), &value); err == nil {
		return scanDoctorTokenValues(value, scope, names, environments)
	}
	// Import processors also return streams of JSON configuration objects.
	decoder := json.NewDecoder(strings.NewReader(section))
	for {
		var value any
		err := decoder.Decode(&value)
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("invalid resolved token configuration")
		}
		if err := scanDoctorTokenValues(value, scope, names, environments); err != nil {
			return err
		}
	}
}

func scanDoctorTokenValues(value any, field string, names, environments map[string]struct{}) error {
	switch node := value.(type) {
	case map[string]any:
		if assignment, found := node["assign-to-agent"]; found && assignment != nil && assignment != false {
			config, mapping := assignment.(map[string]any)
			if !mapping || (config["github-token"] == nil && config["github-app"] == nil) {
				names["GH_AW_AGENT_TOKEN"] = struct{}{}
				names["GH_AW_GITHUB_TOKEN"] = struct{}{}
			}
		}
		for key, child := range node {
			if key == "environment" && (field == "workflow" || field == "safe-outputs" || node["runs-on"] != nil || node["uses"] != nil) {
				if err := scanDoctorTokenEnvironment(child, environments); err != nil {
					return err
				}
			}
			if err := scanDoctorTokenValues(child, key, names, environments); err != nil {
				return err
			}
		}
	case []any:
		for _, child := range node {
			if err := scanDoctorTokenValues(child, field, names, environments); err != nil {
				return err
			}
		}
	case string:
		return scanDoctorTokenExpression(node, field, names)
	}
	return nil
}

func scanDoctorTokenEnvironment(value any, environments map[string]struct{}) error {
	if mapping, ok := value.(map[string]any); ok {
		value = mapping["name"]
	}
	if name, ok := value.(string); ok && strings.TrimSpace(name) != "" {
		environments[name] = struct{}{}
		return nil
	}
	return errors.New("token validation environment must have a non-empty name")
}

func scanDoctorTokenExpression(value, field string, names map[string]struct{}) error {
	spans, err := actionslanguageservice.ExtractExpressions(value)
	if err != nil {
		return fmt.Errorf("invalid Actions expression in token validation field %s", field)
	}
	for _, span := range spans {
		// Use the AW expression AST boundary; never match reference-like text
		// in quoted expression literals or YAML comments.
		if _, _, err := aw.ParseActionsExpression(span.Content); err != nil {
			return fmt.Errorf("invalid Actions expression in token validation field %s", field)
		}
		expression, err := actionslanguageservice.ParseSyntax(span.Content)
		if err != nil {
			return fmt.Errorf("invalid Actions expression in token validation field %s", field)
		}
		for _, ref := range actionslanguageservice.References(expression) {
			if len(ref) != 2 {
				continue
			}
			reference := [2]string(ref)
			if !strings.EqualFold(reference[0], "secrets") {
				continue
			}
			name := reference[1]
			if !strings.EqualFold(name, "GITHUB_TOKEN") && name != "*" && (doctorTokenField(field) || doctorTokenField(name)) {
				names[strings.ToUpper(name)] = struct{}{}
			}
		}
	}
	return nil
}

func doctorTokenField(field string) bool {
	field = strings.ToUpper(field)
	return field == "TOKEN" || field == "GITHUB-TOKEN" || field == "HEAD-GITHUB-TOKEN" || strings.HasSuffix(field, "_TOKEN") || strings.HasSuffix(field, "_PAT")
}

func classifyDoctorTokenLog(content string, evidence DoctorAssignmentRunEvidence) DoctorAssignmentRunEvidence {
	for line := range strings.SplitSeq(stripGHALogTimestamps(stringutil.StripANSI(content)), "\n") {
		line = strings.TrimSpace(line)
		diagnostic := strings.HasPrefix(line, "##[error]") || strings.HasPrefix(line, "##[warning]")
		line = strings.TrimPrefix(strings.TrimPrefix(line, "##[warning]"), "##[error]")
		if (diagnostic && strings.HasPrefix(line, "Failed ")) || doctorTokenFailureDiagnostic(line) {
			if doctorAssignmentCredentialFailure(line) || strings.Contains(strings.ToLower(line), "http 403") {
				evidence.Outcome = "unknown"
				evidence.Message = "Workflow logs contain a credential-like diagnostic, but workflow code can emit log text; token validity is unknown."
				return evidence
			}
		}
	}
	evidence.Outcome, evidence.Message = "no_evidence", "No recognized credential failure found; this does not validate tokens or prove an operation succeeded."
	return evidence
}

func doctorTokenFailureDiagnostic(line string) bool {
	for _, prefix := range []string{
		"✗ Message",
		"✗ Retry of message",
		"✗ Failed to submit PR review",
		"✗ Exception while submitting PR review",
		"Agent assignment failed for",
		"Agent assignment credential validation failed:",
	} {
		if strings.HasPrefix(line, prefix) {
			return true
		}
	}
	return false
}

func doctorTokenValidationError(checks []DoctorTokenValidation) error {
	for _, check := range checks {
		if check.Status == "credential_error" || check.Status == "unknown" {
			return errors.New("workflow token validation needs attention; see credential evidence and user replacement guidance")
		}
	}
	return nil
}

func printDoctorTokenValidations(checks []DoctorTokenValidation) error {
	for _, check := range checks {
		fmt.Fprintln(os.Stderr, console.FormatWarningMessage(fmt.Sprintf("%s: token validation list [%s]; available [%s], absent [%s]. %s Secret settings: %s",
			check.Workflow, strings.Join(check.Secrets, ", "), strings.Join(check.Available, ", "), strings.Join(check.Missing, ", "), check.Message, check.SecretsURL)))
		printDoctorRunHistory(check.RecentRuns, "token")
	}
	return doctorTokenValidationError(checks)
}
