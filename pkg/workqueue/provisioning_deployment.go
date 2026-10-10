package workqueue

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

func (b Branch) verifiedDefaultReference(ctx context.Context) (string, string, error) {
	var repository struct {
		FullName      string `json:"full_name"`
		DefaultBranch string `json:"default_branch"`
	}
	if err := b.request(ctx, http.MethodGet, "", nil, &repository); err != nil {
		return "", "", err
	}
	if !strings.EqualFold(repository.FullName, b.Remote) || repository.DefaultBranch == "" ||
		!branchPattern.MatchString(repository.DefaultBranch) || strings.Contains(repository.DefaultBranch, "..") {
		return "", "", queueError("policy_invalid", "AW configuration requires a verified repository and immutable approved default worker revision")
	}
	b.Remote = repository.FullName
	var ref struct {
		Ref    string `json:"ref"`
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	if err := b.request(ctx, http.MethodGet, "git/ref/heads/"+repository.DefaultBranch, nil, &ref); err != nil {
		return "", "", err
	}
	if ref.Ref != "refs/heads/"+repository.DefaultBranch || !revisionPattern.MatchString(ref.Object.SHA) ||
		strings.Trim(ref.Object.SHA, "0") == "" {
		return "", "", queueError("policy_invalid", "default worker revision is not immutable")
	}
	return repository.FullName, ref.Object.SHA, nil
}

// DeploymentOperationsFromConfig proposes route-only changes for existing AW
// workers. Missing artifacts or registrations pause only the affected targets.
func (b Branch) DeploymentOperationsFromConfig(ctx context.Context, state Projection, poolName, workerName string) ([]Operation, error) {
	selected, err := nativeDeploymentTargets(state, poolName, workerName)
	if err != nil {
		return nil, err
	}
	b, err = b.withClient()
	if err != nil {
		return nil, err
	}
	repository, revision, err := b.verifiedDefaultReference(ctx)
	if err != nil {
		return nil, err
	}
	b.Remote = repository
	builder := nativeDeploymentBuilder{
		branch: b, state: state, routes: map[string]nativeDeploymentRoute{}, operations: []Operation{},
	}
	policy := FuturePolicy(state)
	for _, target := range selected {
		current := policy.Pools[target.Pool].Profiles[target.WorkerProfile]
		if err := builder.updateTarget(ctx, target, current, revision); err != nil {
			return nil, err
		}
	}
	return builder.operations, nil
}

type nativeDeploymentRoute struct {
	contract  string
	available bool
}

type nativeDeploymentBuilder struct {
	branch     Branch
	state      Projection
	routes     map[string]nativeDeploymentRoute
	operations []Operation
}

type nativeDeploymentCursor struct {
	target           Assignment
	expectedRef      string
	expectedContract string
}

func (builder *nativeDeploymentBuilder) checkedRoute(ctx context.Context, profile WorkerProfile, revision, key string) (nativeDeploymentRoute, error) {
	result, checked := builder.routes[key]
	if !checked {
		var err error
		result.contract, result.available, err = builder.branch.configuredWorkerDeployment(ctx, profile, revision)
		if err != nil {
			return nativeDeploymentRoute{}, err
		}
		builder.routes[key] = result
	}
	return result, nil
}

func (builder *nativeDeploymentBuilder) resolvedProfile(ctx context.Context, current WorkerProfile, revision string) (WorkerProfile, bool, error) {
	result, err := builder.checkedRoute(ctx, current, revision, current.Workflow+"\n"+revision)
	if err != nil {
		return WorkerProfile{}, false, err
	}
	profile := current
	if result.contract != "" {
		profile.Ref, profile.LogicalContract = revision, result.contract
	}
	return profile, result.available, nil
}

func (builder *nativeDeploymentBuilder) updateTarget(ctx context.Context, target Assignment, current WorkerProfile, revision string) error {
	profile, available, err := builder.resolvedProfile(ctx, current, revision)
	if err != nil {
		return err
	}
	deployment := builder.state.Deployments[target.Pool][target.WorkerProfile]
	prior := deployment.Revisions[deployment.CurrentRef]
	cursor := nativeDeploymentCursor{
		target: target, expectedRef: deployment.CurrentRef, expectedContract: deployment.CurrentContract,
	}
	if profile != prior.Profile || available != prior.Available {
		if err := builder.appendUpdate(&cursor, profile, available, true); err != nil {
			return err
		}
	}
	return builder.refreshPins(ctx, &cursor, deployment)
}

func (builder *nativeDeploymentBuilder) appendUpdate(cursor *nativeDeploymentCursor, profile WorkerProfile, available, activate bool) error {
	reason := "worker_deployed"
	if !available {
		reason = "worker_unavailable"
	}
	update := DeploymentOperation{
		Kind: "Deployment", Pool: cursor.target.Pool, WorkerProfile: cursor.target.WorkerProfile,
		ExpectedRef: cursor.expectedRef, ExpectedContract: cursor.expectedContract,
		Profile: profile, Available: available, Reason: reason,
	}
	if !activate {
		update.Activate = &activate
	}
	operation, err := Op(update)
	if err != nil {
		return err
	}
	builder.operations = append(builder.operations, operation)
	if len(builder.operations) > builder.state.Policy.Limits.Operations {
		return queueError("resource_limit", "changed deployment target count exceeds the operation limit; select a smaller prefix, for example --pool default --worker-profile reviewer")
	}
	if activate {
		cursor.expectedRef, cursor.expectedContract = profile.Ref, profile.LogicalContract
	}
	return nil
}

func (builder *nativeDeploymentBuilder) refreshPins(ctx context.Context, cursor *nativeDeploymentCursor, deployment *WorkerDeployment) error {
	pins := map[string]struct{}{}
	for _, work := range builder.state.Works {
		if work.State == "available" && work.Pool == cursor.target.Pool && work.WorkerProfile == cursor.target.WorkerProfile &&
			work.ExecutionRef != "" && work.ExecutionRef != cursor.expectedRef {
			pins[work.ExecutionRef] = struct{}{}
		}
	}
	for _, ref := range sortedSettingsKeys(pins) {
		historical := deployment.Revisions[ref]
		key := historical.Profile.Workflow + "\n" + ref
		checked, err := builder.checkedRoute(ctx, historical.Profile, ref, key)
		if err != nil {
			return err
		}
		available := checked.available && checked.contract == historical.Profile.LogicalContract
		if available != historical.Available {
			if err := builder.appendUpdate(cursor, historical.Profile, available, false); err != nil {
				return err
			}
		}
	}
	return nil
}

func nativeDeploymentTargets(state Projection, poolName, workerName string) ([]Assignment, error) {
	if state.Policy == nil || state.Policy.Authorization != "aw" {
		return nil, queueError("deployment_invalid", "deploy requires an existing contract-marked AW queue; historical queues need a quiescent policy migration")
	}
	if poolName != "" {
		if _, exists := state.Deployments[poolName]; !exists {
			return nil, queueError("deployment_invalid", "pool %s has no contract-marked AW workers; select an existing pool, for example --pool default", poolName)
		}
	}
	selected := []Assignment{}
	for _, pool := range sortedSettingsKeys(state.Deployments) {
		if poolName != "" && pool != poolName {
			continue
		}
		for _, worker := range sortedSettingsKeys(state.Deployments[pool]) {
			if workerName == "" || workerName == worker {
				selected = append(selected, Assignment{Pool: pool, WorkerProfile: worker})
			}
		}
	}
	if len(selected) == 0 {
		return nil, queueError("deployment_invalid", "no selected contract-marked AW workers; use a quiescent policy migration for historical queues")
	}
	return selected, nil
}

func deploymentRouteUnavailable(err error) bool {
	var protocol *ProtocolError
	return hasStatus(err, http.StatusNotFound) || errors.As(err, &protocol) && protocol.Code == "policy_missing"
}

func (b Branch) configuredWorkerDeployment(ctx context.Context, profile WorkerProfile, revision string) (string, bool, error) {
	source := strings.TrimSuffix(profile.Workflow, ".lock.yml") + ".md"
	content, err := b.configFile(ctx, source, revision)
	if err != nil {
		if deploymentRouteUnavailable(err) {
			return "", false, nil
		}
		return "", false, err
	}
	approved, err := configSourceIsWorker(content)
	if err != nil || !approved {
		return "", false, nil
	}
	content, err = b.configFile(ctx, profile.Workflow, revision)
	if err != nil {
		if deploymentRouteUnavailable(err) {
			return "", false, nil
		}
		return "", false, err
	}
	contract, err := compiledWorkerContract(content)
	if err != nil {
		return "", false, nil
	}
	profile.Ref, profile.LogicalContract = revision, contract
	if err := b.verifyRegisteredWorker(ctx, profile); err != nil {
		if deploymentRouteUnavailable(err) {
			return contract, false, nil
		}
		return "", false, err
	}
	return contract, true, nil
}

func (b Branch) verifyNativeDeploymentEvidence(ctx context.Context, state Projection, request Request) error {
	if state.Policy.Authorization != "aw" {
		return queueError("deployment_invalid", "deployment requires an installed AW policy")
	}
	var parameters OperationsParameters
	if err := json.Unmarshal(request.Parameters, &parameters); err != nil {
		return err
	}
	repository, defaultRevision, err := b.verifiedDefaultReference(ctx)
	if err != nil {
		return err
	}
	b.Remote = repository
	builder := nativeDeploymentBuilder{branch: b, routes: map[string]nativeDeploymentRoute{}}
	expected := map[string][2]string{}
	for _, operation := range parameters.Operations {
		var update DeploymentOperation
		if err := json.Unmarshal(operation, &update); err != nil {
			return err
		}
		deployment := state.Deployments[update.Pool][update.WorkerProfile]
		if update.Kind != "Deployment" || deployment == nil {
			return queueError("deployment_invalid", "deployment target is not an installed contract-marked AW worker")
		}
		key := update.Pool + "\n" + update.WorkerProfile
		prior, checked := expected[key]
		if !checked {
			prior = [2]string{deployment.CurrentRef, deployment.CurrentContract}
		}
		if update.ExpectedRef != prior[0] || update.ExpectedContract != prior[1] {
			return queueError("deployment_conflict", "worker deployment changed; refresh the expected revision and contract")
		}
		profile := state.Policy.Pools[update.Pool].Profiles[update.WorkerProfile]
		profile.Ref, profile.LogicalContract = update.Profile.Ref, update.Profile.LogicalContract
		if profile != update.Profile || !revisionPattern.MatchString(profile.Ref) || strings.Trim(profile.Ref, "0") == "" ||
			!checkpointDigestPattern.MatchString(profile.LogicalContract) {
			return queueError("deployment_invalid", "deployment cannot change scope, identity or scheduling economics")
		}
		current := profile
		current.Ref, current.LogicalContract = prior[0], prior[1]
		if err := builder.verifyUpdate(ctx, deployment, current, update, defaultRevision); err != nil {
			return err
		}
		if update.Activate == nil || *update.Activate {
			expected[key] = [2]string{profile.Ref, profile.LogicalContract}
		} else {
			expected[key] = prior
		}
	}
	return nil
}

func (builder *nativeDeploymentBuilder) verifyUpdate(ctx context.Context, deployment *WorkerDeployment, current WorkerProfile, update DeploymentOperation, defaultRevision string) error {
	if update.Activate == nil || *update.Activate {
		profile, available, err := builder.resolvedProfile(ctx, current, defaultRevision)
		if err != nil {
			return err
		}
		if update.Profile != profile || update.Available != available {
			return queueError("deployment_invalid", "activation requires the verified default revision's AW worker contract and exact native availability; refresh with deploy --from-config")
		}
		return nil
	}
	revision, exists := deployment.Revisions[update.Profile.Ref]
	if !exists || revision.Profile != update.Profile {
		return queueError("deployment_invalid", "availability-only updates require an already registered immutable worker revision")
	}
	profile := revision.Profile
	result, err := builder.checkedRoute(ctx, profile, profile.Ref, profile.Workflow+"\n"+profile.Ref)
	if err != nil {
		return err
	}
	available := result.available && result.contract == profile.LogicalContract
	if update.Available != available {
		return queueError("deployment_invalid", "historical availability must match the registered immutable worker's compiler contract and native availability")
	}
	return nil
}

func compiledWorkerContract(content []byte) (string, error) {
	var workflow struct {
		On struct {
			WorkflowDispatch struct {
				Inputs map[string]struct {
					Type string `yaml:"type"`
				} `yaml:"inputs"`
			} `yaml:"workflow_dispatch"`
		} `yaml:"on"`
		Env  map[string]any `yaml:"env"`
		Jobs map[string]struct {
			Env   map[string]any `yaml:"env"`
			Steps []struct {
				Env map[string]any `yaml:"env"`
			} `yaml:"steps"`
		} `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return "", fmt.Errorf("compiled worker has invalid YAML: %w", err)
	}
	if workflow.On.WorkflowDispatch.Inputs["work_queue_assignment"].Type != "string" {
		return "", queueError("deployment_invalid", "compiled worker must accept work_queue_assignment as a string")
	}
	environments := []map[string]any{workflow.Env}
	for _, job := range workflow.Jobs {
		environments = append(environments, job.Env)
		for _, step := range job.Steps {
			environments = append(environments, step.Env)
		}
	}
	contract := ""
	for _, environment := range environments {
		value, present := environment["GH_AW_WORK_QUEUE_CONTRACT"]
		if !present {
			continue
		}
		stamp, ok := value.(string)
		if !ok || !checkpointDigestPattern.MatchString(stamp) || contract != "" && contract != stamp {
			return "", queueError("deployment_invalid", "compiled worker requires one consistent literal GH_AW_WORK_QUEUE_CONTRACT SHA256 stamp")
		}
		contract = stamp
	}
	if contract == "" {
		return "", queueError("deployment_invalid", "compiled worker lacks GH_AW_WORK_QUEUE_CONTRACT; recompile the approved worker")
	}
	return contract, nil
}
