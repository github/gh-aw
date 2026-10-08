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
			for part := range strings.SplitSeq(value, "/") {
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
	if !bytes.HasPrefix(data, []byte{'{'}) {
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
	seen := identitySet{}
	for _, selector := range scope.Resources {
		if err := validateEffectResource(selector); err != nil {
			return nil, err
		}
		encoded, err := canonicalValue(selector)
		if err != nil || seen.contains(string(encoded)) {
			return nil, queueError("claim_scope_invalid", "Work resource_scope selectors must be unique canonical objects")
		}
		seen.add(string(encoded))
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

func hasNativeResourceBinding(selector EffectResource) bool {
	if selector["host"] != "github.com" || !decimalIdentity(selector["repository_id"]) {
		return false
	}
	for _, field := range []string{"kind", "number", "resource_id", "comment_id"} {
		if _, present := selector[field]; present {
			return selector["kind"] != "" && decimalIdentity(selector["number"]) &&
				decimalIdentity(selector["resource_id"])
		}
	}
	return true
}

func matchesWorkResourceSelector(selector, target EffectResource) bool {
	if !matchesEffectResource(selector, target) {
		return false
	}
	for _, field := range []string{"run_id", "ref", "path"} {
		if value, present := target[field]; present && selector[field] != value {
			return false
		}
	}
	return true
}

func assertWorkTarget(work *WorkState, target EffectResource) error {
	scope, err := frozenResourceScope(work.Payload)
	if err != nil {
		return err
	}
	matched := false
	if scope != nil {
		for _, selector := range scope.Resources {
			if (work.Subject != nil || hasNativeResourceBinding(selector)) &&
				matchesWorkResourceSelector(selector, target) {
				matched = true
				break
			}
		}
	}
	if work.Subject == nil && !matched {
		return queueError("claim_scope_invalid", "effect requires a positive immutable Work target binding with native repository and resource identities")
	}
	if scope != nil && !matched {
		return queueError("claim_scope_invalid", "effect target lies outside immutable Work resource_scope")
	}
	if subject := work.Subject; subject != nil {
		expected := EffectResource{
			"kind": subject.Kind, "host": subject.Host, "repository": subject.Repository,
			"repository_id": subject.RepositoryID, "resource_id": subject.ResourceID, "number": subject.Number,
		}
		if err := validateEffectResource(expected); err != nil {
			return err
		}
		if !hasNativeResourceBinding(expected) || !matchesEffectResource(expected, target) {
			return queueError("claim_scope_invalid", "effect target does not match the full immutable Work subject")
		}
		for _, field := range []string{"run_id", "ref", "path"} {
			if _, present := target[field]; present && scope == nil {
				return queueError("claim_scope_invalid", "generic effects require an explicit immutable Work selector and cannot bypass Subject")
			}
		}
	}
	return nil
}

// Requests retain the causal commits, including duplicate immutable Work
// submissions. Walking backwards preserves the original creator, not a retry.
func immutableWorkCreators(state Projection) (map[string]Actor, error) {
	commits := make(map[string]QueueCommit, len(state.Requests))
	for _, commit := range state.Requests {
		commits[commit.ID] = commit
	}
	creators := map[string]Actor{}
	visited := identitySet{}
	for id := state.Tip; id != ""; {
		commit, ok := commits[id]
		if !ok || visited.contains(id) {
			return nil, queueError("claim_scope_invalid", "immutable Work origin history is missing or cyclic")
		}
		visited.add(id)
		for _, operation := range commit.Operations {
			kind, err := operationKind(operation)
			if err != nil {
				return nil, queueError("claim_scope_invalid", "immutable Work origin cannot be decoded")
			}
			if kind != "Work" {
				continue
			}
			var work WorkDefinition
			if err := json.Unmarshal(operation, &work); err != nil || work.WorkID == "" {
				return nil, queueError("claim_scope_invalid", "immutable Work origin cannot be decoded")
			}
			creators[work.WorkID] = commit.Actor
		}
		if commit.Previous == nil {
			break
		}
		id = *commit.Previous
	}
	return creators, nil
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
	for _, member := range dispatch.Claims {
		if member.ClaimID == claim.ClaimID && sameJSON(member.Work, work.Payload) {
			frozen = true
			break
		}
	}
	if !frozen {
		return queueError("claim_scope_invalid", "Work payload differs from the original immutable assignment")
	}
	if err := assertWorkTarget(work, target); err != nil {
		return err
	}
	return authorizeAncestorTargets(state, work, target)
}

func authorizeAncestorTargets(state Projection, work *WorkState, target EffectResource) error {
	creators, err := immutableWorkCreators(state)
	if err != nil {
		return err
	}
	visited := identitySet{work.WorkID: {}}
	for descendant := work; ; {
		creator, ok := creators[descendant.WorkID]
		if !ok {
			return queueError("claim_scope_invalid", "immutable Work creator is missing")
		}
		if creator.Role != "worker" {
			break
		}
		parentDispatch := state.Dispatches[creator.DispatchID]
		var parent *WorkState
		if parentDispatch != nil {
			for _, member := range parentDispatch.Claims {
				if member.Handle == creator.ClaimHandle {
					parent = state.Works[member.WorkID]
					break
				}
			}
		}
		if parent == nil || visited.contains(parent.WorkID) {
			return queueError("claim_scope_invalid", "immutable Work ancestor authority is missing or cyclic")
		}
		parentPool, poolOK := state.Policy.Pools[parent.Pool]
		parentProfile, profileOK := parentPool.Profiles[parent.WorkerProfile]
		if !poolOK || !profileOK || target["repository"] != parentProfile.EffectScope ||
			target["repository"] != parentDispatch.Profile.EffectScope {
			return queueError("claim_scope_invalid", "effect target lies outside immutable ancestor profile scope")
		}
		visited.add(parent.WorkID)
		if err := assertWorkTarget(parent, target); err != nil {
			return err
		}
		descendant = parent
	}
	return nil
}
