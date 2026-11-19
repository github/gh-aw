package workflow

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"reflect"
	"strings"
	"unicode"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/stringutil"
	"github.com/github/gh-aw/pkg/workflow/compilerenv"
)

var safeOutputAuthorizationLog = logger.New("workflow:safe_outputs_authorization")

// SafeOutputAuthorization is a required, fail-closed authority binding.
// Source is compiler-owned and is never accepted from workflow configuration.
type SafeOutputAuthorization struct {
	Endpoint        string `yaml:"endpoint" json:"endpoint,omitempty"`
	Audience        string `yaml:"audience" json:"audience,omitempty"`
	Source          string `yaml:"-" json:"source,omitempty"`
	FromEnvironment bool   `yaml:"-" json:"from_environment,omitempty"`
	parseError      error
}

func parseSafeOutputAuthorization(raw any) (*SafeOutputAuthorization, error) {
	if raw == nil {
		return nil, nil
	}
	m, ok := raw.(map[string]any)
	if !ok || len(m) != 2 {
		return nil, errors.New("safe-output authorization must contain only endpoint and audience")
	}
	endpoint, endpointOK := m["endpoint"].(string)
	audience, audienceOK := m["audience"].(string)
	if !endpointOK || !audienceOK || strings.TrimSpace(audience) != audience || audience == "" || len(audience) > 2048 || strings.ContainsAny(audience, "\r\n") || strings.Contains(audience, "${{") {
		return nil, errors.New("safe-output authorization requires a static nonempty audience")
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Hostname() == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.ForceQuery || u.Opaque != "" || strings.TrimSpace(endpoint) != endpoint || strings.ContainsAny(endpoint, "\\?#") || strings.IndexFunc(endpoint, unicode.IsSpace) >= 0 || strings.Contains(endpoint, "${{") || len(endpoint) > 2048 {
		return nil, errors.New("safe-output authorization endpoint must be an absolute HTTPS URL without credentials, query, fragment, or expressions")
	}
	return &SafeOutputAuthorization{Endpoint: endpoint, Audience: audience}, nil
}

func parseLocalSafeOutputAuthorization(raw any) *SafeOutputAuthorization {
	authorization, err := parseSafeOutputAuthorization(raw)
	if err != nil {
		return &SafeOutputAuthorization{parseError: err}
	}
	return authorization
}

func parseAuthorizationStaged(raw any) *TemplatableBool {
	if raw == nil {
		return nil
	}
	value := TemplatableBool(fmt.Sprint(raw))
	return &value
}

func validateRawSafeOutputAuthorizations(raw map[string]any) error {
	for key, value := range raw {
		if key == "authorization" {
			if value == nil {
				return errors.New("safe-output authorization must be an endpoint/audience object")
			}
			if _, err := parseSafeOutputAuthorization(value); err != nil {
				return err
			}
			continue
		}
		m, ok := value.(map[string]any)
		if !ok {
			continue
		}
		if key == "scripts" || key == "actions" || key == "jobs" || key == "dispatch-repository" || key == "dispatch_repository" {
			if err := validateRawSafeOutputAuthorizations(m); err != nil {
				return err
			}
		} else if auth, exists := m["authorization"]; exists {
			if auth == nil {
				return fmt.Errorf("safe-output %q authorization must be an endpoint/audience object", key)
			}
			if _, err := parseSafeOutputAuthorization(auth); err != nil {
				return fmt.Errorf("safe-output %q: %w", key, err)
			}
		}
	}
	return nil
}

func validateSafeOutputAuthorizationAliases(raw map[string]any) error {
	for _, handler := range safeOutputHandlers {
		var requirement *SafeOutputAuthorization
		keys := append([]string{handler.Key}, handler.Aliases...)
		if handler.ToolName == "dispatch_repository" {
			keys = append(keys, "dispatch-repository")
		}
		configured := 0
		for _, key := range keys {
			if _, exists := raw[key]; exists {
				configured++
			}
			config, ok := raw[key].(map[string]any)
			if !ok {
				continue
			}
			auth, err := parseSafeOutputAuthorization(config["authorization"])
			if err != nil {
				return err
			}
			if !sameSafeOutputAuthority(requirement, auth) {
				return fmt.Errorf("conflicting authorization authorities for safe-output aliases of %q", handler.Key)
			}
			if auth != nil {
				requirement = auth
			}
		}
		if configured > 1 && (requirement != nil || raw["authorization"] != nil) {
			return fmt.Errorf("safe-output %q authorization is configured through multiple aliases; use one canonical configuration", handler.Key)
		}
	}
	return nil
}

func sameSafeOutputAuthority(a, b *SafeOutputAuthorization) bool {
	return a == nil || b == nil || (a.Endpoint == b.Endpoint && a.Audience == b.Audience && a.FromEnvironment == b.FromEnvironment)
}

func nativeSafeOutputAuthorization(config *SafeOutputsConfig, handler safeOutputHandlerDescriptor) *SafeOutputAuthorization {
	field, ok := safeOutputPointerFieldValue(config, handler.StructField)
	if !ok || field.IsNil() {
		return nil
	}
	auth := field.Elem().FieldByName("Authorization")
	if auth.IsValid() && auth.CanInterface() {
		value, valid := auth.Interface().(*SafeOutputAuthorization)
		if !valid {
			return &SafeOutputAuthorization{parseError: errors.New("compiler authorization field has an unexpected type")}
		}
		return value
	}
	return nil
}

func setNativeSafeOutputAuthorization(config *SafeOutputsConfig, handler safeOutputHandlerDescriptor, value *SafeOutputAuthorization) {
	field, ok := safeOutputPointerFieldValue(config, handler.StructField)
	if !ok || field.IsNil() {
		return
	}
	auth := field.Elem().FieldByName("Authorization")
	if auth.IsValid() && auth.CanSet() {
		auth.Set(reflect.ValueOf(value))
	}
}

func mergeSafeOutputAuthorizations(result, imported *SafeOutputsConfig) error {
	safeOutputAuthorizationLog.Printf("Merging imported authorization: global=%t scripts=%d actions=%d jobs=%d", imported.Authorization != nil, len(imported.Scripts), len(imported.Actions), len(imported.Jobs))
	if imported.authorizationParseError != nil {
		safeOutputAuthorizationLog.Print("Imported authorization rejected: invalid_configuration")
		return imported.authorizationParseError
	}
	if !sameSafeOutputAuthority(result.Authorization, imported.Authorization) {
		safeOutputAuthorizationLog.Print("Imported authorization rejected: conflicting_authorities")
		return errors.New("conflicting safe-outputs authorization authorities across imports")
	}
	if result.Authorization == nil {
		result.Authorization = imported.Authorization
	}
	for _, handler := range safeOutputHandlers {
		a, b := nativeSafeOutputAuthorization(result, handler), nativeSafeOutputAuthorization(imported, handler)
		if !sameSafeOutputAuthority(a, b) {
			return fmt.Errorf("conflicting authorization authorities for safe-output %q across imports", handler.Key)
		}
		if a == nil && b != nil {
			setNativeSafeOutputAuthorization(result, handler, b)
		}
	}
	if imported.DispatchRepository != nil {
		for name, tool := range imported.DispatchRepository.Tools {
			if err := mergeImportedCustomAuthorization(result, name, tool.Authorization); err != nil {
				return err
			}
		}
	}
	for name, script := range imported.Scripts {
		if err := mergeImportedCustomAuthorization(result, name, script.Authorization); err != nil {
			return err
		}
	}
	for name, action := range imported.Actions {
		if err := mergeImportedCustomAuthorization(result, name, action.Authorization); err != nil {
			return err
		}
	}
	for name, job := range imported.Jobs {
		if err := mergeImportedCustomAuthorization(result, name, job.Authorization); err != nil {
			return err
		}
	}
	for name, authority := range imported.importedCustomAuthorizations {
		if err := mergeImportedCustomAuthorization(result, name, authority); err != nil {
			return err
		}
	}
	return nil
}

// Imported requirements must survive implementation overrides without enabling
// author code that the ordinary import merge does not select. The normalized tool
// identity remains protected even when the selected executor kind changes.
func mergeImportedCustomAuthorization(result *SafeOutputsConfig, name string, authority *SafeOutputAuthorization) error {
	if authority == nil {
		return nil
	}
	if authority.parseError != nil {
		return authority.parseError
	}
	if result.importedCustomAuthorizations == nil {
		result.importedCustomAuthorizations = make(map[string]*SafeOutputAuthorization)
	}
	key := stringutil.NormalizeSafeOutputIdentifier(name)
	if !sameSafeOutputAuthority(result.importedCustomAuthorizations[key], authority) {
		return fmt.Errorf("conflicting authorization authorities for custom safe-output %q across imports", name)
	}
	result.importedCustomAuthorizations[key] = authority
	return nil
}

func importedCustomAuthorization(config *SafeOutputsConfig, name string, local *SafeOutputAuthorization) (*SafeOutputAuthorization, error) {
	imported := config.importedCustomAuthorizations[stringutil.NormalizeSafeOutputIdentifier(name)]
	if !sameSafeOutputAuthority(imported, local) {
		return nil, fmt.Errorf("conflicting authorization authorities for custom safe-output %q across imports", name)
	}
	if local != nil {
		return local, nil
	}
	return imported, nil
}

func resolveSafeOutputAuthorizations(config *SafeOutputsConfig) error {
	if config == nil {
		return nil
	}
	if config.authorizationParseError != nil {
		return config.authorizationParseError
	}
	config.AuthorizationRequirements = make(map[string]*SafeOutputAuthorization)
	seen, err := resolveNativeSafeOutputAuthorizations(config)
	if err != nil {
		return err
	}
	if err := resolveCustomSafeOutputAuthorizations(config, seen); err != nil {
		return err
	}
	if safeOutputAuthorizationEnabled(config) && len(config.Steps) != 0 {
		return errors.New("safe-outputs.steps pre-output hooks are not supported with authorization; move reviewed steps into an authorized safe-output job")
	}
	if safeOutputsAnyAuthorizationNeedsOIDC(config) && config.IDToken != nil && *config.IDToken == "none" {
		return errors.New("safe-output authorization requires id-token: write; remove safe-outputs.id-token: none")
	}
	if err := buildSafeOutputAuthorizationCoverage(config); err != nil {
		return err
	}
	if err := validateEffectAdapterConfigurations(config); err != nil {
		return err
	}
	safeOutputAuthorizationLog.Printf("Resolved authorization: global=%t outputs=%d coverage=%d", config.Authorization != nil, len(config.AuthorizationRequirements), len(config.AuthorizationCoverage))
	return nil
}

func resolveNativeSafeOutputAuthorizations(config *SafeOutputsConfig) (map[string]string, error) {
	seen := map[string]string{"authorization": "configuration metadata"}
	configType := reflect.TypeOf(*config)
	for field := range configType.Fields() {
		key, _, _ := strings.Cut(field.Tag.Get("yaml"), ",")
		if key != "" && key != "-" {
			seen[stringutil.NormalizeSafeOutputIdentifier(key)] = key
		}
	}
	for _, name := range []string{commentMemoryHandlerKey, ledgerMutationHandlerKey, "ledger_append", ledgerRequestCompactionHandlerKey, "_token_broker", "raw_steps", "max_bot_mentions"} {
		seen[name] = name
	}
	for _, handler := range safeOutputHandlers {
		for _, name := range append([]string{handler.Key, handler.ToolName}, handler.Aliases...) {
			seen[stringutil.NormalizeSafeOutputIdentifier(name)] = handler.Key
		}
		if handler.ToolName == "" || !hasSafeOutputFieldSet(config, handler.StructField) {
			continue
		}
		requirement, err := effectiveSafeOutputAuthorization(config.Authorization, nativeSafeOutputAuthorization(config, handler), handler.Builtin)
		if err != nil {
			return nil, fmt.Errorf("safe-output %q: %w", handler.Key, err)
		}
		config.AuthorizationRequirements[handler.ToolName] = requirement
	}
	return seen, nil
}

func resolveCustomSafeOutputAuthorizations(config *SafeOutputsConfig, seen map[string]string) error {
	add := func(name string, local *SafeOutputAuthorization) error {
		normalized := stringutil.NormalizeSafeOutputIdentifier(name)
		if previous, exists := seen[normalized]; exists {
			return fmt.Errorf("custom safe-output %q collides with reserved or normalized name %q", name, previous)
		}
		seen[normalized] = name
		local, err := importedCustomAuthorization(config, name, local)
		if err != nil {
			return err
		}
		requirement, err := effectiveSafeOutputAuthorization(config.Authorization, local, false)
		if err != nil {
			return fmt.Errorf("safe-output %q: %w", name, err)
		}
		config.AuthorizationRequirements[normalized] = requirement
		return nil
	}
	for name, script := range config.Scripts {
		if err := add(name, script.Authorization); err != nil {
			return err
		}
	}
	for name, action := range config.Actions {
		if err := add(name, action.Authorization); err != nil {
			return err
		}
	}
	for name, job := range config.Jobs {
		if err := add(name, job.Authorization); err != nil {
			return err
		}
	}
	if config.DispatchRepository != nil {
		parent := config.AuthorizationRequirements["dispatch_repository"]
		for name, tool := range config.DispatchRepository.Tools {
			normalized := stringutil.NormalizeSafeOutputIdentifier(name)
			if previous, exists := seen[normalized]; exists {
				return fmt.Errorf("repository dispatch tool %q collides with reserved or normalized name %q", name, previous)
			}
			seen[normalized] = name
			local, err := importedCustomAuthorization(config, name, tool.Authorization)
			if err != nil {
				return err
			}
			requirement, err := effectiveSafeOutputAuthorization(parent, local, false)
			if err != nil {
				return fmt.Errorf("repository dispatch tool %q: %w", name, err)
			}
			config.AuthorizationRequirements[normalized] = requirement
		}
	}
	return nil
}

func resolveWorkflowSafeOutputAuthorizations(data *WorkflowData) error {
	if err := resolveSafeOutputAuthorizations(data.SafeOutputs); err != nil {
		return err
	}
	config := data.SafeOutputs
	if config == nil {
		return nil
	}
	for _, name := range workQueuePreparedAdapterNames(data) {
		config.AuthorizationCoverage[name+":prepare_claim_adapter"] = config.AuthorizationCoverage[name]
	}
	if config.CallWorkflow != nil {
		for _, name := range config.CallWorkflow.Workflows {
			config.AuthorizationCoverage["call_workflow:launch:"+stringutil.NormalizeSafeOutputIdentifier(name)] = config.AuthorizationCoverage["call_workflow"]
		}
	}
	internal := map[string]bool{
		commentMemoryHandlerKey:           buildCommentMemoryHandlerConfig(data.CommentMemoryConfig, config.Footer, config.BodyFooter) != nil,
		ledgerMutationHandlerKey:          buildLedgerMutationHandlerConfig(data.RepoMemoryConfig) != nil,
		"ledger_append":                   data.LedgerConfig != nil && data.LedgerConfig.Enabled(),
		ledgerRequestCompactionHandlerKey: buildLedgerRequestCompactionHandlerConfig(data.LedgerConfig) != nil,
	}
	for name, enabled := range internal {
		if !enabled {
			continue
		}
		if config.Authorization == nil {
			config.AuthorizationRequirements[name] = nil
			config.AuthorizationCoverage[name] = "unconfigured"
			continue
		}
		requirement := *config.Authorization
		requirement.Source = "global"
		config.AuthorizationRequirements[name] = &requirement
		config.AuthorizationCoverage[name] = "manager"
		staged := templatableBoolIsTrue(config.Staged)
		if name == commentMemoryHandlerKey && data.CommentMemoryConfig != nil {
			staged = staged || templatableBoolIsTrue(data.CommentMemoryConfig.Staged)
		}
		if staged {
			config.AuthorizationCoverage[name] = "staged"
		} else if config.IDToken != nil && *config.IDToken == "none" {
			return fmt.Errorf("safe-output %q authorization requires id-token: write; remove safe-outputs.id-token: none", name)
		}
	}
	return nil
}

func buildSafeOutputAuthorizationCoverage(config *SafeOutputsConfig) error {
	config.AuthorizationCoverage = make(map[string]string)
	add := func(name, executor string, builtin bool, staged *TemplatableBool) {
		requirement := config.AuthorizationRequirements[name]
		switch {
		case requirement != nil && isHandlerStaged(templatableBoolIsTrue(config.Staged), staged):
			config.AuthorizationCoverage[name] = "staged"
		case requirement != nil:
			config.AuthorizationCoverage[name] = executor
		case config.Authorization != nil && builtin:
			config.AuthorizationCoverage[name] = "builtin-exempt"
		default:
			config.AuthorizationCoverage[name] = "unconfigured"
		}
	}
	for _, handler := range safeOutputHandlers {
		if handler.ToolName == "" {
			continue
		}
		if field, ok := safeOutputPointerFieldValue(config, handler.StructField); ok && !field.IsNil() {
			add(handler.ToolName, safeOutputAuthorizationExecutor(handler.ToolName), handler.Builtin, safeOutputHandlerStaged(field))
		}
	}
	for name, script := range config.Scripts {
		add(stringutil.NormalizeSafeOutputIdentifier(name), "script", false, safeScriptAuthorizationStaged(script))
	}
	for name, action := range config.Actions {
		add(stringutil.NormalizeSafeOutputIdentifier(name), "action", false, action.Staged)
	}
	for name, job := range config.Jobs {
		add(stringutil.NormalizeSafeOutputIdentifier(name), "job", false, job.Staged)
	}
	if config.DispatchRepository != nil {
		for name, tool := range config.DispatchRepository.Tools {
			add(stringutil.NormalizeSafeOutputIdentifier(name), "manager", false, tool.Staged)
		}
	}
	for name, requirement := range config.AuthorizationRequirements {
		if requirement != nil && config.AuthorizationCoverage[name] == "" {
			return fmt.Errorf("safe-output %q has authorization but no authorization-capable executor", name)
		}
	}
	return nil
}

func effectiveSafeOutputAuthorization(global, local *SafeOutputAuthorization, builtin bool) (*SafeOutputAuthorization, error) {
	for _, requirement := range []*SafeOutputAuthorization{global, local} {
		if requirement != nil && requirement.parseError != nil {
			return nil, requirement.parseError
		}
	}
	if !sameSafeOutputAuthority(global, local) {
		return nil, errors.New("local authorization conflicts with inherited global authority")
	}
	if local != nil {
		result := *local
		result.Source = "local"
		return &result, nil
	}
	if global != nil && !builtin {
		result := *global
		if result.Source == "" {
			result.Source = "global"
		}
		return &result, nil
	}
	return nil, nil
}

func safeOutputAuthorizationEnabled(config *SafeOutputsConfig) bool {
	if config == nil {
		return false
	}
	if config.Authorization != nil {
		return true
	}
	for _, requirement := range config.AuthorizationRequirements {
		if requirement != nil {
			return true
		}
	}
	return false
}

func safeScriptAuthorizationStaged(script *SafeScriptConfig) *TemplatableBool {
	if script.Staged != nil {
		return script.Staged
	}
	if script.connectorOptions != nil {
		return script.connectorOptions.Staged
	}
	return nil
}

func safeOutputsAuthorizationNeedsOIDC(config *SafeOutputsConfig) bool {
	return safeOutputsAuthorizationNeedsOIDCForExecutor(config, true)
}

func safeOutputsAnyAuthorizationNeedsOIDC(config *SafeOutputsConfig) bool {
	return safeOutputsAuthorizationNeedsOIDCForExecutor(config, false)
}

func safeOutputsAuthorizationNeedsOIDCForExecutor(config *SafeOutputsConfig, consolidated bool) bool {
	if config == nil || templatableBoolIsTrue(config.Staged) {
		return false
	}
	for _, handler := range safeOutputHandlers {
		if handler.ToolName == "dispatch_repository" {
			continue
		}
		if consolidated && (handler.ToolName == "upload_asset" || handler.ToolName == "upload_code_coverage") {
			continue
		}
		if config.AuthorizationRequirements[handler.ToolName] != nil && isSafeOutputHandlerEnabledAndUnstaged(config, handler.StructField) {
			return true
		}
	}
	for name, script := range config.Scripts {
		if config.AuthorizationRequirements[stringutil.NormalizeSafeOutputIdentifier(name)] != nil && !templatableBoolIsTrue(safeScriptAuthorizationStaged(script)) {
			return true
		}
	}
	for name, action := range config.Actions {
		if config.AuthorizationRequirements[stringutil.NormalizeSafeOutputIdentifier(name)] != nil && !templatableBoolIsTrue(action.Staged) {
			return true
		}
	}
	if !consolidated {
		for name, job := range config.Jobs {
			if config.AuthorizationRequirements[stringutil.NormalizeSafeOutputIdentifier(name)] != nil && !templatableBoolIsTrue(job.Staged) {
				return true
			}
		}
	}
	if config.DispatchRepository != nil {
		for name, tool := range config.DispatchRepository.Tools {
			if config.AuthorizationRequirements[stringutil.NormalizeSafeOutputIdentifier(name)] != nil && !templatableBoolIsTrue(tool.Staged) {
				return true
			}
		}
	}
	for _, name := range []string{commentMemoryHandlerKey, ledgerMutationHandlerKey, "ledger_append", ledgerRequestCompactionHandlerKey} {
		if config.AuthorizationRequirements[name] != nil && config.AuthorizationCoverage[name] != "staged" {
			return true
		}
	}
	return false
}

func safeOutputAuthorizationExecutor(name string) string {
	switch name {
	case "upload_asset", "upload_artifact", "upload_code_coverage":
		return "standalone"
	case "call_workflow":
		return "workflow"
	default:
		return "manager"
	}
}

func addSafeOutputAuthorizationRuntimeConfig(target map[string]any, config *SafeOutputsConfig) {
	if config == nil {
		return
	}
	if config.Authorization != nil {
		target["authorization"] = config.Authorization
	}
	for _, handler := range safeOutputHandlers {
		if entry, ok := target[handler.ToolName].(map[string]any); ok && safeOutputAuthorizationEnabled(config) {
			entry["builtin"] = handler.Builtin
			entry["authorization_executor"] = safeOutputAuthorizationExecutor(handler.ToolName)
			if requirement := config.AuthorizationRequirements[handler.ToolName]; requirement != nil {
				entry["authorization"] = requirement
			}
		}
	}
	for name, script := range config.Scripts {
		addCustomAuthorizationRuntimeConfig(target, config, name, "script", safeScriptAuthorizationStaged(script))
	}
	for name, action := range config.Actions {
		addCustomAuthorizationRuntimeConfig(target, config, name, "action", action.Staged)
	}
	for name, job := range config.Jobs {
		addCustomAuthorizationRuntimeConfig(target, config, name, "job", job.Staged)
	}
	if dispatch, ok := target["dispatch_repository"].(map[string]any); ok && config.DispatchRepository != nil && safeOutputAuthorizationEnabled(config) {
		if tools, ok := dispatch["tools"].(map[string]any); ok {
			for name := range config.DispatchRepository.Tools {
				if entry, ok := tools[name].(map[string]any); ok {
					entry["builtin"] = false
					entry["authorization_executor"] = "manager"
					if requirement := config.AuthorizationRequirements[stringutil.NormalizeSafeOutputIdentifier(name)]; requirement != nil {
						entry["authorization"] = requirement
					}
				}
			}
		}
	}
	addWorkflowLaunchAuthorizationConfig(target, config)
	for _, name := range []string{commentMemoryHandlerKey, ledgerMutationHandlerKey, "ledger_append", ledgerRequestCompactionHandlerKey} {
		if entry, ok := target[name].(map[string]any); ok && config.Authorization != nil {
			requirement := *config.Authorization
			requirement.Source = "global"
			entry["authorization"] = &requirement
			entry["builtin"] = false
			entry["authorization_executor"] = "manager"
		}
	}
}

func addWorkflowLaunchAuthorizationConfig(target map[string]any, config *SafeOutputsConfig) {
	if entry, ok := target["call_workflow"].(map[string]any); ok && config.CallWorkflow != nil && config.AuthorizationRequirements["call_workflow"] != nil {
		implementations := make(map[string]any)
		for _, name := range config.CallWorkflow.Workflows {
			path := config.CallWorkflow.WorkflowFiles[name]
			if path == "" {
				path = "./.github/workflows/" + name + ".lock.yml"
			}
			implementations[name] = map[string]any{
				"path":          path,
				"revision":      "${{ github.workflow_sha }}",
				"repository_id": "${{ github.repository_id }}",
			}
		}
		entry["workflow-implementations"] = implementations
	}
}

func addCustomAuthorizationRuntimeConfig(target map[string]any, config *SafeOutputsConfig, name, executor string, staged *TemplatableBool) {
	if !safeOutputAuthorizationEnabled(config) && staged == nil {
		return
	}
	normalized := stringutil.NormalizeSafeOutputIdentifier(name)
	entry, ok := target[name].(map[string]any)
	if !ok {
		entry, ok = target[normalized].(map[string]any)
		if !ok {
			entry = make(map[string]any)
			target[normalized] = entry
		}
	}
	entry["builtin"] = false
	entry["authorization_executor"] = executor
	if requirement := config.AuthorizationRequirements[normalized]; requirement != nil {
		entry["authorization"] = requirement
	}
	if staged != nil {
		entry["staged"] = authorizationStagedValue(staged)
	}
}

func authorizationStagedValue(staged *TemplatableBool) any {
	if staged == nil {
		return false
	}
	if isExpression(string(*staged)) {
		return "${{ format('{0}', " + stripExpressionWrapper(string(*staged)) + ") == 'true' }}"
	}
	return templatableBoolIsTrue(staged)
}

func authorizationImplementationDigest(value any) (string, error) {
	data, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("cannot serialize safe-output authorization implementation: %w", err)
	}
	digest := sha256.Sum256(data)
	return hex.EncodeToString(digest[:]), nil
}

