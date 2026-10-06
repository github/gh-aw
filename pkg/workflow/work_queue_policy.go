package workflow

import (
	"bytes"
	"encoding/json"
	"fmt"
	"maps"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/github/gh-aw/pkg/workqueue"
)

var workQueueImmutableRevision = regexp.MustCompile(`^[a-f0-9]{40}$|^[a-f0-9]{64}$`)
var workQueueNativePrincipal = regexp.MustCompile(`^[1-9][0-9]{0,255}$`)

const workQueueWorkerRunName = `run-name: "${{ github.event_name == 'workflow_dispatch' && inputs.work_queue_assignment != '' && format('gh-aw work-queue {0}', fromJSON(inputs.work_queue_assignment).dispatch_id) || 'gh-aw work-queue unassigned' }}"`

// WorkQueuePolicyConfig is a proposal. Only the installed ledger Policy grants authority.
type WorkQueuePolicyConfig struct {
	Policy                 workqueue.Policy
	ForeignReadCredentials map[string]string
}

func validateWorkQueueConfiguration(data *WorkflowData) error {
	if data == nil {
		return nil
	}
	if data.SafeOutputs != nil && len(data.SafeOutputs.ClaimAdapters) > 0 && !isWorkQueueWorker(data) {
		return fmt.Errorf("safe-outputs.claim-adapters require a declared work-queue worker; unassigned dispatchers cannot delegate ordinary effects")
	}
	value, configured := data.Tools["work-queue"]
	if configured && value == false {
		return fmt.Errorf("tools.work-queue: scheduling cannot be disabled; remove the tool to use an ordinary workflow")
	}
	if !isWorkQueueEnabled(data) {
		if _, exists := data.RawFrontmatter["work-queue-policy"]; exists {
			return fmt.Errorf("work-queue-policy requires tools.work-queue")
		}
		return nil
	}
	if workQueueStorage(data) != "git" {
		return fmt.Errorf("tools.work-queue.storage: only the version-3 Git backend is supported; Issues storage is unsupported")
	}
	if isWorkQueueWorker(data) {
		if _, configured := data.RawFrontmatter["run-name"]; configured {
			return fmt.Errorf("work-queue: worker run-name is reserved for immutable dispatch correlation; remove run-name from frontmatter")
		}
		data.RunName = workQueueWorkerRunName
	}
	if data.StatusComment != nil && *data.StatusComment {
		return fmt.Errorf("work-queue: status-comment writes are unscoped; use a Claim-scoped add-comment output")
	}
	if config, ok := value.(map[string]any); ok {
		if required, exists := config["require-assignment"]; exists && required == false && isWorkQueueWorker(data) {
			return fmt.Errorf("tools.work-queue.require-assignment: queue workers cannot disable immutable assignment validation")
		}
		if isWorkQueueWorker(data) && data.RawFrontmatter != nil {
			if on, exists := data.RawFrontmatter["on"]; exists && !containsWorkflowDispatch(on) {
				return fmt.Errorf("work-queue: workers require workflow_dispatch for immutable version-3 run binding")
			}
		}
	}
	config, err := parseWorkQueuePolicy(data)
	if err != nil {
		return err
	}
	data.WorkQueuePolicy = config
	// Delegated effects require explicit trusted adapters and independent delivery evidence.
	if data.SafeOutputs != nil {
		for name, builder := range handlerRegistry {
			if (strings.HasPrefix(name, "linear_") || strings.HasPrefix(name, "jira_") || strings.HasPrefix(name, "ado_")) && builder(data.SafeOutputs) != nil {
				return fmt.Errorf("work-queue: external handler %q requires a trusted per-Claim target and delivery adapter", name)
			}
		}
		if err := validateWorkQueueClaimAdapters(data); err != nil {
			return err
		}
		if data.SafeOutputs.UploadCodeCoverage != nil {
			target := data.SafeOutputs.UploadCodeCoverage.TargetRef
			if target == "" || !strings.HasPrefix(target, "refs/heads/") && !strings.HasPrefix(target, "refs/tags/") || strings.Contains(target, "${{") {
				return fmt.Errorf("work-queue: upload-code-coverage requires a trusted per-Claim delivery adapter target-ref independently resolved to the immutable worker revision")
			}
		}
		if data.SafeOutputs.CreateCodeScanningAlerts != nil {
			target := data.SafeOutputs.CreateCodeScanningAlerts.TargetRef
			if target == "" || !strings.HasPrefix(target, "refs/heads/") && !strings.HasPrefix(target, "refs/tags/") || strings.Contains(target, "${{") {
				return fmt.Errorf("work-queue: create-code-scanning-alert requires a trusted per-Claim delivery adapter target-ref independently resolved to the immutable worker revision")
			}
		}
		if data.SafeOutputs.CallWorkflow != nil || data.SafeOutputs.DispatchRepository != nil {
			return fmt.Errorf("work-queue: delegated workflow/repository dispatch requires a trusted per-Claim delivery adapter; use work_queue_dispatch_next")
		}
	}
	if data.LedgerConfig != nil || data.RepoMemoryConfig != nil || data.DriveMemoryConfig != nil {
		return fmt.Errorf("work-queue: standalone persistent ledger/repository/drive writes require a trusted per-Claim delivery adapter")
	}
	return nil
}

