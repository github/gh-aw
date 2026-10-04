package cli

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"net"
	"os"
	"os/exec"
	"runtime"
	"slices"
	"sort"
	"strings"

	"charm.land/huh/v2"
	"github.com/github/gh-aw/pkg/console"
	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/repoutil"
	"github.com/github/gh-aw/pkg/tty"
	"github.com/github/gh-aw/pkg/workflow"
)

var bootstrapProfileHelpersLog = logger.New("cli:bootstrap_profile_helpers")

func runBootstrapRequireOwnerType(ctx context.Context, repo string, action repositoryPackageBootstrapAction) error {
	owner, _, err := repoutil.SplitRepoSlug(repo)
	if err != nil {
		return err
	}
	ownerType, err := bootstrapCheckOwnerType(ctx, owner)
	if err != nil {
		return err
	}
	normalized := normalizeSetupOwnerType(ownerType)
	if action.Value != "" && action.Value != "any" && normalized != action.Value {
		return fmt.Errorf("owner %s is %s, but bootstrap profile requires %s. Example: set config[].value to %s or use a repository owned by a matching account type", owner, normalized, action.Value, normalized)
	}
	return nil
}

func parseBootstrapBool(raw string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "1", "true", "yes", "on":
		return true, nil
	case "0", "false", "no", "off":
		return false, nil
	default:
		return false, errors.New("expected one of: 1, true, yes, on, 0, false, no, off. Example: GH_AW_BOOTSTRAP_NO_OPEN_BROWSER=true")
	}
}

func parseBootstrapNames(output []byte) []string {
	trimmed := strings.TrimSpace(string(output))
	if trimmed == "" {
		return nil
	}
	lines := strings.Split(trimmed, "\n")
	result := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			result = append(result, line)
		}
	}
	sort.Strings(result)
	return result
}

func resolveBootstrapTextValue(envName, title, description, defaultValue string, allowed []string, optional bool) (string, bool, error) {
	bootstrapProfileHelpersLog.Printf("Resolving text value: env=%s, optional=%v, hasDefault=%v", envName, optional, defaultValue != "")
	if envValue := strings.TrimSpace(lookupEnv(envName)); envValue != "" {
		bootstrapProfileHelpersLog.Printf("Resolved %s from environment variable", envName)
		if err := validateBootstrapEnumValue(envValue, allowed, optional); err != nil {
			return "", false, err
		}
		return envValue, true, nil
	}
	if !tty.IsStderrTerminal() {
		bootstrapProfileHelpersLog.Printf("Resolving %s non-interactively (stderr is not a terminal)", envName)
		if defaultValue != "" {
			if err := validateBootstrapEnumValue(defaultValue, allowed, optional); err != nil {
				return "", false, err
			}
			return defaultValue, true, nil
		}
		if optional {
			return "", false, nil
		}
		return "", false, fmt.Errorf("%s is required; set environment variable %s or rerun interactively. Example: export %s='example-value'", title, envName, envName)
	}

	var value string
	input := huh.NewInput().Title(title).Description(description).Value(&value)
	if defaultValue != "" {
		input = input.Placeholder(defaultValue)
	}
	input = input.Validate(func(v string) error {
		trimmed := strings.TrimSpace(v)
		if trimmed == "" && defaultValue != "" {
			trimmed = defaultValue
		}
		if trimmed == "" && optional {
			return nil
		}
		if trimmed == "" {
			return errors.New("value cannot be empty. Example: enter a non-empty value such as example-value")
		}
		return validateBootstrapEnumValue(trimmed, allowed, optional)
	})
	if err := console.NewInputForm(input).Run(); err != nil {
		return "", false, err
	}
	value = strings.TrimSpace(value)
	if value == "" {
		value = defaultValue
	}
	if value == "" && optional {
		return "", false, nil
	}
	return value, true, nil
}

func resolveBootstrapSecretValue(envName, title, description string, optional bool) (string, bool, error) {
	if envValue := strings.TrimRight(lookupEnv(envName), "\r\n"); envValue != "" {
		return envValue, true, nil
	}
	if !tty.IsStderrTerminal() {
		if optional {
			return "", false, nil
		}
		return "", false, fmt.Errorf("%s is required; set environment variable %s or rerun interactively. Example: export %s='example-secret'", title, envName, envName)
	}
	value, err := console.PromptSecretInput(title, description)
	if err != nil {
		return "", false, err
	}
	return normalizeBootstrapPromptSecretValue(value, optional)
}

