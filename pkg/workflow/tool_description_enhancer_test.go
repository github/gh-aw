//go:build !integration

package workflow

import (
	"strings"
	"testing"
)

func TestEnhanceToolDescriptionCreateIssueAllowedFieldsWildcard(t *testing.T) {
	description := enhanceToolDescription("create_issue", "Create an issue.", &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{
			AllowedFields: []string{"*"},
		},
	})

	if !strings.Contains(description, "Any issue field is allowed.") {
		t.Fatalf("expected wildcard message in description, got: %s", description)
	}
	if strings.Contains(description, "Only these issue fields are allowed") {
		t.Fatalf("did not expect restrictive fields message for wildcard, got: %s", description)
	}
}

func TestEnhanceToolDescriptionCreateIssueAllowedFieldsList(t *testing.T) {
	description := enhanceToolDescription("create_issue", "Create an issue.", &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{
			AllowedFields: []string{"Priority", "Iteration"},
		},
	})

	if !strings.Contains(description, "Only these issue fields are allowed: [\"Priority\" \"Iteration\"].") {
		t.Fatalf("expected restrictive fields message in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionSetIssueFieldAllowedFieldsWildcard(t *testing.T) {
	description := enhanceToolDescription("set_issue_field", "Set one issue field.", &SafeOutputsConfig{
		SetIssueField: &SetIssueFieldConfig{
			AllowedFields: []string{"*"},
		},
	})

	if !strings.Contains(description, "Any issue field is allowed.") {
		t.Fatalf("expected wildcard message in description, got: %s", description)
	}
	if strings.Contains(description, "Only these issue fields are allowed") {
		t.Fatalf("did not expect restrictive fields message for wildcard, got: %s", description)
	}
}

func TestEnhanceToolDescriptionSetIssueFieldAllowedFieldsList(t *testing.T) {
	description := enhanceToolDescription("set_issue_field", "Set one issue field.", &SafeOutputsConfig{
		SetIssueField: &SetIssueFieldConfig{
			AllowedFields: []string{"Priority", "Iteration"},
		},
	})

	if !strings.Contains(description, "Only these issue fields are allowed: [\"Priority\" \"Iteration\"].") {
		t.Fatalf("expected restrictive fields message in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionCloseDiscussionAllowBodyFalse(t *testing.T) {
	f := false
	description := enhanceToolDescription("close_discussion", "Close a discussion.", &SafeOutputsConfig{
		CloseDiscussions: &CloseDiscussionsConfig{
			AllowBody: &f,
		},
	})

	if !strings.Contains(description, "Closing comments are disabled: do not include a body field.") {
		t.Fatalf("expected body-not-allowed constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionCloseDiscussionAllowBodyTrue(t *testing.T) {
	tr := true
	description := enhanceToolDescription("close_discussion", "Close a discussion.", &SafeOutputsConfig{
		CloseDiscussions: &CloseDiscussionsConfig{
			AllowBody: &tr,
		},
	})

	if strings.Contains(description, "Closing comments are disabled") {
		t.Fatalf("did not expect body-not-allowed constraint when allow-body is true, got: %s", description)
	}
}

func TestEnhanceToolDescriptionCloseIssueAllowBodyFalse(t *testing.T) {
	f := false
	description := enhanceToolDescription("close_issue", "Close an issue.", &SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			AllowBody: &f,
		},
	})

	if !strings.Contains(description, "Closing comments are disabled: do not include a body field.") {
		t.Fatalf("expected body-not-allowed constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionCloseIssueAllowBodyTrue(t *testing.T) {
	tr := true
	description := enhanceToolDescription("close_issue", "Close an issue.", &SafeOutputsConfig{
		CloseIssues: &CloseIssuesConfig{
			AllowBody: &tr,
		},
	})

	if strings.Contains(description, "Closing comments are disabled") {
		t.Fatalf("did not expect body-not-allowed constraint when allow-body is true, got: %s", description)
	}
}

func TestEnhanceToolDescriptionCloseDiscussionTargetRepo(t *testing.T) {
	description := enhanceToolDescription("close_discussion", "Close a discussion.", &SafeOutputsConfig{
		CloseDiscussions: &CloseDiscussionsConfig{
			BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: defaultIntStr(5)},
			SafeOutputTargetConfig: SafeOutputTargetConfig{TargetRepoSlug: "myorg/myrepo"},
		},
	})

	if !strings.Contains(description, `"myorg/myrepo"`) {
		t.Fatalf("expected target repo constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionAssignMilestoneTargetRepo(t *testing.T) {
	description := enhanceToolDescription("assign_milestone", "Assign a milestone.", &SafeOutputsConfig{
		AssignMilestone: &AssignMilestoneConfig{
			BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: defaultIntStr(5)},
			SafeOutputTargetConfig: SafeOutputTargetConfig{TargetRepoSlug: "myorg/myrepo"},
		},
	})

	if !strings.Contains(description, `"myorg/myrepo"`) {
		t.Fatalf("expected target repo constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionLinkSubIssueTargetRepo(t *testing.T) {
	description := enhanceToolDescription("link_sub_issue", "Link a sub-issue.", &SafeOutputsConfig{
		LinkSubIssue: &LinkSubIssueConfig{
			BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: defaultIntStr(5)},
			SafeOutputTargetConfig: SafeOutputTargetConfig{TargetRepoSlug: "myorg/myrepo"},
		},
	})

	if !strings.Contains(description, `"myorg/myrepo"`) {
		t.Fatalf("expected target repo constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionLinkSubIssueAllowedRepos(t *testing.T) {
	description := enhanceToolDescription("link_sub_issue", "Link a sub-issue.", &SafeOutputsConfig{
		LinkSubIssue: &LinkSubIssueConfig{
			BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: defaultIntStr(5)},
			SafeOutputTargetConfig: SafeOutputTargetConfig{AllowedRepos: []string{"myorg/repo-a", "myorg/repo-b"}},
		},
	})

	if !strings.Contains(description, "myorg/repo-a") {
		t.Fatalf("expected allowed repos constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionMarkPRReadyForReviewTargetRepo(t *testing.T) {
	description := enhanceToolDescription("mark_pull_request_as_ready_for_review", "Mark PR as ready.", &SafeOutputsConfig{
		MarkPullRequestAsReadyForReview: &MarkPullRequestAsReadyForReviewConfig{
			BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: defaultIntStr(10)},
			SafeOutputTargetConfig: SafeOutputTargetConfig{TargetRepoSlug: "myorg/myrepo"},
		},
	})

	if !strings.Contains(description, `"myorg/myrepo"`) {
		t.Fatalf("expected target repo constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionMarkPRReadyForReviewMaxCount(t *testing.T) {
	description := enhanceToolDescription("mark_pull_request_as_ready_for_review", "Mark PR as ready.", &SafeOutputsConfig{
		MarkPullRequestAsReadyForReview: &MarkPullRequestAsReadyForReviewConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: defaultIntStr(3)},
		},
	})

	if !strings.Contains(description, "Maximum 3 pull request(s) can be marked as ready for review.") {
		t.Fatalf("expected max count constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionSubmitPullRequestReviewTarget(t *testing.T) {
	description := enhanceToolDescription("submit_pull_request_review", "Submit a PR review.", &SafeOutputsConfig{
		SubmitPullRequestReview: &SubmitPullRequestReviewConfig{
			BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: defaultIntStr(1)},
			SafeOutputTargetConfig: SafeOutputTargetConfig{Target: "*"},
		},
	})

	if !strings.Contains(description, "Target: *.") {
		t.Fatalf("expected target constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionSubmitPullRequestReviewTargetRepo(t *testing.T) {
	description := enhanceToolDescription("submit_pull_request_review", "Submit a PR review.", &SafeOutputsConfig{
		SubmitPullRequestReview: &SubmitPullRequestReviewConfig{
			BaseSafeOutputConfig:   BaseSafeOutputConfig{Max: defaultIntStr(1)},
			SafeOutputTargetConfig: SafeOutputTargetConfig{TargetRepoSlug: "myorg/myrepo"},
		},
	})

	if !strings.Contains(description, `"myorg/myrepo"`) {
		t.Fatalf("expected target repo constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionNormalizeClosingKeywordsCreateIssue(t *testing.T) {
	description := enhanceToolDescription("create_issue", "Create an issue.", &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{NormalizeClosingKeywords: boolPtr(true)},
		},
	})
	if !strings.Contains(description, "Backtick-wrapped issue-closing keyword references") {
		t.Fatalf("expected normalize-closing-keywords note in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionNormalizeClosingKeywordsFalseCreateIssue(t *testing.T) {
	description := enhanceToolDescription("create_issue", "Create an issue.", &SafeOutputsConfig{
		CreateIssues: &CreateIssuesConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{NormalizeClosingKeywords: boolPtr(false)},
		},
	})
	if strings.Contains(description, "Backtick-wrapped issue-closing keyword references") {
		t.Fatalf("did not expect normalize-closing-keywords note when disabled, got: %s", description)
	}
}

func TestEnhanceToolDescriptionNormalizeClosingKeywordsAddComment(t *testing.T) {
	description := enhanceToolDescription("add_comment", "Add a comment.", &SafeOutputsConfig{
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{NormalizeClosingKeywords: boolPtr(true)},
		},
	})
	if !strings.Contains(description, "Backtick-wrapped issue-closing keyword references") {
		t.Fatalf("expected normalize-closing-keywords note in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionNormalizeClosingKeywordsFalseAddComment(t *testing.T) {
	description := enhanceToolDescription("add_comment", "Add a comment.", &SafeOutputsConfig{
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{NormalizeClosingKeywords: boolPtr(false)},
		},
	})
	if strings.Contains(description, "Backtick-wrapped issue-closing keyword references") {
		t.Fatalf("did not expect normalize-closing-keywords note when disabled, got: %s", description)
	}
}

func TestEnhanceToolDescriptionAddCommentDoesNotDuplicateConstraintsMarker(t *testing.T) {
	description := enhanceToolDescription("add_comment", "Add a comment. CONSTRAINTS: Input is limited to 524288 characters.", &SafeOutputsConfig{
		AddComments: &AddCommentsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{Max: defaultIntStr(3)},
		},
	})
	if strings.Count(description, "CONSTRAINTS:") != 1 {
		t.Fatalf("expected a single constraints marker, got: %s", description)
	}
	if !strings.Contains(description, "Maximum 3 comment(s) can be added.") {
		t.Fatalf("expected configured max constraint in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionNormalizeClosingKeywordsCreatePullRequest(t *testing.T) {
	description := enhanceToolDescription("create_pull_request", "Create a pull request.", &SafeOutputsConfig{
		CreatePullRequests: &CreatePullRequestsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{NormalizeClosingKeywords: boolPtr(true)},
		},
	})
	if !strings.Contains(description, "Backtick-wrapped issue-closing keyword references") {
		t.Fatalf("expected normalize-closing-keywords note in description, got: %s", description)
	}
}

func TestEnhanceToolDescriptionNormalizeClosingKeywordsFalseCreatePullRequest(t *testing.T) {
	description := enhanceToolDescription("create_pull_request", "Create a pull request.", &SafeOutputsConfig{
		CreatePullRequests: &CreatePullRequestsConfig{
			BaseSafeOutputConfig: BaseSafeOutputConfig{NormalizeClosingKeywords: boolPtr(false)},
		},
	})
	if strings.Contains(description, "Backtick-wrapped issue-closing keyword references") {
		t.Fatalf("did not expect normalize-closing-keywords note when disabled, got: %s", description)
	}
}

func TestAddCommentConstraintsNilConfig(t *testing.T) {
	constraints := addCommentConstraints(nil)

	want := []string{"Supports reply_to_id for discussion threading."}
	if len(constraints) != len(want) {
		t.Fatalf("expected %d constraint(s), got %d: %v", len(want), len(constraints), constraints)
	}
	for i, expected := range want {
		if constraints[i] != expected {
			t.Fatalf("constraint %d: expected %q, got %q", i, expected, constraints[i])
		}
	}
}
