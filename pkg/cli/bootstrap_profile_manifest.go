package cli

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

const bootstrapActionTypeExample = "require-owner-type, repo-variable, repo-secret, repo-label, github-app, copilot-auth, commit-and-push, or handoff"

const (
	bootstrapLabelNameMaxLength        = 50
	bootstrapLabelDescriptionMaxLength = 100
)

var bootstrapLabelColorPattern = regexp.MustCompile(`^[0-9A-Fa-f]{6}$`)

type repositoryPackageBootstrap struct {
	Config []repositoryPackageBootstrapAction
}

type repositoryPackageBootstrapAction struct {
	Type             string                               `json:"type"`
	Owner            string                               `json:"owner"`
	Value            string                               `json:"value"`
	Name             string                               `json:"name"`
	Prompt           string                               `json:"prompt"`
	Description      string                               `json:"description"`
	Color            string                               `json:"color"`
	Default          string                               `json:"default"`
	Optional         bool                                 `json:"optional"`
	Enum             []string                             `json:"enum"`
	When             *repositoryPackageBootstrapCondition `json:"when"`
	Secret           string                               `json:"secret"`
	Strategy         string                               `json:"strategy"`
	Message          string                               `json:"message"`
	Mode             string                               `json:"mode"`
	AppIDVariable    string                               `json:"app-id-variable"`
	PrivateKeySecret string                               `json:"private-key-secret"`
	AppName          string                               `json:"app-name"`
	HomepageURL      string                               `json:"homepage-url"`
	Permissions      map[string]string                    `json:"permissions"`
	Events           []string                             `json:"events"`
	ExistingOnly     bool                                 `json:"existing-only"`
}

type repositoryPackageBootstrapCondition struct {
	Variable string
	Equals   string
}

type resolvedBootstrapProfile struct {
	PackageID string
	Source    string
	Profile   *repositoryPackageBootstrap
}

func extractManifestConfig(value any, manifestPath string) (*repositoryPackageBootstrap, error) {
	configItems, ok := value.([]any)
	if !ok {
		return nil, fmt.Errorf("invalid Agentic Workflow manifest %q: config must be a list. Example: config: [{ type: repo-variable, name: EXAMPLE, prompt: Enter a value }]", manifestPath)
	}
	if len(configItems) == 0 {
		return nil, fmt.Errorf("invalid Agentic Workflow manifest %q: config must not be empty. Example: config: [{ type: repo-variable, name: EXAMPLE, prompt: Enter a value }]", manifestPath)
	}

	bootstrap := &repositoryPackageBootstrap{}
	for index, item := range configItems {
		actionMap, ok := item.(map[string]any)
		if !ok {
			return nil, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d] must be a mapping. Example: { type: repo-variable, name: EXAMPLE, prompt: Enter a value }", manifestPath, index)
		}

		actionType, ok := stringValue(actionMap["type"])
		if !ok || strings.TrimSpace(actionType) == "" {
			return nil, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].type must be a non-empty string. Example: type: repo-variable", manifestPath, index)
		}

		action, err := parseManifestBootstrapAction(strings.TrimSpace(actionType), actionMap, manifestPath, index)
		if err != nil {
			return nil, err
		}
		bootstrap.Config = append(bootstrap.Config, action)
	}

	bootstrapLog.Printf("Extracted bootstrap manifest config: actions=%d, manifest=%s", len(bootstrap.Config), manifestPath)
	return bootstrap, nil
}

