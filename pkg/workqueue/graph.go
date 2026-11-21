package workqueue

import (
	"maps"
	"slices"
	"strings"
)

var (
	workRequired     = []string{"kind", "work_id", "graph_id", "node_key", "pool", "priority", "fairness_key", "worker_profile", "payload", "depends_on"}
	workOptional     = []string{"subject", "backing_issue", "replacement_of"}
	resourceFields   = []string{"kind", "host", "repository", "repository_id", "resource_id", "number"}
	issueConditions  = []string{"completed", "closed"}
	pullConditions   = []string{"merged"}
	observationState = []string{"ready", "waiting", "failed", "unknown"}
)

// GateKey identifies an external dependency gate independently of repository renames.
func GateKey(resource Resource, condition string) string {
	return canonicalStringObject(map[string]string{"host": resource.Host, "repository_id": resource.RepositoryID, "resource_id": resource.ResourceID, "kind": resource.Kind, "condition": condition})
}

func issueKey(resource Resource) string {
	return canonicalStringArray(resource.Host, resource.RepositoryID, resource.ResourceID)
}

func conditionsFor(kind string) []string {
	if kind == "issue" {
		return issueConditions
	}
	return pullConditions
}

// validateResourceValue validates a generic Issue or Pull Request coordinate and returns its typed form.
func validateResourceValue(value any) (Resource, error) {
	resource, err := closed(value, resourceFields, nil, "resource", "resource_invalid")
	if err != nil {
		return Resource{}, err
	}
	kind, ok := resource["kind"].(string)
	if !ok || kind != "issue" && kind != "pull_request" {
		return Resource{}, queueError("resource_invalid", "resource kind must be Issue or Pull Request")
	}
	host, err := identity(resource["host"], "host")
	if err != nil {
		return Resource{}, err
	}
	repository, err := identity(resource["repository"], "repository")
	if err != nil {
		return Resource{}, err
	}
	expected, err := expectedResourceHost()
	if err != nil {
		return Resource{}, err
	}
	if host != expected || !repositoryPattern.MatchString(repository) {
		return Resource{}, queueError("resource_unauthorized", "resource host/repository must be normalized GitHub coordinates")
	}
	repositoryID, err := decimal(resource["repository_id"], "repository_id", "resource_invalid")
	if err != nil {
		return Resource{}, err
	}
	resourceID, err := decimal(resource["resource_id"], "resource_id", "resource_invalid")
	if err != nil {
		return Resource{}, err
	}
	number, err := decimal(resource["number"], "number", "resource_invalid")
	if err != nil {
		return Resource{}, err
	}
	return Resource{Kind: kind, Host: host, Repository: repository, RepositoryID: repositoryID, ResourceID: resourceID, Number: number}, nil
}

// validateDependencyValue validates one predecessor edge and returns its edge key.
func validateDependencyValue(value any) (string, error) {
	if dependency, ok := value.(map[string]any); ok && dependency["kind"] == "work" {
		if _, err := closed(dependency, []string{"kind", "work_id"}, nil, "Work edge", "dependency_invalid"); err != nil {
			return "", err
		}
		id, err := identity(dependency["work_id"], "predecessor Work")
		if err != nil {
			return "", err
		}
		return "work:" + id, nil
	}
	dependency, err := closed(value, []string{"kind", "resource", "condition"}, nil, "external edge", "dependency_invalid")
	if err != nil {
		return "", err
	}
	resource, err := validateResourceValue(dependency["resource"])
	if err != nil {
		return "", err
	}
	condition, ok := dependency["condition"].(string)
	if dependency["kind"] != resource.Kind || !ok || !slices.Contains(conditionsFor(resource.Kind), condition) {
		return "", queueError("dependency_invalid", "typed external condition does not match resource")
	}
	return "gate:" + GateKey(resource, condition), nil
}

func edgeKey(dependency Dependency) string {
	if dependency.Kind == "work" {
		return "work:" + dependency.WorkID
	}
	return "gate:" + GateKey(*dependency.Resource, dependency.Condition)
}

