package aw

import "github.com/github/gh-aw/pkg/constants"

// JobDependencyGraph follows needs edges, including transitive dependencies.
type JobDependencyGraph map[string][]string

// DependsOn is cycle-safe so callers can inspect a graph before validation.
func (graph JobDependencyGraph) DependsOn(job, dependency string) bool {
	visited := make(map[string]struct{})
	pending := graph[job]
	for len(pending) > 0 {
		current := pending
		pending = nil
		for _, id := range current {
			if id == dependency {
				return true
			}
			if _, seen := visited[id]; !seen {
				visited[id] = struct{}{}
				pending = append(pending, graph[id]...)
			}
		}
	}
	return false
}

// IsPreAgentCustomJob reports whether the graph proves that a custom job runs
// strictly before agent, without a dependency on agent or detection data.
func (graph JobDependencyGraph) IsPreAgentCustomJob(job string) bool {
	return !constants.IsCompilerOwnedJobName(job) &&
		graph.DependsOn(string(constants.AgentJobName), job) &&
		!graph.DependsOn(job, string(constants.AgentJobName)) &&
		!graph.DependsOn(job, string(constants.DetectionJobName))
}

// AuthoredJobDependencies includes the compiler-owned post-agent dependencies.
// Malformed needs are left to schema validation; they never create graph edges.
func AuthoredJobDependencies(jobs map[string]any) JobDependencyGraph {
	graph := make(JobDependencyGraph)
	for id := range constants.KnownBuiltInJobNames {
		switch constants.JobName(id) {
		case constants.PreActivationJobName, constants.PreActivationHyphenJobName,
			constants.ActivationJobName, constants.AdmissionJobName, constants.AgentJobName:
		default:
			graph[id] = []string{string(constants.AgentJobName)}
		}
	}
	for id, dependency := range constants.MaintenanceJobDependencies {
		if _, authored := jobs[id]; !authored {
			graph[id] = []string{string(dependency)}
		}
	}
	for id, value := range jobs {
		fields, ok := value.(map[string]any)
		if !ok {
			continue
		}
		switch needs := fields["needs"].(type) {
		case string:
			graph[id] = append(graph[id], needs)
		case []any:
			for _, need := range needs {
				if dependency, ok := need.(string); ok {
					graph[id] = append(graph[id], dependency)
				}
			}
		case []string:
			graph[id] = append(graph[id], needs...)
		}
	}
	return graph
}