func parseManifestBootstrapAction(actionType string, actionMap map[string]any, manifestPath string, index int) (repositoryPackageBootstrapAction, error) {
	if _, exists := actionMap["when"]; exists {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].when is not supported yet. Example: remove the when field and keep only supported keys such as type, name, and prompt", manifestPath, index)
	}
	var action repositoryPackageBootstrapAction
	if err := decodeManifestBootstrapAction(actionMap, &action); err != nil {
		var typeErr *json.UnmarshalTypeError
		if errors.As(err, &typeErr) {
			return repositoryPackageBootstrapAction{}, manifestBootstrapFieldError(manifestPath, index, typeErr.Field, fmt.Errorf("must be a %s, got %s", manifestFieldJSONTypeName(typeErr.Type), typeErr.Value))
		}
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d] is invalid: %w", manifestPath, index, err)
	}
	action.Type = actionType
	action.Owner = strings.TrimSpace(action.Owner)
	action.Value = strings.TrimSpace(action.Value)
	action.Name = strings.TrimSpace(action.Name)
	action.Prompt = strings.TrimSpace(action.Prompt)
	action.Description = strings.TrimSpace(action.Description)
	action.Color = strings.TrimSpace(action.Color)
	action.Secret = strings.TrimSpace(action.Secret)
	action.Strategy = strings.TrimSpace(action.Strategy)
	action.Message = strings.TrimSpace(action.Message)
	action.Mode = strings.TrimSpace(action.Mode)
	action.AppIDVariable = strings.TrimSpace(action.AppIDVariable)
	action.PrivateKeySecret = strings.TrimSpace(action.PrivateKeySecret)
	action.AppName = strings.TrimSpace(action.AppName)
	action.HomepageURL = strings.TrimSpace(action.HomepageURL)

	return validateManifestBootstrapAction(action, manifestPath, index)
}

func validateManifestBootstrapAction(action repositoryPackageBootstrapAction, manifestPath string, index int) (repositoryPackageBootstrapAction, error) {
	switch action.Type {
	case "require-owner-type":
		if action.Owner != "" && action.Owner != "repo" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].owner must be 'repo' when type=require-owner-type. Example: { type: require-owner-type, owner: repo, value: org }", manifestPath, index)
		}
		if action.Value != "any" && action.Value != "org" && action.Value != "user" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].value must be one of: any, org, user. Example: { type: require-owner-type, value: org }", manifestPath, index)
		}
	case "repo-variable":
		if action.Name == "" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].name is required when type=repo-variable. Example: { type: repo-variable, name: EXAMPLE, prompt: Enter a value }", manifestPath, index)
		}
		if action.Prompt == "" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].prompt is required when type=repo-variable. Example: { type: repo-variable, name: EXAMPLE, prompt: Enter a value }", manifestPath, index)
		}
	case "repo-secret":
		if action.Name == "" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].name is required when type=repo-secret. Example: { type: repo-secret, name: EXAMPLE_SECRET, prompt: Enter a secret }", manifestPath, index)
		}
		if action.Prompt == "" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].prompt is required when type=repo-secret. Example: { type: repo-secret, name: EXAMPLE_SECRET, prompt: Enter a secret }", manifestPath, index)
		}
	case "repo-label":
		return validateRepoLabelBootstrapAction(action, manifestPath, index)
	case "github-app":
		return validateGitHubAppBootstrapAction(action, manifestPath, index)
	case "copilot-auth":
		if action.Secret == "" {
			action.Secret = "COPILOT_GITHUB_TOKEN"
		}
		if action.Strategy == "" {
			action.Strategy = "prompt-if-actions-auth-unavailable"
		}
		if action.Strategy != "prompt-if-actions-auth-unavailable" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].strategy must be 'prompt-if-actions-auth-unavailable'. Example: { type: copilot-auth, strategy: prompt-if-actions-auth-unavailable }", manifestPath, index)
		}
	case "commit-and-push":
		if action.Message == "" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].message is required when type=commit-and-push. Example: { type: commit-and-push, message: Bootstrap repository changes }", manifestPath, index)
		}
	case "handoff":
		if action.Message == "" {
			return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].message is required when type=handoff. Example: { type: handoff, message: Continue with repository-specific setup. }", manifestPath, index)
		}
	default:
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].type %q is not supported. Example: use one of %s", manifestPath, index, action.Type, bootstrapActionTypeExample)
	}

	return action, nil
}