func parseWorkQueuePolicy(data *WorkflowData) (*WorkQueuePolicyConfig, error) {
	defaults := workqueue.DefaultPolicy("${{ github.actor_id }}", "${{ github.repository }}")
	result := &WorkQueuePolicyConfig{Policy: defaults, ForeignReadCredentials: map[string]string{}}
	raw, exists := data.RawFrontmatter["work-queue-policy"]
	if !exists {
		return result, nil
	}
	block, ok := raw.(map[string]any)
	if !ok {
		return nil, fmt.Errorf("work-queue-policy must be an object; scheduling cannot be disabled")
	}
	allowed := []string{"mode", "class-weights", "accounting-weights", "producers", "pools", "limits", "worker-profiles", "outstanding", "dependencies"}
	for key := range block {
		if !slices.Contains(allowed, key) {
			return nil, fmt.Errorf("work-queue-policy: unsupported field %q", key)
		}
	}
	base := map[string]any{}
	for _, key := range []string{"mode", "class-weights", "accounting-weights", "producers", "pools", "limits"} {
		if value, exists := block[key]; exists {
			base[key] = value
		}
	}
	encoded, err := json.Marshal(normalizeWorkQueuePolicyKeys(base))
	if err != nil {
		return nil, fmt.Errorf("work-queue-policy: %w", err)
	}
	_, explicitPools := block["pools"]
	_, explicitProfiles := block["worker-profiles"]
	if explicitPools {
		result.Policy.Pools = map[string]workqueue.PoolPolicy{}
	}
	if _, explicit := block["producers"]; explicit {
		result.Policy.Producers = map[string]workqueue.ProducerRule{}
	}
	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&result.Policy); err != nil {
		return nil, fmt.Errorf("work-queue-policy: invalid policy proposal: %w", err)
	}
	if result.Policy.Mode != "weighted-priority" && result.Policy.Mode != "strict-priority" {
		return nil, fmt.Errorf("work-queue-policy.mode must be weighted-priority or strict-priority; scheduling cannot be disabled")
	}
	if len(result.Policy.ClassWeights) != 5 {
		return nil, fmt.Errorf("work-queue-policy.class-weights requires exactly five positive weights")
	}
	for _, weight := range result.Policy.ClassWeights {
		if weight < 1 || weight > 1000 {
			return nil, fmt.Errorf("work-queue-policy weights must be integers from 1 to 1000")
		}
	}
	if result.Policy.AccountingWeights[""] != 1 || len(result.Policy.AccountingWeights) > 1024 {
		return nil, fmt.Errorf("work-queue-policy.accounting-weights requires default key weight 1 and at most 1024 keys")
	}
	for key, weight := range result.Policy.AccountingWeights {
		if len(key) > 128 || weight < 1 || weight > 1000 {
			return nil, fmt.Errorf("work-queue-policy.accounting-weights: keys must be at most 128 bytes and weights 1 to 1000")
		}
	}
	pool := result.Policy.Pools["default"]
	if profiles, exists := block["worker-profiles"]; exists {
		if _, explicit := block["pools"]; explicit {
			return nil, fmt.Errorf("work-queue-policy: worker-profiles shorthand cannot be combined with pools")
		}
		encoded, _ := json.Marshal(normalizeWorkQueuePolicyValue(profiles, true))
		decoder := json.NewDecoder(bytes.NewReader(encoded))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&pool.Profiles); err != nil {
			return nil, fmt.Errorf("work-queue-policy.worker-profiles: %w", err)
		}
		if err := validateConfiguredWorkQueueProfileBounds(profiles); err != nil {
			return nil, err
		}
	}
	if pools, ok := block["pools"].(map[string]any); ok {
		for name, value := range pools {
			if configured, ok := value.(map[string]any); ok {
				if _, supplied := configured["per-account-limit"]; supplied && result.Policy.Pools[name].PerAccountLimit < 1 {
					return nil, fmt.Errorf("work-queue-policy.pools.%s.per-account-limit must be positive when configured; omit it for no per-account cap", name)
				}
				if err := validateConfiguredWorkQueueProfileBounds(configured["profiles"]); err != nil {
					return nil, err
				}
			}
		}
	}
	if outstanding, ok := block["outstanding"].(map[string]any); ok {
		encoded, _ := json.Marshal(outstanding)
		var limits struct {
			Claims     int `json:"claims"`
			PerAccount int `json:"per-account-claims"`
			Dispatches int `json:"dispatches"`
		}
		if err := json.Unmarshal(encoded, &limits); err != nil {
			return nil, fmt.Errorf("work-queue-policy.outstanding: %w", err)
		}
		if _, supplied := outstanding["claims"]; supplied {
			pool.LogicalLimit = limits.Claims
		}
		if _, supplied := outstanding["dispatches"]; supplied {
			pool.NativeLimit = limits.Dispatches
		}
		if _, supplied := outstanding["per-account-claims"]; supplied && limits.PerAccount < 1 {
			return nil, fmt.Errorf("work-queue-policy.outstanding.per-account-claims must be positive when configured; omit it for no per-account cap")
		}
		pool.PerAccountLimit = limits.PerAccount
	}
	if dependencies, ok := block["dependencies"].(map[string]any); ok {
		if duration, ok := dependencies["max-observation-age"].(string); ok {
			age, err := time.ParseDuration(duration)
			if err != nil || age.Milliseconds() < 1 || age > time.Hour {
				return nil, fmt.Errorf("work-queue-policy.dependencies.max-observation-age must be between 1ms and 1h")
			}
			pool.MaxObservationAgeMS = age.Milliseconds()
		}
		if repositories, ok := dependencies["repositories"].([]any); ok {
			for _, repository := range repositories {
				slug, ok := repository.(string)
				if !ok || strings.Contains(slug, "${{") {
					return nil, fmt.Errorf("work-queue-policy.dependencies.repositories requires literal repository slugs")
				}
				if _, _, ok := parseRepoSlugLiteral(slug); !ok {
					return nil, fmt.Errorf("work-queue-policy.dependencies.repositories: invalid repository %q", slug)
				}
				pool.AllowedRepositories = append(pool.AllowedRepositories, slug)
			}
		}
		if credentials, ok := dependencies["read-credentials"].(map[string]any); ok {
			if len(credentials) > 64 {
				return nil, fmt.Errorf("work-queue-policy.dependencies.read-credentials: at most 64 separate repository bindings are supported")
			}
			seen := make(map[string]bool, len(credentials))
			for repository, value := range credentials {
				token, ok := value.(string)
				if !ok || !SecretsExpressionPattern.MatchString(token) || !repoSlugPattern.MatchString(repository) {
					return nil, fmt.Errorf("work-queue-policy.dependencies.read-credentials: bind %q to a separate secrets expression", repository)
				}
				if seen[strings.ToLower(repository)] {
					return nil, fmt.Errorf("work-queue-policy.dependencies.read-credentials: repository %q has conflicting case-insensitive bindings", repository)
				}
				seen[strings.ToLower(repository)] = true
				result.ForeignReadCredentials[repository] = token
			}
		}
		for _, repository := range pool.AllowedRepositories {
			if repository != "${{ github.repository }}" && result.ForeignReadCredentials[repository] == "" {
				return nil, fmt.Errorf("work-queue-policy.dependencies: foreign repository %q requires a separately bound read credential", repository)
			}
		}
	}
	if !explicitPools {
		result.Policy.Pools["default"] = pool
	}
	for poolName, pool := range result.Policy.Pools {
		if pool.MaxObservationAgeMS == 0 {
			pool.MaxObservationAgeMS = 60000
		}
		for _, repository := range pool.AllowedRepositories {
			if repository != "${{ github.repository }}" && result.ForeignReadCredentials[repository] == "" {
				return nil, fmt.Errorf("work-queue-policy.pools.%s: foreign dependency repository %q requires a separately bound read credential", poolName, repository)
			}
		}
		if pool.Retry.MaxAttempts == 0 {
			pool.Retry = workqueue.RetryPolicy{MaxAttempts: 3, BackoffMS: 30000}
		}
		if pool.Reconciliation.MaxAttempts == 0 {
			pool.Reconciliation = workqueue.ReconciliationPolicy{MaxAttempts: 5, DeadlineMS: 300000}
		}
		if pool.LogicalLimit < 1 || pool.LogicalLimit > 4096 || pool.NativeLimit < 1 || pool.NativeLimit > 4096 || pool.PerAccountLimit < 0 || pool.PerAccountLimit > 4096 {
			return nil, fmt.Errorf("work-queue-policy.pools.%s: capacity bounds must be 1 to 4096", poolName)
		}
		if _, exists := pool.Profiles[pool.DefaultProfile]; !exists {
			return nil, fmt.Errorf("work-queue-policy.pools.%s.default-profile must name an approved profile", poolName)
		}
		for profileName, profile := range pool.Profiles {
			if profile.MaxClaims == 0 {
				profile.MaxClaims = 1
			}
			if profile.MaxClaims < 1 || profile.MaxClaims > 16 {
				return nil, fmt.Errorf("work-queue-policy worker profile %q: max-claims-per-dispatch must be 1 to 16", profileName)
			}
			if profile.Workflow == "" || profile.Principal == "" || profile.TrustDomain == "" || profile.CredentialScope == "" || profile.EffectScope == "" {
				return nil, fmt.Errorf("work-queue-policy worker profile %q requires complete workflow, principal, trust-domain, credential-scope and effect-scope", profileName)
			}
			if (explicitPools || explicitProfiles) && !workQueueImmutableRevision.MatchString(profile.Ref) {
				return nil, fmt.Errorf("work-queue-policy worker profile %q: ref must be an immutable lowercase 40- or 64-character Git revision", profileName)
			}
			if (explicitPools || explicitProfiles) && !workQueueNativePrincipal.MatchString(profile.Principal) {
				return nil, fmt.Errorf("work-queue-policy worker profile %q: principal must be the stable positive decimal GitHub actor ID, not a mutable login", profileName)
			}
			pool.Profiles[profileName] = profile
		}
		result.Policy.Pools[poolName] = pool
	}
	// Validate the proposal without constructing an authenticated origin or ledger.
	validationPolicy := bindWorkQueuePolicyValidationTemplates(result.Policy)
	if err := workqueue.ValidatePolicy(validationPolicy); err != nil {
		return nil, fmt.Errorf("work-queue-policy: invalid policy proposal: %w", err)
	}
	return result, nil
}