func normalizeBootstrapPromptSecretValue(value string, optional bool) (string, bool, error) {
	value = strings.TrimRight(value, "\r\n")
	if value == "" {
		if optional {
			return "", false, nil
		}
		return "", false, errors.New("secret value cannot be empty. Example: enter a non-empty secret such as github_pat_example")
	}
	return value, true, nil
}

func validateBootstrapEnumValue(value string, allowed []string, optional bool) error {
	if value == "" && optional {
		return nil
	}
	if len(allowed) == 0 {
		return nil
	}
	if slices.Contains(allowed, value) {
		return nil
	}
	return fmt.Errorf("value must be one of: %s. Example: %s", strings.Join(allowed, ", "), allowed[0])
}

func profileSourcesUseActionsTokenCopilotAuth(ctx context.Context, sources []string) (bool, error) {
	if len(sources) == 0 {
		return false, nil
	}
	resolved, err := ResolveWorkflows(ctx, sources, false)
	if err != nil {
		return false, err
	}
	hasCopilot := false
	for _, candidate := range resolved.Workflows {
		if candidate == nil || candidate.IsActionWorkflow || candidate.IsPackageSkillFile || candidate.IsPackageAgentFile || candidate.IsPackageResourceFile {
			continue
		}
		engine := strings.TrimSpace(candidate.Engine)
		if engine != "" && engine != "copilot" {
			continue
		}
		hasCopilot = true
		if !workflowGrantsCopilotRequestsWrite(candidate.Content) {
			return false, nil
		}
	}
	return hasCopilot, nil
}

func deriveBootstrapAppName(repo, explicitName string) string {
	candidate := strings.TrimSpace(explicitName)
	if candidate == "" {
		candidate = repo
	}
	candidate = strings.ReplaceAll(candidate, "/", "-")
	clean := strings.Builder{}
	previousDash := false
	for _, ch := range candidate {
		allowed := ch >= 'a' && ch <= 'z' || ch >= 'A' && ch <= 'Z' || ch >= '0' && ch <= '9'
		if allowed {
			clean.WriteRune(ch)
			previousDash = false
			continue
		}
		if !previousDash {
			clean.WriteRune('-')
			previousDash = true
		}
	}
	result := strings.Trim(clean.String(), "-")
	if len(result) <= 34 {
		return result
	}
	suffix := strings.TrimLeft(result[len(result)-15:], "-")
	prefixLength := max(1, 34-len(suffix)-1)
	prefix := strings.TrimRight(result[:prefixLength], "-")
	return strings.Trim(prefix+"-"+suffix, "-")
}

func buildBootstrapGitHubAppManifest(action repositoryPackageBootstrapAction, appName, homepageURL, redirectURL, description string) map[string]any {
	permissions := action.Permissions
	if len(permissions) == 0 {
		permissions = map[string]string{
			"metadata": "read",
		}
	}
	events := action.Events
	if events == nil {
		events = []string{}
	}
	return map[string]any{
		"name":                     appName,
		"url":                      homepageURL,
		"hook_attributes":          map[string]any{"url": homepageURL, "active": false},
		"redirect_url":             redirectURL,
		"public":                   false,
		"request_oauth_on_install": false,
		"description":              description,
		"default_permissions":      permissions,
		"default_events":           events,
	}
}

func buildBootstrapGitHubAppRegistrationURL(owner, ownerType, state string) string {
	if strings.EqualFold(ownerType, "Organization") {
		return fmt.Sprintf("https://github.com/organizations/%s/settings/apps/new?state=%s", owner, state)
	}
	return "https://github.com/settings/apps/new?state=" + state
}

//go:embed bootstrap_registration_page.html
var bootstrapRegistrationPageFS embed.FS

// The embedded template is static; request-derived values are only execution data.
var bootstrapRegistrationPage = template.Must(template.ParseFS(bootstrapRegistrationPageFS, "bootstrap_registration_page.html"))

