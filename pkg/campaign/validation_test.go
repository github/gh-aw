//go:build !integration

package campaign

import (
	"strings"
	"testing"
)

func TestValidateSpec_ValidSpec(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Version:    "v1",
		State:      "active",
		Workflows:  []string{"workflow1", "workflow2"},
	}

	problems := ValidateSpec(spec)
	if len(problems) != 0 {
		t.Errorf("Expected no validation problems, got: %v", problems)
	}
}

func TestValidateSpec_MissingID(t *testing.T) {
	spec := &CampaignSpec{
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Workflows:  []string{"workflow1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for missing ID")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "id is required") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected ID validation problem, got: %v", problems)
	}
}

func TestValidateSpec_InvalidIDCharacters(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "Test_Campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Workflows:  []string{"workflow1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for invalid ID characters")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "lowercase letters, digits, and hyphens") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected ID character validation problem, got: %v", problems)
	}
}

func TestValidateSpec_MissingName(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Workflows:  []string{"workflow1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for missing name")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "name should be provided") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected name validation problem, got: %v", problems)
	}
}

func TestValidateSpec_MissingWorkflows(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for missing workflows")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "workflows should list at least one workflow") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected workflows validation problem, got: %v", problems)
	}
}

func TestValidateSpec_InvalidState(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Workflows:  []string{"workflow1"},
		State:      "invalid-state",
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for invalid state")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "state must be one of") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected state validation problem, got: %v", problems)
	}
}

func TestValidateSpec_ValidStates(t *testing.T) {
	validStates := []string{"planned", "active", "paused", "completed", "archived"}

	for _, state := range validStates {
		spec := &CampaignSpec{
			ID:         "test-campaign",
			Name:       "Test Campaign",
			ProjectURL: "https://github.com/orgs/org/projects/1",
			Scope:      []string{"org/repo1"},
			Workflows:  []string{"workflow1"},
			State:      state,
		}

		problems := ValidateSpec(spec)
		if len(problems) != 0 {
			t.Errorf("Expected no validation problems for state '%s', got: %v", state, problems)
		}
	}
}

func TestValidateSpec_VersionDefault(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Workflows:  []string{"workflow1"},
	}

	_ = ValidateSpec(spec)

	if spec.Version != "v1" {
		t.Errorf("Expected version to default to 'v1', got '%s'", spec.Version)
	}
}

func TestValidateSpec_RiskLevel(t *testing.T) {
	validRiskLevels := []string{"low", "medium", "high"}

	for _, riskLevel := range validRiskLevels {
		spec := &CampaignSpec{
			ID:         "test-campaign",
			Name:       "Test Campaign",
			ProjectURL: "https://github.com/orgs/org/projects/1",
			Scope:      []string{"org/repo1"},
			Workflows:  []string{"workflow1"},
			RiskLevel:  riskLevel,
		}

		problems := ValidateSpec(spec)
		// Risk level validation is currently not enforced beyond schema
		// This test ensures the field is accepted
		if len(problems) != 0 {
			t.Errorf("Expected no validation problems for risk level '%s', got: %v", riskLevel, problems)
		}
	}
}

func TestValidateSpec_WithApprovalPolicy(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Workflows:  []string{"workflow1"},
		ApprovalPolicy: &CampaignApprovalPolicy{
			RequiredApprovals: 2,
			RequiredRoles:     []string{"admin", "security"},
			ChangeControl:     true,
		},
	}

	problems := ValidateSpec(spec)
	if len(problems) != 0 {
		t.Errorf("Expected no validation problems with approval policy, got: %v", problems)
	}
}

func TestValidateSpec_CompleteSpec(t *testing.T) {
	spec := &CampaignSpec{
		ID:                 "complete-campaign",
		Name:               "Complete Campaign",
		Description:        "A complete campaign spec for testing",
		ProjectURL:         "https://github.com/orgs/org/projects/1",
		Scope:              []string{"org/repo1"},
		Version:            "v1",
		Workflows:          []string{"workflow1", "workflow2"},
		MemoryPaths:        []string{"memory/campaigns/complete/**"},
		MetricsGlob:        "memory/campaigns/complete-*.json",
		Owners:             []string{"owner1", "owner2"},
		ExecutiveSponsors:  []string{"sponsor1"},
		RiskLevel:          "medium",
		State:              "active",
		Tags:               []string{"security", "compliance"},
		AllowedSafeOutputs: []string{"create-issue", "create-pull-request"},
		ApprovalPolicy: &CampaignApprovalPolicy{
			RequiredApprovals: 3,
			RequiredRoles:     []string{"admin", "security", "compliance"},
			ChangeControl:     true,
		},
	}

	problems := ValidateSpec(spec)
	if len(problems) != 0 {
		t.Errorf("Expected no validation problems for complete spec, got: %v", problems)
	}
}

func TestValidateSpec_MissingScopeIsValid(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		// No workflows - scope is not required
	}

	problems := ValidateSpec(spec)
	// Should have one problem: missing workflows
	if len(problems) != 1 {
		t.Errorf("Expected 1 validation problem (workflows), got: %v", problems)
	}
}

func TestValidateSpec_InvalidScopeRepoFormat(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Workflows:  []string{"workflow1"},
		Scope:      []string{"invalid-repo-format", "org/repo1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for invalid repo format")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "must be 'owner/repo'") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected repo format validation problem, got: %v", problems)
	}
}

