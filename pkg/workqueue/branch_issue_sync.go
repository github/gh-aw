package workqueue

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/cli/go-gh/v2/pkg/api"
	"github.com/github/gh-aw/pkg/githubapi"
)

type IssueSynchronization struct {
	RequestID    string                        `json:"request_id"`
	Synchronized int                           `json:"synchronized"`
	Disabled     bool                          `json:"disabled,omitempty"`
	Pending      []IssueSynchronizationPending `json:"pending,omitempty"`
}

type issueGraphQL func(context.Context, string, map[string]any, any) error

var issueLabelWhitespace = regexp.MustCompile("[\\t-\\r \u00a0\u1680\u2000-\u200a\u2028\u2029\u202f\u205f\u3000\ufeff]+")

// SynchronizeIssueTransition is an explicit maintain/admin action over one
// checked terminal request, never a search for dirty Work or a workflow rerun.
func (b Branch) SynchronizeIssueTransition(ctx context.Context, requestID string) (result IssueSynchronization, err error) {
	return b.synchronizeConfiguredIssueTransition(ctx, requestID, nil)
}

// SynchronizePublishedIssueTransition retains a native publisher's existing
// writer authority, but only for its exact checked publication, not arbitrary recovery.
func (b Branch) SynchronizePublishedIssueTransition(ctx context.Context, publication *Publication) (IssueSynchronization, error) {
	if publication == nil || publication.Commit == nil {
		return IssueSynchronization{}, nil
	}
	return b.synchronizeConfiguredIssueTransition(ctx, publication.Commit.Request.ID, publication.Commit)
}

func (b Branch) synchronizeConfiguredIssueTransition(ctx context.Context, requestID string, publication *QueueCommit) (result IssueSynchronization, err error) {
	defer func() {
		if err == nil {
			return
		}
		if hasStatus(err, http.StatusUnauthorized) || hasStatus(err, http.StatusForbidden) {
			err = queueError("projection_unauthorized", "GitHub denied Issue synchronization; verify native API access and the appropriate publisher or recovery permission")
			return
		}
		code := "projection_unavailable"
		var protocol *ProtocolError
		if errors.As(err, &protocol) {
			code = protocol.Code
		}
		err = queueError(code, "Issue synchronization remains pending; inspect the exact request and immutable receipts; never recreate mirrors or remove uncertain locks")
	}()
	result = IssueSynchronization{RequestID: requestID}
	if !validIdentity(requestID, false, 256) {
		return result, queueError("request_invalid", "synchronization requires --request-id for one committed terminal request")
	}
	b, err = b.withClient()
	if err != nil {
		return result, err
	}
	role := "administrator"
	if publication != nil {
		role = publication.Actor.Role
	}
	actor, err := b.authenticate(ctx, role, publication == nil || role == "administrator")
	if err != nil {
		return result, err
	}
	if publication != nil && !sameJSON(actor, publication.Actor) {
		return result, queueError("projection_unauthorized", "automatic synchronization requires the authenticated publisher's exact committed transition")
	}
	b.Remote = actor.Repository
	_, revision, err := b.verifiedDefaultReference(ctx)
	if err != nil {
		return result, err
	}
	data, err := b.configFile(ctx, repositorySettingsPath, revision)
	if hasStatus(err, http.StatusNotFound) {
		result.Disabled = true
		return result, nil
	}
	if err != nil {
		return result, err
	}
	settings, err := ParseRepositorySettings(data)
	if err != nil {
		return result, err
	}
	if settings.Issues == nil {
		result.Disabled = true
		return result, nil
	}
	graph, err := api.NewGraphQLClient(githubapi.ClientOptions("", ""))
	if err != nil {
		return result, err
	}
	return b.synchronizeCheckedIssueTransition(ctx, requestID, settings.Issues, graph.DoWithContext, publication)
}

