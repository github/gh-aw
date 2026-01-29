//go:build !integration

package campaign

import (
	"testing"
)

func TestFilterSpecs_EmptyPattern(t *testing.T) {
	specs := []CampaignSpec{
		{ID: "campaign1", Name: "Campaign One"},
		{ID: "campaign2", Name: "Campaign Two"},
		{ID: "campaign3", Name: "Campaign Three"},
	}

	filtered := FilterSpecs(specs, "")

	if len(filtered) != len(specs) {
		t.Errorf("Expected %d specs with empty pattern, got %d", len(specs), len(filtered))
	}
}

func TestFilterSpecs_MatchByID(t *testing.T) {
	specs := []CampaignSpec{
		{ID: "security-alpha", Name: "Security Alpha"},
		{ID: "incident-beta", Name: "Incident Beta"},
		{ID: "modernization-gamma", Name: "Modernization Gamma"},
	}

	filtered := FilterSpecs(specs, "security")

	if len(filtered) != 1 {
		t.Fatalf("Expected 1 spec matching 'security', got %d", len(filtered))
	}

	if filtered[0].ID != "security-alpha" {
		t.Errorf("Expected to find 'security-alpha', got '%s'", filtered[0].ID)
	}
}

func TestFilterSpecs_MatchByName(t *testing.T) {
	specs := []CampaignSpec{
		{ID: "sec-comp", Name: "Security Compliance"},
		{ID: "incident", Name: "Incident Response"},
		{ID: "modernization", Name: "Org Modernization"},
	}

	filtered := FilterSpecs(specs, "Response")

	if len(filtered) != 1 {
		t.Fatalf("Expected 1 spec matching 'Response', got %d", len(filtered))
	}

	if filtered[0].ID != "incident" {
		t.Errorf("Expected to find 'incident', got '%s'", filtered[0].ID)
	}
}

func TestFilterSpecs_CaseInsensitive(t *testing.T) {
	specs := []CampaignSpec{
		{ID: "security-alpha", Name: "Security Alpha"},
		{ID: "incident-beta", Name: "Incident Beta"},
	}

	tests := []struct {
		pattern string
		wantLen int
	}{
		{"SECURITY", 1},
		{"Security", 1},
		{"security", 1},
		{"INCIDENT", 1},
		{"incident", 1},
	}

	for _, tt := range tests {
		filtered := FilterSpecs(specs, tt.pattern)
		if len(filtered) != tt.wantLen {
			t.Errorf("Pattern '%s': expected %d matches, got %d", tt.pattern, tt.wantLen, len(filtered))
		}
	}
}

func TestFilterSpecs_MultipleMatches(t *testing.T) {
	specs := []CampaignSpec{
		{ID: "security-q1", Name: "Security Q1"},
		{ID: "security-q2", Name: "Security Q2"},
		{ID: "incident-beta", Name: "Incident Beta"},
	}

	filtered := FilterSpecs(specs, "security")

	if len(filtered) != 2 {
		t.Fatalf("Expected 2 specs matching 'security', got %d", len(filtered))
	}

	foundQ1, foundQ2 := false, false
	for _, spec := range filtered {
		if spec.ID == "security-q1" {
			foundQ1 = true
		}
		if spec.ID == "security-q2" {
			foundQ2 = true
		}
	}

	if !foundQ1 || !foundQ2 {
		t.Error("Expected to find both security-q1 and security-q2")
	}
}

func TestFilterSpecs_NoMatches(t *testing.T) {
	specs := []CampaignSpec{
		{ID: "security-alpha", Name: "Security Alpha"},
		{ID: "incident-beta", Name: "Incident Beta"},
	}

	filtered := FilterSpecs(specs, "nonexistent")

	if len(filtered) != 0 {
		t.Errorf("Expected 0 specs matching 'nonexistent', got %d", len(filtered))
	}
}

func TestFilterSpecs_PartialMatch(t *testing.T) {
	specs := []CampaignSpec{
		{ID: "compliance-alpha", Name: "Compliance Alpha"},
		{ID: "incident-beta", Name: "Incident Beta"},
		{ID: "modernization-gamma", Name: "Modernization Gamma"},
	}

	filtered := FilterSpecs(specs, "comp")

	if len(filtered) != 1 {
		t.Fatalf("Expected 1 spec matching 'comp', got %d", len(filtered))
	}

	if filtered[0].ID != "compliance-alpha" {
		t.Errorf("Expected to find 'compliance-alpha', got '%s'", filtered[0].ID)
	}
}
