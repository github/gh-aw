package workqueue

import (
	"context"
	"encoding/json"
	"net/http"
	"slices"
	"strconv"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
)

func (b Branch) dependencyClient(repository string) (*api.RESTClient, error) {
	if repository == b.Remote {
		return b.client, nil
	}
	if client := b.DependencyClients[repository]; client != nil {
		return client, nil
	}
	return nil, queueError("dependency_credentials_missing", "foreign repository %s needs separately bound read credentials", repository)
}

func dependencyReadStatus(err error) string {
	for _, status := range []int{401, 403, 404, 429, 500, 502, 503} {
		if hasStatus(err, status) {
			return "http_" + strconv.Itoa(status)
		}
	}
	return "read_unavailable"
}

func (b Branch) readObservation(ctx context.Context, edge Dependency, generation, id string) Observation {
	observation := Observation{
		Kind: "Observation", ObservationID: id, Resource: *edge.Resource,
		Condition: edge.Condition, State: "unknown", ObservedAt: time.Now().UnixMilli(),
		CredentialGeneration: generation, ReadStatus: "read_unavailable",
	}
	client, err := b.dependencyClient(edge.Resource.Repository)
	if err != nil {
		observation.ReadStatus = "credentials_missing"
		return observation
	}
	var repository struct {
		ID       json.Number `json:"id"`
		FullName string      `json:"full_name"`
	}
	if err := client.DoWithContext(ctx, http.MethodGet, "repos/"+edge.Resource.Repository, nil, &repository); err != nil {
		observation.ReadStatus = dependencyReadStatus(err)
		return observation
	}
	if repository.ID.String() != edge.Resource.RepositoryID || repository.FullName != edge.Resource.Repository {
		observation.ReadStatus = "resource_identity_conflict"
		return observation
	}
	if edge.Kind == "issue" {
		var issue struct {
			ID          json.Number     `json:"id"`
			Number      json.Number     `json:"number"`
			State       string          `json:"state"`
			StateReason string          `json:"state_reason"`
			PullRequest json.RawMessage `json:"pull_request"`
		}
		err := client.DoWithContext(ctx, http.MethodGet, "repos/"+edge.Resource.Repository+"/issues/"+edge.Resource.Number, nil, &issue)
		if err != nil {
			observation.ReadStatus = dependencyReadStatus(err)
			return observation
		}
		if issue.ID.String() != edge.Resource.ResourceID || issue.Number.String() != edge.Resource.Number ||
			len(issue.PullRequest) > 0 {
			observation.ReadStatus = "resource_identity_conflict"
			return observation
		}
		observation.ReadStatus = "ok"
		if !slices.Contains([]string{"open", "closed"}, issue.State) {
			observation.ReadStatus = "predicate_unknown"
			return observation
		}
		observation.ResourceState = issue.State
		if slices.Contains([]string{"completed", "not_planned", "reopened"}, issue.StateReason) {
			observation.StateReason = issue.StateReason
		}
		observation.State = "waiting"
		if issue.State == "closed" && edge.Condition == "closed" ||
			issue.State == "closed" && issue.StateReason == "completed" && edge.Condition == "completed" {
			observation.State = "ready"
		} else if issue.State == "closed" {
			if issue.StateReason == "" {
				observation.State = "unknown"
			} else {
				observation.State = "failed"
			}
		}
	} else {
		var pull struct {
			ID       json.Number `json:"id"`
			Number   json.Number `json:"number"`
			State    string      `json:"state"`
			Merged   *bool       `json:"merged"`
			MergeSHA string      `json:"merge_commit_sha"`
		}
		err := client.DoWithContext(ctx, http.MethodGet, "repos/"+edge.Resource.Repository+"/pulls/"+edge.Resource.Number, nil, &pull)
		if err != nil {
			observation.ReadStatus = dependencyReadStatus(err)
			return observation
		}
		if pull.ID.String() != edge.Resource.ResourceID || pull.Number.String() != edge.Resource.Number {
			observation.ReadStatus = "resource_identity_conflict"
			return observation
		}
		observation.ReadStatus = "ok"
		if pull.Merged == nil || !slices.Contains([]string{"open", "closed"}, pull.State) {
			observation.ReadStatus = "predicate_unknown"
			return observation
		}
		observation.Merged = pull.Merged
		observation.ResourceState = pull.State
		if *pull.Merged && pull.MergeSHA != "" {
			observation.State, observation.MergeCommit = "ready", pull.MergeSHA
		} else if pull.State == "closed" {
			observation.State = "failed"
		} else {
			observation.State = "waiting"
		}
	}
	return observation
}

func (b Branch) refreshForDispatch(ctx context.Context, state Projection, poolName, requestID string) ([]Observation, error) {
	pool, ok := state.Policy.Pools[poolName]
	if !ok {
		return nil, queueError("pool_invalid", "unknown pool %s", poolName)
	}
	at := time.Now().UnixMilli()
	works := []*WorkState{}
	for _, work := range state.Works {
		if work.Pool == poolName && work.State == "available" && work.RetryNotBefore <= at {
			works = append(works, work)
		}
	}
	slices.SortFunc(works, func(a, b *WorkState) int {
		if positionLess(a.Position, b.Position) {
			return -1
		}
		if positionLess(b.Position, a.Position) {
			return 1
		}
		return 0
	})
	observations := []Observation{}
	seen := map[string]bool{}
	budget := min(255, state.Policy.Limits.Operations-1)
	if state.GrantsPaused {
		return observations, nil
	}
	for _, work := range works {
		workReady := true
		for _, edge := range work.DependsOn {
			if edge.Kind == "work" && state.Works[edge.WorkID].Barrier != "verified" {
				workReady = false
			}
		}
		if !workReady {
			continue
		}
		for _, edge := range work.DependsOn {
			if edge.Kind == "work" {
				continue
			}
			key := resourceKey(*edge.Resource, edge.Condition)
			if seen[key] {
				continue
			}
			seen[key] = true
			current := state.Observations[key]
			if current != nil &&
				current.CredentialGeneration == state.CredentialGeneration &&
				current.ObservedAt <= at && at-current.ObservedAt <= pool.MaxObservationAgeMS {
				continue
			}
			if len(observations) >= budget {
				return observations, nil
			}
			id := "o_" + hashBytes([]byte(requestID+"\n"+state.Tip+"\n"+key+"\n"+strconv.FormatInt(at, 10)))
			observations = append(observations, b.readObservation(ctx, edge, state.CredentialGeneration, id))
		}
	}
	return observations, nil
}
