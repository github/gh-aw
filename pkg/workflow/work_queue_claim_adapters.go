package workflow

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/stringutil"
)

var workQueueAdapterFieldPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z_0-9]*$`)

// WorkQueueClaimAdapter binds custom output semantics to a trusted, independently
// verified effect. Delegated code prepares data; only the guarded handler applies it.
type WorkQueueClaimAdapter struct {
	Mode       string                 `json:"mode" yaml:"mode"`
	EffectType string                 `json:"effect-type" yaml:"effect-type"`
	TargetRepo string                 `json:"target-repo" yaml:"target-repo"`
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

func parseWorkQueueClaimAdapters(raw map[string]any) map[string]*WorkQueueClaimAdapter {
	adapters := make(map[string]*WorkQueueClaimAdapter, len(raw))
	for name, value := range raw {
		fields, ok := value.(map[string]any)
		if !ok {
			continue
		}
		adapter := &WorkQueueClaimAdapter{}
		adapter.Mode, _ = fields["mode"].(string)
		adapter.EffectType, _ = fields["effect-type"].(string)
		adapter.TargetRepo, _ = fields["target-repo"].(string)
		adapter.Expected, _ = fields["expected"].(map[string]any)
		if raw, ok := fields["graphql"].(map[string]any); ok {
			adapter.GraphQL = &WorkQueueGraphQL{}
			adapter.GraphQL.Mutation, _ = raw["mutation"].(string)
			adapter.GraphQL.InputType, _ = raw["input-type"].(string)
			adapter.GraphQL.ResponseField, _ = raw["response-field"].(string)
			adapter.GraphQL.ResourceType, _ = raw["resource-type"].(string)
			adapter.GraphQL.ResourceKind, _ = raw["resource-kind"].(string)
			adapter.GraphQL.RepositoryField, _ = raw["repository-field"].(string)
			adapter.GraphQL.RepositoryInput, _ = raw["repository-input"].(string)
			adapter.GraphQL.Permission, _ = raw["permission"].(string)
			adapter.GraphQL.NumberField, _ = raw["number-field"].(string)
			if fields, ok := raw["fields"].(map[string]any); ok {
				adapter.GraphQL.Fields = map[string]string{}
				for desired, observed := range fields {
					adapter.GraphQL.Fields[desired], _ = observed.(string)
				}
			}
		}
		if raw, ok := fields["git-tree"].(map[string]any); ok {
			adapter.GitTree = &WorkQueueGitTree{}
			adapter.GitTree.BaseRevision, _ = raw["base-revision"].(string)
			adapter.GitTree.BranchPrefix, _ = raw["branch-prefix"].(string)
			adapter.GitTree.PullRequest, _ = raw["pull-request"].(bool)
			adapter.GitTree.BaseBranch, _ = raw["base-branch"].(string)
		}
		if raw, ok := fields["request"].(map[string]any); ok {
			adapter.Request = &WorkQueueRestRequest{}
			adapter.Request.Method, _ = raw["method"].(string)
			adapter.Request.Route, _ = raw["route"].(string)
			adapter.Request.Permission, _ = raw["permission"].(string)
		}
		if raw, ok := fields["verifier"].(map[string]any); ok {
			adapter.Verifier = &WorkQueueRestVerifier{}
			adapter.Verifier.Route, _ = raw["route"].(string)
			adapter.Verifier.ResourceKind, _ = raw["resource-kind"].(string)
			adapter.Verifier.NumberField, _ = raw["number-field"].(string)
			if fields, ok := raw["fields"].(map[string]any); ok {
				adapter.Verifier.Fields = map[string]string{}
				for desired, observed := range fields {
					adapter.Verifier.Fields[desired], _ = observed.(string)
				}
			}
		}
		if mapping, ok := fields["field-map"].(map[string]any); ok {
			adapter.FieldMap = make(map[string]string, len(mapping))
			for destination, source := range mapping {
				adapter.FieldMap[destination], _ = source.(string)
			}
		}
		adapters[stringutil.NormalizeSafeOutputIdentifier(name)] = adapter
	}
	return adapters
}

func validateWorkQueueClaimAdapters(data *WorkflowData) error {
	if data.SafeOutputs == nil {
		return nil
	}
	effects := []string{"create_issue", "update_issue", "close_issue", "add_comment", "add_labels", "remove_labels", "replace_label", "github_rest", "git_tree", "github_graphql"}
	fields := []string{"title", "body", "labels", "assignees", "milestone", "state", "status", "state_reason", "item_number", "issue_number", "pull_request_number", "label_to_add", "label_to_remove"}
	for name, adapter := range data.SafeOutputs.ClaimAdapters {
		if adapter == nil || !slices.Contains([]string{"prepared", "script"}, adapter.Mode) || !slices.Contains(effects, adapter.EffectType) {
			return fmt.Errorf("work-queue: claim-adapters.%s requires a supported mode and independently verifiable effect-type", name)
		}
		if !repoSlugPattern.MatchString(adapter.TargetRepo) || strings.Contains(adapter.TargetRepo, "${{") {
			return fmt.Errorf("work-queue: claim-adapters.%s.target-repo requires a fixed approved repository", name)
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
	}
	for name, script := range data.SafeOutputs.Scripts {
		adapter := data.SafeOutputs.ClaimAdapters[stringutil.NormalizeSafeOutputIdentifier(name)]
		if adapter == nil || adapter.Mode != "script" {
			return fmt.Errorf("work-queue: script %q requires a trusted per-Claim delivery adapter with mode script", name)
		}
		if strings.Contains(script.Script, "secrets.") {
			return fmt.Errorf("work-queue: prepared script %q cannot expose secret credentials", name)
		}
	}
	for name, job := range data.SafeOutputs.Jobs {
		adapter := data.SafeOutputs.ClaimAdapters[stringutil.NormalizeSafeOutputIdentifier(name)]
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
	for name, action := range data.SafeOutputs.Actions {
		adapter := data.SafeOutputs.ClaimAdapters[stringutil.NormalizeSafeOutputIdentifier(name)]
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
			if input != nil && strings.Contains(fmt.Sprint(input.Default), "secrets.") {
				return fmt.Errorf("work-queue: prepared action %q cannot pass secret credentials as inputs", name)
			}
		}
	}
	if len(data.SafeOutputs.Steps) > 0 {
		if adapter := data.SafeOutputs.ClaimAdapters["raw_steps"]; adapter == nil || adapter.Mode != "prepared" {
			return fmt.Errorf("work-queue: raw safe-outputs.steps require a trusted per-Claim delivery adapter named raw_steps")
		}
		if strings.Contains(fmt.Sprint(data.SafeOutputs.Steps), "secrets.") {
			return fmt.Errorf("work-queue: prepared raw safe-output steps cannot expose write credentials or secrets")
		}
		if err := validateWorkQueueAdapterSteps("raw_steps", data.SafeOutputs.Steps); err != nil {
			return err
		}
	}
	for name, adapter := range data.SafeOutputs.ClaimAdapters {
		matches := 0
		for rawName := range data.SafeOutputs.Jobs {
			if stringutil.NormalizeSafeOutputIdentifier(rawName) == name {
				matches++
			}
		}
		for rawName := range data.SafeOutputs.Actions {
			if stringutil.NormalizeSafeOutputIdentifier(rawName) == name {
				matches++
			}
		}
		for rawName := range data.SafeOutputs.Scripts {
			if stringutil.NormalizeSafeOutputIdentifier(rawName) == name {
				matches++
			}
		}
		if name == "raw_steps" && len(data.SafeOutputs.Steps) > 0 {
			matches++
		}
		if matches != 1 {
			return fmt.Errorf("work-queue: claim-adapters.%s requires exactly one matching custom executable", name)
		}
		if adapter.Mode == "script" {
			found := false
			for rawName := range data.SafeOutputs.Scripts {
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
	namePattern := regexp.MustCompile(`^[A-Za-z][A-Za-z_0-9]*$`)
	pathPattern := regexp.MustCompile(`^[A-Za-z][A-Za-z_0-9]*(\.[A-Za-z][A-Za-z_0-9]*){0,7}$`)
	reserved := []string{"owner", "repo", "repositoryId", "repositoryNameWithOwner", "query", "variables", "method", "url", "baseUrl", "headers", "request", "token", "auth", "data", "mediaType", "constructor", "prototype", "claim_handle", "claim_id", "work_id", "dispatch_id", "receipt_id"}
	for _, value := range []string{config.Mutation, config.InputType, config.ResponseField, config.ResourceType} {
		if len(value) > 128 || !namePattern.MatchString(value) || slices.Contains(reserved, value) {
			return fail("GraphQL requires fixed native operation names")
		}
	}
	if !slices.Contains([]string{"none", "repositoryId", "repositoryNameWithOwner"}, config.RepositoryInput) ||
		!slices.Contains([]string{"checks", "contents", "issues", "pull-requests", "deployments", "discussions"}, config.Permission) ||
		!slices.Contains([]string{"unknown", "issue", "pull_request", "comment", "discussion", "repository"}, config.ResourceKind) {
		return fail("GraphQL requires explicit repository binding, native permission and resource kind")
	}
	fields := map[string]bool{}
	for field := range adapter.FieldMap {
		fields[field] = true
	}
	for field := range adapter.Expected {
		fields[field] = true
	}
	if len(fields) == 0 || len(fields) > 64 || len(config.Fields) != len(fields) {
		return fail("every GraphQL effect field requires independent readback")
	}
	for field := range fields {
		if !namePattern.MatchString(field) || slices.Contains(reserved, field) || config.Fields[field] == "" {
			return fail("GraphQL contains an invalid, reserved or unverified effect field")
		}
	}
	paths := []string{"id", config.RepositoryField}
	if config.NumberField != "" {
		paths = append(paths, config.NumberField)
	}
	for field, observed := range config.Fields {
		if !fields[field] {
			return fail("GraphQL verifier contains an undeclared effect field")
		}
		paths = append(paths, observed)
	}
	for _, path := range paths {
		if len(path) > 256 || !pathPattern.MatchString(path) {
			return fail("GraphQL verifier requires bounded native field paths")
		}
		for _, component := range strings.Split(path, ".") {
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
	branch := regexp.MustCompile(`^[A-Za-z0-9_-]+(/[A-Za-z0-9_-]+)*$`)
	revision := regexp.MustCompile(`^([a-f0-9]{40}|[a-f0-9]{64})$`)
	if config == nil || !revision.MatchString(config.BaseRevision) || len(config.BranchPrefix) > 128 || !branch.MatchString(config.BranchPrefix) {
		return fail("git_tree requires an immutable base revision and fixed branch namespace")
	}
	if config.PullRequest && (len(config.BaseBranch) > 128 || !branch.MatchString(config.BaseBranch)) {
		return fail("git_tree pull request requires a fixed base branch")
	}
	fields := map[string]bool{}
	for field := range adapter.FieldMap {
		fields[field] = true
	}
	for field := range adapter.Expected {
		fields[field] = true
	}
	if !fields["files"] || (config.PullRequest && !fields["title"]) {
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
	routePattern := regexp.MustCompile(`^/repos/\{owner\}/\{repo\}/[A-Za-z0-9_{}./-]+$`)
	placeholderPattern := regexp.MustCompile(`\{([A-Za-z_][A-Za-z_0-9]*)\}`)
	routeFields := func(route string) ([]string, error) {
		if !routePattern.MatchString(route) || strings.Contains(route, "..") || strings.Contains(route, "//") || strings.ContainsAny(placeholderPattern.ReplaceAllString(route, ""), "{}") {
			return nil, fail("REST routes must be fixed repository-relative paths without origin overrides")
		}
		var fields []string
		for _, match := range placeholderPattern.FindAllStringSubmatch(route, -1) {
			fields = append(fields, match[1])
		}
		return fields, nil
	}
	requestFields, err := routeFields(adapter.Request.Route)
	if err != nil {
		return err
	}
	readFields, err := routeFields(adapter.Verifier.Route)
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
	fields := map[string]bool{}
	for field := range adapter.FieldMap {
		fields[field] = true
	}
	for field := range adapter.Expected {
		fields[field] = true
	}
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
		if !fields[field] || !workQueueAdapterFieldPattern.MatchString(observed) || slices.Contains(reserved, observed) {
			return fail("invalid or undeclared REST field readback")
		}
	}
	for _, field := range append(requestFields, readFields...) {
		if !slices.Contains([]string{"owner", "repo", "receipt_id"}, field) && !fields[field] {
			return fail("REST route has an unbound selector")
		}
	}
	if adapter.Verifier.NumberField != "" && (!workQueueAdapterFieldPattern.MatchString(adapter.Verifier.NumberField) || slices.Contains(reserved, adapter.Verifier.NumberField)) {
		return fail("invalid REST resource number field")
	}
	return nil
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
		if id, _ := step["id"].(string); slices.Contains([]string{"claim_adapter_context", "claim_adapter_artifact", "redact_secrets", "setup"}, id) {
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
