package workqueue

import (
	"encoding/json"
	"maps"
	"slices"
)

func (parameters DispatchParameters) MarshalJSON() ([]byte, error) {
	type plain DispatchParameters
	value := map[string]any{}
	data, err := json.Marshal(plain(parameters))
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &value); err != nil {
		return nil, err
	}
	if parameters.WorkerProfiles != nil {
		value["worker_profiles"] = parameters.WorkerProfiles
	}
	return json.Marshal(value)
}

func (state *Projection) initializeDeployments() {
	state.Deployments = map[string]map[string]*WorkerDeployment{}
	if state.Policy.Authorization != "aw" {
		return
	}
	for pool, policy := range state.Policy.Pools {
		for name, profile := range policy.Profiles {
			if profile.LogicalContract == "" {
				continue
			}
			if state.Deployments[pool] == nil {
				state.Deployments[pool] = map[string]*WorkerDeployment{}
			}
			state.Deployments[pool][name] = &WorkerDeployment{
				CurrentRef: profile.Ref, CurrentContract: profile.LogicalContract,
				Revisions: map[string]WorkerRevision{profile.Ref: {Profile: profile, Available: true}},
			}
		}
	}
}

func deploymentBoundary(a, b WorkerProfile) bool {
	return a.Workflow == b.Workflow && a.Principal == b.Principal &&
		a.TrustDomain == b.TrustDomain && a.CredentialScope == b.CredentialScope &&
		a.EffectScope == b.EffectScope && a.MaxClaims == b.MaxClaims && a.ShareKeys == b.ShareKeys
}

func (state *Projection) applyDeployment(operation Operation) error {
	var update DeploymentOperation
	if err := json.Unmarshal(operation, &update); err != nil {
		return err
	}
	deployment := state.Deployments[update.Pool][update.WorkerProfile]
	if deployment == nil || state.Policy.Authorization != "aw" {
		return queueError("deployment_invalid", "deployment requires an installed contract-marked AW worker")
	}
	if deployment.CurrentRef != update.ExpectedRef || deployment.CurrentContract != update.ExpectedContract {
		return queueError("deployment_conflict", "worker deployment changed; refresh the expected revision and contract")
	}
	profile := update.Profile
	activate := update.Activate == nil || *update.Activate
	original := state.Policy.Pools[update.Pool].Profiles[update.WorkerProfile]
	if !checkpointDigestPattern.MatchString(profile.LogicalContract) ||
		!revisionPattern.MatchString(profile.Ref) || !deploymentBoundary(original, profile) {
		return queueError("deployment_invalid", "deployment cannot change worker scope, identity or scheduling economics")
	}
	if prior, exists := deployment.Revisions[profile.Ref]; exists && prior.Profile != profile {
		return queueError("deployment_invalid", "immutable worker revision cannot change its logical contract or authority")
	}
	if _, exists := deployment.Revisions[profile.Ref]; !activate && !exists {
		return queueError("deployment_invalid", "availability updates require an existing immutable revision")
	}
	deployment.Revisions[profile.Ref] = WorkerRevision{Profile: profile, Available: update.Available}
	if activate {
		deployment.CurrentRef, deployment.CurrentContract = profile.Ref, profile.LogicalContract
	}
	return nil
}

func (state Projection) admissionProfile(node WorkDefinition) (WorkerProfile, error) {
	profile, ok := state.Policy.Pools[node.Pool].Profiles[node.WorkerProfile]
	if !ok {
		return WorkerProfile{}, queueError("work_invalid", "worker profile is not installed")
	}
	deployment := state.Deployments[node.Pool][node.WorkerProfile]
	if deployment == nil {
		if node.ExecutionRef != "" && node.ExecutionRef != profile.Ref || node.LogicalContract != "" && node.LogicalContract != profile.LogicalContract {
			return WorkerProfile{}, queueError("work_invalid", "historical worker has no deployment revision authority")
		}
		return profile, nil
	}
	ref := deployment.CurrentRef
	if node.ExecutionRef != "" {
		ref = node.ExecutionRef
	}
	revision, exists := deployment.Revisions[ref]
	if !exists || node.LogicalContract != "" && node.LogicalContract != revision.Profile.LogicalContract {
		return WorkerProfile{}, queueError("work_invalid", "Work contract or explicit pin does not match a registered immutable revision")
	}
	return revision.Profile, nil
}

