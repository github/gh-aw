package intent

import "github.com/github/gh-aw/pkg/logger"

var resolverLog = logger.New("intent:resolver")

type AttributionStatus string

const (
	AttributionMapped    AttributionStatus = "mapped"
	AttributionUnmapped  AttributionStatus = "unmapped"
	AttributionUnlinked  AttributionStatus = "unlinked"
	AttributionAmbiguous AttributionStatus = "ambiguous"
	AttributionSuggested AttributionStatus = "suggested"
)

type AttributionSource string

const (
	SourceExplicitMetadata AttributionSource = "explicit_metadata"
	SourceClosingIssue     AttributionSource = "closing_issue"
	SourceParentIssue      AttributionSource = "parent_issue"
	SourceReferencedIssue  AttributionSource = "referenced_issue"
	SourceProject          AttributionSource = "project"
	SourceMilestone        AttributionSource = "milestone"
	SourceIssueLabels      AttributionSource = "issue_labels"
	SourceArtifactLabels   AttributionSource = "artifact_labels"
	SourceSuggestion       AttributionSource = "suggestion"
	SourceNone             AttributionSource = "none"
)

type IntentRecord struct {
	Status    AttributionStatus `json:"status"`
	Source    AttributionSource `json:"source"`
	IntentKey *string           `json:"intent_key,omitempty"`

	RootNodeID string `json:"root_node_id,omitempty"`
	RootType   string `json:"root_type,omitempty"`
	RootURL    string `json:"root_url,omitempty"`

	Labels []string `json:"labels,omitempty"`

	// Domains, Priority, and Risk are optional classification dimensions used by
	// ResolveRisk to derive a risk level when Risk is not explicitly set. They are
	// distinct from Labels, which PolicyCondition matches against directly.
	Domains  []string `json:"domains,omitempty"`
	Priority string   `json:"priority,omitempty"`
	Risk     string   `json:"risk,omitempty"`

	Rule            string `json:"rule,omitempty"`
	ResolverVersion string `json:"resolver_version,omitempty"`
}

type RootReference struct {
	NodeID string
	Type   string
	URL    string
	Labels []string
}

type PullRequestData struct {
	NodeID         string
	URL            string
	Labels         []string
	ExplicitIntent *IntentRecord
	ClosingIssues  []RootReference
}

type Resolver struct {
	ResolverVersion string
	MatchLabels     func(labels []string) []string
}

func (r Resolver) ResolvePullRequest(pr PullRequestData) IntentRecord {
	resolverLog.Printf("ResolvePullRequest: nodeID=%s explicitIntent=%t closingIssues=%d labels=%d", pr.NodeID, pr.ExplicitIntent != nil, len(pr.ClosingIssues), len(pr.Labels))

	if pr.ExplicitIntent != nil {
		intent := *pr.ExplicitIntent
		if intent.ResolverVersion == "" {
			intent.ResolverVersion = r.ResolverVersion
		}
		resolverLog.Printf("ResolvePullRequest: using explicit metadata for %s", pr.NodeID)
		return intent
	}

	if len(pr.ClosingIssues) > 0 {
		if len(pr.ClosingIssues) == 1 {
			resolverLog.Printf("ResolvePullRequest: attributing %s to single closing issue", pr.NodeID)
			return r.fromRoot(pr.ClosingIssues[0], SourceClosingIssue, "single_closing_issue")
		}
		resolverLog.Printf("ResolvePullRequest: %d closing issues for %s, marking ambiguous", len(pr.ClosingIssues), pr.NodeID)
		return r.ambiguous(SourceClosingIssue, "multiple_closing_issues")
	}
	if len(pr.Labels) > 0 {
		resolverLog.Printf("ResolvePullRequest: no closing issue, falling back to %d PR label(s) for %s", len(pr.Labels), pr.NodeID)
		return r.fromRoot(RootReference{
			NodeID: pr.NodeID,
			Type:   "artifact",
			URL:    pr.URL,
			Labels: pr.Labels,
		}, SourceArtifactLabels, "pull_request_label_fallback")
	}
	resolverLog.Printf("ResolvePullRequest: no intent source for %s, marking unlinked", pr.NodeID)
	return r.unlinked("no_supported_intent_source")
}

func (r Resolver) ResolveIssue(nodeID, url string, labels []string) IntentRecord {
	resolverLog.Printf("ResolveIssue: nodeID=%s labels=%d", nodeID, len(labels))
	if len(labels) == 0 {
		resolverLog.Printf("ResolveIssue: no labels for %s, marking unlinked", nodeID)
		return r.unlinked("no_supported_intent_source")
	}
	return r.fromRoot(RootReference{
		NodeID: nodeID,
		Type:   "issue",
		URL:    url,
		Labels: labels,
	}, SourceIssueLabels, "issue_label_fallback")
}

func (r Resolver) fromRoot(root RootReference, source AttributionSource, rule string) IntentRecord {
	return IntentRecord{
		Status:          r.statusForLabels(root.Labels),
		Source:          source,
		RootNodeID:      root.NodeID,
		RootType:        root.Type,
		RootURL:         root.URL,
		Labels:          cloneStrings(root.Labels),
		Rule:            rule,
		ResolverVersion: r.ResolverVersion,
	}
}

func (r Resolver) unlinked(rule string) IntentRecord {
	return IntentRecord{
		Status:          AttributionUnlinked,
		Source:          SourceNone,
		Rule:            rule,
		ResolverVersion: r.ResolverVersion,
	}
}

func (r Resolver) ambiguous(source AttributionSource, rule string) IntentRecord {
	return IntentRecord{
		Status:          AttributionAmbiguous,
		Source:          source,
		Rule:            rule,
		ResolverVersion: r.ResolverVersion,
	}
}

func (r Resolver) statusForLabels(labels []string) AttributionStatus {
	if len(labels) == 0 {
		return AttributionUnlinked
	}
	if r.MatchLabels == nil {
		return AttributionUnmapped
	}
	if len(r.MatchLabels(labels)) > 0 {
		return AttributionMapped
	}
	return AttributionUnmapped
}