// validateWorkValue is the structural validation of one Work operation.
func validateWorkValue(value any) (object, error) {
	node, err := closed(value, workRequired, workOptional, "Work", "request_invalid")
	if err != nil {
		return nil, err
	}
	if node["kind"] != "Work" {
		return nil, queueError("request_invalid", "graph admission requires Work operations")
	}
	identities := make(map[string]string, 5)
	for _, field := range []string{"work_id", "graph_id", "node_key", "pool", "worker_profile"} {
		text, err := identity(node[field], field)
		if err != nil {
			return nil, err
		}
		identities[field] = text
	}
	workID := identities["work_id"]
	if workID != NodeID(identities["graph_id"], identities["node_key"]) {
		return nil, queueError("work_identity_invalid", "Work identity must bind graph_id/node_key")
	}
	if _, err := integer(node["priority"], 1, 5, "priority"); err != nil {
		return nil, err
	}
	if _, err := identityBounded(node["fairness_key"], "fairness key", true, 128); err != nil {
		return nil, err
	}
	if !plainObject(node["payload"]) {
		return nil, queueError("work_invalid", "payload must be a JSON object")
	}
	if err := boundedBytes(node["payload"], PayloadBytes, "Work payload"); err != nil {
		return nil, err
	}
	workEdges, err := validateWorkEdges(node["depends_on"])
	if err != nil {
		return nil, err
	}
	if err := validateWorkReferences(node, workID); err != nil {
		return nil, err
	}
	payload, err := canonicalGeneric(node["payload"])
	if err != nil {
		return nil, err
	}
	if len(payload)+workEdges*EffectsBytes > AssignmentBytes {
		return nil, queueError("assignment_limit", "Work payload and predecessor effects cannot fit one assignment")
	}
	return node, nil
}

// validateWorkEdges validates a Work's predecessor list and returns its count of Work edges.
func validateWorkEdges(value any) (int, error) {
	edges, ok := value.([]any)
	if !ok || len(edges) > Predecessors {
		return 0, queueError("resource_limit", "Work may declare at most %d predecessors", Predecessors)
	}
	seen := map[string]struct{}{}
	workEdges := 0
	for _, edge := range edges {
		key, err := validateDependencyValue(edge)
		if err != nil {
			return 0, err
		}
		if _, duplicate := seen[key]; duplicate {
			return 0, queueError("dependency_invalid", "duplicate predecessor")
		}
		seen[key] = struct{}{}
		if strings.HasPrefix(key, "work:") {
			workEdges++
		}
	}
	return workEdges, nil
}

// validateWorkReferences validates a Work's optional subject, backing Issue and replacement.
func validateWorkReferences(node object, workID string) error {
	if has(node, "subject") {
		if _, err := validateResourceValue(node["subject"]); err != nil {
			return err
		}
	}
	if has(node, "backing_issue") {
		resource, err := validateResourceValue(node["backing_issue"])
		if err != nil {
			return err
		}
		if resource.Kind != "issue" {
			return queueError("resource_invalid", "backing Issue must be an Issue")
		}
	}
	if has(node, "replacement_of") {
		replacement, err := closed(node["replacement_of"], []string{"work_id"}, nil, "replacement", "request_invalid")
		if err != nil {
			return err
		}
		replaced, err := identity(replacement["work_id"], "replaced Work")
		if err != nil {
			return err
		}
		if replaced == workID {
			return queueError("work_invalid", "Work cannot replace itself")
		}
	}
	return nil
}

// ValidateWork checks one typed Work definition structurally.
func ValidateWork(work WorkDefinition) error {
	value, err := genericValue(work)
	if err != nil {
		return err
	}
	_, err = validateWorkValue(value)
	return err
}

// NewWork builds a Work definition whose identity binds graph and node key.
func NewWork(graphID, nodeKey, pool, profile, fairnessKey string, priority int, payload []byte, dependsOn []Dependency) (WorkDefinition, error) {
	canonicalPayload, err := Canonical(payload)
	if err != nil {
		return WorkDefinition{}, err
	}
	if dependsOn == nil {
		dependsOn = []Dependency{}
	}
	work := WorkDefinition{Kind: "Work", WorkID: NodeID(graphID, nodeKey), GraphID: graphID, NodeKey: nodeKey, Pool: pool, Priority: priority, FairnessKey: fairnessKey, WorkerProfile: profile, Payload: canonicalPayload, DependsOn: dependsOn}
	return work, ValidateWork(work)
}

func pendingWork(state string) bool {
	return state == "available" || state == "claimed"
}

type graphIndex struct {
	pool     string
	vertices map[string]struct{}
}

