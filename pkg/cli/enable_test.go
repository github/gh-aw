//go:build !integration

package cli

import (
	"reflect"
	"testing"
)

func TestCollectWorkflowToggleTargets(t *testing.T) {
	workflows := map[string]*GitHubWorkflow{
		"disabled": {ID: 1, State: "disabled_manually"},
		"active":   {ID: 2, State: "active"},
	}
	names := []string{"disabled", "active", "missing"}
	files := []string{"workflows/disabled.md", "workflows/active.md"}
	targets, missing := collectWorkflowToggleTargets(t.Context(), names, files, workflows, false)
	if !reflect.DeepEqual(missing, []string{"missing"}) {
		t.Fatalf("unexpected missing workflows: %v", missing)
	}
	want := []workflowToggleTarget{{
		Name: "active", ID: 2, LockFileBase: "active.lock.yml",
		CurrentState: "active", HasGitHubEntry: true,
	}}
	if !reflect.DeepEqual(targets, want) {
		t.Fatalf("got targets %+v, want %+v", targets, want)
	}
}

func TestCollectWorkflowToggleTargetsEmpty(t *testing.T) {
	targets, missing := collectWorkflowToggleTargets(t.Context(), nil, nil, nil, false)
	if targets != nil || missing != nil {
		t.Fatalf("empty selection must retain nil slices: %v, %v", targets, missing)
	}
}

func TestFilterWorkflowsToDisable(t *testing.T) {
	files := []string{"workflows/keep.lock.yml", "workflows/keep.yaml", "workflows/first.yml", "workflows/full.yml", "workflows/last.yaml"}
	got := filterWorkflowsToDisable(files, []string{"keep", "full.yml"}, false)
	want := []string{"first.yml", "last.yaml"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if got := filterWorkflowsToDisable(nil, nil, false); got != nil {
		t.Fatalf("empty selection must retain nil slice: %v", got)
	}
}
