package aw

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExecutionBlocksSafeOutputSteps(t *testing.T) {
	document, err := Parse(`---
safe-outputs:
  steps:
    - uses: actions/checkout@v6
  jobs:
    publish:
      steps:
        - run: echo public
---
`)
	require.NoError(t, err)
	blocks, err := ExecutionBlocks(context.Background(), document)
	require.NoError(t, err)
	require.Len(t, blocks, 3)
	assert.Empty(t, blocks[0].Path)
	assert.Equal(t, "safe-outputs", blocks[1].Path)
	require.Len(t, blocks[1].Steps, 1)
	assert.Equal(t, "safe-outputs.steps[0]", blocks[1].Steps[0].Path)
	assert.Equal(t, "safe-outputs.jobs.publish", blocks[2].Path)
	require.Len(t, blocks[2].Steps, 1)
	assert.Equal(t, "safe-outputs.jobs.publish.steps[0]", blocks[2].Steps[0].Path)
}

func TestExecutionBlocksMalformedSafeOutputSteps(t *testing.T) {
	document, err := Parse("---\nsafe-outputs: {steps: PRIVATE_VALUE}\n---\n")
	require.NoError(t, err)
	_, err = ExecutionBlocks(context.Background(), document)
	require.ErrorContains(t, err, "safe-outputs.steps")
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
}

func TestExecutionBlocksStepSections(t *testing.T) {
	document, err := Parse(`---
pre-steps:
  - run: echo root-pre
steps:
  - run: echo root
pre-agent-steps:
  - run: echo root-pre-agent
post-steps:
  - run: echo root-post
jobs:
  build:
    setup-steps:
      - run: echo setup
    pre-steps:
      - run: echo job-pre
    steps:
      - run: echo job
---
`)
	require.NoError(t, err)
	blocks, err := ExecutionBlocks(context.Background(), document)
	require.NoError(t, err)
	require.Len(t, blocks, 2)
	paths := func(block ExecutionBlock) (paths, sections []string, post []bool) {
		for _, step := range block.Steps {
			paths = append(paths, step.Path)
			sections = append(sections, step.Section)
			post = append(post, step.Post)
		}
		return paths, sections, post
	}
	rootPaths, rootSections, rootPost := paths(blocks[0])
	assert.Equal(t, []string{"pre-steps[0]", "steps[0]", "pre-agent-steps[0]", "post-steps[0]"}, rootPaths, "root steps should follow compiler order")
	assert.Equal(t, []string{"pre-steps", "steps", "pre-agent-steps", "post-steps"}, rootSections)
	assert.Equal(t, []bool{false, false, false, true}, rootPost, "only post-steps run after the agent")
	jobPaths, jobSections, _ := paths(blocks[1])
	assert.Equal(t, []string{"jobs.build.setup-steps[0]", "jobs.build.pre-steps[0]", "jobs.build.steps[0]"}, jobPaths, "job steps should follow compiler order")
	assert.Equal(t, []string{"setup-steps", "pre-steps", "steps"}, jobSections)
}

func TestExecutionBlocksSectionsAreScoped(t *testing.T) {
	document, err := Parse(`---
setup-steps:
  - run: echo ignored
safe-outputs:
  jobs:
    publish:
      pre-steps:
        - run: echo ignored
---
`)
	require.NoError(t, err)
	blocks, err := ExecutionBlocks(context.Background(), document)
	require.NoError(t, err)
	for _, block := range blocks {
		assert.Empty(t, block.Steps, "unsupported step sections should not be visited at %q", block.Path)
	}
}

func TestExecutionBlocksMalformedJobSetupSteps(t *testing.T) {
	document, err := Parse("---\njobs: {build: {setup-steps: PRIVATE_VALUE}}\n---\n")
	require.NoError(t, err)
	_, err = ExecutionBlocks(context.Background(), document)
	require.ErrorContains(t, err, "jobs.build.setup-steps")
	assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
}