// admissionIndex is derived bookkeeping, rebuilt from Works whenever absent.
type admissionIndex struct {
	graphs  map[string]*graphIndex
	pending map[string]int
	issues  map[string]string
}

func (state *Projection) admissionIndex() *admissionIndex {
	if state.admission == nil {
		index := &admissionIndex{graphs: map[string]*graphIndex{}, pending: map[string]int{}, issues: map[string]string{}}
		for _, work := range state.Works {
			index.add(work)
		}
		state.admission = index
	}
	return state.admission
}

func (index *admissionIndex) add(work *WorkState) {
	graph := index.graphs[work.GraphID]
	if graph == nil {
		graph = &graphIndex{pool: work.Pool, vertices: map[string]struct{}{}}
		index.graphs[work.GraphID] = graph
	}
	graph.vertices["work:"+work.WorkID] = struct{}{}
	for _, edge := range work.DependsOn {
		if edge.Kind != "work" {
			graph.vertices[edgeKey(edge)] = struct{}{}
		}
	}
	if pendingWork(work.State) {
		index.pending[work.Pool]++
	}
	if work.BackingIssue != nil {
		index.issues[issueKey(*work.BackingIssue)] = work.WorkID
	}
}

func (state *Projection) setWorkState(work *WorkState, next string) {
	previous := work.State
	work.State = next
	if state.admission != nil && pendingWork(previous) != pendingWork(next) {
		if pendingWork(next) {
			state.admission.pending[work.Pool]++
		} else {
			state.admission.pending[work.Pool]--
		}
	}
}

// validateGraphAdmission validates new Work against the projection: DAG shape,
// graph bounds and the per-pool pending ceiling. Existing Work is never re-validated.
func (state *Projection) validateGraphAdmission(nodes []any) error {
	index := state.admissionIndex()
	admitted, order, err := admitWorkNodes(index, nodes)
	if err != nil {
		return err
	}
	if err := state.checkAdmissionBounds(index, admitted, order); err != nil {
		return err
	}
	return detectCycles(admitted)
}

// admitWorkNodes validates and decodes new Work in submission order and enforces
// one Work per backing Issue.
func admitWorkNodes(index *admissionIndex, nodes []any) (map[string]WorkDefinition, []string, error) {
	admitted := map[string]WorkDefinition{}
	order := []string{}
	for _, value := range nodes {
		node, err := validateWorkValue(value)
		if err != nil {
			return nil, nil, err
		}
		var work WorkDefinition
		if err := decodeInto(node, &work); err != nil {
			return nil, nil, err
		}
		if _, duplicate := admitted[work.WorkID]; duplicate {
			return nil, nil, queueError("graph_invalid", "duplicate node key within one submission")
		}
		admitted[work.WorkID] = work
		order = append(order, work.WorkID)
		if work.BackingIssue == nil {
			continue
		}
		key := issueKey(*work.BackingIssue)
		if owner, ok := index.issues[key]; ok && owner != work.WorkID {
			return nil, nil, queueError("issue_binding_conflict", "one Work per backing Issue")
		}
		for _, id := range order[:len(order)-1] {
			other := admitted[id]
			if other.BackingIssue != nil && issueKey(*other.BackingIssue) == key {
				return nil, nil, queueError("issue_binding_conflict", "one Work per backing Issue")
			}
		}
	}
	return admitted, order, nil
}

// checkAdmissionBounds checks predecessors, graph pools, graph vertex counts and
// per-pool pending counts of new Work.
func (state *Projection) checkAdmissionBounds(index *admissionIndex, admitted map[string]WorkDefinition, order []string) error {
	graphVertices := map[string]map[string]struct{}{}
	graphOrder := []string{}
	graphPools := map[string]string{}
	pending := map[string]int{}
	for _, id := range order {
		node := admitted[id]
		existing := index.graphs[node.GraphID]
		vertices, ok := graphVertices[node.GraphID]
		if !ok {
			vertices = map[string]struct{}{}
			if existing != nil {
				maps.Copy(vertices, existing.vertices)
			}
			graphOrder = append(graphOrder, node.GraphID)
		}
		vertices["work:"+node.WorkID] = struct{}{}
		if err := state.addEdgeVertices(node, admitted, vertices); err != nil {
			return err
		}
		graphVertices[node.GraphID] = vertices
		pool, known := graphPools[node.GraphID]
		if !known && existing != nil {
			pool, known = existing.pool, true
		}
		if known && pool != node.Pool {
			return queueError("graph_invalid", "graph must stay in one pool")
		}
		graphPools[node.GraphID] = node.Pool
		count, counted := pending[node.Pool]
		if !counted {
			count = index.pending[node.Pool]
		}
		pending[node.Pool] = count + 1
	}
	for _, graph := range graphOrder {
		if len(graphVertices[graph]) > GraphNodes {
			return queueError("resource_limit", "graph exceeds %d Work and gate vertices", GraphNodes)
		}
	}
	for _, count := range pending {
		if count > MaxPendingWork {
			return queueError("resource_limit", "pool exceeds %d pending Work", MaxPendingWork)
		}
	}
	return nil
}

