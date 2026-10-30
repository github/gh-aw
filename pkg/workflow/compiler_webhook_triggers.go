package workflow

import (
	"errors"
	"fmt"
	"slices"

	"github.com/goccy/go-yaml"
)

// Keep this non-trigger allowlist in sync with the gateway-dispatch schema dependency.
var gatewayDispatchOnSettings = []string{
	"admission", "allow-bot-authored-trigger-comment", "bots", "cooldown", "decision", "github-app", "github-token",
	"labels", "manual-approval", "needs", "permissions", "reaction", "report-blocked-version",
	"restore-memory", "roles", "skip-author-associations", "skip-bots", "skip-if-check-failing",
	"skip-if-match", "skip-if-no-match", "skip-roles", "stale-check", "status-comment", "steps", "stop-after",
}

func validateGatewayDispatchExclusive(on map[string]any) error {
	for _, name := range sortedWebhookKeys(on) {
		if name != "gateway-dispatch" && !slices.Contains(gatewayDispatchOnSettings, name) {
			return fmt.Errorf("on.gateway-dispatch is exclusive and cannot be combined with on.%s; remove every other trigger and let the compiler generate the gateway dispatch route", name)
		}
	}
	return nil
}

func validateCompiledWebhookTriggers(data *WorkflowData) error {
	var frontmatter struct {
		On map[string]any `yaml:"on"`
	}
	if err := yaml.Unmarshal([]byte(data.On), &frontmatter); err != nil {
		return fmt.Errorf("cannot validate exclusive gateway dispatch route: %w", err)
	}
	const message = "on.gateway-dispatch is exclusive; gateway subscriptions require only the compiler-generated repository_dispatch route"
	if len(data.Command) != 0 || len(data.LabelCommand) != 0 {
		return errors.New(message)
	}
	for _, name := range sortedWebhookKeys(frontmatter.On) {
		if name != "repository_dispatch" && !slices.Contains(gatewayDispatchOnSettings, name) {
			return errors.New(message)
		}
	}
	dispatch, ok := frontmatter.On["repository_dispatch"].(map[string]any)
	if !ok || len(dispatch) != 1 {
		return errors.New(message)
	}
	types, err := webhookStringList(dispatch["types"])
	if err != nil || !slices.Equal(types, []string{WebhookDispatchEventType(data.WorkflowID)}) {
		return errors.New(message)
	}
	return nil
}

func (c *Compiler) validateWebhookWorkflow(data *WorkflowData) error {
	if len(data.WebhookSubscriptions) == 0 {
		return nil
	}
	if err := c.requireWebhookTriggers(data.Roles); err != nil {
		return err
	}
	return validateCompiledWebhookTriggers(data)
}

func (c *Compiler) requireWebhookTriggers(roles []string) error {
	config, err := c.loadRepoConfig()
	if err != nil {
		return fmt.Errorf("cannot enable hosted CAO triggers: %w", err)
	}
	if config == nil || !config.GatewayDispatch {
		return fmt.Errorf("webhook triggers are experimental synthetic webhook events that must be triggered by a hosted CAO server; enable them by setting {\"gateway-dispatch\":true} in %s and defining nested triggers under on.gateway-dispatch in the workflow", RepoConfigFileName)
	}
	return validateWebhookTriggerRoles(roles)
}

func validateWebhookTriggerRoles(roles []string) error {
	const message = "webhook triggers require on.roles with write access or higher; use [admin, maintainer, write] or a stricter subset"
	if len(roles) == 0 {
		return errors.New(message)
	}
	for _, role := range roles {
		switch role {
		case "admin", "maintainer", "maintain", "write":
		default:
			return fmt.Errorf("%s; role %q is not allowed", message, role)
		}
	}
	return nil
}
