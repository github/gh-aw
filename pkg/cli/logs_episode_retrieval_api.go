package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/github/gh-aw/pkg/workflow"
)

type episodeAPIRequest func(context.Context, string, string) ([]byte, error)

func requestEpisodeAPI(ctx context.Context, host, endpoint string) ([]byte, error) {
	return workflow.RunGHContext(ctx, "Retrieving episode evidence...", "api", "--hostname", host, endpoint)
}

func (c *episodeCollector) api(ctx context.Context, endpoint string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if c.report.APIRequests >= c.opts.MaxAPIRequests {
		return nil, errEpisodeAPILimit
	}
	c.report.APIRequests++
	output, err := c.request(ctx, c.report.Host, endpoint)
	if err != nil {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		return nil, fmt.Errorf("retrieve %s: %w", endpoint, err)
	}
	return output, nil
}

func (c *episodeCollector) getRun(ctx context.Context, candidate episodeCandidate) (WorkflowRun, error) {
	endpoint := fmt.Sprintf("repos/%s/actions/runs/%d", candidate.repository, candidate.runID)
	data, err := c.api(ctx, endpoint)
	if err != nil {
		return WorkflowRun{}, err
	}
	return parseWorkflowRunAPIResponse(data)
}

func (c *episodeCollector) listRuns(ctx context.Context, repository string, page int) ([]WorkflowRun, error) {
	data, err := c.api(ctx, c.runListEndpoint(repository, page))
	if err != nil {
		return nil, err
	}
	var response struct {
		TotalCount int                      `json:"total_count"`
		Runs       []workflowRunAPIResponse `json:"workflow_runs"`
	}
	if err := json.Unmarshal(data, &response); err != nil {
		return nil, fmt.Errorf("decode run discovery page: %w", err)
	}
	if page == 10 && response.TotalCount > 1000 {
		c.issue(episodeCandidate{repository: repository}, "search_result_limit", errors.New("GitHub limits filtered run discovery to 1000 results; narrow the time window"))
		c.stop("search_result_limit")
	}
	runs := make([]WorkflowRun, 0, len(response.Runs))
	for _, responseRun := range response.Runs {
		runs = append(runs, responseRun.workflowRun())
	}
	return runs, nil
}

type episodeArtifact struct {
	ID          int64     `json:"id"`
	Name        string    `json:"name"`
	Expired     bool      `json:"expired"`
	SizeInBytes int64     `json:"size_in_bytes"`
	CreatedAt   time.Time `json:"created_at"`
}

func (c *episodeCollector) listArtifacts(ctx context.Context, run WorkflowRun) ([]episodeArtifact, error) {
	var artifacts []episodeArtifact
	for page := 1; ; page++ {
		endpoint := fmt.Sprintf("repos/%s/actions/runs/%d/artifacts?per_page=100&page=%d", run.Repository, run.DatabaseID, page)
		data, err := c.api(ctx, endpoint)
		if err != nil {
			return nil, err
		}
		var response struct {
			Artifacts []episodeArtifact `json:"artifacts"`
		}
		if err := json.Unmarshal(data, &response); err != nil {
			return nil, fmt.Errorf("decode artifact discovery page: %w", err)
		}
		for _, artifact := range response.Artifacts {
			filter := []string{"info", "usage"}
			if c.opts.Outcomes || c.opts.Delivery {
				filter = append(filter, artifactSetArtifacts[ArtifactSetOutcomes]...)
			}
			if artifact.ID <= 0 || !artifactMatchesFilter(artifact.Name, filter) {
				continue
			}
			artifacts = append(artifacts, artifact)
		}
		if len(response.Artifacts) < 100 {
			return artifacts, nil
		}
	}
}
