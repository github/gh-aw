package workqueue

import (
	"encoding/json"
	"slices"
	"strings"
)

func validateProjectors(policy Policy) error {
	if policy.Projectors != nil && (len(policy.Projectors) == 0 || len(policy.Projectors) > 256) {
		return queueError("policy_invalid", "projectors require 1..256 installed rules")
	}
	for _, rule := range policy.Projectors {
		if !decimalIdentity(rule.Principal) || !revisionPattern.MatchString(rule.Ref) ||
			!strings.HasPrefix(rule.Workflow, workerWorkflowDirectory) ||
			!strings.HasSuffix(rule.Workflow, ".lock.yml") || strings.Contains(rule.Workflow, "..") ||
			len(rule.Pools) == 0 || len(rule.Pools) > 256 || len(rule.Repositories) == 0 || len(rule.Repositories) > 256 {
			return queueError("policy_invalid", "projectors require explicit immutable workflow and target authority")
		}
		for _, pool := range rule.Pools {
			if _, ok := policy.Pools[pool]; !ok {
				return queueError("policy_invalid", "projector pool is not installed")
			}
		}
		for _, repository := range rule.Repositories {
			if !repoPattern.MatchString(repository) {
				return queueError("policy_invalid", "invalid projector repository")
			}
		}
		if rule.CompletionPolicy != "" && rule.CompletionPolicy != "keep-open" && rule.CompletionPolicy != "close-on-result" {
			return queueError("policy_invalid", "invalid projector completion policy")
		}
		if rule.BackingIssues != nil && (len(rule.BackingIssues) == 0 || len(rule.BackingIssues) > 256) {
			return queueError("policy_invalid", "backing Issues require 1..256 explicit targets")
		}
		for _, resource := range rule.BackingIssues {
			if resource.Kind != "issue" {
				return queueError("policy_invalid", "backing target must be an Issue")
			}
			if err := validateResource(resource, PoolPolicy{AllowedRepositories: rule.Repositories}); err != nil {
				return queueError("policy_invalid", "invalid backing Issue target: %v", err)
			}
		}
	}
	return nil
}

func issueIdentity(resource Resource) string {
	return resource.Host + ":" + resource.RepositoryID + ":" + resource.ResourceID
}

func backingIssue(work *WorkState) *Resource {
	if work.BackingIssue != nil {
		return work.BackingIssue
	}
	return work.IssueLink
}

func (state Projection) projectionAuthority(actor Actor, workID, ref, repository, claimID string) error {
	work := state.Works[workID]
	if work == nil || actor.Role != "projector" || actor.Workflow == "" || actor.RunID == "" || actor.RunAttempt < 1 {
		return queueError("projection_unauthorized", "projection requires an originating run")
	}
	awOwned := state.Policy.Authorization == "aw" && work.BackingIssue == nil &&
		repository == state.Repository && actor.Repository == state.Repository
	if awOwned && (!revisionPattern.MatchString(ref) ||
		!strings.HasPrefix(actor.Workflow, workerWorkflowDirectory) ||
		!strings.HasSuffix(actor.Workflow, ".lock.yml") || strings.Contains(actor.Workflow, "..")) {
		return queueError("projection_unauthorized", "AW Issue projection requires an immutable compiled originating workflow")
	}
	installed := slices.ContainsFunc(state.Policy.Projectors, func(rule ProjectorRule) bool {
		return rule.Principal == actor.Principal && rule.Workflow == actor.Workflow && rule.Ref == ref &&
			slices.Contains(rule.Pools, work.Pool) && slices.Contains(rule.Repositories, repository) &&
			(work.BackingIssue == nil || slices.Contains(rule.BackingIssues, *work.BackingIssue))
	})
	if !awOwned && !installed {
		return queueError("projection_unauthorized", "no installed projector authority for this revision and target")
	}
	if claimID != "" {
		claim := state.Claims[claimID]
		if claim == nil || claim.WorkID != workID {
			return queueError("projection_unauthorized", "foreign Claim")
		}
		dispatch := state.Dispatches[claim.DispatchID]
		if dispatch == nil || dispatch.Run == nil {
			return queueError("projection_unauthorized", "Claim has no authenticated run")
		}
		run := dispatch.Run
		if actor.DispatchID != dispatch.DispatchID || run.Repository != actor.Repository ||
			run.Workflow != actor.Workflow || run.Ref != ref || run.Principal != actor.Principal ||
			run.RunID != actor.RunID || actor.RunAttempt != 1 || run.RunAttempt != actor.RunAttempt ||
			!slices.ContainsFunc(dispatch.Claims, func(member AssignmentClaim) bool {
				return member.ClaimID == claimID && member.WorkID == workID && member.Handle == claim.Handle
			}) {
			return queueError("projection_unauthorized", "projection is outside original authenticated Claims")
		}
		return nil
	}
	origin := state.WorkCreators[workID]
	if awOwned && !slices.Contains([]string{"producer", "dispatcher", "worker"}, origin.Role) {
		return queueError("projection_unauthorized", "AW Issue projection requires its own trusted producer or worker admission")
	}
	if origin.Principal == actor.Principal && origin.Repository == actor.Repository &&
		origin.Workflow == actor.Workflow && origin.RunID == actor.RunID && origin.RunAttempt == actor.RunAttempt {
		return nil
	}
	return queueError("projection_unauthorized", "projection is outside this run's checked admissions")
}

