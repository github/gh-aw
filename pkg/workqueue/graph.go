package workqueue

import (
	"encoding/json"
	"slices"
	"sort"
	"strings"
)

func resourceKey(resource Resource, condition string) string {
	data, _ := canonicalValue(map[string]any{
		"host": resource.Host, "repository_id": resource.RepositoryID,
		"resource_id": resource.ResourceID, "kind": resource.Kind, "condition": condition,
	})
	return string(data)
}

func validateResource(resource Resource, pool PoolPolicy) error {
	if resource.Kind != "issue" && resource.Kind != "pull_request" {
		return queueError("resource_invalid", "expected Issue or PR type")
	}
	if resource.Host != "github.com" || !repoPattern.MatchString(resource.Repository) ||
		!slices.Contains(pool.AllowedRepositories, resource.Repository) {
		return queueError("resource_unauthorized", "host/repository is not approved")
	}
	for _, id := range []string{resource.RepositoryID, resource.ResourceID, resource.Number} {
		if id == "" || id == "0" || id[0] == '0' {
			return queueError("resource_invalid", "resource IDs must be positive canonical decimal strings")
		}
		for _, char := range id {
			if char < '0' || char > '9' {
				return queueError("resource_invalid", "invalid decimal identity")
			}
		}
	}
	return nil
}

