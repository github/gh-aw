package workqueue

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"maps"
	"net/http"
	"strings"

	"gopkg.in/yaml.v3"
)

const contractBridgeManifest = ".github/aw/work-queue-contract-routes.json"

type contractBridgeApproval struct {
	Version int                          `json:"version"`
	Readers map[string]string            `json:"readers"`
	Routes  map[string]map[string]string `json:"routes"`
}

func requiresContractBridgeApproval(state Projection) (bool, error) {
	for _, commit := range state.Requests {
		operations := commit.Operations
		if commit.Request.Kind == "deployment" {
			var parameters OperationsParameters
			if err := json.Unmarshal(commit.Request.Parameters, &parameters); err != nil {
				return false, err
			}
			operations = append(append([]Operation{}, operations...), parameters.Operations...)
		}
		for _, operation := range operations {
			kind, err := operationKind(operation)
			if err != nil {
				return false, err
			}
			if kind != "Deployment" {
				continue
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(operation, &fields); err != nil {
				return false, err
			}
			if _, exists := fields["contract_routes"]; exists {
				return true, nil
			}
			if _, exists := fields["register"]; exists {
				return true, nil
			}
		}
	}
	return false, nil
}

func (b Branch) approvedContractBridges(ctx context.Context, revision string) (*contractBridgeApproval, error) {
	content, err := b.configFile(ctx, contractBridgeManifest, revision)
	if err != nil {
		if hasStatus(err, http.StatusNotFound) {
			return nil, nil
		}
		return nil, err
	}
	var approval contractBridgeApproval
	if _, err := decodeStrict(content); err != nil {
		return nil, queueError("deployment_invalid", "contract bridge approval requires strict JSON without duplicate keys")
	}
	decoder := json.NewDecoder(bytes.NewReader(content))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&approval); err != nil || !json.Valid(content) {
		return nil, queueError("deployment_invalid", "contract bridge approval must be valid JSON")
	}
	if approval.Version != 1 || approval.Routes == nil || len(approval.Routes) > 4096 || len(approval.Readers) != 2 {
		return nil, queueError("deployment_invalid", "contract bridge approval requires version 1 and both reviewed reader digests")
	}
	for _, path := range []string{"actions/setup/js/work_queue_replay.cjs", "actions/setup/js/work_queue_deployment.cjs"} {
		if !checkpointDigestPattern.MatchString(approval.Readers[path]) {
			return nil, queueError("deployment_invalid", "contract bridge approval requires exact reviewed queue reader SHA256 digests")
		}
	}
	for workflow, routes := range approval.Routes {
		if !strings.HasPrefix(workflow, workerWorkflowDirectory) || !strings.HasSuffix(workflow, ".lock.yml") ||
			strings.Contains(strings.TrimPrefix(workflow, workerWorkflowDirectory), "/") || routes == nil || len(routes) > 4096 {
			return nil, queueError("deployment_invalid", "contract bridge approval requires exact worker paths and bounded routes")
		}
		for contract, ref := range routes {
			if !checkpointDigestPattern.MatchString(contract) || !revisionPattern.MatchString(ref) || strings.Trim(ref, "0") == "" {
				return nil, queueError("deployment_invalid", "contract bridge approval requires exact contract and immutable revision bindings")
			}
		}
	}
	return &approval, nil
}

func (b Branch) verifyBridgeReaders(ctx context.Context, approval *contractBridgeApproval, ref, workflow string) error {
	lock, err := b.configFile(ctx, workflow, ref)
	if err != nil {
		return err
	}
	if !compiledBridgeRuntime(lock, b.Remote, ref) {
		return queueError("deployment_invalid", "selected worker must load reviewed readers through same-repository immutable checkout and local actions/setup")
	}
	for _, path := range sortedSettingsKeys(approval.Readers) {
		content, err := b.configFile(ctx, path, ref)
		if err != nil {
			return err
		}

		digest := sha256.Sum256(content)
		if hex.EncodeToString(digest[:]) != approval.Readers[path] {
			return queueError("deployment_invalid", "selected execution revision lacks the approved queue reader ABI; publish a reviewed exact-contract bridge")
		}
	}
	return nil
}

type bridgeRuntimeStep struct {
	ID              string    `yaml:"id"`
	Uses            string    `yaml:"uses"`
	Run             string    `yaml:"run"`
	With            yaml.Node `yaml:"with"`
	If              yaml.Node `yaml:"if"`
	ContinueOnError yaml.Node `yaml:"continue-on-error"`
}

