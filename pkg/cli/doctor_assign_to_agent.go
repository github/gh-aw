package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/workflow"
	"github.com/github/gh-aw/pkg/workflow/actionslanguageservice"
)

// DoctorAgentAssignmentCheck reports configuration presence, not credential validity.
type DoctorAgentAssignmentCheck struct {
	Workflow        string                      `json:"workflow"`
	Status          string                      `json:"status"`
	Secret          string                      `json:"secret,omitempty"`
	SuggestedSecret string                      `json:"suggested_secret"`
	Message         string                      `json:"message"`
	PATURL          string                      `json:"pat_url"`
	SecretsURL      string                      `json:"secrets_url"`
	RecentRuns      *DoctorAssignmentRunHistory `json:"recent_runs,omitempty"`
}

type doctorAssignmentWorkflow struct {
	path        string
	config      *workflow.SafeOutputsConfig
	environment string
}

func checkDoctorAgentAssignments(ctx context.Context, repo, dir string) ([]DoctorAgentAssignmentCheck, error) {
	root, err := gitutil.FindGitRootFrom(dir)
	if errors.Is(err, gitutil.ErrNotGitRepository) && repo == "" {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("failed to locate checkout for assignment diagnostics: %w", err)
	}
	workflows, err := discoverDoctorAssignmentWorkflows(root)
	if err != nil || len(workflows) == 0 {
		return nil, err
	}

	remote, _, err := resolveRemoteURL(root)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve repository for assignment diagnostics: %w", err)
	}
	host := getGitHubHost()
	if !isAnyGitHubHostEnvVarSet() {
		host = "https://" + extractHostFromRemoteURL(remote)
	}
	if repo == "" {
		repo = doctorAssignmentRepositoryFromRemote(remote)
	}
	if !isValidOwnerRepoSlug(repo) {
		return nil, errors.New("cannot resolve owner/repo for assignment diagnostics")
	}

	checks := diagnoseDoctorAssignments(ctx, repo, host, workflows, listDoctorAssignmentSecrets)
	collectDoctorAssignmentRunEvidence(ctx, repo, host, checks)
	return checks, nil
}

func doctorAssignmentRepositoryFromRemote(remote string) string {
	var repo string
	if strings.Contains(remote, "://") {
		parsed, err := url.Parse(remote)
		if err != nil || parsed.Host == "" {
			return ""
		}
		repo = parsed.Path
	} else {
		host, remotePath, found := strings.Cut(remote, ":")
		if !found || host == "" || strings.Contains(host, "/") {
			return ""
		}
		repo = remotePath
	}
	return strings.TrimSuffix(strings.TrimPrefix(repo, "/"), ".git")
}

func discoverDoctorAssignmentWorkflows(root string) ([]doctorAssignmentWorkflow, error) {
	dir := getWorkflowsDir()
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(root, dir)
	}
	if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
		return nil, nil
	} else if err != nil {
		return nil, fmt.Errorf("failed to inspect workflow directory: %w", err)
	}
	files, err := getMarkdownWorkflowFiles(dir)
	if err != nil {
		return nil, err
	}
	var result []doctorAssignmentWorkflow
	compiler := &workflow.Compiler{}
	cache := parser.NewImportCache(root)
	for _, file := range files {
		config, environment, err := readDoctorAssignmentConfig(file, cache, compiler)
		if err != nil {
			return nil, err
		}
		if config != nil && config.AssignToAgent != nil {
			rel, err := filepath.Rel(root, file)
			if err != nil {
				return nil, err
			}
			result = append(result, doctorAssignmentWorkflow{path: filepath.ToSlash(rel), config: config, environment: environment})
		}
	}
	return result, nil
}