func (b Branch) synchronizeCheckedIssueTransition(ctx context.Context, requestID string, config *IssuesSettings, graph issueGraphQL, publication *QueueCommit) (IssueSynchronization, error) {
	result := IssueSynchronization{RequestID: requestID}
	commits, err := b.Read(ctx)
	if err != nil {
		return result, err
	}
	state, err := Replay(commits)
	if err != nil {
		return result, err
	}
	if publication != nil {
		checked, ok := state.Requests[requestID]
		// Checkpoint-normalized receipts intentionally fail exact publication
		// matching; explicit privileged recovery handles that case.
		if !ok || !sameJSON(checked, *publication) {
			return result, queueError("projection_unauthorized", "automatic synchronization requires an exact checked publication receipt")
		}
	}
	targets, pending, err := state.issueTransitionSelection(requestID)
	result.Pending = pending
	if err != nil {
		return result, err
	}
	for _, target := range targets {
		if err := b.synchronizeIssueTarget(ctx, requestID, config, target, graph); err != nil {
			return result, fmt.Errorf("queue committed; Issue synchronization pending for %s: %w", target.WorkID, err)
		}
		result.Synchronized++
	}
	if len(result.Pending) != 0 {
		return result, queueError("projection_receipt_pending", "some terminal Work lack existing Issue or canonical summary receipts; valid mirrors were synchronized")
	}
	return result, nil
}

func projectionLockName(key string) string {
	value, _ := canonicalValue(key)
	hash := sha256.Sum256(value)
	return "gh-aw-issue-projection/" + hex.EncodeToString(hash[:])
}

func (b Branch) synchronizeIssueTarget(ctx context.Context, requestID string, config *IssuesSettings, target issueTransitionTarget, graph issueGraphQL) (err error) {
	head, err := b.resolveHeadRef(ctx, b.Name)
	if err != nil {
		return err
	}
	resource, _ := canonicalValue([]string{target.Resource.Host, target.Resource.RepositoryID, target.Resource.ResourceID})
	keys := []string{"work:" + target.WorkID, "issue:" + string(resource)}
	slices.Sort(keys)
	held := []string{}
	uncertain := false
	defer func() {
		if uncertain {
			return
		}
		cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
		defer cancel()
		for _, name := range held {
			if releaseErr := b.request(cleanup, http.MethodDelete, "git/refs/heads/"+name, nil, nil); releaseErr != nil {
				err = errors.Join(err, queueError("projection_coordination_pending", "acknowledged projection lock cleanup remains pending"))
			}
		}
	}()
	for _, key := range keys {
		name := projectionLockName(key)
		var receipt gitReference
		if err := b.request(ctx, http.MethodPost, "git/refs", map[string]string{"ref": "refs/heads/" + name, "sha": head}, &receipt); err != nil {
			return queueError("projection_coordination_pending", "projection lock is held or acquisition is uncertain")
		}
		if receipt.Ref != "refs/heads/"+name || receipt.Object.SHA != head {
			return queueError("projection_coordination_pending", "projection lock has no exact native acknowledgement")
		}
		held = append(held, name)
	}
	commits, err := b.Read(ctx)
	if err != nil {
		return err
	}
	state, err := Replay(commits)
	if err != nil {
		return err
	}
	fresh, _, err := state.issueTransitionSelection(requestID)
	if err != nil {
		return err
	}
	if !slices.Contains(fresh, target) {
		return queueError("projection_scope_changed", "terminal target changed after coordination")
	}
	return syncExistingIssue(ctx, graph, config, target, state, b.Name, &uncertain)
}

type issueSyncLabels struct {
	Nodes    []issueSyncLabel
	PageInfo struct {
		HasNextPage bool
		EndCursor   string
	}
}

type issueSyncLabel struct{ ID, Name string }

type issueSyncRepository struct {
	ID, NameWithOwner string
	DatabaseID        json.Number
	Tracking, Status  *issueSyncLabel
	Issue             struct {
		ID         string
		DatabaseID json.Number
		Number     int
		Labels     issueSyncLabels
	}
}

