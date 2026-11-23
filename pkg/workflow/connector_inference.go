package workflow

import (
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/url"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/github/gh-aw/pkg/sliceutil"
)

type ConnectorInferenceSelection struct {
	Connector string `json:"connector"`
	Profile   string `json:"profile"`
	Model     string `json:"model"`
}

// ResolvedConnectorInference is compiler-only. It is never a connector tool plan.
type ResolvedConnectorInference struct {
	Selection ConnectorInferenceSelection
	Env       map[string]string
	Auth      *EngineAuthConfig
	// AllowedDomains are the firewall hosts the selected profile requires.
	AllowedDomains []string
	// ModelCatalogDisabled mirrors the profile's `model-catalog: false`.
	ModelCatalogDisabled bool
}

// connectorInferenceAllowedDomains returns firewall hosts required by selected connector inference profiles.
func connectorInferenceAllowedDomains(data *WorkflowData) []string {
	if data == nil {
		return nil
	}
	var domains []string
	for _, alias := range sliceutil.SortedKeys(data.Connectors) {
		if config := data.Connectors[alias]; config != nil && config.Inference != nil {
			domains = append(domains, config.Inference.AllowedDomains...)
		}
	}
	return domains
}

func consumeConnectorInferenceSelection(frontmatter, registrations map[string]any) error {
	for _, alias := range sliceutil.SortedKeys(registrations) {
		if registration, ok := registrations[alias].(map[string]any); ok {
			if _, authored := registration["selected-inference"]; authored {
				return fmt.Errorf("connectors.%s.selected-inference is internal; select inference with the main workflow's engine.inference", alias)
			}
		}
	}
	engine, ok := frontmatter["engine"].(map[string]any)
	if !ok {
		return nil
	}
	raw, selected := engine["inference"]
	if !selected {
		return nil
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	var selection ConnectorInferenceSelection
	if err := connectors.Decode(encoded, &selection); err != nil {
		return fmt.Errorf("engine.inference: %w", err)
	}
	if !connectorNamePattern.MatchString(selection.Connector) || !connectorNamePattern.MatchString(selection.Profile) ||
		selection.Model == "" || len(selection.Model) > 256 || strings.ContainsAny(selection.Model, "*${}\r\n\t") || strings.TrimSpace(selection.Model) != selection.Model {
		return errors.New("engine.inference requires exact connector, profile and literal model names")
	}
	registration, ok := registrations[selection.Connector].(map[string]any)
	if !ok {
		return fmt.Errorf("engine.inference references unknown connector %q; import its declaration first", selection.Connector)
	}
	registration["selected-inference"] = raw
	return nil
}

func resolveConnectorInference(alias string, registration ConnectorRegistration) (*ResolvedConnectorInference, error) {
	if err := validateConnectorAzureOIDC(alias, registration); err != nil {
		return nil, err
	}
	if err := validateConnectorInferenceProfiles(registration); err != nil {
		return nil, err
	}
	selection := registration.Inference
	if selection == nil {
		return nil, nil
	}
	if registration.Spec == nil || selection.Connector != alias {
		return nil, errors.New("inference requires an inline declaration and matching connector selection")
	}
	profile, ok := registration.Spec.Inference[selection.Profile]
	if !ok {
		return nil, fmt.Errorf("unknown inference profile %q", selection.Profile)
	}
	if !slices.Contains(registration.Resources[profile.Models], selection.Model) {
		return nil, fmt.Errorf("inference model %q is outside approved resources %q", selection.Model, profile.Models)
	}
	if _, ok := registration.Credentials[profile.Credential]; !ok {
		return nil, errors.New("selected inference profile has no credential reference")
	}
	endpoint, err := url.Parse(registration.Spec.Endpoints[profile.Endpoint])
	if err != nil || endpoint.Hostname() == "" {
		return nil, errors.New("inference endpoint must reference a declared HTTPS origin")
	}
	// Selecting the profile approves egress to its endpoint; importing alone does not.
	result := &ResolvedConnectorInference{Selection: *selection, AllowedDomains: []string{endpoint.Hostname()}, Env: map[string]string{
		"COPILOT_PROVIDER_BASE_URL": strings.TrimSuffix(endpoint.String(), "/") + profile.Path,
		"COPILOT_PROVIDER_TYPE":     "openai",
		"COPILOT_PROVIDER_MODEL_ID": selection.Model,
	}, ModelCatalogDisabled: profile.ModelCatalog != nil && !*profile.ModelCatalog}
	switch profile.Protocol {
	case "openai-responses":
		result.Env["COPILOT_PROVIDER_WIRE_API"] = "responses"
	case "openai-chat-completions":
		result.Env["COPILOT_PROVIDER_WIRE_API"] = "completions"
	default:
		return nil, fmt.Errorf("unsupported inference protocol %q", profile.Protocol)
	}
	if azure := registration.AzureOIDC[profile.Credential]; azure != nil {
		result.Auth = azure.engineAuth()
		result.AllowedDomains = append(result.AllowedDomains, azure.loginHost())
	} else {
		result.Env["COPILOT_PROVIDER_API_KEY"] = "${{ secrets." + registration.Credentials[profile.Credential].Secret + " }}"
	}
	return result, nil
}

func validateConnectorInferenceProfiles(registration ConnectorRegistration) error {
	if registration.Spec == nil {
		return nil
	}
	for _, name := range sliceutil.SortedKeys(registration.Spec.Inference) {
		profile := registration.Spec.Inference[name]
		if _, exists := registration.Resources[profile.Models]; !exists || len(registration.Resources[profile.Models]) == 0 {
			return fmt.Errorf("inference %q requires a nonempty model resource collection %q", name, profile.Models)
		}
		credential := profile.Credential
		if registration.GitHubApps[credential] != nil || registration.OIDCTokens[credential] != nil ||
			(registration.AzureOIDC[credential] == nil && registration.Credentials[credential].Secret == "") {
			return fmt.Errorf("inference %q requires a stored token or Azure OIDC credential", name)
		}
	}
	return nil
}

func expandConnectorInference(frontmatter map[string]any, inference *ResolvedConnectorInference) error {
	engine, ok := frontmatter["engine"].(map[string]any)
	if !ok || (engine["id"] != nil && engine["id"] != "copilot") {
		return errors.New("engine.inference currently supports only the copilot engine")
	}
	for _, field := range []string{"auth", "provider", "model-provider", "runtime", "api-target", "config", "args", "model-routing", "fallback-models", "driver", "command"} {
		if _, exists := engine[field]; exists {
			return fmt.Errorf("engine.%s conflicts with connector-owned inference configuration", field)
		}
	}
	if _, exists := frontmatter["model-routing"]; exists {
		return errors.New("model-routing conflicts with connector inference model grants")
	}
	for _, raw := range []any{engine["env"], frontmatter["env"]} {
		if env, ok := raw.(map[string]any); ok {
			for key := range env {
				if connectorInferenceEnvKey(key) {
					return fmt.Errorf("environment variable %s conflicts with connector-owned inference", key)
				}
			}
		}
	}
	if inference.ModelCatalogDisabled {
		if enabled, ok := engine["model-catalog"].(bool); ok && enabled {
			return errors.New("engine.model-catalog: true conflicts with a connector inference profile that has no model catalogue")
		}
		engine["model-catalog"] = false
	}
	env := connectorMap(engine, "env")
	for key, value := range inference.Env {
		env[key] = value
	}
	if inference.Auth != nil {
		data, err := json.Marshal(inference.Auth)
		if err != nil {
			return err
		}
		var auth map[string]any
		if err := json.Unmarshal(data, &auth); err != nil {
			return err
		}
		// EngineAuthConfig has provider-specific fields without omitempty.
		for key, value := range auth {
			if value == "" {
				delete(auth, key)
			}
		}
		engine["auth"] = auth
	}
	engine["id"] = "copilot"
	delete(engine, "inference")
	return nil
}

func connectorInferenceEnvKey(key string) bool {
	return strings.HasPrefix(key, "COPILOT_PROVIDER_") || strings.HasPrefix(key, "AWF_AUTH_") ||
		strings.HasPrefix(key, "OPENAI_") || strings.HasPrefix(key, "ANTHROPIC_") ||
		key == "GITHUB_COPILOT_BASE_URL" || key == "COPILOT_MODEL"
}

func usesConnectorInference(data *WorkflowData) bool {
	if data != nil {
		for _, connector := range data.Connectors {
			if connector.Inference != nil {
				return true
			}
		}
	}
	return false
}

func (c *Compiler) validateConnectorInferenceConsumers(data *WorkflowData) error {
	if !usesConnectorInference(data) {
		return nil
	}
	if agent := getAgentConfig(data); agent != nil {
		if agent.ModelFallback != nil && *agent.ModelFallback != "false" {
			return errors.New("sandbox.agent.model-fallback must be false for connector inference")
		}
		for _, key := range sliceutil.SortedKeys(agent.Env) {
			if connectorInferenceEnvKey(key) {
				return fmt.Errorf("sandbox.agent.env.%s conflicts with connector-owned inference", key)
			}
		}
		if target := agent.Targets["copilot"]; target != nil && (target.AuthHeader != "" || len(target.ExtraBodyFields) != 0) {
			return errors.New("sandbox.agent.targets.copilot auth-header and extra-body-fields overrides conflict with connector inference")
		}
	}
	if data.SafeOutputs == nil || data.SafeOutputs.ThreatDetection == nil || c.getThreatDetectionEngineID(data) != data.AI {
		return nil
	}
	detector := data.SafeOutputs.ThreatDetection
	if detector.HasRunnableDetection() && (detector.Model != "" || detector.EngineConfig != nil) {
		return errors.New("safe-outputs.threat-detection cannot override an engine that inherits connector inference; use a different detector engine")
	}
	return nil
}

func validateResolvedConnectorInference(data *WorkflowData, configs map[string]*WorkflowConnector, importedEnvSources map[string]string) error {
	for _, config := range configs {
		inference := config.Inference
		if inference == nil {
			continue
		}
		for key, source := range importedEnvSources {
			if connectorInferenceEnvKey(key) {
				return fmt.Errorf("environment variable %s from import %q conflicts with connector-owned inference", key, source)
			}
		}
		engine := data.EngineConfig
		if engine == nil || data.AI != "copilot" {
			return errors.New("connector inference requires the copilot engine after import merging")
		}
		if engine.CopilotSDK {
			return errors.New("engine.copilot-sdk cannot preserve the granted wire model for connector inference")
		}
		if engine.ModelRouting != nil || len(engine.FallbackModels) != 0 || engine.APITarget != "" || len(engine.Args) != 0 ||
			engine.Config != "" || engine.IsInlineDefinition || engine.LLMProvider != "" || engine.Driver != "" ||
			engine.InlineDriver != nil || engine.Command != "" || engine.HarnessScript != "" {
			return errors.New("imported engine routing or provider overrides conflict with connector-owned inference")
		}
		if data.Model == "" || strings.Contains(data.Model, "${{") || isReservedRouterModelName(data.Model) {
			return errors.New("connector inference requires explicit literal engine.model metadata, not a router or expression")
		}
		expected := &EngineConfig{Env: map[string]string{}, Auth: inference.Auth}
		maps.Copy(expected.Env, inference.Env)
		applyEngineAuthEnv(expected)
		for key, value := range engine.Env {
			if want, allowed := expected.Env[key]; connectorInferenceEnvKey(key) && (!allowed || want != value) {
				return fmt.Errorf("imported engine environment %s conflicts with connector-owned inference", key)
			}
		}
		if (engine.Auth == nil) != (inference.Auth == nil) || (engine.Auth != nil && *engine.Auth != *inference.Auth) {
			return errors.New("imported engine.auth conflicts with connector-owned inference")
		}
	}
	return nil
}