func (state Projection) backingIssueAuthorized(pool string, resource Resource) bool {
	return slices.ContainsFunc(state.Policy.Projectors, func(rule ProjectorRule) bool {
		return slices.Contains(rule.Pools, pool) && slices.Contains(rule.Repositories, resource.Repository) &&
			slices.Contains(rule.BackingIssues, resource)
	})
}

func (state Projection) validateBackingIssueAdmission(node WorkDefinition, pool PoolPolicy) error {
	if node.BackingIssue == nil {
		return nil
	}
	if node.BackingIssue.Kind != "issue" {
		return queueError("resource_unauthorized", "backing resource must be an Issue")
	}
	if err := validateResource(*node.BackingIssue, pool); err != nil {
		return err
	}
	if !state.backingIssueAuthorized(node.Pool, *node.BackingIssue) {
		return queueError("resource_unauthorized", "pre-existing backing Issue requires an installed projector exact-target grant")
	}
	for _, other := range state.Works {
		resource := backingIssue(other)
		if resource != nil && other.WorkID != node.WorkID && issueIdentity(*resource) == issueIdentity(*node.BackingIssue) {
			return queueError("issue_binding_conflict", "one Work per backing Issue")
		}
	}
	return nil
}

func (state Projection) applyIssueBinding(operation Operation, kind string, commit QueueCommit) error {
	var link IssueLinkOperation
	var comment IssueCommentOperation
	if kind == "IssueLink" {
		if err := json.Unmarshal(operation, &link); err != nil {
			return err
		}
	} else {
		if err := json.Unmarshal(operation, &comment); err != nil {
			return err
		}
		link.WorkID, link.ProjectorRef, link.ClaimID = comment.WorkID, comment.ProjectorRef, comment.ClaimID
		if comment.AuthorityClaimID != "" {
			link.ClaimID = comment.AuthorityClaimID
		}
	}
	work := state.Works[link.WorkID]
	if work == nil {
		return queueError("issue_binding_invalid", "missing Work")
	}
	resource := backingIssue(work)
	if kind == "IssueLink" {
		resource = &link.Resource
	}
	if resource == nil || resource.Kind != "issue" {
		return queueError("issue_binding_invalid", "binding requires an Issue")
	}
	if err := validateResource(*resource, state.Policy.Pools[work.Pool]); err != nil {
		return err
	}
	if err := state.projectionAuthority(commit.Actor, link.WorkID, link.ProjectorRef, resource.Repository, link.ClaimID); err != nil {
		return err
	}
	if kind == "IssueLink" {
		return state.bindBackingIssue(work, resource)
	}
	return state.bindIssueComment(work, comment)
}

func (state Projection) bindBackingIssue(work *WorkState, resource *Resource) error {
	if existing := backingIssue(work); existing != nil && !sameJSON(existing, resource) {
		return queueError("issue_binding_conflict", "backing Issue cannot be rebound")
	}
	for _, other := range state.Works {
		bound := backingIssue(other)
		if bound != nil && other.WorkID != work.WorkID && issueIdentity(*bound) == issueIdentity(*resource) {
			return queueError("issue_binding_conflict", "one Work per backing Issue")
		}
	}
	if work.BackingIssue == nil {
		work.IssueLink = resource
	}
	return nil
}

func (state Projection) bindIssueComment(work *WorkState, comment IssueCommentOperation) error {
	for _, other := range state.Works {
		if other.IssueSummary == comment.CommentID && (other.WorkID != work.WorkID || comment.ClaimID != "") {
			return queueError("issue_binding_conflict", "comment handle already belongs to another summary")
		}
	}
	for _, claim := range state.Claims {
		if claim.IssueComment == comment.CommentID && claim.ClaimID != comment.ClaimID {
			return queueError("issue_binding_conflict", "comment handle already belongs to another Claim")
		}
	}
	if comment.ClaimID != "" {
		if comment.AuthorityClaimID != "" && comment.AuthorityClaimID != comment.ClaimID {
			return queueError("projection_unauthorized", "Claim comment requires its own original Claim")
		}
		claim := state.Claims[comment.ClaimID]
		if claim == nil || claim.WorkID != work.WorkID {
			return queueError("issue_binding_invalid", "comment names a foreign Claim")
		}
		if claim.IssueComment != "" && claim.IssueComment != comment.CommentID {
			return queueError("issue_binding_conflict", "Claim comment cannot be rebound")
		}
		claim.IssueComment = comment.CommentID
	} else {
		if work.IssueSummary != "" && work.IssueSummary != comment.CommentID {
			return queueError("issue_binding_conflict", "canonical summary cannot be rebound")
		}
		work.IssueSummary = comment.CommentID
	}
	return nil
}