func (state Projection) validateGraph() error {
	vertices := map[string]map[string]bool{}
	pending := map[string]int{}
	graphPools := map[string]string{}
	for _, work := range state.Works {
		if pool, ok := graphPools[work.GraphID]; ok && pool != work.Pool {
			return queueError("graph_pool_conflict", "graph %s spans pools", work.GraphID)
		}
		graphPools[work.GraphID] = work.Pool
		if vertices[work.GraphID] == nil {
			vertices[work.GraphID] = map[string]bool{}
		}
		vertices[work.GraphID]["work:"+work.WorkID] = true
		if work.State != "completed" && work.State != "cancelled" || work.Barrier == "pending" {
			pending[work.Pool]++
		}
		if len(work.DependsOn) > state.Policy.Limits.Predecessors {
			return queueError("graph_limit", "too many predecessors at %s", work.NodeKey)
		}
		seen := map[string]bool{}
		for _, edge := range work.DependsOn {
			var key string
			if edge.Kind == "work" {
				predecessor, ok := state.Works[edge.WorkID]
				if !ok || predecessor.GraphID != work.GraphID {
					return queueError("dependency_missing", "%s -> %s", work.NodeKey, edge.WorkID)
				}
				if edge.WorkID == work.WorkID {
					return queueError("dependency_cycle", "%s -> %s", work.NodeKey, work.NodeKey)
				}
				key = "work:" + edge.WorkID
			} else {
				if edge.Resource == nil || edge.Resource.Kind != edge.Kind {
					return queueError("resource_invalid", "dependency type does not match resource")
				}
				if err := validateResource(*edge.Resource, state.Policy.Pools[work.Pool]); err != nil {
					return err
				}
				if edge.Kind == "issue" && edge.Condition != "completed" && edge.Condition != "closed" ||
					edge.Kind == "pull_request" && edge.Condition != "merged" {
					return queueError("condition_invalid", "condition does not match resource type")
				}
				key = resourceKey(*edge.Resource, edge.Condition)
				vertices[work.GraphID][key] = true
			}
			if seen[key] {
				return queueError("dependency_duplicate", "duplicate edge at %s", work.NodeKey)
			}
			seen[key] = true
		}
	}
	for graph, nodes := range vertices {
		if len(nodes) > state.Policy.Limits.GraphNodes {
			return queueError("graph_limit", "graph %s exceeds node bound", graph)
		}
	}
	for pool, count := range pending {
		if count > state.Policy.Limits.PendingNodes {
			return queueError("pending_limit", "pool %s exceeds pending-node bound", pool)
		}
	}
	color := map[string]int{}
	path := []string{}
	var visit func(string) error
	visit = func(id string) error {
		if color[id] == 1 {
			start := slices.Index(path, id)
			cycle := append(slices.Clone(path[start:]), id)
			names := []string{}
			for _, node := range cycle {
				names = append(names, state.Works[node].NodeKey)
			}
			return queueError("dependency_cycle", "%s", strings.Join(names, " -> "))
		}
		if color[id] == 2 {
			return nil
		}
		color[id] = 1
		path = append(path, id)
		for _, edge := range state.Works[id].DependsOn {
			if edge.Kind == "work" {
				if err := visit(edge.WorkID); err != nil {
					return err
				}
			}
		}
		path = path[:len(path)-1]
		color[id] = 2
		return nil
	}
	ids := make([]string, 0, len(state.Works))
	for id := range state.Works {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

func (state Projection) admitWork(node WorkDefinition, commit QueueCommit, position Position) error {
	pool, ok := state.Policy.Pools[node.Pool]
	if !ok || !state.allowedProducer(commit.Actor, node) {
		return queueError("admission_unauthorized", "principal has no pool/priority/account entitlement")
	}
	profile, ok := pool.Profiles[node.WorkerProfile]
	if !ok || profile.TrustDomain != node.BatchTrustDomain ||
		node.WorkID != NodeID(node.GraphID, node.NodeKey) || !validKey(node.FairnessKey) {
		return queueError("work_invalid", "node identity/profile/trust/account mismatch")
	}
	payload, err := Canonical(node.Payload)
	if err != nil || int64(len(payload)) > state.Policy.Limits.PayloadBytes {
		return queueError("payload_limit", "invalid or oversized Work payload")
	}
	if node.Subject != nil {
		if err := validateResource(*node.Subject, pool); err != nil {
			return err
		}
	}
	if existing := state.Works[node.WorkID]; existing != nil {
		if !sameJSON(existing.WorkDefinition, node) {
			return queueError("work_conflict", "immutable node %s differs", node.WorkID)
		}
		return nil
	}
	if state.AdmissionPaused {
		return queueError("admission_paused", "new Work admission is paused")
	}
	for _, other := range state.Works {
		if other.GraphID == node.GraphID && other.NodeKey == node.NodeKey {
			return queueError("node_conflict", "graph/node key already exists")
		}
	}
	if commit.Actor.Role == "worker" {
		claim, err := state.scopedClaim(commit.Actor, "", "")
		if err != nil {
			return err
		}
		parent := state.Works[claim.WorkID]
		if claim.State != "completed" || parent.State != "completed" ||
			node.Priority != parent.Priority || node.FairnessKey != parent.FairnessKey ||
			node.Pool != parent.Pool {
			return queueError("child_entitlement", "worker children require scoped Completion and inherited parent accounting scope")
		}
	}
	if node.ReplacementOf != nil {
		old := state.Works[node.ReplacementOf.WorkID]
		if old == nil || old.Barrier != "failed" || old.GraphID != node.GraphID ||
			old.Pool != node.Pool || old.Priority != node.Priority || old.FairnessKey != node.FairnessKey {
			return queueError("replacement_invalid", "replacement must inherit a delivery-failed node's scope")
		}
	}
	state.Works[node.WorkID] = &WorkState{
		WorkDefinition: node, State: "available", Position: position, Barrier: "none",
	}
	// Future opaque IDs can contain 256 maximally JSON-escaped codepoints.
	testAssignment := Assignment{
		Version: Version, DispatchID: strings.Repeat("d", 70),
		RequestID: strings.Repeat("\x01", 256), CommitID: strings.Repeat("\x01", 256), PolicyEpoch: commit.PolicyEpoch,
		Pool: node.Pool, WorkerProfile: node.WorkerProfile,
		Claims: []AssignmentClaim{{
			Handle: "h16", ClaimID: strings.Repeat("c", 70), WorkID: node.WorkID,
			Work: payload, ResultRefs: []ResultReference{},
		}},
	}
	var unresolvedBytes int64
	for _, edge := range node.DependsOn {
		if edge.Kind == "work" {
			reference := ResultReference{
				WorkID: edge.WorkID, ResultCommitID: strings.Repeat("\x01", 256),
				Descriptor: json.RawMessage(`{}`),
			}
			parent := state.Works[edge.WorkID]
			if parent != nil && parent.Barrier == "verified" {
				reference.ResultCommitID, reference.Descriptor = parent.ResultCommitID, parent.Result
			} else {
				unresolvedBytes += max(0, state.Policy.Limits.ResultBytes-2)
			}
			testAssignment.Claims[0].ResultRefs = append(testAssignment.Claims[0].ResultRefs, reference)
		}
	}
	encoded, _ := canonicalValue(testAssignment)
	assignmentBytes := int64(len(encoded)) + unresolvedBytes
	if assignmentBytes > state.Policy.Limits.AssignmentBytes {
		return queueError("assignment_limit", "Work cannot fit a single-Claim assignment")
	}
	return nil
}

func (state Projection) readiness(work *WorkState, at int64) (string, []string) {
	observations := []string{}
	for _, edge := range work.DependsOn {
		if edge.Kind == "work" {
			parent := state.Works[edge.WorkID]
			if parent == nil || parent.State == "cancelled" {
				return "dependency_failed", observations
			}
			if parent.Barrier == "failed" {
				return "dependency_delivery_failed", observations
			}
			if parent.State != "completed" || parent.Barrier != "verified" {
				return "dependency_result_unavailable", observations
			}
		} else {
			observation := state.Observations[resourceKey(*edge.Resource, edge.Condition)]
			if observation == nil || observation.State == "unknown" {
				return "external_unavailable", observations
			}
			if observation.CredentialGeneration != state.CredentialGeneration ||
				observation.ObservedAt > at ||
				at-observation.ObservedAt > state.Policy.Pools[work.Pool].MaxObservationAgeMS {
				return "observation_stale", observations
			}
			if observation.State != "ready" {
				return "dependency_external_waiting", observations
			}
			observations = append(observations, observation.ObservationID)
		}
	}
	return "ready", observations
}

type WorkExplanation struct {
	WorkID             string   `json:"work_id"`
	State              string   `json:"state"`
	Barrier            string   `json:"barrier"`
	Ready              bool     `json:"ready"`
	SchedulingEligible bool     `json:"scheduling_eligible"`
	Reason             string   `json:"reason"`
	Path               []string `json:"path"`
	Observations       []string `json:"observations"`
}

func ExplainWork(state Projection, id string, at int64) (WorkExplanation, error) {
	work := state.Works[id]
	if work == nil {
		return WorkExplanation{}, queueError("work_missing", "unknown Work %s", id)
	}
	reason, observations := state.readiness(work, at)
	result := WorkExplanation{
		WorkID: id, State: work.State, Barrier: work.Barrier,
		Ready:  work.State == "available" && reason == "ready",
		Reason: reason, Observations: observations, Path: []string{work.NodeKey},
	}
	logical, _, accounts := state.outstanding(work.Pool)
	eligibility := state.eligibility(work, at, logical, accounts)
	result.SchedulingEligible = eligibility == "ready"
	if result.Reason == "ready" && eligibility != "ready" {
		result.Reason = eligibility
	}
	if work.State != "available" {
		result.Reason = "ownership_" + work.State
	}
	current := work
	for len(result.Path) <= state.Policy.Limits.GraphNodes {
		var next *WorkState
		for _, edge := range current.DependsOn {
			if edge.Kind != "work" {
				continue
			}
			parent := state.Works[edge.WorkID]
			if parent != nil && parent.Barrier != "verified" {
				next = parent
				break
			}
		}
		if next == nil {
			break
		}
		result.Path = append(result.Path, next.NodeKey)
		current = next
	}
	return result, nil
}

func (state Projection) recordObservation(observation Observation, commit QueueCommit) error {
	key := resourceKey(observation.Resource, observation.Condition)
	authorized := false
	for _, pool := range state.Policy.Pools {
		if validateResource(observation.Resource, pool) == nil {
			authorized = true
		}
	}
	if !authorized || observation.ObservedAt > commit.At ||
		observation.CredentialGeneration != state.CredentialGeneration {
		return queueError("observation_invalid", "unauthorized identity, time, or credential generation")
	}
	if observation.Resource.Kind == "issue" && observation.Condition != "completed" && observation.Condition != "closed" ||
		observation.Resource.Kind == "pull_request" && observation.Condition != "merged" {
		return queueError("condition_invalid", "observation predicate mismatch")
	}
	if observation.State == "ready" {
		satisfies := observation.ResourceState == "closed" &&
			(observation.Condition == "closed" ||
				observation.Condition == "completed" && observation.StateReason == "completed")
		if observation.Condition == "merged" {
			satisfies = observation.Merged != nil && *observation.Merged && observation.MergeCommit != ""
		}
		if !satisfies || observation.ReadStatus != "ok" {
			return queueError("observation_invalid", "ready must have positive typed predicate evidence")
		}
	}
	for _, current := range state.Observations {
		if current.ObservationID == observation.ObservationID {
			if sameJSON(current, observation) {
				return nil
			}
			return queueError("observation_conflict", "observation identity reused")
		}
	}
	if state.ObservationWrites[key] >= state.Policy.Limits.ObservationWrites {
		return queueError("observation_limit", "finite observation-write budget exhausted")
	}
	if current := state.Observations[key]; current != nil && observation.ObservedAt < current.ObservedAt {
		return queueError("observation_invalid", "observation time moved backwards")
	}
	data, _ := json.Marshal(observation)
	if int64(len(data)) > state.Policy.Limits.EvidenceBytes {
		return queueError("evidence_limit", "observation evidence too large")
	}
	state.ObservationWrites[key]++
	state.Observations[key] = &observation
	return nil
}

func (state Projection) resultRefs(work *WorkState) []ResultReference {
	refs := []ResultReference{}
	for _, dependency := range work.DependsOn {
		if dependency.Kind == "work" {
			parent := state.Works[dependency.WorkID]
			refs = append(refs, ResultReference{
				WorkID: parent.WorkID, ResultCommitID: parent.ResultCommitID, Descriptor: parent.Result,
			})
		}
	}
	return refs
}

func (state Projection) nodeCount() int {
	nodes := map[string]bool{}
	for _, work := range state.Works {
		nodes["work:"+work.WorkID] = true
		for _, dependency := range work.DependsOn {
			if dependency.Kind != "work" {
				nodes[work.GraphID+resourceKey(*dependency.Resource, dependency.Condition)] = true
			}
		}
	}
	return len(nodes)
}