func readDoctorAssignmentConfig(file string, cache *parser.ImportCache, compiler *workflow.Compiler) (*workflow.SafeOutputsConfig, string, error) {
	content, err := readWorkflowFileContent(file)
	if err != nil {
		return nil, "", err
	}
	frontmatter, err := parser.ExtractFrontmatterFromContent(content)
	if err != nil {
		return nil, "", fmt.Errorf("failed to parse %s for assignment diagnostics: %w", file, err)
	}
	// Shared fragments are checked through their importing workflows.
	if on, ok := frontmatter.Frontmatter["on"]; !ok || parser.IsImportSafeSharedWorkflowOn(on) {
		return nil, "", nil
	}
	imports, err := parser.ProcessImportsFromFrontmatterWithSource(frontmatter.Frontmatter, filepath.Dir(file), cache, file, strings.Join(frontmatter.FrontmatterLines, "\n"))
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve imports for %s: %w", file, err)
	}
	included, err := parser.ExpandIncludesForSafeOutputs(frontmatter.Markdown, filepath.Dir(file), cache)
	if err != nil {
		return nil, "", fmt.Errorf("failed to expand includes for %s: %w", file, err)
	}
	var raw map[string]any
	if value, exists := frontmatter.Frontmatter["safe-outputs"]; exists && value != nil {
		var ok bool
		raw, ok = value.(map[string]any)
		if !ok {
			return nil, "", fmt.Errorf("safe-outputs in %s must be a mapping", file)
		}
	}
	var config *workflow.SafeOutputsConfig
	if raw != nil {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read safe outputs in %s: %w", file, err)
		}
		config, err = compiler.MergeSafeOutputs(nil, []string{string(encoded)}, nil)
		if err != nil {
			return nil, "", fmt.Errorf("failed to read safe outputs in %s: %w", file, err)
		}
	}
	config, err = compiler.MergeSafeOutputs(config, append(imports.MergedSafeOutputs, included...), raw)
	if err != nil {
		return nil, "", fmt.Errorf("failed to merge safe outputs in %s: %w", file, err)
	}
	if config == nil || config.AssignToAgent == nil {
		return config, "", nil
	}
	environment, err := resolveDoctorAssignmentEnvironment(frontmatter.Frontmatter, raw)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve environment in %s: %w", file, err)
	}
	return config, environment, nil
}

func resolveDoctorAssignmentEnvironment(frontmatter, rawSafeOutputs map[string]any) (string, error) {
	environment, exists := frontmatter["environment"]
	field := "environment"
	// The compiler serializes any explicit override, including empty values.
	// Imported job-level environment settings are not merged by MergeSafeOutputs.
	if override, ok := rawSafeOutputs["environment"]; ok {
		environment, exists, field = override, true, "safe-outputs.environment"
	}
	if !exists {
		return "", nil
	}
	if object, ok := environment.(map[string]any); ok {
		environment = object["name"]
	}
	name, ok := environment.(string)
	if !ok || strings.TrimSpace(name) == "" {
		return "", fmt.Errorf("%s must be a non-empty environment name or a mapping with a non-empty string name", field)
	}
	return name, nil
}

// Return known names even if inherited organization secrets cannot be inspected.
func listDoctorAssignmentSecrets(ctx context.Context, repo, host string) (map[string]struct{}, error) {
	names, err := listDoctorAssignmentSecretScope(ctx, host, path.Join("repos", repo, "actions", "secrets"), "secrets")
	if err != nil {
		return names, err
	}
	owner, err := workflow.RunGHContext(ctx, "Checking repository owner type...", "api", "--hostname", strings.TrimPrefix(host, "https://"),
		path.Join("repos", repo), "--jq", ".owner.type")
	if err != nil {
		return names, fmt.Errorf("cannot determine repository owner type for organization secret metadata: %w", err)
	}
	if strings.TrimSpace(string(owner)) == "User" {
		return names, nil
	}
	if strings.TrimSpace(string(owner)) != "Organization" {
		return names, errors.New("cannot determine repository owner type for organization secret metadata")
	}
	inherited, err := listDoctorAssignmentSecretScope(ctx, host, path.Join("repos", repo, "actions", "organization-secrets"), "organization-secrets")
	for name := range inherited {
		names[name] = struct{}{}
	}
	return names, err
}

func listDoctorAssignmentEnvironmentSecrets(ctx context.Context, repo, host, environment string) (map[string]struct{}, error) {
	if strings.Contains(environment, "${{") {
		return nil, errors.New("the assignment environment is dynamic; environment secret availability cannot be determined")
	}
	endpoint := fmt.Sprintf("%s/%s/secrets", path.Join("repos", repo, "environments"), url.PathEscape(environment))
	return listDoctorAssignmentSecretScope(ctx, host, endpoint, "environment secrets")
}