func (state Projection) executionProfile(work *WorkState) (WorkerProfile, string) {
	deployment := state.Deployments[work.Pool][work.WorkerProfile]
	if deployment == nil {
		return state.Policy.Pools[work.Pool].Profiles[work.WorkerProfile], "ready"
	}
	ref := deployment.CurrentRef
	if work.ExecutionRef != "" {
		ref = work.ExecutionRef
	}
	revision, exists := deployment.Revisions[ref]
	if !exists || !revision.Available {
		return WorkerProfile{}, "worker_unavailable"
	}
	if revision.Profile.LogicalContract != work.AdmissionContract {
		return WorkerProfile{}, "worker_incompatible"
	}
	return revision.Profile, "ready"
}

// FuturePolicy exposes current routes for trusted admission builders only.
// It does not replace the installed policy or affect existing assignments.
func FuturePolicy(state Projection) Policy {
	policy := *state.Policy
	policy.ClassWeights = slices.Clone(policy.ClassWeights)
	policy.AccountingWeights = maps.Clone(policy.AccountingWeights)
	policy.Producers = maps.Clone(policy.Producers)
	for name, rule := range policy.Producers {
		rule.Pools = slices.Clone(rule.Pools)
		rule.Priorities = slices.Clone(rule.Priorities)
		rule.FairnessKeys = slices.Clone(rule.FairnessKeys)
		policy.Producers[name] = rule
	}
	policy.Projectors = slices.Clone(policy.Projectors)
	for index := range policy.Projectors {
		rule := &policy.Projectors[index]
		rule.Pools = slices.Clone(rule.Pools)
		rule.Repositories = slices.Clone(rule.Repositories)
		rule.BackingIssues = slices.Clone(rule.BackingIssues)
	}
	policy.Pools = make(map[string]PoolPolicy, len(state.Policy.Pools))
	for pool, current := range state.Policy.Pools {
		current.AllowedRepositories = slices.Clone(current.AllowedRepositories)
		current.Profiles = make(map[string]WorkerProfile, len(current.Profiles))
		for name, profile := range state.Policy.Pools[pool].Profiles {
			if deployment := state.Deployments[pool][name]; deployment != nil {
				profile = deployment.Revisions[deployment.CurrentRef].Profile
			}
			current.Profiles[name] = profile
		}
		policy.Pools[pool] = current
	}
	return policy
}

func validateDeploymentCheckpoint(state Projection, receipts []checkpointReceipt) error {
	expected := newProjection()
	expected.Policy = state.Policy
	expected.initializeDeployments()
	for _, receipt := range receipts {
		if receipt.PolicyEpoch != state.PolicyEpoch {
			continue
		}
		for _, operation := range receipt.Events {
			kind, err := operationKind(operation)
			if err != nil {
				return err
			}
			if kind == "Deployment" {
				if !permitted(receipt.Actor.Role, kind) || receipt.Kind != "deployment" {
					return queueError("checkpoint_invalid", "deployment receipt has no trusted authority")
				}
				if err := expected.applyDeployment(operation); err != nil {
					return err
				}
			}
		}
	}
	if len(expected.Deployments) == 0 && len(state.Deployments) == 0 {
		return nil
	}
	if !sameJSON(expected.Deployments, state.Deployments) {
		return queueError("checkpoint_invalid", "deployment registry does not match immutable deployment receipts")
	}
	if err := validateDeploymentDispatches(state); err != nil {
		return err
	}
	for _, work := range state.Works {
		if work.State == "completed" || work.State == "cancelled" {
			continue
		}
		deployment := state.Deployments[work.Pool][work.WorkerProfile]
		if deployment == nil {
			continue
		}
		found := false
		for ref, revision := range deployment.Revisions {
			if revision.Profile.LogicalContract == work.AdmissionContract &&
				(work.ExecutionRef == "" || work.ExecutionRef == ref) {
				found = true
			}
		}
		if !found || work.LogicalContract != "" && work.LogicalContract != work.AdmissionContract {
			return queueError("checkpoint_invalid", "Work admission contract has no immutable revision authority")
		}
	}
	return nil
}

func validateDeploymentDispatches(state Projection) error {
	for _, dispatch := range state.Dispatches {
		if dispatch.Profile.LogicalContract == "" {
			continue
		}
		if !dispatch.Released {
			deployment := state.Deployments[dispatch.Pool][dispatch.WorkerProfile]
			if deployment == nil || deployment.Revisions[dispatch.Profile.Ref].Profile != dispatch.Profile {
				return queueError("checkpoint_invalid", "frozen dispatch profile has no immutable registered revision")
			}
		}
		for _, claim := range dispatch.Claims {
			work := state.Works[claim.WorkID]
			if work == nil || work.AdmissionContract != dispatch.Profile.LogicalContract {
				return queueError("checkpoint_invalid", "frozen dispatch differs from its admitted Work contract")
			}
		}
	}
	return nil
}