// Resolve only compiler-owned templates on a copy; the installed Policy remains authoritative.
func bindWorkQueuePolicyValidationTemplates(policy workqueue.Policy) workqueue.Policy {
	policy.Producers = maps.Clone(policy.Producers)
	policy.Pools = maps.Clone(policy.Pools)
	principal := "1"
	for index := 1; ; index++ {
		principal = strconv.Itoa(index)
		if _, occupied := policy.Producers[principal]; !occupied {
			break
		}
	}
	if rule, templated := policy.Producers["${{ github.actor_id }}"]; templated {
		delete(policy.Producers, "${{ github.actor_id }}")
		policy.Producers[principal] = rule
	}
	for name, pool := range policy.Pools {
		pool.Profiles = maps.Clone(pool.Profiles)
		pool.AllowedRepositories = slices.Clone(pool.AllowedRepositories)
		for index, repository := range pool.AllowedRepositories {
			if repository == "${{ github.repository }}" {
				pool.AllowedRepositories[index] = "compiler/validation"
			}
		}
		for name, profile := range pool.Profiles {
			if profile.Principal == "${{ github.actor_id }}" {
				profile.Principal = principal
			}
			if profile.EffectScope == "${{ github.repository }}" {
				profile.EffectScope = "compiler/validation"
			}
			pool.Profiles[name] = profile
		}
		policy.Pools[name] = pool
	}
	return policy
}

