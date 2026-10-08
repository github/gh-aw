package workqueue

import (
	"encoding/json"
	"os"
	"slices"
	"testing"
)

func TestSharedNativeConformanceFixtures(t *testing.T) {
	data, err := os.ReadFile("../../actions/setup/js/work_queue_conformance_fixtures.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixtures struct {
		Version   int `json:"version"`
		Selection []struct {
			Name              string         `json:"name"`
			Mode              string         `json:"mode"`
			AccountingWeights map[string]int `json:"accounting_weights"`
			Works             []struct {
				Name        string `json:"name"`
				Priority    int    `json:"priority"`
				FairnessKey string `json:"fairness_key"`
				Enqueued    int64  `json:"enqueued"`
			} `json:"works"`
			ExpectedNames []string `json:"expected_names"`
		} `json:"selection"`
		Codec []struct {
			Name      string `json:"name"`
			Input     string `json:"input"`
			Canonical string `json:"canonical"`
			Invalid   bool   `json:"invalid"`
		} `json:"codec"`
	}
	if err := json.Unmarshal(data, &fixtures); err != nil {
		t.Fatal(err)
	}
	if fixtures.Version != Version || len(fixtures.Selection) == 0 || len(fixtures.Codec) == 0 {
		t.Fatal("shared native fixture contract is missing or unsupported")
	}
	for _, fixture := range fixtures.Selection {
		t.Run("selection/"+fixture.Name, func(t *testing.T) {
			commits := testGenesis(t, func(policy *Policy) {
				policy.Mode = fixture.Mode
				policy.AccountingWeights = fixture.AccountingWeights
				rule := policy.Producers[testPrincipal]
				rule.FairnessKeys = []string{}
				for key := range fixture.AccountingWeights {
					rule.FairnessKeys = append(rule.FairnessKeys, key)
				}
				slices.Sort(rule.FairnessKeys)
				policy.Producers[testPrincipal] = rule
				pool := policy.Pools["default"]
				pool.LogicalLimit, pool.NativeLimit = 256, 256
				policy.Pools["default"] = pool
			})
			nodes := make([]WorkDefinition, 0, len(fixture.Works))
			for _, input := range fixture.Works {
				work := testNode(t, commits, input.Name)
				work.Priority, work.FairnessKey, work.Enqueued = input.Priority, input.FairnessKey, input.Enqueued
				nodes = append(nodes, work)
			}
			commits = testSubmit(t, commits, "shared-native-submit", nodes...)
			granted, decision := testGrant(t, commits, "shared-native-grant", len(fixture.ExpectedNames), 256)
			state, err := Replay(granted)
			if err != nil {
				t.Fatal(err)
			}
			names := make([]string, 0, len(decision.Operations))
			for _, operation := range decision.Operations {
				var claim ClaimOperation
				if err := json.Unmarshal(operation, &claim); err != nil {
					t.Fatal(err)
				}
				names = append(names, state.Works[claim.WorkID].NodeKey)
			}
			if !slices.Equal(names, fixture.ExpectedNames) {
				t.Fatalf("hand-derived native fixture: got %v, expected %v", names, fixture.ExpectedNames)
			}
			if len(decision.Assignments) != len(names) {
				t.Fatal("default profile must preserve one Claim per original assignment")
			}
		})
	}
	for _, fixture := range fixtures.Codec {
		t.Run("codec/"+fixture.Name, func(t *testing.T) {
			canonical, err := Canonical([]byte(fixture.Input))
			if fixture.Invalid {
				if err == nil {
					t.Fatalf("shared invalid input accepted: %s", canonical)
				}
				return
			}
			if err != nil || string(canonical) != fixture.Canonical {
				t.Fatalf("hand-derived canonical fixture: got %s, expected %s, error=%v", canonical, fixture.Canonical, err)
			}
		})
	}
}
