package actions

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/goccy/go-yaml"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const commentedWorkflow = `# License header
---
name: 'Original' # workflow name
'on': [push] # trigger
jobs: # all jobs
  build: # build job
    runs-on: ubuntu-latest
    steps:
      # Checkout sources
      - id: checkout
        uses: actions/checkout@v6 # checkout version
      - # Test sources
        id: test
        run: |- # shell explanation
          # This is shell content, not a YAML comment.
          echo "test"
      # End of steps
  call:
    uses: ./.github/workflows/worker.yml # reusable workflow
    needs: build # dependency
# Workflow footer
`

func sourceComments(t *testing.T, content string) []string {
	t.Helper()
	var texts []string
	for _, item := range lexer.Tokenize(content) {
		if item.Type == token.CommentType {
			texts = append(texts, item.Value)
		}
	}
	slices.Sort(texts)
	return texts
}

func TestSourceExactRoundTrip(t *testing.T) {
	for _, content := range []string{
		commentedWorkflow,
		strings.TrimSuffix(commentedWorkflow, "\n"),
		strings.ReplaceAll(commentedWorkflow, "\n", "\r\n"),
		"jobs: {call: {uses: './worker.yml'}} # compact",
		"env: &shared {VALUE: yes}\njobs: {a: {runs-on: ubuntu-latest, env: *shared, steps: [{run: echo}]}}\n",
	} {
		t.Run(content, func(t *testing.T) {
			input, err := Parse([]byte(content))
			require.NoError(t, err)
			resolved, err := Resolve(input)
			require.NoError(t, err)
			for range 2 {
				emitted, err := Emit(resolved)
				require.NoError(t, err)
				assert.Equal(t, content, emitted)
			}
		})
	}
}

func TestSourceCommentsSurviveASTEdits(t *testing.T) {
	input, err := Parse([]byte(commentedWorkflow))
	require.NoError(t, err)
	input.Name = "Edited"
	emitted, err := Emit(input)
	require.NoError(t, err)
	assert.Equal(t, sourceComments(t, commentedWorkflow), sourceComments(t, emitted))
	reparsed, err := Parse([]byte(emitted))
	require.NoError(t, err)
	assert.Equal(t, "Edited", reparsed.Name)
	again, err := Emit(input)
	require.NoError(t, err)
	assert.Equal(t, emitted, again, "emission must not mutate retained syntax")
	input.Name = "Original"
	original, err := Emit(input)
	require.NoError(t, err)
	assert.Equal(t, commentedWorkflow, original, "editing back must restore the original bytes")
}

func TestSourceStepChangesKeepComments(t *testing.T) {
	for _, operation := range []string{"edit", "script", "insert", "reorder"} {
		t.Run(operation, func(t *testing.T) {
			input, err := Parse([]byte(commentedWorkflow))
			require.NoError(t, err)
			job := input.Jobs[0]
			switch operation {
			case "edit":
				job.Steps[0].(RawStep).Fields["uses"] = "actions/checkout@v7"
			case "script":
				job.Steps[1].(RawStep).Fields["run"] = "# This is shell content, not a YAML comment.\necho edited\n"
			case "insert":
				job.Steps = append([]Step{RunStep{Run: "echo first"}}, job.Steps...)
			case "reorder":
				slices.Reverse(job.Steps)
			}
			emitted, err := Emit(input)
			require.NoError(t, err)
			assert.Equal(t, sourceComments(t, commentedWorkflow), sourceComments(t, emitted))
			assert.Regexp(t, `uses: ['"]?actions/checkout@v[67]['"]? # checkout version`, emitted)
			assert.Contains(t, emitted, "# This is shell content, not a YAML comment.")
		})
	}
}

func TestSourceQuotedKeysAndLineEndings(t *testing.T) {
	for _, ending := range []string{"\n", "\r\n"} {
		input, err := Parse([]byte(strings.ReplaceAll(commentedWorkflow, "\n", ending)))
		require.NoError(t, err)
		input.On = []string{"push", "workflow_dispatch"}
		emitted, err := Emit(input)
		require.NoError(t, err)
		assert.Equal(t, sourceComments(t, commentedWorkflow), sourceComments(t, emitted))
		if ending == "\r\n" {
			assert.NotContains(t, strings.ReplaceAll(emitted, "\r\n", ""), "\n")
		}
	}
}