func validateConfiguredWorkQueueProfileBounds(value any) error {
	profiles, _ := value.(map[string]any)
	for name, value := range profiles {
		profile, _ := value.(map[string]any)
		if maximum, supplied := profile["max-claims-per-dispatch"]; supplied {
			encoded, err := json.Marshal(maximum)
			var number int
			if err != nil || json.Unmarshal(encoded, &number) != nil || number < 1 || number > 16 {
				return fmt.Errorf("work-queue-policy worker profile %q: max-claims-per-dispatch must be an integer from 1 to 16", name)
			}
		}
	}
	return nil
}

func normalizeWorkQueuePolicyKeys(value any) any {
	return normalizeWorkQueuePolicyValue(value, false)
}

func normalizeWorkQueuePolicyValue(value any, dictionary bool) any {
	switch value := value.(type) {
	case map[string]any:
		result := map[string]any{}
		for key, nested := range value {
			normalized := key
			if !dictionary {
				normalized = strings.ReplaceAll(key, "-", "_")
			}
			if !dictionary && key == "max-claims-per-dispatch" {
				normalized = "max_claims"
			}
			nestedDictionary := !dictionary && slices.Contains([]string{"accounting-weights", "pools", "profiles", "producers", "read-credentials"}, key)
			result[normalized] = normalizeWorkQueuePolicyValue(nested, nestedDictionary)
		}
		return result
	case []any:
		result := make([]any, len(value))
		for i, nested := range value {
			result[i] = normalizeWorkQueuePolicyValue(nested, false)
		}
		return result
	default:
		return value
	}
}

