package workqueue

import "encoding/json"

func validateContractRoutes(routes map[string]string, revisions map[string]WorkerRevision) error {
	if routes == nil || len(routes) > 4096 {
		return queueError("deployment_invalid", "contract routes require an object with at most 4096 exact contract/revision bindings")
	}
	for contract, ref := range routes {
		revision, exists := revisions[ref]
		if !checkpointDigestPattern.MatchString(contract) || !revisionPattern.MatchString(ref) ||
			!exists || revision.Profile.LogicalContract != contract {
			return queueError("deployment_invalid", "contract routes must bind exact admission contracts to registered immutable worker revisions")
		}
	}
	return nil
}

// Retain the last authenticated activation for each needed contract, never an
// arbitrary historical revision or an availability-only registration.
func contractRoutesForDeployment(state Projection, pool, worker string, next WorkerProfile) (map[string]string, error) {
	installed := state.Policy.Pools[pool].Profiles[worker]
	activated := map[string]string{installed.LogicalContract: installed.Ref}
	for _, id := range state.RequestOrder {
		commit := state.Requests[id]
		if commit.PolicyEpoch != state.PolicyEpoch {
			continue
		}
		for _, operation := range commit.Operations {
			kind, err := operationKind(operation)
			if err != nil {
				return nil, err
			}
			if kind != "Deployment" {
				continue
			}
			var update DeploymentOperation
			if err := json.Unmarshal(operation, &update); err != nil {
				return nil, err
			}
			if update.Pool == pool && update.WorkerProfile == worker && (update.Activate == nil || *update.Activate) {
				activated[update.Profile.LogicalContract] = update.Profile.Ref
			}
		}
	}
	routes := map[string]string{}
	for _, work := range state.Works {
		if work.Pool != pool || work.WorkerProfile != worker || work.ExecutionRef != "" ||
			(work.State != "available" && work.State != "claimed") || work.AdmissionContract == next.LogicalContract {
			continue
		}
		ref := activated[work.AdmissionContract]
		if ref == "" {
			return nil, queueError("deployment_invalid", "unfinished Work contract has no authenticated worker activation; inspect the installed deployment history")
		}
		routes[work.AdmissionContract] = ref
	}
	if err := validateContractRoutes(routes, state.Deployments[pool][worker].Revisions); err != nil {
		return nil, err
	}
	return routes, nil
}

func retainedDeploymentRefs(state Projection, pool, worker, currentRef string, routes map[string]string) map[string]struct{} {
	refs := map[string]struct{}{}
	for _, ref := range routes {
		refs[ref] = struct{}{}
	}
	for _, work := range state.Works {
		if work.State == "available" && work.Pool == pool && work.WorkerProfile == worker &&
			work.ExecutionRef != "" && work.ExecutionRef != currentRef {
			refs[work.ExecutionRef] = struct{}{}
		}
	}
	return refs
}