func compiledBridgeCheckout(step bridgeRuntimeStep, pin, repository, ref string) (approved, valid bool) {
	values := map[string]any{}
	if step.With.Kind != 0 {
		if step.With.Kind != yaml.MappingNode || step.With.Decode(&values) != nil {
			return false, false
		}
	}
	repoValue, hasRepo := values["repository"]
	from, repoString := repoValue.(string)
	refValue, hasRef := values["ref"]
	revision, refString := refValue.(string)
	pathValue, hasPath := values["path"]
	path, pathString := pathValue.(string)
	approved = revisionPattern.MatchString(pin) && strings.Trim(pin, "0") != "" &&
		(!hasRepo || repoString && strings.EqualFold(from, repository)) &&
		(!hasRef || refString && (revision == ref || revision == "${{ github.sha }}")) &&
		(!hasPath || pathString && path == ".") && step.If.Kind == 0 && step.ContinueOnError.Kind == 0
	return approved, true
}

func compiledBridgeRuntime(content []byte, repository, ref string) bool {
	var workflow struct {
		Jobs map[string]yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &workflow); err != nil {
		return false
	}
	found := false
	for name, node := range workflow.Jobs {
		var job struct {
			Steps []bridgeRuntimeStep `yaml:"steps"`
		}
		var fields map[string]any
		if node.Decode(&job) != nil || node.Decode(&fields) != nil {
			return false
		}
		normalized, err := yaml.Marshal(fields)
		if err != nil {
			return false
		}
		// The compiler fixes the execution job as agent. Queue readers in other
		// jobs also need their own loader, not an unrelated job's setup.
		text := strings.ToLower(string(normalized))
		queueJob := name == "agent" || strings.Contains(text, "work_queue") || strings.Contains(text, "work-queue")
		checkout, loaded := false, false
		for _, step := range job.Steps {
			if pin, ok := strings.CutPrefix(step.Uses, "actions/checkout@"); ok {
				var valid bool
				checkout, valid = compiledBridgeCheckout(step, pin, repository, ref)
				if !valid || queueJob && !loaded && !checkout {
					return false
				}
			} else if step.Uses == "./actions/setup" {
				if !checkout || step.If.Kind != 0 || step.ContinueOnError.Kind != 0 {
					return false
				}
				loaded = true
				checkout = false
			} else {
				if strings.Contains(strings.ToLower(step.Uses), "/setup@") {
					return false
				}
				if step.Uses != "" || step.Run != "" {
					if queueJob && !loaded {
						return false
					}
					checkout = false
				}
			}
			if step.ID == "agentic_execution" {
				if name != "agent" || !loaded || found || step.Run == "" {
					return false
				}
				found = true
			}
		}
		if queueJob && !loaded {
			return false
		}
	}
	return found
}

func bridgeContractRoutes(state Projection, pool, worker string, next WorkerProfile, approval *contractBridgeApproval) (map[string]string, error) {
	routes := map[string]string{}
	for _, work := range state.Works {
		if work.Pool == pool && work.WorkerProfile == worker && work.ExecutionRef == "" &&
			(work.State == "available" || work.State == "claimed") && work.AdmissionContract != next.LogicalContract {
			ref := approval.Routes[next.Workflow][work.AdmissionContract]
			if ref == "" {
				return nil, queueError("deployment_invalid", "unfinished contract requires an approved exact-contract bridge")
			}
			routes[work.AdmissionContract] = ref
		}
	}
	return routes, nil
}

func cloneDeploymentState(state Projection) Projection {
	state.Deployments = maps.Clone(state.Deployments)
	for pool, workers := range state.Deployments {
		state.Deployments[pool] = maps.Clone(workers)
		for name, worker := range workers {
			copy := *worker
			copy.Revisions = maps.Clone(worker.Revisions)
			copy.ContractRoutes = maps.Clone(worker.ContractRoutes)
			state.Deployments[pool][name] = &copy
		}
	}
	return state
}

func (b Branch) verifyProspectiveBridgeReaders(ctx context.Context, state Projection, approval *contractBridgeApproval, operations []Operation) error {
	state = cloneDeploymentState(state)
	for _, operation := range operations {
		if err := state.applyDeployment(operation); err != nil {
			return err
		}
	}
	profiles := map[string]WorkerProfile{}
	for _, deployment := range state.Deployments {
		for _, worker := range deployment {
			if worker.Revisions[worker.CurrentRef].Available {
				profile := worker.Revisions[worker.CurrentRef].Profile
				profiles[profile.Workflow+"\n"+profile.Ref] = profile
			}
		}
	}
	for _, work := range state.Works {
		if work.State != "available" {
			continue
		}
		if profile, reason := state.executionProfile(work); reason == "ready" {
			profiles[profile.Workflow+"\n"+profile.Ref] = profile
		}
	}
	for _, dispatch := range state.Dispatches {
		if !dispatch.Released {
			profile := dispatch.Profile
			profiles[profile.Workflow+"\n"+profile.Ref] = profile
		}
	}
	for _, key := range sortedSettingsKeys(profiles) {
		profile := profiles[key]
		if err := b.verifyBridgeReaders(ctx, approval, profile.Ref, profile.Workflow); err != nil {
			return err
		}
	}
	return nil
}