func renderBootstrapGitHubAppRegistrationPage(registrationURL string, manifest map[string]any) (string, error) {
	encoded, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	data := struct {
		Action   string
		Manifest string
	}{
		Action:   registrationURL,
		Manifest: string(encoded),
	}
	if err = bootstrapRegistrationPage.Execute(&buf, data); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func printBootstrapGitHubAppManifestReview(owner string, manifest map[string]any) {
	permissions := map[string]string{}
	switch raw := manifest["default_permissions"].(type) {
	case map[string]string:
		permissions = raw
	case map[string]any:
		for name, value := range raw {
			text, ok := value.(string)
			if ok {
				permissions[name] = text
				continue
			}
			permissions[name] = "<non-string-value>"
		}
	}
	permissionNames := make([]string, 0, len(permissions))
	for name := range permissions {
		permissionNames = append(permissionNames, name)
	}
	sort.Strings(permissionNames)
	getManifestStringOrDefault := func(key string) string {
		value, ok := manifest[key].(string)
		if !ok {
			return "<unavailable>"
		}
		if strings.TrimSpace(value) == "" {
			return "<unavailable>"
		}
		return value
	}
	lines := []string{
		"GitHub App manifest for " + owner + ":",
		"- name: " + getManifestStringOrDefault("name"),
		"- homepage: " + getManifestStringOrDefault("url"),
		"- redirect URL: " + getManifestStringOrDefault("redirect_url"),
		"- description: " + getManifestStringOrDefault("description"),
		"- permissions:",
	}
	for _, name := range permissionNames {
		lines = append(lines, fmt.Sprintf("  - %s: %s", name, permissions[name]))
	}
	lines = append(lines, "")
	for _, line := range lines {
		fmt.Fprintln(os.Stderr, console.FormatInfoMessage(line))
	}
}

func buildBootstrapGitHubAppInstallURL(slug string) string {
	if strings.TrimSpace(slug) == "" {
		return ""
	}
	return "https://github.com/apps/" + slug + "/installations/new"
}

func bootstrapRandomHex(size int) (string, error) {
	buf := make([]byte, size)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func openBootstrapBrowser(url string) bool {
	bootstrapProfileHelpersLog.Printf("Opening browser for bootstrap URL: goos=%s", runtime.GOOS)
	commands := [][]string{{"gh", "browse", url}}
	switch runtime.GOOS {
	case "darwin":
		commands = append([][]string{{"open", url}}, commands...)
	case "windows":
		commands = append([][]string{{"cmd", "/c", "start", "", url}}, commands...)
	default:
		commands = append([][]string{{"xdg-open", url}}, commands...)
	}
	for _, args := range commands {
		cmd := exec.Command(args[0], args[1:]...)
		if err := cmd.Start(); err == nil {
			bootstrapProfileHelpersLog.Printf("Launched browser via %q", args[0])
			go func() {
				defer func() {
					if r := recover(); r != nil {
						bootstrapProfileHelpersLog.Printf("Panic waiting for browser process (recovered): %v", r)
					}
				}()
				_ = cmd.Wait()
			}()
			return true
		}
	}
	bootstrapProfileHelpersLog.Print("Failed to launch a browser: no launcher command succeeded")
	return false
}

func netListener() (net.Listener, error) {
	return net.Listen("tcp", "127.0.0.1:0")
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func bootstrapRepositoryVariableEnvName(name string) string {
	return bootstrapInputEnvName("VAR", name)
}

func bootstrapRepositorySecretEnvName(name string) string {
	return bootstrapInputEnvName("SECRET", name)
}

func bootstrapInputEnvName(kind, name string) string {
	suffix := strings.TrimSpace(name)
	if suffix == "" {
		suffix = "VALUE"
	}
	suffix = strings.ToUpper(suffix)
	var builder strings.Builder
	lastUnderscore := false
	for _, ch := range suffix {
		switch {
		case ch >= 'A' && ch <= 'Z', ch >= '0' && ch <= '9':
			builder.WriteRune(ch)
			lastUnderscore = false
		default:
			if !lastUnderscore {
				builder.WriteByte('_')
				lastUnderscore = true
			}
		}
	}
	suffix = strings.Trim(builder.String(), "_")
	if suffix == "" {
		suffix = "VALUE"
	}
	return "GH_AW_BOOTSTRAP_" + kind + "_" + suffix
}

func workflowGrantsCopilotRequestsWrite(content []byte) bool {
	frontmatter, err := parser.ExtractFrontmatterFromContent(string(content))
	if err != nil || frontmatter == nil {
		return false
	}
	permissions, ok := frontmatter.Frontmatter["permissions"].(map[string]any)
	if !ok {
		return false
	}
	level, ok := permissions[string(workflow.PermissionCopilotRequests)].(string)
	return ok && strings.TrimSpace(level) == "write"
}

func isRetryableBootstrapGitHubAppInstallationError(err error) bool {
	if err == nil {
		return false
	}
	message := err.Error()
	return strings.Contains(message, "HTTP 404") ||
		strings.Contains(message, "HTTP 500") ||
		strings.Contains(message, "HTTP 502") ||
		strings.Contains(message, "HTTP 503") ||
		strings.Contains(message, "HTTP 504")
}
