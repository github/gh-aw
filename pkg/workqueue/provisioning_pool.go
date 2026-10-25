package workqueue

import (
	"context"
	"encoding/json"
)

func (b Branch) verifyPoolInstallEvidence(ctx context.Context, state Projection, request Request) error {
	var parameters OperationsParameters
	if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
		return err
	}
	var defaultRef string
	seen := identitySet{}
	for _, operation := range parameters.Operations {
		var install PoolInstallOperation
		if err := json.Unmarshal(operation, &install); err != nil {
			return err
		}
		if install.Kind != "PoolInstall" || state.Policy == nil || state.Policy.Authorization != "aw" {
			return queueError("policy_invalid", "pool installation requires an AW-managed Policy")
		}
		installed := state.Policy.Pools[install.Pool]
		for name, profile := range install.Config.Profiles {
			if prior, exists := installed.Profiles[name]; exists && prior == profile {
				continue
			}
			known := false
			for _, pool := range state.Policy.Pools {
				if _, exists := pool.Profiles[name]; exists {
					known = true
					break
				}
			}
			if !known {
				if defaultRef == "" {
					repository, revision, err := b.verifiedDefaultReference(ctx)
					if err != nil {
						return err
					}
					b.Remote, defaultRef = repository, revision
				}
				if install.ConfigRef != defaultRef || !newDefaultWorkerProfile(name, profile, defaultRef) {
					return queueError("policy_invalid", "new worker registration requires the verified default revision")
				}
				contract, available, err := b.configuredWorkerDeployment(ctx, profile, defaultRef)
				if err != nil {
					return err
				}
				if !available || contract != profile.LogicalContract {
					return queueError("policy_invalid", "new worker registration requires an active AW source and its exact compiled contract")
				}
			}
			if err := b.verifyWorkerRouteOnce(ctx, profile, seen); err != nil {
				return err
			}
		}
	}
	return nil
}
