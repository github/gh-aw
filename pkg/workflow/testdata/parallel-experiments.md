---
name: Parallel fixture
on: workflow_dispatch
strict: false
engine:
  id: copilot
  model: ${{ experiments.model }}
model-routing: false
network:
  allowed: []
tools:
  github: false
experiments:
  storage: cache
  model: [gpt-4.1, gpt-5]
  prompt: [concise, detailed]
safe-outputs:
  add-comment:
    max: 1
parallel-experiments:
  include:
    - experiment: model
      variant: gpt-5
      harness: copilot-cli
    - experiment: prompt
      variant: concise
      model: gpt-4.1
      harness: copilot-sdk
    - experiment: prompt
      variant: detailed
---
{{#if experiments.prompt == 'concise'}}
Be concise.
{{else}}
Explain your reasoning.
{{/if}}
Summarize the repository.
