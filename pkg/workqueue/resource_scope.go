package workqueue

import (
	"bytes"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// EffectResource is a broker-resolved target, not an assertion of authenticated evidence.
type EffectResource map[string]string

type workResourceScope struct {
	Version   int              `json:"version"`
	Resources []EffectResource `json:"resources"`
}

func validateEffectResource(resource EffectResource) error {
	if !repoPattern.MatchString(resource["repository"]) {
		return queueError("claim_scope_invalid", "effect target requires a canonical repository")
	}
	for key, value := range resource {
		if value == "" || len(value) > 256 || !utf8.ValidString(value) {
			return queueError("claim_scope_invalid", "effect target fields must be bounded nonempty identities")
		}
		for _, char := range value {
			if char < 0x20 || char == 0x7f {
				return queueError("claim_scope_invalid", "effect target identities cannot contain controls")
			}
		}
		switch key {
		case "repository", "ref":
		case "host":
			if value != "github.com" {
				return queueError("claim_scope_invalid", "effect target host is unsupported")
			}
		case "kind":
			if value != "issue" && value != "pull_request" {
				return queueError("claim_scope_invalid", "effect target has an unknown resource kind")
			}
		case "repository_id", "resource_id", "number", "comment_id", "run_id":
			if !decimalIdentity(value) {
				return queueError("claim_scope_invalid", "effect resource IDs must be positive canonical decimals")
			}
		case "path":
			if strings.HasPrefix(value, "/") || strings.Contains(value, "\\") {
				return queueError("claim_scope_invalid", "effect paths must be exact repository-relative paths")
			}
			for _, part := range strings.Split(value, "/") {
				if part == "" || part == "." || part == ".." {
					return queueError("claim_scope_invalid", "effect paths cannot contain ambiguous or traversing segments")
				}
			}
		default:
			return queueError("claim_scope_invalid", "unknown effect target field %q", key)
		}
	}
	return nil
}

func frozenResourceScope(payload json.RawMessage) (*workResourceScope, error) {
	data := bytes.TrimSpace(payload)
	if len(data) == 0 || !json.Valid(data) {
		return nil, queueError("claim_scope_invalid", "immutable Work payload is invalid")
	}
	if data[0] != '{' {
		return nil, nil
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, queueError("claim_scope_invalid", "immutable Work payload cannot be decoded")
	}
	raw, declared := fields["resource_scope"]
	if !declared {
		return nil, nil
	}
	var scope workResourceScope
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&scope); err != nil || scope.Version != 1 ||
		scope.Resources == nil || len(scope.Resources) > 128 {
		return nil, queueError("claim_scope_invalid", "Work resource_scope requires closed version 1 and at most 128 resource selectors")
	}
	seen := map[string]bool{}
	for _, selector := range scope.Resources {
		if err := validateEffectResource(selector); err != nil {
			return nil, err
		}
		encoded, err := canonicalValue(selector)
		if err != nil || seen[string(encoded)] {
			return nil, queueError("claim_scope_invalid", "Work resource_scope selectors must be unique canonical objects")
		}
		seen[string(encoded)] = true
	}
	return &scope, nil
}

func matchesEffectResource(selector, target EffectResource) bool {
	for field, expected := range selector {
		if target[field] != expected {
			return false
		}
	}
	return true
}

func authorizeEffectResource(state Projection, claim *ClaimState, work *WorkState, target EffectResource) error {
	if err := validateEffectResource(target); err != nil {
		return err
	}
	dispatch := state.Dispatches[claim.DispatchID]
	if state.Policy == nil || dispatch == nil || dispatch.Run == nil {
		return queueError("claim_scope_invalid", "effect target has no installed and bound authority")
	}
	pool, ok := state.Policy.Pools[work.Pool]
	if !ok {
		return queueError("claim_scope_invalid", "Work pool has no installed effect authority")
	}
	profile, ok := pool.Profiles[work.WorkerProfile]
	if !ok || target["repository"] != profile.EffectScope ||
		target["repository"] != dispatch.Profile.EffectScope {
		return queueError("claim_scope_invalid", "effect target lies outside frozen and installed profile scope")
	}
	if target["run_id"] != "" && target["run_id"] != dispatch.Run.RunID {
		return queueError("claim_scope_invalid", "effect target is not the original native worker run")
	}
	frozen := false
	for _, member := range dispatch.Assignment.Claims {
		if member.ClaimID == claim.ClaimID && sameJSON(member.Work, work.Payload) {
			frozen = true
			break
		}
	}
	if !frozen {
		return queueError("claim_scope_invalid", "Work payload differs from the original immutable assignment")
	}
	scope, err := frozenResourceScope(work.Payload)
	if err != nil {
		return err
	}
	if scope != nil {
		matched := false
		for _, selector := range scope.Resources {
			if matchesEffectResource(selector, target) {
				matched = true
				break
			}
		}
		if !matched {
			return queueError("claim_scope_invalid", "effect target lies outside immutable Work resource_scope")
		}
	}
	if subject := work.Subject; subject != nil {
		if target["repository"] != subject.Repository {
			return queueError("claim_scope_invalid", "effect target lies outside immutable Work subject repository")
		}
		typedTarget := target["kind"] != "" || target["number"] != "" ||
			target["resource_id"] != "" || target["comment_id"] != ""
		if typedTarget || target["run_id"] == "" && target["ref"] == "" && target["path"] == "" {
			expected := EffectResource{
				"kind": subject.Kind, "host": subject.Host, "repository": subject.Repository,
				"repository_id": subject.RepositoryID, "resource_id": subject.ResourceID, "number": subject.Number,
			}
			if !matchesEffectResource(expected, target) {
				return queueError("claim_scope_invalid", "effect target does not match the full immutable Work subject")
			}
		}
	}
	return nil
}
