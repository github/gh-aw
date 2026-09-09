package workflow

import (
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"slices"
	"strings"
)

const (
	defaultJiraMCPURL        = "https://mcp.atlassian.com/v1/mcp"
	jiraBasicAuthEnvVar      = "GH_AW_JIRA_BASIC_AUTH"
	jiraEmailEnvVar          = "GH_AW_JIRA_EMAIL"
	jiraTokenEnvVar          = "GH_AW_JIRA_TOKEN"
	jiraServiceAccountAuth   = "service-account"
	jiraAPITokenAuth         = "api-token"
	jiraAllowedToolsWildcard = "*"
)

var jiraSecretExpressionPattern = regexp.MustCompile(`^\$\{\{\s*secrets\.[A-Z_][A-Z0-9_]*\s*\}\}$`)

// jiraApprovedReadOnlyToolsList is the fixed, ordered set of Jira MCP tools
// that may ever be enabled. "*" in tools.jira.allowed is expanded to exactly
// this list; the full, unrestricted MCP tool set is never exposed.
var jiraApprovedReadOnlyToolsList = []string{
	"getIssueLinkTypes",
	"getJiraIssue",
	"getJiraIssueRemoteIssueLinks",
	"getJiraIssueTypeMetaWithFields",
	"getJiraProjectIssueTypesMetadata",
	"getTransitionsForJiraIssue",
	"getVisibleJiraProjects",
	"lookupJiraAccountId",
	"searchJiraIssuesUsingJql",
}

var jiraApprovedReadOnlyTools = func() map[string]struct{} {
	set := make(map[string]struct{}, len(jiraApprovedReadOnlyToolsList))
	for _, tool := range jiraApprovedReadOnlyToolsList {
		set[tool] = struct{}{}
	}
	return set
}()

// expandJiraToolConfig converts the first-class Jira configuration into the
// generic HTTP MCP shape consumed by the existing gateway pipeline.
func expandJiraToolConfig(tools map[string]any) error {
	raw, exists := tools["jira"]
	if !exists {
		return nil
	}
	if enabled, ok := raw.(bool); ok && !enabled {
		delete(tools, "jira")
		return nil
	}

	config, ok := raw.(map[string]any)
	if !ok {
		return errors.New("tools.jira must be an object with an auth configuration")
	}
	if err := validateJiraToolConfig(config); err != nil {
		return err
	}
	auth, ok := config["auth"].(map[string]any)
	if !ok {
		return errors.New("tools.jira.auth is required")
	}
	authType, ok := auth["type"].(string)
	if !ok {
		return fmt.Errorf("tools.jira.auth.type must be %q or %q", jiraServiceAccountAuth, jiraAPITokenAuth)
	}
	token, ok := auth["token"].(string)
	if !ok {
		return errors.New("tools.jira.auth.token is required")
	}

	headers := map[string]any{}
	switch authType {
	case jiraServiceAccountAuth:
		headers["Authorization"] = "Bearer " + token
	case jiraAPITokenAuth:
		headers["Authorization"] = "Basic ${{ env." + jiraBasicAuthEnvVar + " }}"
	}

	url, _ := config["url"].(string)
	if strings.TrimSpace(url) == "" {
		url = defaultJiraMCPURL
	}

	expanded := map[string]any{
		"type":    "http",
		"url":     url,
		"headers": headers,
		"auth":    auth,
	}
	if allowed, exists := config["allowed"]; exists {
		expanded["allowed"] = expandJiraAllowedTools(allowed)
	}
	tools["jira"] = expanded
	return nil
}

func validateJiraToolConfig(config map[string]any) error {
	for field := range config {
		switch field {
		case "auth", "url", "allowed":
		default:
			return fmt.Errorf("tools.jira.%s is not supported; valid fields are: allowed, auth, url", field)
		}
	}

	if rawURL, exists := config["url"]; exists {
		endpoint, ok := rawURL.(string)
		if !ok {
			return errors.New("tools.jira.url must be an HTTPS URL")
		}
		parsed, err := url.Parse(endpoint)
		if err != nil || parsed.Scheme != "https" || parsed.Host == "" || parsed.User != nil {
			return errors.New("tools.jira.url must be an HTTPS URL without embedded credentials")
		}
	}

	auth, ok := config["auth"].(map[string]any)
	if !ok {
		return errors.New("tools.jira.auth is required")
	}
	if err := validateJiraAuthConfig(auth); err != nil {
		return err
	}

	allowed, exists := config["allowed"]
	if !exists {
		return errors.New("tools.jira.allowed is required and must contain approved read-only tools")
	}
	return validateJiraAllowedTools(allowed)
}