func listDoctorAssignmentSecretScope(ctx context.Context, host, endpoint, scope string) (map[string]struct{}, error) {
	names := make(map[string]struct{})
	output, err := workflow.RunGHContext(ctx, "Checking assignment secrets...", "api", "--hostname", strings.TrimPrefix(host, "https://"),
		"--paginate", "--slurp", endpoint+"?per_page=100")
	if err != nil {
		return names, fmt.Errorf("cannot inspect Actions %s; verify access to secret metadata: %w", scope, err)
	}
	var pages []struct {
		Secrets []struct {
			Name string `json:"name"`
		} `json:"secrets"`
	}
	if err := json.Unmarshal(output, &pages); err != nil {
		return names, fmt.Errorf("failed to parse Actions secret metadata: %w", err)
	}
	if len(pages) == 0 {
		return names, errors.New("no pages in Actions secret metadata")
	}
	for _, page := range pages {
		if page.Secrets == nil {
			return names, errors.New("missing the secrets array in Actions secret metadata")
		}
		for _, secret := range page.Secrets {
			names[strings.ToUpper(secret.Name)] = struct{}{}
		}
	}
	return names, nil
}

func diagnoseDoctorAssignments(ctx context.Context, repo, host string, workflows []doctorAssignmentWorkflow, listSecrets func(context.Context, string, string) (map[string]struct{}, error)) []DoctorAgentAssignmentCheck {
	return diagnoseDoctorAssignmentsWithEnvironment(ctx, repo, host, workflows, listSecrets, listDoctorAssignmentEnvironmentSecrets)
}

func diagnoseDoctorAssignmentsWithEnvironment(ctx context.Context, repo, host string, workflows []doctorAssignmentWorkflow, listSecrets func(context.Context, string, string) (map[string]struct{}, error), listEnvironmentSecrets func(context.Context, string, string, string) (map[string]struct{}, error)) []DoctorAgentAssignmentCheck {
	secrets, lookupErr := listSecrets(ctx, repo, host)
	type environmentLookup struct {
		names map[string]struct{}
		err   error
	}
	environments := make(map[string]environmentLookup)
	checks := make([]DoctorAgentAssignmentCheck, 0, len(workflows))
	for _, item := range workflows {
		check := DoctorAgentAssignmentCheck{
			Workflow:        item.path,
			SecretsURL:      fmt.Sprintf("%s/%s/settings/secrets/actions/new", host, repo),
			SuggestedSecret: "GH_AW_AGENT_TOKEN",
		}
		config := item.config.AssignToAgent
		if names, ok := doctorAssignmentSecretNames(strings.TrimSpace(config.GitHubToken)); ok {
			for _, name := range names {
				if name != "GITHUB_TOKEN" {
					check.SuggestedSecret = name
					break
				}
			}
		}
		check.PATURL = doctorAssignmentPATURL(host, repo, check.SuggestedSecret)
		if config.GitHubApp != nil {
			check.Status = "missing"
			check.Message = "assign-to-agent uses a GitHub App installation token, which the Copilot assignment API rejects. Remove assign-to-agent.github-app and configure a PAT."
		} else {
			token := config.GitHubToken
			if token == "" {
				token = "${{ secrets.GH_AW_AGENT_TOKEN || secrets.GH_AW_GITHUB_TOKEN || secrets.GITHUB_TOKEN }}"
			}
			var environment environmentLookup
			if item.environment != "" {
				var cached bool
				environment, cached = environments[item.environment]
				if !cached {
					environment.names, environment.err = listEnvironmentSecrets(ctx, repo, host, item.environment)
					environments[item.environment] = environment
				}
			}
			check.Status, check.Secret, check.Message = doctorAssignmentCredentialWithEnvironment(token, secrets, lookupErr, environment.names, environment.err)
		}
		if check.Status != "configured" {
			check.Message = fmt.Sprintf("%s %s", check.Message, doctorAssignmentRemedy(repo, check.SuggestedSecret))
		}
		checks = append(checks, check)
	}
	return checks
}