func TestSourceDeletionRetainsOtherComments(t *testing.T) {
	input, err := Parse([]byte(commentedWorkflow))
	require.NoError(t, err)
	input.Jobs[1].Needs = nil
	emitted, err := Emit(input)
	require.NoError(t, err)
	expected := sourceComments(t, commentedWorkflow)
	expected = slices.DeleteFunc(expected, func(text string) bool { return text == " dependency" })
	assert.Equal(t, expected, sourceComments(t, emitted))
}

func TestSourceMergedAliasEdits(t *testing.T) {
	source := `env: &shared
  VALUE: original # definition
jobs:
  build:
    runs-on: ubuntu-latest
    env:
      <<: *shared # merged values
      LOCAL: keep # local value
    steps: [{run: echo}]
`
	input, err := Parse([]byte(source))
	require.NoError(t, err)
	input.Env["VALUE"] = Literal("changed")
	emitted, err := Emit(input)
	require.NoError(t, err)
	assert.Equal(t, sourceComments(t, source), sourceComments(t, emitted))
}

func TestSourceDependencyCommentsFollowTheirIDs(t *testing.T) {
	source := `jobs:
  z: {uses: './worker.yml'}
  a: {uses: './worker.yml'}
  consumer:
    uses: ./worker.yml
    needs:
      - z # z dependency
      - a # a dependency
`
	input, err := Parse([]byte(source))
	require.NoError(t, err)
	for _, job := range input.Jobs {
		if job.ID == "consumer" {
			job.Needs = append(job.Needs, "middle")
		}
	}
	input.Jobs = append(input.Jobs, &Job{ID: "middle", Uses: "./worker.yml"})
	emitted, err := Emit(input)
	require.NoError(t, err)
	assert.Equal(t, sourceComments(t, source), sourceComments(t, emitted))
	assert.Contains(t, emitted, "- a # a dependency")
	assert.Contains(t, emitted, "- z # z dependency")
}

func TestSourceIDLessStepCommentsFollowReordering(t *testing.T) {
	source := `jobs:
  build:
    runs-on: ubuntu-latest
    steps:
      # First step
      - uses: actions/checkout@v6 # pinned version
      # Second step
      - run: echo test
`
	input, err := Parse([]byte(source))
	require.NoError(t, err)
	slices.Reverse(input.Jobs[0].Steps)
	emitted, err := Emit(input)
	require.NoError(t, err)
	assert.Equal(t, sourceComments(t, source), sourceComments(t, emitted))
	assert.Less(t, strings.Index(emitted, "# Second step"), strings.Index(emitted, "# First step"))
	assert.Contains(t, emitted, "uses: actions/checkout@v6 # pinned version")
}

func TestSourceNestedEditsAndAliases(t *testing.T) {
	source := `# Header
env: &shared
  VALUE: original # shared value
jobs:
  build:
    runs-on: ubuntu-latest
    env: *shared # inherited value
    steps: [{run: echo}]
# Footer
`
	input, err := Parse([]byte(source))
	require.NoError(t, err)
	input.Env["VALUE"] = Literal("changed")
	emitted, err := Emit(input)
	require.NoError(t, err)
	assert.Equal(t, sourceComments(t, source), sourceComments(t, emitted))
	actual, err := Parse([]byte(emitted))
	require.NoError(t, err)
	assert.Equal(t, Literal("changed"), actual.Env["VALUE"])
	assert.Equal(t, Literal("original"), actual.Jobs[0].Env["VALUE"], "an anchor edit must not silently change a different AST value")
}

func TestSourceMappingEdits(t *testing.T) {
	for _, content := range []string{
		"jobs: {call: {uses: './worker.yml'}} # compact",
		"jobs:\n  build:\n    runs-on: ubuntu-latest # runner\n    steps: [{run: echo}]\n",
	} {
		t.Run(content, func(t *testing.T) {
			input, err := Parse([]byte(content))
			require.NoError(t, err)
			input.Name = "Added"
			if input.Jobs[0].Uses != "" {
				input.Jobs[0].With = map[string]Expression{"NEW": Literal("value")}
			} else {
				input.Jobs[0].Env = map[string]Expression{"NEW": Literal("value")}
			}
			emitted, err := Emit(input)
			require.NoError(t, err)
			assert.Equal(t, sourceComments(t, content), sourceComments(t, emitted))
		})
	}
}