func readExistingIssue(ctx context.Context, graph issueGraphQL, config *IssuesSettings, target issueTransitionTarget, statusName string) (issueSyncRepository, error) {
	var read struct {
		Repository issueSyncRepository
		Node       struct {
			ID, Body string
			Issue    struct{ ID string }
		}
	}
	owner, repo, ok := strings.Cut(target.Resource.Repository, "/")
	if !ok || owner == "" || repo == "" {
		return read.Repository, queueError("projection_target_unavailable", "Issue repository identity is invalid")
	}
	query := `query($owner:String!,$repo:String!,$number:Int!,$summary:ID!,$tracking:String!,$status:String!) {
 repository(owner:$owner,name:$repo) { id databaseId nameWithOwner tracking:label(name:$tracking) { id name } status:label(name:$status) { id name }
 issue(number:$number) { id databaseId number labels(first:100) { nodes { id name } pageInfo { hasNextPage endCursor } } } }
 node(id:$summary) { id ... on IssueComment { body issue { id } } } }`
	var number int
	if _, err := fmt.Sscan(target.Resource.Number, &number); err != nil {
		return read.Repository, err
	}
	if err := graph(ctx, query, map[string]any{"owner": owner, "repo": repo, "number": number, "summary": target.SummaryID, "tracking": config.Label, "status": statusName}, &read); err != nil {
		return read.Repository, err
	}
	repository, issue := read.Repository, read.Repository.Issue
	if repository.NameWithOwner != target.Resource.Repository || repository.DatabaseID.String() != target.Resource.RepositoryID || issue.DatabaseID.String() != target.Resource.ResourceID ||
		issue.Number != number || issue.ID == "" || read.Node.ID != target.SummaryID || read.Node.Issue.ID != issue.ID {
		return repository, queueError("projection_target_unavailable", "existing Issue or canonical summary identity changed; recreation is forbidden")
	}
	labels, err := readIssueSyncLabels(ctx, graph, repository)
	repository.Issue.Labels = labels
	return repository, err
}

func readIssueSyncLabels(ctx context.Context, graph issueGraphQL, repository issueSyncRepository) (issueSyncLabels, error) {
	labels := repository.Issue.Labels
	cursors := map[string]struct{}{}
	for page := 0; labels.PageInfo.HasNextPage; page++ {
		cursor := labels.PageInfo.EndCursor
		_, repeated := cursors[cursor]
		if page == 16 || cursor == "" || repeated {
			return labels, queueError("projection_pagination_pending", "existing Issue label pagination is incomplete")
		}
		cursors[cursor] = struct{}{}
		var next struct {
			Node struct {
				ID         string
				Repository struct{ ID string }
				Labels     issueSyncLabels
			}
		}
		if err := graph(ctx, `query($id:ID!,$cursor:String!){node(id:$id){... on Issue{id repository{id} labels(first:100,after:$cursor){nodes{id name} pageInfo{hasNextPage endCursor}}}}}`, map[string]any{"id": repository.Issue.ID, "cursor": cursor}, &next); err != nil {
			return labels, err
		}
		if next.Node.ID != repository.Issue.ID || next.Node.Repository.ID != repository.ID {
			return labels, queueError("projection_target_unavailable", "Issue identity changed during label pagination")
		}
		labels.Nodes = append(labels.Nodes, next.Node.Labels.Nodes...)
		labels.PageInfo = next.Node.Labels.PageInfo
	}
	return labels, nil
}

func ensureIssueSyncLabel(ctx context.Context, graph issueGraphQL, repositoryID, name string, label *issueSyncLabel, uncertain *bool) (*issueSyncLabel, error) {
	if label == nil {
		var created struct {
			CreateLabel struct{ Label issueSyncLabel }
		}
		*uncertain = true
		if err := graph(ctx, `mutation($repository:ID!,$name:String!){createLabel(input:{repositoryId:$repository,name:$name,color:"7057FF",description:"Git-backed Work queue"}){label{id name}}}`, map[string]any{"repository": repositoryID, "name": name}, &created); err != nil {
			if created.CreateLabel.Label == (issueSyncLabel{}) && definitiveIssueLabelRejection(err) {
				*uncertain = false
				return readIssueSyncLabel(ctx, graph, repositoryID, name)
			}
			return nil, queueError("projection_write_ambiguous", "label creation has no definitive native acknowledgement")
		}
		if created.CreateLabel.Label.ID == "" || created.CreateLabel.Label.Name != name {
			return nil, queueError("projection_write_ambiguous", "status label creation returned no exact native acknowledgement")
		}
		label = &created.CreateLabel.Label
		*uncertain = false
	}
	if label.ID == "" || label.Name != name {
		return nil, queueError("projection_label_pending", "tracking/status label has no verified native identity")
	}
	return label, nil
}