func authorizationGateConfig(config *SafeOutputsConfig, name, executor string, implementation any, staged *TemplatableBool, phase string) (string, error) {
	normalized := stringutil.NormalizeSafeOutputIdentifier(name)
	digest, err := authorizationImplementationDigest(implementation)
	if err != nil {
		return "", err
	}
	value := map[string]any{
		"type": normalized, "instance": normalized, "executor": executor,
		"implementation": digest,
		"authorization":  config.AuthorizationRequirements[normalized],
		"staged":         false,
	}
	if staged != nil {
		value["staged"] = authorizationStagedValue(staged)
	}
	if templatableBoolIsTrue(config.Staged) {
		value["staged"] = true
	}
	if phase != "" {
		value["instance"] = normalized + ":" + phase
	}
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("cannot serialize safe-output authorization gate: %w", err)
	}
	return string(encoded), nil
}

type authorizationGateOptions struct {
	Payload      string
	OutputPath   string
	Condition    string
	Phase        string
	ArtifactRoot string
}

func (c *Compiler) buildAuthorizationGateStep(data *WorkflowData, name, executor string, implementation any, staged *TemplatableBool, options authorizationGateOptions) ([]string, error) {
	normalized := stringutil.NormalizeSafeOutputIdentifier(name)
	configuration, err := authorizationGateConfig(data.SafeOutputs, name, executor, implementation, staged, options.Phase)
	if err != nil {
		return nil, err
	}
	env := []string{
		fmt.Sprintf("          GH_AW_SAFE_OUTPUT_AUTHORIZATION_GATE: %q\n", configuration),
		fmt.Sprintf("          GH_AW_AUTHORIZATION_WORKFLOW_HASH: %q\n", data.FrontmatterHash+":"+data.BodyHash),
	}
	defaultEnv := safeOutputAuthorizationDefaultEnv(data.SafeOutputs)
	for _, name := range []string{compilerenv.DefaultSafeOutputAuthorizationURL, compilerenv.DefaultSafeOutputAuthorizationAudience} {
		if value, ok := defaultEnv[name]; ok {
			env = append(env, fmt.Sprintf("          %s: %s\n", name, value))
		}
	}
	if options.Payload != "" {
		env = append(env, "          GH_AW_AUTHORIZATION_PAYLOAD: "+options.Payload+"\n")
	}
	if options.OutputPath != "" {
		env = append(env, fmt.Sprintf("          GH_AW_AGENT_OUTPUT: %s\n", options.OutputPath))
	}
	if options.ArtifactRoot != "" {
		env = append(env, fmt.Sprintf("          GH_AW_AUTHORIZATION_ARTIFACT_ROOT: %s\n", options.ArtifactRoot))
	}
	if options.Phase == "prepare_claim_adapter" {
		env = append(env, "          GH_AW_AUTHORIZATION_CLAIM_HANDLE: ${{ steps.claim_adapter_context.outputs.claim_handle }}\n")
	}
	step := []string{
		"      - name: Authorize " + normalized + "\n",
		"        id: authorize_" + normalized + "\n",
	}
	if options.Condition != "" {
		step = append(step, "        if: "+options.Condition+"\n")
	}
	step = append(step,
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/github-script")),
		"        env:\n",
	)
	step = append(step, env...)
	step = append(step,
		"        with:\n",
		"          script: |\n",
		"            const { setupGlobals } = require(`${process.env.RUNNER_TEMP}/gh-aw/actions/setup_globals.cjs`);\n",
		"            setupGlobals(core, github, context, exec, io, getOctokit);\n",
		"            await require(`${process.env.RUNNER_TEMP}/gh-aw/actions/safe_output_authorization_gate.cjs`).main({core, context});\n",
	)
	return step, nil
}

func (c *Compiler) buildAuthorizationEvidenceUploadStep(data *WorkflowData, name string) []string {
	return []string{
		"      - name: Upload safe-output authorization evidence\n",
		"        if: always()\n",
		fmt.Sprintf("        uses: %s\n", c.getActionPin("actions/upload-artifact")),
		"        with:\n",
		fmt.Sprintf("          name: %ssafe-output-authorization-%s\n", artifactPrefixExprForAgentDownstreamJob(data), stringutil.NormalizeSafeOutputIdentifier(name)),
		"          path: ${{ runner.temp }}/gh-aw/safe-output-authorization.jsonl\n",
		"          if-no-files-found: ignore\n",
		"          retention-days: 1\n",
	}
}
