package workqueue

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"maps"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
)

// PolicyEnvVersion is the protocol version of the compiled policy environment.
const PolicyEnvVersion = 4

var (
	policyEnvProfileWorkflow = regexp.MustCompile(`^\.github/workflows/[^/]+\.lock\.yml$`)
	policyEnvRepository      = regexp.MustCompile(`^[^/\s]+/[^/\s]+$`)
	policyEnvWorkerID        = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]*$`)
)

// PolicyEnv is the compiled per-invocation queue policy carried in
// GH_AW_WORK_QUEUE_POLICY. It replaces any installed ledger Policy: pool limits
// take effect on the next run that carries them.
type PolicyEnv struct {
	Version      int
	PendingLimit int
	// Issues is nil when Issue projection is disabled (encoded as false).
	Issues *IssuesEnv
	Pools  map[string]PoolEnv
}

// IssuesEnv configures the best-effort Issue projection.
type IssuesEnv struct {
	Label             string `json:"label"`
	TitlePrefix       string `json:"title_prefix"`
	OrphanedAfterDays int    `json:"orphaned_after_days,omitempty"`
}

// PoolEnv holds one pool's scheduling limits and routable worker profiles.
// PerAccountLimit 0 means unlimited.
type PoolEnv struct {
	Concurrency     int                   `json:"concurrency"`
	PerAccountLimit int                   `json:"per_account_limit"`
	MaxAttempts     int                   `json:"max_attempts"`
	DefaultProfile  string                `json:"default_profile"`
	Profiles        map[string]ProfileEnv `json:"profiles"`
}

// ProfileEnv routes Work to one worker workflow whose effects stay in EffectScope.
type ProfileEnv struct {
	Workflow    string `json:"workflow"`
	EffectScope string `json:"effect_scope"`
}

type policyEnvWire struct {
	Version      int                `json:"version"`
	PendingLimit int                `json:"pending_limit"`
	Issues       json.RawMessage    `json:"issues"`
	Pools        map[string]PoolEnv `json:"pools"`
}

// MarshalJSON encodes disabled Issues as false.
func (env PolicyEnv) MarshalJSON() ([]byte, error) {
	issues := json.RawMessage("false")
	if env.Issues != nil {
		encoded, err := json.Marshal(env.Issues)
		if err != nil {
			return nil, err
		}
		issues = encoded
	}
	return json.Marshal(policyEnvWire{Version: env.Version, PendingLimit: env.PendingLimit, Issues: issues, Pools: env.Pools})
}

// ParsePolicyEnv decodes and validates a closed v4 policy environment.
func ParsePolicyEnv(data []byte) (PolicyEnv, error) {
	var wire policyEnvWire
	if err := decodeClosedJSON(data, &wire); err != nil {
		return PolicyEnv{}, fmt.Errorf("policy_invalid: %w", err)
	}
	env := PolicyEnv{Version: wire.Version, PendingLimit: wire.PendingLimit, Pools: wire.Pools}
	if trimmed := bytes.TrimSpace(wire.Issues); !bytes.Equal(trimmed, []byte("false")) {
		issues := &IssuesEnv{}
		if err := decodeClosedJSON(trimmed, issues); err != nil {
			return PolicyEnv{}, fmt.Errorf("policy_invalid: issues: %w", err)
		}
		env.Issues = issues
	}
	return env, ValidatePolicyEnv(env)
}

// decodeClosedJSON decodes exactly one JSON value without unknown fields.
func decodeClosedJSON(data []byte, target any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		return err
	}
	if err := decoder.Decode(new(any)); !errors.Is(err, io.EOF) {
		return errors.New("trailing JSON data")
	}
	return nil
}

// ValidatePolicyEnv checks the closed v4 shape and bounds; repository-bound
// templates must already be bound.
func ValidatePolicyEnv(env PolicyEnv) error {
	if env.Version != PolicyEnvVersion {
		return errors.New("unsupported_protocol: work queue policy environment v4 required")
	}
	if env.PendingLimit < 1 || env.PendingLimit > 4096 {
		return errors.New("policy_invalid: pending_limit must be an integer in 1..4096")
	}
	if env.Issues != nil {
		if env.Issues.Label == "" || len(env.Issues.Label) > 64 || env.Issues.TitlePrefix == "" || len(env.Issues.TitlePrefix) > 128 {
			return errors.New("policy_invalid: issues require a label and title_prefix")
		}
		if env.Issues.OrphanedAfterDays < 0 || env.Issues.OrphanedAfterDays > 3650 {
			return errors.New("policy_invalid: orphaned_after_days must be an integer in 1..3650")
		}
	}
	if len(env.Pools) == 0 || len(env.Pools) > 64 {
		return errors.New("policy_invalid: policy must define 1..64 pools")
	}
	for name, pool := range env.Pools {
		if !validIdentity(name, false, 256) {
			return fmt.Errorf("policy_invalid: invalid pool %q", name)
		}
		if pool.Concurrency < 1 || pool.Concurrency > 4096 || pool.PerAccountLimit < 0 || pool.PerAccountLimit > 4096 || pool.MaxAttempts < 1 || pool.MaxAttempts > 16 {
			return fmt.Errorf("policy_invalid: pool %s limits are out of range", name)
		}
		if len(pool.Profiles) == 0 || len(pool.Profiles) > 256 {
			return fmt.Errorf("policy_invalid: pool %s must define 1..256 worker profiles", name)
		}
		if _, ok := pool.Profiles[pool.DefaultProfile]; !ok {
			return fmt.Errorf("policy_invalid: pool %s default_profile must name one of its profiles", name)
		}
		for profileName, profile := range pool.Profiles {
			if !validIdentity(profileName, false, 256) || !policyEnvProfileWorkflow.MatchString(profile.Workflow) || !policyEnvRepository.MatchString(profile.EffectScope) {
				return fmt.Errorf("policy_invalid: pool %s profile %q requires .github/workflows/<name>.lock.yml and an owner/name effect scope", name, profileName)
			}
		}
	}
	return nil
}

// BuildPolicyEnv derives the v4 environment from aw.json preferences and the
// approved AW worker workflow IDs. Every pool ("default" plus each configured
// pool) may route to every worker. repository may be an Actions expression that
// is bound at run time; callers validate after binding it.
func BuildPolicyEnv(settings Settings, repository string, workflows []string) (PolicyEnv, error) {
	names := slices.Clone(workflows)
	slices.Sort(names)
	names = slices.Compact(names)
	if len(names) == 0 || len(names) > 256 {
		return PolicyEnv{}, errors.New("work_queue.pools: AW must approve 1..256 worker workflows; update .github/workflows/aw.json")
	}
	profiles := make(map[string]ProfileEnv, len(names))
	for _, name := range names {
		if !policyEnvWorkerID.MatchString(name) || strings.Contains(name, "..") {
			return PolicyEnv{}, fmt.Errorf("work_queue.pools: AW worker target %q must be a workflow ID, not a path", name)
		}
		profiles[name] = ProfileEnv{Workflow: path.Join(constants.WorkflowsDir, name+".lock.yml"), EffectScope: repository}
	}
	// names is sorted and non-empty, so its minimum is the first approved worker.
	defaultProfile := slices.Min(names)
	pool := func(preferences PoolSettings) PoolEnv {
		return PoolEnv{Concurrency: preferences.Concurrency, PerAccountLimit: preferences.PerAccountLimit, MaxAttempts: preferences.Retry.MaxAttempts, DefaultProfile: defaultProfile, Profiles: maps.Clone(profiles)}
	}
	env := PolicyEnv{Version: PolicyEnvVersion, PendingLimit: settings.PendingLimit, Pools: map[string]PoolEnv{}}
	defaults := PoolSettings{Concurrency: settings.Concurrency, Retry: settings.Retry}
	if configured, ok := settings.Pools["default"]; ok {
		defaults = configured
	}
	env.Pools["default"] = pool(defaults)
	for name, preferences := range settings.Pools {
		if name != "default" {
			env.Pools[name] = pool(preferences)
		}
	}
	if settings.Issues != nil {
		env.Issues = &IssuesEnv{Label: settings.Issues.Label, TitlePrefix: settings.Issues.TitlePrefix, OrphanedAfterDays: settings.Issues.OrphanedAfterDays}
	}
	return env, nil
}