func (c *Compiler) validateWorkQueueTargets(data *WorkflowData, markdownPath string) error {
	if data.WorkQueuePolicy == nil {
		return nil
	}
	if data.SafeOutputs != nil && data.SafeOutputs.DispatchWorkflow != nil {
		for _, name := range data.SafeOutputs.DispatchWorkflow.Workflows {
			target, err := findWorkflowFile(name, markdownPath)
			if err != nil {
				return fmt.Errorf("work-queue: approved worker %q: %w", name, err)
			}
			if !target.mdExists {
				return fmt.Errorf("work-queue: approved worker %q requires compiler-managed workflow source", name)
			}
			worker, err := mdHasWorkQueueWorker(target.mdPath)
			if err != nil {
				return err
			}
			if !worker {
				return fmt.Errorf("work-queue: approved target %q must declare tools.work-queue.worker: true; ordinary delegated workflows have no Claim-scoped delivery adapter", name)
			}
		}
	}
	policyBlock, _ := data.RawFrontmatter["work-queue-policy"].(map[string]any)
	_, explicitPools := policyBlock["pools"]
	_, explicitProfiles := policyBlock["worker-profiles"]
	explicit := explicitPools || explicitProfiles
	if !explicit {
		pool := data.WorkQueuePolicy.Policy.Pools["default"]
		template := pool.Profiles["default"]
		template.Ref = "${{ github.sha }}"
		template.Workflow = ".github/workflows/" + GetWorkflowIDFromPath(markdownPath) + ".lock.yml"
		pool.Profiles = map[string]workqueue.WorkerProfile{"default": template}
		if data.SafeOutputs != nil && data.SafeOutputs.DispatchWorkflow != nil {
			pool.Profiles = map[string]workqueue.WorkerProfile{}
			for _, name := range data.SafeOutputs.DispatchWorkflow.Workflows {
				target, err := findWorkflowFile(name, markdownPath)
				if err != nil {
					return fmt.Errorf("work-queue: worker profile %q: %w", name, err)
				}
				if !target.mdExists {
					return fmt.Errorf("work-queue: worker profile %q requires compiler-managed workflow source", name)
				}
				worker, err := mdHasWorkQueueWorker(target.mdPath)
				if err != nil {
					return err
				}
				if !worker {
					continue
				}
				profile := template
				profile.Workflow = ".github/workflows/" + name + ".lock.yml"
				pool.Profiles[name] = profile
			}
			names := make([]string, 0, len(pool.Profiles))
			for name := range pool.Profiles {
				names = append(names, name)
			}
			slices.Sort(names)
			if len(names) == 0 {
				return fmt.Errorf("work-queue: dispatcher must approve at least one compiler-managed queue worker")
			}
			pool.DefaultProfile = names[0]
		}
		data.WorkQueuePolicy.Policy.Pools["default"] = pool
	}
	for poolName, pool := range data.WorkQueuePolicy.Policy.Pools {
		for profileName, profile := range pool.Profiles {
			name := filepath.Base(profile.Workflow)
			name = strings.TrimSuffix(strings.TrimSuffix(strings.TrimSuffix(name, ".lock.yml"), ".yml"), ".yaml")
			if name == GetWorkflowIDFromPath(markdownPath) && isWorkQueueWorker(data) {
				continue
			}
			target, err := findWorkflowFile(name, markdownPath)
			if err != nil {
				return fmt.Errorf("work-queue: profile %s/%s target: %w", poolName, profileName, err)
			}
			if !target.mdExists {
				return fmt.Errorf("work-queue: profile %s/%s requires compiler-managed worker source", poolName, profileName)
			}
			worker, err := mdHasWorkQueueWorker(target.mdPath)
			if err != nil {
				return err
			}
			if !worker {
				return fmt.Errorf("work-queue: profile %s/%s target must declare tools.work-queue.worker: true", poolName, profileName)
			}
		}
	}
	return nil
}