func validateJiraAllowedTools(value any) error {
	allowed, err := parseJiraAllowedTools(value)
	if err != nil {
		return err
	}
	if len(allowed) > 1 && slices.Contains(allowed, jiraAllowedToolsWildcard) {
		return fmt.Errorf("tools.jira.allowed must contain only %q when the wildcard is used", jiraAllowedToolsWildcard)
	}
	seen := make(map[string]struct{}, len(allowed))
	for _, tool := range allowed {
		if strings.TrimSpace(tool) == "" {
			return errors.New("tools.jira.allowed must contain only non-empty tool names")
		}
		if tool != jiraAllowedToolsWildcard {
			if _, approved := jiraApprovedReadOnlyTools[tool]; !approved {
				return fmt.Errorf("tools.jira.allowed tool %q is not an approved read-only Jira tool", tool)
			}
		}
		if _, duplicate := seen[tool]; duplicate {
			return fmt.Errorf("tools.jira.allowed contains duplicate tool %q", tool)
		}
		seen[tool] = struct{}{}
	}
	return nil
}

func parseJiraAllowedTools(value any) ([]string, error) {
	var allowed []string
	switch values := value.(type) {
	case []string:
		allowed = values
	case []any:
		allowed = make([]string, 0, len(values))
		for _, item := range values {
			tool, ok := item.(string)
			if !ok {
				return nil, errors.New("tools.jira.allowed must contain only non-empty tool names")
			}
			allowed = append(allowed, tool)
		}
	default:
		return nil, errors.New("tools.jira.allowed must be a non-empty array of tool names")
	}
	if len(allowed) == 0 {
		return nil, errors.New("tools.jira.allowed must be a non-empty array of tool names")
	}
	return allowed, nil
}

// expandJiraAllowedTools resolves tools.jira.allowed into the concrete list of
// Jira MCP tools that will be exposed to the agent. The wildcard "*" is never
// forwarded to the MCP configuration verbatim: it always expands to the fixed
// approved read-only tool list so the full, unrestricted MCP tool set can
// never be enabled, regardless of what value is configured.
func expandJiraAllowedTools(value any) any {
	allowed, err := parseJiraAllowedTools(value)
	if err != nil {
		return value
	}
	if slices.Contains(allowed, jiraAllowedToolsWildcard) {
		expanded := make([]string, len(jiraApprovedReadOnlyToolsList))
		copy(expanded, jiraApprovedReadOnlyToolsList)
		return expanded
	}
	return value
}

func validateJiraAuthConfig(auth map[string]any) error {
	authType, _ := auth["type"].(string)
	for field := range auth {
		if field != "type" && field != "token" && (authType != jiraAPITokenAuth || field != "email") {
			return fmt.Errorf("tools.jira.auth.%s is not supported for %s authentication", field, authType)
		}
	}

	token, _ := auth["token"].(string)
	if token == "" {
		return errors.New("tools.jira.auth.token is required")
	}
	if !jiraSecretExpressionPattern.MatchString(token) {
		return errors.New("tools.jira.auth.token must be a direct GitHub Actions secret expression")
	}
	switch authType {
	case jiraServiceAccountAuth:
		return nil
	case jiraAPITokenAuth:
		email, _ := auth["email"].(string)
		if email == "" {
			return errors.New("tools.jira.auth.email is required for api-token authentication")
		}
		if !jiraSecretExpressionPattern.MatchString(email) {
			return errors.New("tools.jira.auth.email must be a direct GitHub Actions secret expression for api-token authentication")
		}
		return nil
	default:
		return fmt.Errorf("tools.jira.auth.type must be %q or %q", jiraServiceAccountAuth, jiraAPITokenAuth)
	}
}

func jiraAuthConfig(tools map[string]any) (map[string]any, bool) {
	config, ok := tools["jira"].(map[string]any)
	if !ok {
		return nil, false
	}
	auth, ok := config["auth"].(map[string]any)
	return auth, ok
}

func jiraAPIAuthStepEnv(tools map[string]any) map[string]string {
	auth, ok := jiraAuthConfig(tools)
	if !ok || auth["type"] != jiraAPITokenAuth {
		return nil
	}
	email, emailOK := auth["email"].(string)
	token, tokenOK := auth["token"].(string)
	if !emailOK || !tokenOK {
		return nil
	}
	return map[string]string{
		jiraEmailEnvVar: email,
		jiraTokenEnvVar: token,
	}
}

func writeJiraAPIAuthPreparation(yaml *strings.Builder, tools map[string]any) {
	if len(jiraAPIAuthStepEnv(tools)) == 0 {
		return
	}
	fmt.Fprintf(yaml, "          %s=\"$(printf '%%s:%%s' \"$%s\" \"$%s\" | base64 | tr -d '\\n')\"\n", jiraBasicAuthEnvVar, jiraEmailEnvVar, jiraTokenEnvVar)
	fmt.Fprintf(yaml, "          echo \"::add-mask::${%s}\"\n", jiraBasicAuthEnvVar)
	fmt.Fprintf(yaml, "          export %s\n", jiraBasicAuthEnvVar)
}
