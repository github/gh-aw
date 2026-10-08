package workqueue_test

import (
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/workqueue"
)

func TestValidatePolicySharesWireAndSemanticContract(t *testing.T) {
	tests := []struct {
		name   string
		update func(*workqueue.Policy)
		valid  bool
	}{
		{name: "default", valid: true},
		{name: "unsupported mode", update: func(policy *workqueue.Policy) { policy.Mode = "fifo" }},
		{name: "missing class weight", update: func(policy *workqueue.Policy) { policy.ClassWeights = []int{1} }},
		{name: "account weight ceiling", update: func(policy *workqueue.Policy) { policy.AccountingWeights[""] = 1001 }},
		{name: "default account must weigh one", update: func(policy *workqueue.Policy) { policy.AccountingWeights[""] = 2 }},
		{name: "unsafe account identity", update: func(policy *workqueue.Policy) { policy.AccountingWeights["\t"] = 1 }},
		{name: "missing producer entitlement", update: func(policy *workqueue.Policy) {
			policy.Producers["1001"] = workqueue.ProducerRule{}
		}},
		{name: "unknown producer pool", update: func(policy *workqueue.Policy) {
			policy.Producers["1001"] = workqueue.ProducerRule{Pools: []string{"foreign"}, Priorities: []int{3}, FairnessKeys: []string{""}}
		}},
		{name: "producer login", update: func(policy *workqueue.Policy) {
			policy.Producers["operator"] = policy.Producers["1001"]
		}},
		{name: "producer zero", update: func(policy *workqueue.Policy) {
			policy.Producers["0"] = policy.Producers["1001"]
		}},
		{name: "producer leading zero", update: func(policy *workqueue.Policy) {
			policy.Producers["01001"] = policy.Producers["1001"]
		}},
		{name: "lossless large producer", valid: true, update: func(policy *workqueue.Policy) {
			policy.Producers["9007199254740993"] = policy.Producers["1001"]
		}},
		{name: "operation closure budget", update: func(policy *workqueue.Policy) { policy.Limits.Operations = 1 }},
		{name: "engineering byte ceiling", update: func(policy *workqueue.Policy) { policy.Limits.AssignmentBytes = 49 << 10 }},
		{name: "noncanonical integer", update: func(policy *workqueue.Policy) { policy.Limits.LedgerBytes = workqueue.MaxTimestamp + 1 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := workqueue.DefaultPolicy("1001", "owner/repo")
			if test.update != nil {
				test.update(&policy)
			}
			err := workqueue.ValidatePolicy(policy)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), "policy_invalid")) {
				t.Fatalf("invalid standalone policy escaped contract validation: %v", err)
			}
		})
	}
}

func TestValidatePolicyChecksStandaloneWorkerProfiles(t *testing.T) {
	tests := []struct {
		name   string
		update func(*workqueue.WorkerProfile)
		valid  bool
	}{
		{name: "immutable SHA-256", update: func(profile *workqueue.WorkerProfile) { profile.Ref = strings.Repeat("a", 64) }, valid: true},
		{name: "engineering Claim ceiling", update: func(profile *workqueue.WorkerProfile) { profile.MaxClaims = 16 }, valid: true},
		{name: "missing principal", update: func(profile *workqueue.WorkerProfile) { profile.Principal = "" }},
		{name: "principal login", update: func(profile *workqueue.WorkerProfile) { profile.Principal = "operator" }},
		{name: "principal zero", update: func(profile *workqueue.WorkerProfile) { profile.Principal = "0" }},
		{name: "principal leading zero", update: func(profile *workqueue.WorkerProfile) { profile.Principal = "01001" }},
		{name: "lossless large principal", valid: true, update: func(profile *workqueue.WorkerProfile) { profile.Principal = "9007199254740993" }},
		{name: "missing trust domain", update: func(profile *workqueue.WorkerProfile) { profile.TrustDomain = "" }},
		{name: "missing credential scope", update: func(profile *workqueue.WorkerProfile) { profile.CredentialScope = "" }},
		{name: "missing effect scope", update: func(profile *workqueue.WorkerProfile) { profile.EffectScope = "" }},
		{name: "mutable ref", update: func(profile *workqueue.WorkerProfile) { profile.Ref = "refs/heads/main" }},
		{name: "uppercase ref", update: func(profile *workqueue.WorkerProfile) { profile.Ref = strings.Repeat("A", 40) }},
		{name: "workflow escape", update: func(profile *workqueue.WorkerProfile) { profile.Workflow = ".github/workflows/../foreign.lock.yml" }},
		{name: "oversized assignment", update: func(profile *workqueue.WorkerProfile) { profile.MaxClaims = 17 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			policy := workqueue.DefaultPolicy("1001", "owner/repo")
			pool := policy.Pools["default"]
			profile := pool.Profiles["default"]
			test.update(&profile)
			pool.Profiles["default"] = profile
			policy.Pools["default"] = pool
			err := workqueue.ValidatePolicy(policy)
			if test.valid && err != nil {
				t.Fatal(err)
			}
			if !test.valid && (err == nil || !strings.Contains(err.Error(), "policy_invalid")) {
				t.Fatalf("invalid standalone profile escaped contract validation: %v", err)
			}
		})
	}
}
