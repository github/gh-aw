package parser

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

func taskFixture(args ...string) map[string]any {
	if args == nil {
		args = []string{}
	}
	return map[string]any{"description": "Check source", "command": "go", "args": args}
}

func TestTasksResolve(t *testing.T) {
	tasks, err := ResolveTasks(map[string]any{"go": true, "check": taskFixture("vet", "./...")})
	require.NoError(t, err)
	require.Len(t, tasks, 6)
	require.Equal(t, []string{"test", "-count=1", "./..."}, tasks["go.test"].Args)
	require.Equal(t, []string{"-w", "."}, tasks["go.fmt"].Args)
	require.Equal(t, 60, tasks["check"].Timeout)
	require.Equal(t, 300, tasks["go.readiness"].Timeout)
	fresh := GoTasks()
	fresh["go.test"].Args[0] = "changed"
	require.Equal(t, "test", GoTasks()["go.test"].Args[0])
	for _, value := range []any{false, map[string]any{"go": false}, map[string]any{}} {
		tasks, err := ResolveTasks(value)
		require.NoError(t, err)
		require.Empty(t, tasks)
	}
}

func TestTasksRejectInvalidDefinitions(t *testing.T) {
	for name, value := range map[string]any{
		"enabled scalar":    true,
		"null":              nil,
		"preset map":        map[string]any{"go": map[string]any{}},
		"reserved built-in": map[string]any{"go.test": taskFixture()},
		"prototype":         map[string]any{"constructor": taskFixture()},
		"invalid name":      map[string]any{"bad/name": taskFixture()},
		"unknown field":     map[string]any{"check": map[string]any{"description": "Check", "command": "go", "env": map[string]any{}}},
		"path command":      map[string]any{"check": map[string]any{"description": "Check", "command": "./go"}},
		"expression":        map[string]any{"check": taskFixture("${{ github.event.issue.title }}")},
		"nul":               map[string]any{"check": taskFixture("a\x00b")},
		"null argv":         map[string]any{"check": map[string]any{"description": "Check", "command": "go", "args": nil}},
		"zero timeout":      map[string]any{"check": map[string]any{"description": "Check", "command": "go", "timeout": 0}},
		"large timeout":     map[string]any{"check": map[string]any{"description": "Check", "command": "go", "timeout": 601}},
		"argv limit":        map[string]any{"check": taskFixture(make([]string, 129)...)},
	} {
		t.Run(name, func(t *testing.T) {
			_, err := ResolveTasks(value)
			require.Error(t, err)
		})
	}
	tasks := map[string]any{"go": true}
	for i := range 60 {
		tasks[fmt.Sprintf("task%d", i)] = taskFixture()
	}
	_, err := ResolveTasks(tasks)
	require.ErrorContains(t, err, "64 expanded")
}

func TestTasksMergeAtomically(t *testing.T) {
	base := map[string]any{"tasks": map[string]any{"go": true, "check": taskFixture("vet", "./...")}}
	extra := map[string]any{"tasks": map[string]any{"check": taskFixture("vet", "./..."), "build": taskFixture("build", "./...")}}
	merged, err := MergeTools(base, extra)
	require.NoError(t, err)
	tasks, err := ResolveTasks(merged["tasks"])
	require.NoError(t, err)
	require.Len(t, tasks, 7)
	require.Equal(t, []string{"vet", "./..."}, tasks["check"].Args)
	_, err = MergeTools(base, map[string]any{"tasks": map[string]any{"check": taskFixture("vet", "-race", "./...")}})
	require.ErrorContains(t, err, "conflicting definitions of tools.tasks.check")
	for _, pair := range [][2]map[string]any{{base, {"tasks": false}}, {{"tasks": false}, base}} {
		merged, err := MergeTools(pair[0], pair[1])
		require.NoError(t, err)
		require.Equal(t, false, merged["tasks"])
	}
	merged, err = MergeTools(base, map[string]any{"tasks": map[string]any{"go": false}})
	require.NoError(t, err)
	tasks, err = ResolveTasks(merged["tasks"])
	require.NoError(t, err)
	require.Len(t, tasks, 1)
}

func TestTasksSchema(t *testing.T) {
	for _, tasks := range []any{false, map[string]any{"go": true}, map[string]any{"check": taskFixture("vet", "./...")}} {
		require.NoError(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "tools": map[string]any{"tasks": tasks},
		}, "tasks.md"))
	}
	for _, tasks := range []any{true, map[string]any{"check": map[string]any{"description": "Check", "command": "go", "cwd": "."}}} {
		require.Error(t, ValidateMainWorkflowFrontmatterWithSchemaAndLocation(map[string]any{
			"on": "workflow_dispatch", "tools": map[string]any{"tasks": tasks},
		}, "tasks.md"))
	}
}