func validateRepoLabelBootstrapAction(action repositoryPackageBootstrapAction, manifestPath string, index int) (repositoryPackageBootstrapAction, error) {
	if action.Name == "" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].name is required when type=repo-label. Example: { type: repo-label, name: automation, description: Managed by automation, color: 1f6feb }", manifestPath, index)
	}
	if len(action.Name) > bootstrapLabelNameMaxLength {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].name must be at most %d characters when type=repo-label. Example: name: automation", manifestPath, index, bootstrapLabelNameMaxLength)
	}
	if action.Description == "" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].description is required when type=repo-label. Example: { type: repo-label, name: automation, description: Managed by automation, color: 1f6feb }", manifestPath, index)
	}
	if len(action.Description) > bootstrapLabelDescriptionMaxLength {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].description must be at most %d characters when type=repo-label. Example: description: Managed by automation", manifestPath, index, bootstrapLabelDescriptionMaxLength)
	}
	if action.Color == "" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].color is required when type=repo-label. Example: { type: repo-label, name: automation, description: Managed by automation, color: 1f6feb }", manifestPath, index)
	}
	if !bootstrapLabelColorPattern.MatchString(action.Color) {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].color must be a 6-character hexadecimal color without '#'. Example: color: 1f6feb", manifestPath, index)
	}
	return action, nil
}

func validateGitHubAppBootstrapAction(action repositoryPackageBootstrapAction, manifestPath string, index int) (repositoryPackageBootstrapAction, error) {
	if action.AppName == "" && action.Name != "" {
		action.AppName = action.Name
	}
	if action.ExistingOnly && action.Mode != "" && action.Mode != "existing" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].existing-only requires mode to be 'existing' or unset. Remove mode=%q or set it to 'existing'", manifestPath, index, action.Mode)
	}
	if action.ExistingOnly && action.Mode == "" {
		action.Mode = "existing"
	}
	if action.Mode == "" {
		action.Mode = "create-or-existing"
	}
	if action.Mode != "create-or-existing" && action.Mode != "existing" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].mode must be one of: create-or-existing, existing. Example: { type: github-app, mode: existing, app-id-variable: APP_ID, private-key-secret: APP_PRIVATE_KEY }", manifestPath, index)
	}
	if action.Owner != "" && action.Owner != "repo" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].owner must be 'repo' when type=github-app. Example: { type: github-app, owner: repo, app-id-variable: APP_ID, private-key-secret: APP_PRIVATE_KEY }", manifestPath, index)
	}
	if action.AppIDVariable == "" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].app-id-variable is required when type=github-app. Example: { type: github-app, app-id-variable: APP_ID, private-key-secret: APP_PRIVATE_KEY }", manifestPath, index)
	}
	if action.PrivateKeySecret == "" {
		return repositoryPackageBootstrapAction{}, fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].private-key-secret is required when type=github-app. Example: { type: github-app, app-id-variable: APP_ID, private-key-secret: APP_PRIVATE_KEY }", manifestPath, index)
	}
	return action, nil
}

func decodeManifestBootstrapAction(actionMap map[string]any, action *repositoryPackageBootstrapAction) error {
	data, err := json.Marshal(actionMap)
	if err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	return decoder.Decode(action)
}

func manifestBootstrapFieldError(manifestPath string, index int, field string, err error) error {
	if example, ok := manifestBootstrapFieldExample(field); ok {
		return fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].%s %w. Example: config[%d].%s: %s", manifestPath, index, field, err, index, field, example)
	}
	return fmt.Errorf("invalid Agentic Workflow manifest %q: config[%d].%s %w", manifestPath, index, field, err)
}

func manifestBootstrapFieldExample(field string) (string, bool) {
	switch field {
	case "enum", "events":
		return `["issues", "pull_request"]`, true
	case "permissions":
		return `{ contents: "read", issues: "write" }`, true
	default:
		return "", false
	}
}

// manifestFieldJSONTypeName maps a Go type expected by json.Unmarshal to the JSON
// type name manifest authors will recognize (e.g. "array" instead of "[]string"),
// since manifest values are authored as YAML/JSON, not Go.
func manifestFieldJSONTypeName(t reflect.Type) string {
	switch t.Kind() {
	case reflect.String:
		return "string"
	case reflect.Bool:
		return "boolean"
	case reflect.Slice, reflect.Array:
		return "array"
	case reflect.Map, reflect.Struct, reflect.Pointer:
		return "object"
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return "number"
	default:
		return t.String()
	}
}
