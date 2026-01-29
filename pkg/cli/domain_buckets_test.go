//go:build !integration

package cli

import (
	"reflect"
	"testing"
)

func TestDomainBucketsAccessors(t *testing.T) {
	buckets := &DomainBuckets{}

	// Test initial state
	if buckets.GetAllowedDomains() != nil {
		t.Errorf("Expected nil allowed domains initially, got %v", buckets.GetAllowedDomains())
	}
	if buckets.GetBlockedDomains() != nil {
		t.Errorf("Expected nil blocked domains initially, got %v", buckets.GetBlockedDomains())
	}

	// Test SetAllowedDomains
	allowedDomains := []string{"example.com", "github.com"}
	buckets.SetAllowedDomains(allowedDomains)
	if !reflect.DeepEqual(buckets.GetAllowedDomains(), allowedDomains) {
		t.Errorf("Expected allowed domains %v, got %v", allowedDomains, buckets.GetAllowedDomains())
	}

	// Test SetBlockedDomains
	blockedDomains := []string{"blocked.com", "malicious.com"}
	buckets.SetBlockedDomains(blockedDomains)
	if !reflect.DeepEqual(buckets.GetBlockedDomains(), blockedDomains) {
		t.Errorf("Expected blocked domains %v, got %v", blockedDomains, buckets.GetBlockedDomains())
	}
}

func TestDomainBucketsWithEmbedding(t *testing.T) {
	// This test verifies that types embedding DomainBuckets can access
	// the domain accessor methods.
	type TestAnalysis struct {
		DomainBuckets
		TotalRequests int
	}

	analysis := &TestAnalysis{}

	// Test that we can call accessor methods through embedding
	analysis.SetAllowedDomains([]string{"test.com"})
	analysis.SetBlockedDomains([]string{"bad.com"})

	if len(analysis.GetAllowedDomains()) != 1 {
		t.Errorf("Expected 1 allowed domain, got %d", len(analysis.GetAllowedDomains()))
	}
	if len(analysis.GetBlockedDomains()) != 1 {
		t.Errorf("Expected 1 blocked domain, got %d", len(analysis.GetBlockedDomains()))
	}
}
