//go:build !integration

package workflow

import (
	"reflect"
	"testing"
)

func TestGetAllCommentEvents(t *testing.T) {
	events := GetAllCommentEvents()

	// Should have exactly 7 events (added pull_request_comment, discussion, discussion_comment)
	if len(events) != 7 {
		t.Errorf("Expected 7 comment events, got %d", len(events))
	}

	// Check that all expected events are present
	expectedEvents := map[string][]string{
		"issues":                      {"opened", "edited", "reopened"},
		"issue_comment":               {"created", "edited"},
		"pull_request_comment":        {"created", "edited"},
		"pull_request":                {"opened", "edited", "reopened"},
		"pull_request_review_comment": {"created", "edited"},
		"discussion":                  {"created", "edited"},
		"discussion_comment":          {"created", "edited"},
	}

	for _, event := range events {
		expected, ok := expectedEvents[event.EventName]
		if !ok {
			t.Errorf("Unexpected event name: %s", event.EventName)
			continue
		}

		if !reflect.DeepEqual(event.Types, expected) {
			t.Errorf("For event %s, expected types %v, got %v", event.EventName, expected, event.Types)
		}
	}
}

func TestGetCommentEventByIdentifier(t *testing.T) {
	tests := []struct {
		name       string
		identifier string
		wantEvent  string
		wantNil    bool
	}{
		{
			name:       "GitHub Actions event name 'issues'",
			identifier: "issues",
			wantEvent:  "issues",
		},
		{
			name:       "GitHub Actions event name 'issue_comment'",
			identifier: "issue_comment",
			wantEvent:  "issue_comment",
		},
		{
			name:       "GitHub Actions event name 'pull_request'",
			identifier: "pull_request",
			wantEvent:  "pull_request",
		},
		{
			name:       "GitHub Actions event name 'pull_request_review_comment'",
			identifier: "pull_request_review_comment",
			wantEvent:  "pull_request_review_comment",
		},
		{
			name:       "GitHub Actions event name 'pull_request_comment'",
			identifier: "pull_request_comment",
			wantEvent:  "pull_request_comment",
		},
		{
			name:       "GitHub Actions event name 'discussion'",
			identifier: "discussion",
			wantEvent:  "discussion",
		},
		{
			name:       "GitHub Actions event name 'discussion_comment'",
			identifier: "discussion_comment",
			wantEvent:  "discussion_comment",
		},
		{
			name:       "invalid identifier",
			identifier: "invalid",
			wantNil:    true,
		},
		{
			name:       "short identifier 'issue' is not supported",
			identifier: "issue",
			wantNil:    true,
		},
		{
			name:       "short identifier 'comment' is not supported",
			identifier: "comment",
			wantNil:    true,
		},
		{
			name:       "short identifier 'pr' is not supported",
			identifier: "pr",
			wantNil:    true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := GetCommentEventByIdentifier(tt.identifier)

			if tt.wantNil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			if result == nil {
				t.Errorf("Expected non-nil result, got nil")
				return
			}

			if result.EventName != tt.wantEvent {
				t.Errorf("Expected event name %s, got %s", tt.wantEvent, result.EventName)
			}
		})
	}
}

func TestParseCommandEvents(t *testing.T) {
	tests := []struct {
		name        string
		eventsValue any
		want        []string
		wantNil     bool
	}{
		{
			name:        "nil value returns default",
			eventsValue: nil,
			wantNil:     true,
		},
		{
			name:        "wildcard string returns default",
			eventsValue: "*",
			wantNil:     true,
		},
		{
			name:        "single event string",
			eventsValue: "issues",
			want:        []string{"issues"},
		},
		{
			name:        "array of event strings",
			eventsValue: []any{"issues", "issue_comment"},
			want:        []string{"issues", "issue_comment"},
		},
		{
			name:        "empty array returns default",
			eventsValue: []any{},
			wantNil:     true,
		},
		{
			name:        "array with non-strings is filtered",
			eventsValue: []any{"issues", 123, "issue_comment"},
			want:        []string{"issues", "issue_comment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := ParseCommandEvents(tt.eventsValue)

			if tt.wantNil {
				if result != nil {
					t.Errorf("Expected nil, got %v", result)
				}
				return
			}

			if !reflect.DeepEqual(result, tt.want) {
				t.Errorf("Expected %v, got %v", tt.want, result)
			}
		})
	}
}

func TestFilterCommentEvents(t *testing.T) {
	tests := []struct {
		name        string
		identifiers []string
		wantCount   int
		wantEvents  []string
	}{
		{
			name:        "nil identifiers returns all events",
			identifiers: nil,
			wantCount:   7,
			wantEvents:  []string{"issues", "issue_comment", "pull_request_comment", "pull_request", "pull_request_review_comment", "discussion", "discussion_comment"},
		},
		{
			name:        "empty identifiers returns all events",
			identifiers: []string{},
			wantCount:   7,
			wantEvents:  []string{"issues", "issue_comment", "pull_request_comment", "pull_request", "pull_request_review_comment", "discussion", "discussion_comment"},
		},
		{
			name:        "single identifier",
			identifiers: []string{"issues"},
			wantCount:   1,
			wantEvents:  []string{"issues"},
		},
		{
			name:        "multiple identifiers",
			identifiers: []string{"issues", "issue_comment"},
			wantCount:   2,
			wantEvents:  []string{"issues", "issue_comment"},
		},
		{
			name:        "invalid identifiers are filtered out",
			identifiers: []string{"issues", "invalid", "issue_comment"},
			wantCount:   2,
			wantEvents:  []string{"issues", "issue_comment"},
		},
		{
			name:        "GitHub Actions event names",
			identifiers: []string{"pull_request", "pull_request_review_comment"},
			wantCount:   2,
			wantEvents:  []string{"pull_request", "pull_request_review_comment"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := FilterCommentEvents(tt.identifiers)

			if len(result) != tt.wantCount {
				t.Errorf("Expected %d events, got %d", tt.wantCount, len(result))
			}

			gotEvents := GetCommentEventNames(result)
			if !reflect.DeepEqual(gotEvents, tt.wantEvents) {
				t.Errorf("Expected events %v, got %v", tt.wantEvents, gotEvents)
			}
		})
	}
}

func TestGetCommentEventNames(t *testing.T) {
	mappings := []CommentEventMapping{
		{EventName: "issues", Types: []string{"opened"}},
		{EventName: "issue_comment", Types: []string{"created"}},
	}

	result := GetCommentEventNames(mappings)

	expected := []string{"issues", "issue_comment"}
	if !reflect.DeepEqual(result, expected) {
		t.Errorf("Expected %v, got %v", expected, result)
	}
}