func workQueuePolicyEnvironment(data *WorkflowData) []string {
	if data == nil || data.WorkQueuePolicy == nil {
		return nil
	}
	lines := []string{
		"        env:\n",
		"          GH_AW_WORK_QUEUE_ENABLED: \"true\"\n",
	}
	// Runtime-derived defaults are not assertions about the installed global Policy.
	if _, configured := data.RawFrontmatter["work-queue-policy"]; configured {
		encoded, err := json.Marshal(data.WorkQueuePolicy.Policy)
		if err != nil {
			return nil
		}
		lines = append(lines, "          GH_AW_WORK_QUEUE_POLICY: "+fmt.Sprintf("%q", string(encoded))+"\n")
	}
	if data.SafeOutputs != nil && data.SafeOutputs.DispatchWorkflow != nil {
		budget := "1"
		if data.SafeOutputs.DispatchWorkflow.Max != nil {
			budget = *data.SafeOutputs.DispatchWorkflow.Max
		}
		lines = append(lines, "          GH_AW_WORK_QUEUE_DISPATCH_BUDGET: "+fmt.Sprintf("%q", budget)+"\n")
	}
	if len(data.WorkQueuePolicy.ForeignReadCredentials) > 0 {
		repositories := make([]string, 0, len(data.WorkQueuePolicy.ForeignReadCredentials))
		for repository := range data.WorkQueuePolicy.ForeignReadCredentials {
			repositories = append(repositories, repository)
		}
		slices.Sort(repositories)
		bindings := make(map[string]string, len(repositories))
		for index, repository := range repositories {
			name := fmt.Sprintf("GH_AW_WORK_QUEUE_DEPENDENCY_READ_TOKEN_%d", index)
			bindings[repository] = name
			lines = append(lines, "          "+name+": "+data.WorkQueuePolicy.ForeignReadCredentials[repository]+"\n")
		}
		credentials, err := json.Marshal(bindings)
		if err == nil {
			lines = append(lines, "          GH_AW_WORK_QUEUE_DEPENDENCY_READ_CREDENTIALS: "+fmt.Sprintf("%q", string(credentials))+"\n")
		}
	}
	return lines
}