// addEdgeVertices requires every Work predecessor of node to exist in its graph and
// pool, and adds its external gates to the graph vertices.
func (state *Projection) addEdgeVertices(node WorkDefinition, admitted map[string]WorkDefinition, vertices map[string]struct{}) error {
	for _, edge := range node.DependsOn {
		if edge.Kind != "work" {
			vertices[edgeKey(edge)] = struct{}{}
			continue
		}
		graph, pool, found := state.predecessorPlacement(edge.WorkID, admitted)
		if !found || graph != node.GraphID || pool != node.Pool {
			return queueError("dependency_invalid", "missing, foreign, or self predecessor of %s", node.NodeKey)
		}
	}
	return nil
}

// predecessorPlacement returns the graph and pool of new or existing Work.
func (state *Projection) predecessorPlacement(id string, admitted map[string]WorkDefinition) (graph, pool string, found bool) {
	if node, ok := admitted[id]; ok {
		return node.GraphID, node.Pool, true
	}
	if work, ok := state.Works[id]; ok {
		return work.GraphID, work.Pool, true
	}
	return "", "", false
}

// detectCycles reports the first Work dependency cycle among newly admitted Work,
// visiting depth-first roots in Work identity order.
func detectCycles(admitted map[string]WorkDefinition) error {
	colors := map[string]int{}
	path := []string{}
	var visit func(id string) error
	visit = func(id string) error {
		colors[id] = 1
		path = append(path, id)
		for _, edge := range admitted[id].DependsOn {
			if _, isNew := admitted[edge.WorkID]; edge.Kind != "work" || !isNew || colors[edge.WorkID] == 2 {
				continue
			}
			if colors[edge.WorkID] == 1 {
				cycle := []string{}
				for _, entry := range path[slices.Index(path, edge.WorkID):] {
					cycle = append(cycle, admitted[entry].NodeKey)
				}
				cycle = append(cycle, admitted[edge.WorkID].NodeKey)
				return queueError("dependency_cycle", "%s", strings.Join(cycle, " -> "))
			}
			if err := visit(edge.WorkID); err != nil {
				return err
			}
		}
		colors[id] = 2
		path = path[:len(path)-1]
		return nil
	}
	for _, id := range slices.Sorted(maps.Keys(admitted)) {
		if colors[id] == 2 {
			continue
		}
		if err := visit(id); err != nil {
			return err
		}
	}
	return nil
}

// DependencyStatus reports whether every predecessor edge of work is satisfied.
func (state Projection) DependencyStatus(work *WorkState) (bool, string) {
	for _, edge := range work.DependsOn {
		if edge.Kind == "work" {
			parent := state.Works[edge.WorkID]
			if parent == nil || parent.State == "cancelled" {
				return false, "dependency_failed"
			}
			if parent.State != "completed" {
				return false, "dependency_pending"
			}
			continue
		}
		observation, ok := state.Observations[GateKey(*edge.Resource, edge.Condition)]
		if !ok {
			return false, "external_unobserved"
		}
		if observation.State != "ready" {
			return false, "external_waiting"
		}
	}
	return true, "ready"
}

func (state Projection) resultReferences(work *WorkState) ([]ResultReference, error) {
	refs := []ResultReference{}
	for _, edge := range work.DependsOn {
		if edge.Kind != "work" {
			continue
		}
		parent := state.Works[edge.WorkID]
		if parent == nil || parent.State != "completed" {
			return nil, queueError("dependency_pending", "predecessor Work has not completed")
		}
		refs = append(refs, ResultReference{WorkID: parent.WorkID, ResultCommitID: parent.CompletionID, Descriptor: parent.Effects})
	}
	return refs, nil
}