func doctorAssignmentCredentialWithEnvironment(token string, secrets map[string]struct{}, lookupErr error, environmentSecrets map[string]struct{}, environmentErr error) (string, string, string) {
	names, ok := doctorAssignmentSecretNames(strings.TrimSpace(token))
	if !ok {
		return "unknown", "", "The configured assignment credential is not a static secret reference or secret fallback chain; doctor cannot verify it."
	}
	for _, name := range names {
		if name == "GITHUB_TOKEN" {
			return "missing", "", "assign-to-agent falls back to GITHUB_TOKEN, which lacks Copilot assignment permissions."
		}
		// An inaccessible environment can override even a known repository secret.
		if environmentErr != nil {
			return "unknown", "", "Cannot determine assignment secret availability: " + environmentErr.Error()
		}
		if setutil.Contains(environmentSecrets, name) {
			return "configured", name, "Assignment secret is available in the workflow environment. Its value, token type, expiry, repository access, permissions, organization approval/SSO, and Copilot availability cannot be verified from secret metadata."
		}
		if setutil.Contains(secrets, name) {
			return "configured", name, "Assignment secret is available. Its value, token type, expiry, repository access, permissions, organization approval/SSO, and Copilot availability cannot be verified from secret metadata."
		}
		if lookupErr != nil {
			return "unknown", "", "Cannot determine assignment secret availability: " + lookupErr.Error()
		}
	}
	if lookupErr != nil {
		return "unknown", "", "Cannot determine assignment secret availability: " + lookupErr.Error()
	}
	return "missing", "", "No configured PAT secret is available to assign-to-agent."
}

func doctorAssignmentSecretNames(token string) ([]string, bool) {
	if !strings.HasPrefix(token, "${{") || !strings.HasSuffix(token, "}}") {
		return nil, false
	}
	expression, err := actionslanguageservice.ParseSyntax(strings.TrimSpace(token[3 : len(token)-2]))
	if err != nil || expression == nil {
		return nil, false
	}
	return doctorAssignmentSecretReferences(expression)
}

func doctorAssignmentSecretReferences(expression *actionslanguageservice.Expression) ([]string, bool) {
	if expression == nil {
		return nil, false
	}
	if expression.Kind == "group" {
		return doctorAssignmentSecretReferences(expression.Child(0))
	}
	if expression.Kind == "logical" && expression.Token.Text == "||" {
		var names []string
		known := true
		for _, child := range expression.Children {
			childNames, childKnown := doctorAssignmentSecretReferences(child)
			names = append(names, childNames...)
			known = known && childKnown
		}
		return names, known
	}
	path, ok := actionslanguageservice.Reference(expression)
	if !ok || len(path) != 2 {
		return nil, false
	}
	reference := [2]string(path)
	if strings.EqualFold(reference[0], "github") && strings.EqualFold(reference[1], "token") {
		return []string{"GITHUB_TOKEN"}, true
	}
	if !strings.EqualFold(reference[0], "secrets") || reference[1] == "*" {
		return nil, false
	}
	return []string{strings.ToUpper(reference[1])}, true
}

func doctorAssignmentPATURL(host, repo, secret string) string {
	owner, _, _ := strings.Cut(repo, "/")
	query := url.Values{
		"name":          {secret},
		"description":   {"GitHub Agentic Workflows - Agent assignment for " + repo},
		"target_name":   {owner},
		"actions":       {"write"},
		"contents":      {"write"},
		"issues":        {"write"},
		"pull_requests": {"write"},
	}
	return fmt.Sprintf("%s/settings/personal-access-tokens/new?%s", host, query.Encode())
}

func doctorAssignmentRemedy(repo, secret string) string {
	return fmt.Sprintf("Create a fine-grained PAT with Actions, Contents, Issues, and Pull requests: read and write (Metadata: read), select the resource owner and all assignment/PR target repositories, and obtain any required organization approval/SSO. Store it as the Actions secret %s in %s and reference that secret in assign-to-agent.github-token. A classic PAT needs the repo scope. GitHub App installation tokens and GITHUB_TOKEN cannot assign Copilot.", secret, repo)
}

func doctorAgentAssignmentError(checks []DoctorAgentAssignmentCheck) error {
	for _, check := range checks {
		if check.Status != "configured" || doctorAssignmentRunFailure(check.RecentRuns) {
			return errors.New("assign-to-agent diagnostics need attention; see the reported run evidence and credential setup links")
		}
	}
	return nil
}

func printDoctorAgentAssignments(checks []DoctorAgentAssignmentCheck) error {
	for _, check := range checks {
		message := check.Workflow + ": " + check.Message
		if check.Status == "configured" && !doctorAssignmentRunFailure(check.RecentRuns) {
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage(message))
		} else {
			fmt.Fprintln(os.Stderr, console.FormatWarningMessage(message))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Create PAT: "+check.PATURL))
			fmt.Fprintln(os.Stderr, console.FormatInfoMessage("Add repository secret: "+check.SecretsURL))
		}
		printDoctorAssignmentRunHistory(check.RecentRuns)
	}
	return doctorAgentAssignmentError(checks)
}
