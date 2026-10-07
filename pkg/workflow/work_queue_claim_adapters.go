package workflow

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
)

var workQueueAdapterFieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*$`)
var workQueueAdapterVerifierPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.:-]{0,127}$`)
var workQueueAdapterNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z_0-9]*$`)
var workQueueAdapterProjectionPattern = regexp.MustCompile(`^[A-Za-z][A-Za-z_0-9]*(\.[A-Za-z][A-Za-z_0-9]*){0,7}$`)
var workQueueAdapterBranchPattern = regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)
var workQueueAdapterRestRoutePattern = regexp.MustCompile(`^/repos/\{owner\}/\{repo\}/[A-Za-z0-9_{}./-]+$`)
var workQueueAdapterRestFieldPattern = regexp.MustCompile(`\{([A-Za-z_][A-Za-z_0-9]*)\}`)

// WorkQueueClaimAdapter binds custom output semantics to a trusted, independently
// verified effect. Delegated code prepares data; only the guarded handler applies it.
type WorkQueueClaimAdapter struct {
	Mode       string                 `json:"mode" yaml:"mode"`
	EffectType string                 `json:"effect-type" yaml:"effect-type"`
	TargetRepo string                 `json:"target-repo" yaml:"target-repo"`
	VerifierID string                 `json:"verifier-id,omitempty" yaml:"verifier-id,omitempty"`
	FieldMap   map[string]string      `json:"field-map,omitempty" yaml:"field-map,omitempty"`
	Expected   map[string]any         `json:"expected,omitempty" yaml:"expected,omitempty"`
	Request    *WorkQueueRestRequest  `json:"request,omitempty" yaml:"request,omitempty"`
	Verifier   *WorkQueueRestVerifier `json:"verifier,omitempty" yaml:"verifier,omitempty"`
	GitTree    *WorkQueueGitTree      `json:"git-tree,omitempty" yaml:"git-tree,omitempty"`
	GraphQL    *WorkQueueGraphQL      `json:"graphql,omitempty" yaml:"graphql,omitempty"`
}

type WorkQueueGraphQL struct {
	Mutation        string            `json:"mutation" yaml:"mutation"`
	InputType       string            `json:"input-type" yaml:"input-type"`
	ResponseField   string            `json:"response-field" yaml:"response-field"`
	ResourceType    string            `json:"resource-type" yaml:"resource-type"`
	ResourceKind    string            `json:"resource-kind" yaml:"resource-kind"`
	RepositoryField string            `json:"repository-field" yaml:"repository-field"`
	RepositoryInput string            `json:"repository-input" yaml:"repository-input"`
	Permission      string            `json:"permission" yaml:"permission"`
	Fields          map[string]string `json:"fields" yaml:"fields"`
	NumberField     string            `json:"number-field,omitempty" yaml:"number-field,omitempty"`
}

type WorkQueueGitTree struct {
	BaseRevision string `json:"base-revision" yaml:"base-revision"`
	BranchPrefix string `json:"branch-prefix" yaml:"branch-prefix"`
	PullRequest  bool   `json:"pull-request,omitempty" yaml:"pull-request,omitempty"`
	BaseBranch   string `json:"base-branch,omitempty" yaml:"base-branch,omitempty"`
}

type WorkQueueRestRequest struct {
	Method     string `json:"method" yaml:"method"`
	Route      string `json:"route" yaml:"route"`
	Permission string `json:"permission" yaml:"permission"`
}

type WorkQueueRestVerifier struct {
	Route        string            `json:"route" yaml:"route"`
	Fields       map[string]string `json:"fields" yaml:"fields"`
	ResourceKind string            `json:"resource-kind" yaml:"resource-kind"`
	NumberField  string            `json:"number-field,omitempty" yaml:"number-field,omitempty"`
}

func parseWorkQueueClaimAdapters(value any) (map[string]*WorkQueueClaimAdapter, error) {
	raw, ok := value.(map[string]any)
	if !ok || raw == nil {
		return nil, errors.New("safe-outputs.claim-adapters requires an object")
	}
	adapters := make(map[string]*WorkQueueClaimAdapter, len(raw))
	for name, value := range raw {
		fields, ok := value.(map[string]any)
		if !ok || fields == nil {
			return nil, fmt.Errorf("safe-outputs.claim-adapters.%s requires an object", name)
		}
		encoded, err := json.Marshal(fields)
		if err != nil {
			return nil, fmt.Errorf("safe-outputs.claim-adapters.%s: %w", name, err)
		}
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.UseNumber()
		decoder.DisallowUnknownFields()
		adapter := &WorkQueueClaimAdapter{}
		if err := decoder.Decode(adapter); err != nil {
			return nil, fmt.Errorf("safe-outputs.claim-adapters.%s: %w", name, err)
		}
		normalized := stringutil.NormalizeSafeOutputIdentifier(name)
		if _, exists := adapters[normalized]; exists {
			return nil, fmt.Errorf("safe-outputs.claim-adapters.%s conflicts with another normalized adapter name", name)
		}
		adapters[normalized] = adapter
	}
	return adapters, nil
}

func validateWorkQueueClaimAdapters(data *WorkflowData) error {
	if data.SafeOutputs == nil {
		return nil
	}
	builtinFields := map[string][]string{
		"create_issue":  {"title", "body", "labels", "assignees", "milestone"},
		"update_issue":  {"title", "body", "labels", "assignees", "milestone", "state", "status", "state_reason"},
		"close_issue":   {"state_reason"},
		"add_comment":   {"body"},
		"add_labels":    {"labels"},
		"remove_labels": {"labels"},
		"replace_label": {"label_to_add", "label_to_remove"},
	}
	nativeVerifiers := map[string]bool{
		"create_issue":  data.SafeOutputs.CreateIssues != nil,
		"update_issue":  data.SafeOutputs.UpdateIssues != nil,
		"close_issue":   data.SafeOutputs.CloseIssues != nil,
		"add_comment":   data.SafeOutputs.AddComments != nil,
		"add_labels":    data.SafeOutputs.AddLabels != nil,
		"remove_labels": data.SafeOutputs.RemoveLabels != nil,
		"replace_label": data.SafeOutputs.ReplaceLabel != nil,
	}
	verifierIDs := make(map[string]string)
	for name, adapter := range data.SafeOutputs.ClaimAdapters {
		if err := validateWorkQueueClaimAdapterIdentity(name, adapter, data.SafeOutputs, nativeVerifiers, verifierIDs); err != nil {
			return err
		}
		if err := validateWorkQueueClaimAdapterEffects(name, adapter, builtinFields[adapter.EffectType]); err != nil {
			return err
		}
	}
	return validateWorkQueuePreparedAdapters(data.SafeOutputs)
}

func validateWorkQueueClaimAdapterIdentity(name string, adapter *WorkQueueClaimAdapter, outputs *SafeOutputsConfig, nativeVerifiers map[string]bool, verifierIDs map[string]string) error {
	effects := []string{"create_issue", "update_issue", "close_issue", "add_comment", "add_labels", "remove_labels", "replace_label", "github_rest", "git_tree", "github_graphql"}
	if adapter == nil || !slices.Contains([]string{"prepared", "script"}, adapter.Mode) || !slices.Contains(effects, adapter.EffectType) {
		return fmt.Errorf("work-queue: claim-adapters.%s requires a supported mode and independently verifiable effect-type", name)
	}
	if !repoSlugPattern.MatchString(adapter.TargetRepo) || strings.Contains(adapter.TargetRepo, "${{") {
		return fmt.Errorf("work-queue: claim-adapters.%s.target-repo requires a fixed approved repository", name)
	}
	if adapter.VerifierID != "" && !workQueueAdapterVerifierPattern.MatchString(adapter.VerifierID) {
		return fmt.Errorf("work-queue: claim-adapters.%s.verifier-id requires a bounded independently verified native effect identifier", name)
	}
	id := adapter.VerifierID
	if id == "" {
		id = stringutil.NormalizeSafeOutputIdentifier(name)
	}
	if nativeVerifiers[id] && name != id && outputs.ClaimAdapters[id] == nil {
		return fmt.Errorf("work-queue: claim-adapters.%s.verifier-id conflicts with the enabled native %s verifier", name, id)
	}
	if prior, exists := verifierIDs[id]; exists {
		return fmt.Errorf("work-queue: claim-adapters.%s.verifier-id conflicts with claim-adapters.%s; verifier identifiers must be unique", name, prior)
	}
	verifierIDs[id] = name
	return nil
}

func validateWorkQueueClaimAdapterEffects(name string, adapter *WorkQueueClaimAdapter, fields []string) error {
	if fields != nil && adapter.EffectType != "create_issue" {
		fields = append(slices.Clone(fields), "item_number", "issue_number", "pull_request_number")
	}
	if adapter.EffectType == "github_rest" {
		if err := validateWorkQueueRestAdapter(name, adapter); err != nil {
			return err
		}
	} else if adapter.Request != nil || adapter.Verifier != nil {
		return fmt.Errorf("work-queue: claim-adapters.%s REST verifier configuration requires github_rest effect-type", name)
	}
	if adapter.EffectType == "git_tree" {
		if err := validateWorkQueueGitTreeAdapter(name, adapter); err != nil {
			return err
		}
	} else if adapter.GitTree != nil {
		return fmt.Errorf("work-queue: claim-adapters.%s code verifier configuration requires git_tree effect-type", name)
	}
	if adapter.EffectType == "github_graphql" {
		if err := validateWorkQueueGraphQLAdapter(name, adapter); err != nil {
			return err
		}
	} else if adapter.GraphQL != nil {
		return fmt.Errorf("work-queue: claim-adapters.%s GraphQL verifier configuration requires github_graphql effect-type", name)
	}
	return validateWorkQueueClaimAdapterFieldMap(name, adapter, fields)
}

func validateWorkQueueClaimAdapterFieldMap(name string, adapter *WorkQueueClaimAdapter, fields []string) error {
	for destination, source := range adapter.FieldMap {
		if (!slices.Contains([]string{"github_rest", "git_tree", "github_graphql"}, adapter.EffectType) && !slices.Contains(fields, destination)) || !workQueueAdapterFieldPattern.MatchString(source) {
			return fmt.Errorf("work-queue: claim-adapters.%s.field-map contains an undeclared effect field", name)
		}
		for field := range adapter.FieldMap {
			if _, overlaps := adapter.Expected[field]; overlaps {
				return fmt.Errorf("work-queue: claim-adapters.%s cannot both map and fix the same effect field", name)
			}
		}
	}
	for field := range adapter.Expected {
		if !slices.Contains([]string{"github_rest", "git_tree", "github_graphql"}, adapter.EffectType) && !slices.Contains(fields, field) {
			return fmt.Errorf("work-queue: claim-adapters.%s.expected contains an undeclared effect field", name)
		}
	}
	return nil
}

func validateWorkQueuePreparedAdapters(outputs *SafeOutputsConfig) error {
	for _, validate := range []func(*SafeOutputsConfig) error{
		validateWorkQueueAdapterScripts, validateWorkQueueAdapterJobs,
		validateWorkQueueAdapterActions, validateWorkQueueAdapterRawSteps,
		validateWorkQueueAdapterExecutables,
	} {
		if err := validate(outputs); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkQueueAdapterScripts(outputs *SafeOutputsConfig) error {
	for name, script := range outputs.Scripts {
		adapter := outputs.ClaimAdapters[stringutil.NormalizeSafeOutputIdentifier(name)]
		if adapter == nil || adapter.Mode != "script" {
			return fmt.Errorf("work-queue: script %q requires a trusted per-Claim delivery adapter with mode script", name)
		}
		if strings.Contains(script.Script, "secrets.") {
			return fmt.Errorf("work-queue: prepared script %q cannot expose secret credentials", name)
		}
	}
	return nil
}

func validateWorkQueueAdapterJobs(outputs *SafeOutputsConfig) error {
	for name, job := range outputs.Jobs {
		adapter := outputs.ClaimAdapters[stringutil.NormalizeSafeOutputIdentifier(name)]
		if adapter == nil || adapter.Mode != "prepared" {
			return fmt.Errorf("work-queue: custom job %q requires a trusted per-Claim delivery adapter with mode prepared", name)
		}
		for permission, level := range job.Permissions {
			if level == "write" {
				return fmt.Errorf("work-queue: prepared job %q cannot obtain %s write permission; effects execute in the trusted Claim handler", name, permission)
			}
		}
		if job.GitHubToken != "" || strings.Contains(fmt.Sprint(job.RawPermissions), "write") || strings.Contains(fmt.Sprint(job.Env), "secrets.") || strings.Contains(fmt.Sprint(job.Steps), "secrets.") {
			return fmt.Errorf("work-queue: prepared job %q cannot expose write credentials or secrets to custom code", name)
		}
		if err := validateWorkQueueAdapterEnv(name, job.Env); err != nil {
			return err
		}
		if err := validateWorkQueueAdapterSteps(name, job.Steps); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkQueueAdapterActions(outputs *SafeOutputsConfig) error {
	for name, action := range outputs.Actions {
		adapter := outputs.ClaimAdapters[stringutil.NormalizeSafeOutputIdentifier(name)]
		if adapter == nil || adapter.Mode != "prepared" {
			return fmt.Errorf("work-queue: custom action %q requires a trusted per-Claim delivery adapter with mode prepared", name)
		}
		if strings.Contains(fmt.Sprint(action.Env), "secrets.") || strings.Contains(fmt.Sprint(action.Inputs), "secrets.") {
			return fmt.Errorf("work-queue: prepared action %q cannot expose write credentials or secrets", name)
		}
		if err := validateWorkQueueAdapterEnv(name, action.Env); err != nil {
			return err
		}
		for _, input := range action.Inputs {
			if input != nil && strings.Contains(input.Default, "secrets.") {
				return fmt.Errorf("work-queue: prepared action %q cannot pass secret credentials as inputs", name)
			}
		}
	}
	return nil
}

func validateWorkQueueAdapterRawSteps(outputs *SafeOutputsConfig) error {
	if len(outputs.Steps) > 0 {
		if adapter := outputs.ClaimAdapters["raw_steps"]; adapter == nil || adapter.Mode != "prepared" {
			return errors.New("work-queue: raw safe-outputs.steps require a trusted per-Claim delivery adapter named raw_steps")
		}
		if strings.Contains(fmt.Sprint(outputs.Steps), "secrets.") {
			return errors.New("work-queue: prepared raw safe-output steps cannot expose write credentials or secrets")
		}
		if err := validateWorkQueueAdapterSteps("raw_steps", outputs.Steps); err != nil {
			return err
		}
	}
	return nil
}

func validateWorkQueueAdapterExecutables(outputs *SafeOutputsConfig) error {
	for name, adapter := range outputs.ClaimAdapters {
		matches := 0
		for rawName := range outputs.Jobs {
			if stringutil.NormalizeSafeOutputIdentifier(rawName) == name {
				matches++
			}
		}
		for rawName := range outputs.Actions {
			if stringutil.NormalizeSafeOutputIdentifier(rawName) == name {
				matches++
			}
		}
		for rawName := range outputs.Scripts {
			if stringutil.NormalizeSafeOutputIdentifier(rawName) == name {
				matches++
			}
		}
		if name == "raw_steps" && len(outputs.Steps) > 0 {
			matches++
		}
		if matches != 1 {
			return fmt.Errorf("work-queue: claim-adapters.%s requires exactly one matching custom executable", name)
		}
		if adapter.Mode == "script" {
			found := false
			for rawName := range outputs.Scripts {
				found = found || stringutil.NormalizeSafeOutputIdentifier(rawName) == name
			}
			if !found {
				return fmt.Errorf("work-queue: claim-adapters.%s mode script requires a matching script", name)
			}
		}
	}
	return nil
}

func validateWorkQueueGraphQLAdapter(name string, adapter *WorkQueueClaimAdapter) error {
	fail := func(reason string) error { return fmt.Errorf("work-queue: claim-adapters.%s: %s", name, reason) }
	config := adapter.GraphQL
	if config == nil {
		return fail("github_graphql requires a declared native operation and independent verifier")
	}
	reserved := []string{"owner", "repo", "repositoryId", "repositoryNameWithOwner", "query", "variables", "method", "url", "baseUrl", "headers", "request", "token", "auth", "data", "mediaType", "constructor", "prototype", "claim_handle", "claim_id", "work_id", "dispatch_id", "receipt_id"}
	for _, value := range []string{config.Mutation, config.InputType, config.ResponseField, config.ResourceType} {
		if len(value) > 128 || !workQueueAdapterNamePattern.MatchString(value) || slices.Contains(reserved, value) {
			return fail("GraphQL requires fixed native operation names")
		}
	}
	if !slices.Contains([]string{"none", "repositoryId", "repositoryNameWithOwner"}, config.RepositoryInput) ||
		!slices.Contains([]string{"checks", "contents", "issues", "pull-requests", "deployments", "discussions"}, config.Permission) ||
		!slices.Contains([]string{"unknown", "issue", "pull_request", "comment", "discussion", "repository"}, config.ResourceKind) {
		return fail("GraphQL requires explicit repository binding, native permission and resource kind")
	}
	fields := workQueueClaimAdapterFields(adapter)
	if len(fields) == 0 || len(fields) > 64 || len(config.Fields) != len(fields) {
		return fail("every GraphQL effect field requires independent readback")
	}
	for field := range fields {
		if !workQueueAdapterNamePattern.MatchString(field) || slices.Contains(reserved, field) || config.Fields[field] == "" {
			return fail("GraphQL contains an invalid, reserved or unverified effect field")
		}
	}
	paths := []string{"id", config.RepositoryField}
	if config.NumberField != "" {
		paths = append(paths, config.NumberField)
	}
	for field, observed := range config.Fields {
		if _, exists := fields[field]; !exists {
			return fail("GraphQL verifier contains an undeclared effect field")
		}
		paths = append(paths, observed)
	}
	for _, path := range paths {
		if len(path) > 256 || !workQueueAdapterProjectionPattern.MatchString(path) {
			return fail("GraphQL verifier requires bounded native field paths")
		}
		for component := range strings.SplitSeq(path, ".") {
			if slices.Contains(reserved, component) {
				return fail("GraphQL verifier contains a reserved field path")
			}
		}
		for _, other := range paths {
			if strings.HasPrefix(other, path+".") {
				return fail("GraphQL verifier contains conflicting scalar and object projections")
			}
		}
	}
	return nil
}

func validateWorkQueueGitTreeAdapter(name string, adapter *WorkQueueClaimAdapter) error {
	fail := func(reason string) error { return fmt.Errorf("work-queue: claim-adapters.%s: %s", name, reason) }
	config := adapter.GitTree
	if config == nil || !workQueueImmutableRevision.MatchString(config.BaseRevision) || len(config.BranchPrefix) > 128 || !workQueueAdapterBranchPattern.MatchString(config.BranchPrefix) {
		return fail("git_tree requires an immutable base revision and fixed branch namespace")
	}
	if config.PullRequest && (len(config.BaseBranch) > 128 || !workQueueAdapterBranchPattern.MatchString(config.BaseBranch)) {
		return fail("git_tree pull request requires a fixed base branch")
	}
	fields := workQueueClaimAdapterFields(adapter)
	_, files := fields["files"]
	_, title := fields["title"]
	if !files || (config.PullRequest && !title) {
		return fail("git_tree requires complete declared file and pull request fields")
	}
	for field := range fields {
		if field != "files" && (!config.PullRequest || !slices.Contains([]string{"title", "body"}, field)) {
			return fail("git_tree contains an undeclared code delivery field")
		}
	}
	return nil
}

func validateWorkQueueRestAdapter(name string, adapter *WorkQueueClaimAdapter) error {
	fail := func(reason string) error { return fmt.Errorf("work-queue: claim-adapters.%s: %s", name, reason) }
	if adapter.Request == nil || adapter.Verifier == nil || !slices.Contains([]string{"POST", "PUT", "PATCH"}, adapter.Request.Method) {
		return fail("github_rest requires an explicit mutation request and independent native verifier")
	}
	if !slices.Contains([]string{"checks", "contents", "issues", "pull-requests", "deployments", "discussions"}, adapter.Request.Permission) {
		return fail("REST adapter requires an explicit native write permission")
	}
	requestFields, err := workQueueRestRouteFields(name, adapter.Request.Route)
	if err != nil {
		return err
	}
	readFields, err := workQueueRestRouteFields(name, adapter.Verifier.Route)
	if err != nil {
		return err
	}
	if slices.Contains(requestFields, "receipt_id") || !slices.Contains(readFields, "receipt_id") {
		return fail("independent native readback must bind the exact private receipt_id")
	}
	if !slices.Contains([]string{"unknown", "issue", "pull_request", "comment", "release", "check_run", "deployment", "repository"}, adapter.Verifier.ResourceKind) {
		return fail("unsupported REST verifier resource-kind")
	}
	reserved := []string{"owner", "repo", "method", "url", "baseUrl", "headers", "request", "token", "auth", "data", "mediaType", "__proto__", "constructor", "prototype", "claim_handle", "claim_id", "work_id", "dispatch_id", "receipt_id"}
	fields := workQueueClaimAdapterFields(adapter)
	if len(fields) == 0 || len(fields) > 64 || len(adapter.Verifier.Fields) == 0 || len(adapter.Verifier.Fields) > 64 {
		return fail("REST adapter requires bounded declared fields and independent readback")
	}
	for field := range fields {
		if !workQueueAdapterFieldPattern.MatchString(field) || slices.Contains(reserved, field) {
			return fail("invalid or reserved REST effect field")
		}
		if !slices.Contains(requestFields, field) && adapter.Verifier.Fields[field] == "" {
			return fail("every REST effect field requires independent readback")
		}
	}
	for field, observed := range adapter.Verifier.Fields {
		if _, exists := fields[field]; !exists || !workQueueAdapterFieldPattern.MatchString(observed) || slices.Contains(reserved, observed) {
			return fail("invalid or undeclared REST field readback")
		}
	}
	for _, field := range append(requestFields, readFields...) {
		if _, exists := fields[field]; !slices.Contains([]string{"owner", "repo", "receipt_id"}, field) && !exists {
			return fail("REST route has an unbound selector")
		}
	}
	if adapter.Verifier.NumberField != "" && (!workQueueAdapterFieldPattern.MatchString(adapter.Verifier.NumberField) || slices.Contains(reserved, adapter.Verifier.NumberField)) {
		return fail("invalid REST resource number field")
	}
	return nil
}

func workQueueRestRouteFields(name, route string) ([]string, error) {
	fail := func(reason string) error { return fmt.Errorf("work-queue: claim-adapters.%s: %s", name, reason) }
	if !workQueueAdapterRestRoutePattern.MatchString(route) || strings.Contains(route, "..") || strings.Contains(route, "//") || strings.ContainsAny(workQueueAdapterRestFieldPattern.ReplaceAllString(route, ""), "{}") {
		return nil, fail("REST routes must be fixed repository-relative paths without origin overrides")
	}
	var fields []string
	for _, match := range workQueueAdapterRestFieldPattern.FindAllStringSubmatch(route, -1) {
		if len(match) != 2 {
			return nil, fail("REST route placeholder requires exactly one field")
		}
		for index, field := range match {
			if index == 1 {
				fields = append(fields, field)
			}
		}
	}
	return fields, nil
}

func workQueueClaimAdapterFields(adapter *WorkQueueClaimAdapter) map[string]struct{} {
	fields := make(map[string]struct{}, len(adapter.FieldMap)+len(adapter.Expected))
	for field := range adapter.FieldMap {
		fields[field] = struct{}{}
	}
	for field := range adapter.Expected {
		fields[field] = struct{}{}
	}
	return fields
}

func validateWorkQueueAdapterEnv(name string, env map[string]string) error {
	for key := range env {
		if strings.HasPrefix(key, "GH_AW_") || strings.HasPrefix(key, "GITHUB_") || strings.HasPrefix(key, "ACTIONS_") || key == "GH_TOKEN" {
			return fmt.Errorf("work-queue: prepared executable %q cannot override reserved credential or Claim context environment %s", name, key)
		}
	}
	return nil
}

func validateWorkQueueAdapterSteps(name string, steps []any) error {
	for _, raw := range steps {
		step, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		env := make(map[string]string)
		switch values := step["env"].(type) {
		case map[string]string:
			env = values
		case map[string]any:
			for key, value := range values {
				env[key] = fmt.Sprint(value)
			}
		}
		if err := validateWorkQueueAdapterEnv(name, env); err != nil {
			return err
		}
		if id, ok := step["id"].(string); ok && slices.Contains([]string{"claim_adapter_context", "claim_adapter_artifact", "redact_secrets", "setup"}, id) {
			return fmt.Errorf("work-queue: prepared executable %q cannot replace a trusted adapter step ID", name)
		}
	}
	return nil
}

func workQueuePreparedAdapterNames(data *WorkflowData) []string {
	if data == nil || data.SafeOutputs == nil || !isWorkQueueEnabled(data) {
		return nil
	}
	var names []string
	for name, adapter := range data.SafeOutputs.ClaimAdapters {
		if adapter.Mode == "prepared" || adapter.Mode == "script" {
			names = append(names, name)
		}
	}
	slices.Sort(names)
	return names
}

func addWorkQueueClaimAdapterRuntimeConfig(config map[string]any, data *WorkflowData) {
	if !isWorkQueueEnabled(data) || data.SafeOutputs == nil || len(data.SafeOutputs.ClaimAdapters) == 0 {
		return
	}
	config["claim_adapters"] = maps.Clone(data.SafeOutputs.ClaimAdapters)
}