func TestValidateSpec_EmptyScopeIsValid(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{}, // Empty list, no workflows
	}

	problems := ValidateSpec(spec)
	// Should have one problem: missing workflows
	if len(problems) != 1 {
		t.Errorf("Expected 1 validation problem (workflows), got: %v", problems)
	}
}

func TestValidateSpec_ValidScopeWithOrgs(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Workflows:  []string{"workflow1"},
		Scope:      []string{"org/repo1", "org:github", "org:microsoft"},
	}

	problems := ValidateSpec(spec)
	if len(problems) != 0 {
		t.Errorf("Expected no validation problems with valid scope orgs, got: %v", problems)
	}
}

func TestValidateSpec_InvalidScopeOrgFormat(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Workflows:  []string{"workflow1"},
		Scope:      []string{"org/repo1", "org:github/repo"}, // Invalid - contains slash
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for invalid org format")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "must be 'org:<name>'") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected org format validation problem, got: %v", problems)
	}
}

func TestSuggestValidID(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "uppercase to lowercase",
			input:    "Security-Q1-2025",
			expected: "security-q1-2025",
		},
		{
			name:     "underscores to hyphens",
			input:    "test_campaign_id",
			expected: "test-campaign-id",
		},
		{
			name:     "spaces to hyphens",
			input:    "my campaign id",
			expected: "my-campaign-id",
		},
		{
			name:     "mixed invalid characters",
			input:    "Test Campaign! @2025",
			expected: "test-campaign-2025",
		},
		{
			name:     "multiple hyphens collapsed",
			input:    "test--campaign---id",
			expected: "test-campaign-id",
		},
		{
			name:     "leading and trailing hyphens removed",
			input:    "-test-campaign-",
			expected: "test-campaign",
		},
		{
			name:     "already valid id",
			input:    "valid-campaign-123",
			expected: "valid-campaign-123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := suggestValidID(tt.input)
			if result != tt.expected {
				t.Errorf("suggestValidID(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestValidateSpec_InvalidIDWithSuggestion(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "Test Campaign 2025",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo1"},
		Workflows:  []string{"workflow1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for invalid ID")
	}

	// Check that the error message includes a suggestion
	found := false
	for _, p := range problems {
		if strings.Contains(p, "test-campaign-2025") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected ID validation problem with suggestion, got: %v", problems)
	}
}

func TestValidateSpec_ScopeRepoWildcard(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org/repo*"}, // Invalid - wildcards not allowed in repo selectors
		Workflows:  []string{"workflow1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for wildcard in scope")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "cannot contain wildcards") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected wildcard validation problem, got: %v", problems)
	}
}

func TestValidateSpec_ScopeOrgWildcard(t *testing.T) {
	spec := &CampaignSpec{
		ID:         "test-campaign",
		Name:       "Test Campaign",
		ProjectURL: "https://github.com/orgs/org/projects/1",
		Scope:      []string{"org:github*"}, // Invalid - wildcards not allowed in org selectors
		Workflows:  []string{"workflow1"},
	}

	problems := ValidateSpec(spec)
	if len(problems) == 0 {
		t.Fatal("Expected validation problems for wildcard in scope")
	}

	found := false
	for _, p := range problems {
		if strings.Contains(p, "cannot contain wildcards") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("Expected wildcard validation problem, got: %v", problems)
	}
}

func TestValidateSpec_TrackerLabelFormat(t *testing.T) {
	tests := []struct {
		name          string
		campaignID    string
		trackerLabel  string
		expectError   bool
		errorContains string
	}{
		{
			name:         "valid tracker label with z_campaign_ prefix",
			campaignID:   "security-alert-burndown",
			trackerLabel: "z_campaign_security-alert-burndown",
			expectError:  false,
		},
		{
			name:          "invalid tracker label with old campaign: prefix",
			campaignID:    "security-alert-burndown",
			trackerLabel:  "campaign:security-alert-burndown",
			expectError:   true,
			errorContains: "tracker-label should start with 'z_campaign_' prefix",
		},
		{
			name:          "invalid tracker label with wrong format",
			campaignID:    "test-campaign",
			trackerLabel:  "wrong-label-format",
			expectError:   true,
			errorContains: "tracker-label should start with 'z_campaign_' prefix",
		},
		{
			name:         "empty tracker label is valid (optional field)",
			campaignID:   "test-campaign",
			trackerLabel: "",
			expectError:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spec := &CampaignSpec{
				ID:           tt.campaignID,
				Name:         "Test Campaign",
				ProjectURL:   "https://github.com/orgs/org/projects/1",
				Scope:        []string{"org/repo1"},
				Workflows:    []string{"workflow1"},
				TrackerLabel: tt.trackerLabel,
			}

			problems := ValidateSpec(spec)

			if tt.expectError {
				if len(problems) == 0 {
					t.Errorf("Expected validation error but got none")
					return
				}
				found := false
				for _, p := range problems {
					if strings.Contains(p, tt.errorContains) {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("Expected error containing %q, got: %v", tt.errorContains, problems)
				}
			} else {
				// Filter out problems that are not related to tracker-label
				for _, p := range problems {
					if strings.Contains(p, "tracker-label") {
						t.Errorf("Unexpected tracker-label validation error: %s", p)
					}
				}
			}
		})
	}
}