func TestSourceEditSequenceMappingScalar(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		"on:\n  schedule:\n    - schedule: daily at noon # frequency\n",
		"on:\n  schedule:\n    - schedule: daily at noon # frequency\n      timezone: UTC # timezone\n",
		"on: {schedule: [{schedule: daily at noon, timezone: UTC}]} # trigger\n",
	} {
		t.Run(content, func(t *testing.T) {
			t.Parallel()
			var before, after map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(content), &before))
			require.NoError(t, yaml.Unmarshal([]byte(content), &after))
			schedule := after["on"].(map[string]any)["schedule"].([]any)[0].(map[string]any)
			schedule["schedule"] = "daily around noon"
			result, err := EditSource(content, before, after)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(result), &actual))
			assert.Equal(t, after, actual)
			assert.Equal(t, sourceComments(t, content), sourceComments(t, result))
		})
	}
}

func TestSourceEditSequenceMappingKeys(t *testing.T) {
	t.Parallel()
	for _, content := range []string{
		"checkout:\n  - repository: org/repo # repository\n    github-app:\n      app-id: old\n      private-key: secret # private key\n",
		"checkout:\n  - repository: org/repo\n    github-app:\n      'app-id': old # identifier\n      private-key: secret # private key\n",
		"checkout:\n  - github-app: {app-id: old, private-key: secret} # credentials\n    repository: org/repo\n",
		"checkout: [{repository: org/repo, github-app: {app-id: old, private-key: secret}}] # checkout\n",
	} {
		t.Run(content, func(t *testing.T) {
			t.Parallel()
			var before, after map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(content), &before))
			require.NoError(t, yaml.Unmarshal([]byte(content), &after))
			checkout := after["checkout"].([]any)[0].(map[string]any)
			app := checkout["github-app"].(map[string]any)
			app["client-id"] = app["app-id"]
			delete(app, "app-id")
			result, err := EditSource(content, before, after)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(result), &actual))
			assert.Equal(t, after, actual)
			assert.Equal(t, sourceComments(t, content), sourceComments(t, result))
		})
	}
}

func TestSourceEditMappingRenameConflicts(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		source  string
		after   map[string]any
		comment string
	}{
		{
			name:    "ambiguous removed keys",
			source:  "old-a: same # removed a\nold-b: same # removed b\n",
			after:   map[string]any{"new": "same"},
			comment: "# removed",
		},
		{
			name:    "ambiguous added keys",
			source:  "old: same # removed\n",
			after:   map[string]any{"new-a": "same", "new-b": "same"},
			comment: "# removed",
		},
		{
			name:    "existing destination",
			source:  "old: same # removed\nnew: null # destination\n",
			after:   map[string]any{"new": nil},
			comment: "# removed",
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var before map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(tt.source), &before))
			result, err := EditSource(tt.source, before, tt.after)
			require.NoError(t, err)
			var actual map[string]any
			require.NoError(t, yaml.Unmarshal([]byte(result), &actual))
			assert.Equal(t, tt.after, actual)
			assert.NotContains(t, result, tt.comment, "unrelated removals must not be treated as renames")
			if tt.name == "existing destination" {
				assert.Contains(t, result, "# destination")
			}
		})
	}
}

func TestRepositoryWorkflowCommentEdits(t *testing.T) {
	files, err := filepath.Glob(filepath.Join("..", "..", "..", ".github", "workflows", "*.yml"))
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		t.Run(filepath.Base(file), func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(file)
			require.NoError(t, err)
			input, err := Parse(content)
			require.NoError(t, err)
			input.Name += " (edited)"
			emitted, err := Emit(input)
			require.NoError(t, err)
			assert.Equal(t, sourceComments(t, string(content)), sourceComments(t, emitted),
				"editing the AST must not discard or duplicate comments elsewhere in a workflow")
		})
	}
	t.Cleanup(func() {
		t.Logf("Preserved comments through AST edits for all %d repository YAML workflows", len(files))
	})
}