func syncExistingIssue(ctx context.Context, graph issueGraphQL, config *IssuesSettings, target issueTransitionTarget, state Projection, branch string, uncertain *bool) error {
	statusName := issueLabelWhitespace.ReplaceAllString(config.Label, "-") + ":" + strings.ToLower(strings.ReplaceAll(target.Status, " ", "-"))
	repository, err := readExistingIssue(ctx, graph, config, target, statusName)
	if err != nil {
		return err
	}
	for _, desired := range []struct {
		Name  string
		Label **issueSyncLabel
	}{{config.Label, &repository.Tracking}, {statusName, &repository.Status}} {
		*desired.Label, err = ensureIssueSyncLabel(ctx, graph, repository.ID, desired.Name, *desired.Label, uncertain)
		if err != nil {
			return err
		}
	}
	statuses := []string{"queued", "blocked", "assigned", "running", "verifying", "needs-review", "done", "needs-attention", "cancelled"}
	obsolete := []string{}
	prefix := issueLabelWhitespace.ReplaceAllString(config.Label, "-") + ":"
	for _, label := range repository.Issue.Labels.Nodes {
		if label.Name != statusName && strings.HasPrefix(label.Name, prefix) && slices.Contains(statuses, strings.TrimPrefix(label.Name, prefix)) {
			obsolete = append(obsolete, label.ID)
		}
	}
	body := issueTransitionSummary(target, state, branch)
	return writeIssueSynchronization(ctx, graph, target, repository, obsolete, body, uncertain)
}

func writeIssueSynchronization(ctx context.Context, graph issueGraphQL, target issueTransitionTarget, repository issueSyncRepository, obsolete []string, body string, uncertain *bool) error {
	var written struct {
		Add, Remove struct {
			Labelable struct {
				ID     string
				Labels issueSyncLabels
			}
		}
		Comment struct {
			IssueComment struct{ ID, Body string }
		}
	}
	mutation := `mutation($issue:ID!,$add:[ID!]!,$remove:[ID!]!,$comment:ID!,$body:String!) {
 add:addLabelsToLabelable(input:{labelableId:$issue,labelIds:$add}) { labelable { id ... on Issue { labels(first:100) { nodes { id name } pageInfo { hasNextPage } } } } }
 remove:removeLabelsFromLabelable(input:{labelableId:$issue,labelIds:$remove}) { labelable { id ... on Issue { labels(first:100) { nodes { id name } pageInfo { hasNextPage } } } } }
 comment:updateIssueComment(input:{id:$comment,body:$body}) { issueComment { id body } } }`
	// This batch performs three content-generating mutations. Pace at one second
	// per mutation for GitHub's secondary rate limits, not ledger consistency.
	const issueSynchronizationMutationPacing = 3 * time.Second
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(issueSynchronizationMutationPacing):
	}
	*uncertain = true
	if err := graph(ctx, mutation, map[string]any{"issue": repository.Issue.ID, "add": []string{repository.Tracking.ID, repository.Status.ID}, "remove": obsolete, "comment": target.SummaryID, "body": body}, &written); err != nil {
		return err
	}
	hasLabel := func(labels issueSyncLabels, id string) bool {
		return slices.ContainsFunc(labels.Nodes, func(label issueSyncLabel) bool { return label.ID == id })
	}
	if written.Add.Labelable.ID != repository.Issue.ID || written.Remove.Labelable.ID != repository.Issue.ID ||
		written.Add.Labelable.Labels.PageInfo.HasNextPage || written.Remove.Labelable.Labels.PageInfo.HasNextPage ||
		!hasLabel(written.Remove.Labelable.Labels, repository.Tracking.ID) || !hasLabel(written.Remove.Labelable.Labels, repository.Status.ID) ||
		slices.ContainsFunc(obsolete, func(id string) bool { return hasLabel(written.Remove.Labelable.Labels, id) }) ||
		written.Comment.IssueComment.ID != target.SummaryID || written.Comment.IssueComment.Body != body {
		return queueError("projection_write_ambiguous", "terminal Issue synchronization returned no exact native acknowledgement")
	}
	*uncertain = false
	return nil
}
