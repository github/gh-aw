package lint

import (
	"context"
	"testing"

	"github.com/github/gh-aw/pkg/workflow/actions"
	"github.com/github/gh-aw/pkg/workflow/aw"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAdditionalASTPolicyRules(t *testing.T) {
	for _, tt := range []struct {
		name, rule, fields string
		paths              []string
	}{
		{"minimum integrity", "tool-scope", "tools: {github: {min-integrity: none}}", []string{"tools.bash"}},
		{"intentional shell", "tool-scope", "tools: {bash: true, github: {min-integrity: none}}", nil},
		{"imported intent unknown", "tool-scope", "imports: [shared.md]\ntools: {github: {min-integrity: none}}", nil},
		{"object imported intent unknown", "tool-scope", "imports: {aw: [shared.md]}\ntools: {github: {min-integrity: none}}", nil},
		{"empty object imports retain policy", "tool-scope", "imports: {}\ntools: {github: {min-integrity: none}}", []string{"tools.bash"}},
		{"empty aw imports retain policy", "tool-scope", "imports: {aw: []}\ntools: {github: {min-integrity: none}}", []string{"tools.bash"}},
		{"proxy refusal", "tool-scope", "tools: {bash: false}", []string{"tools.cli-proxy"}},
		{"imported proxy unknown", "tool-scope", "imports: [shared.md]\ntools: {bash: false}", nil},
		{"explicit proxy conflict", "tool-scope", "imports: [shared.md]\ntools: {bash: false, cli-proxy: true}", []string{"tools.cli-proxy"}},
		{"github proxy conflict", "tool-scope", "tools: {bash: [], cli-proxy: false, github: {mode: gh-proxy}}", []string{"tools.github.mode"}},
		{"mcp shell refusal", "tool-scope", "tools: {bash: false, cli-proxy: false, github: {mode: local}}", nil},
		{"cache isolation", "tool-scope", "tools: {cache-memory: {scope: repo}}", []string{"tools.cache-memory.scope"}},
		{"cache list isolation", "tool-scope", "tools: {cache-memory: [{scope: workflow}, {scope: repo}]}", []string{"tools.cache-memory[1].scope"}},
		{"cache shorthand", "tool-scope", "tools: {cache-memory: true, github: true}", nil},
		{"sandbox disabled", "sandbox-config", "sandbox: {agent: false}", []string{"sandbox.agent"}},
		{"agent overrides", "sandbox-config", "sandbox: {agent: {command: custom, args: [custom], env: {KEY: value}}}", []string{"sandbox.agent.command", "sandbox.agent.args", "sandbox.agent.env"}},
		{"gateway overrides", "sandbox-config", "sandbox: {mcp: {container: custom, version: custom, entrypoint: custom, args: [custom], entrypoint-args: [custom]}}", []string{"sandbox.mcp.container", "sandbox.mcp.version", "sandbox.mcp.entrypoint", "sandbox.mcp.args", "sandbox.mcp.entrypoint-args"}},
		{"empty overrides", "sandbox-config", "sandbox: {agent: {command: '', args: [], env: {}}, mcp: {version: ''}}", nil},
		{"sandbox shorthand", "sandbox-config", "sandbox: {agent: awf}", nil},
		{"AWF shorthand", "sandbox-config", "sandbox: awf", nil},
		{"default shorthand", "sandbox-config", "sandbox: default", nil},
		{"fork typo", "fork-association", "on: {pull_request: {forks: {author-association: [owner, OWNER]}}}", []string{"on.pull_request.forks.author-association"}},
		{"fork associations", "fork-association", "on: {pull_request: {forks: {author-association: [OWNER, MEMBER, COLLABORATOR, CONTRIBUTOR, FIRST_TIMER, FIRST_TIME_CONTRIBUTOR, MANNEQUIN, NONE]}}}", nil},
		{"fork empty list", "fork-association", "on: {pull_request: {forks: {author-association: []}}}", nil},
		{"fork missing list", "fork-association", "on: {pull_request: {forks: {}}}", []string{"on.pull_request.forks.author-association"}},
		{"fork repository wildcard permitted", "fork-association", "on: {pull_request: {forks: '*'}}", nil},
		{"other event not inferred", "fork-association", "on: {issues: {forks: {author-association: [invalid]}}}", nil},
		{"shorthand trigger", "fork-association", "on: pull_request", nil},
		{"explicit mode", "checkout-mode", "checkout: {multi: 'true'}", []string{"checkout.multi"}},
		{"literal list", "checkout-mode", "checkout: {repository: 'owner/one owner/two'}", []string{"checkout.repository"}},
		{"single checkout", "checkout-mode", "checkout: {repo: '${{ inputs.repository }}', path: .}", nil},
		{"multiline expression", "checkout-mode", "checkout:\n  repository: |\n    ${{\n      vars.REPOSITORY\n    }}", nil},
		{"disabled checkout", "checkout-mode", "checkout: false", nil},
		{"multi prerequisites", "checkout-mode", "checkout: {multi: true}", []string{"checkout.path", "checkout.allowed-repos"}},
		{"imported prerequisites unknown", "checkout-mode", "imports: [shared.md]\ncheckout: {multi: true}", nil},
		{"multi explicit contradictions", "checkout-mode", "checkout: {multi: true, path: ../outside, allowed-repos: ['*/*'], lfs: true, current: true, wiki: true, submodules: recursive}", []string{"checkout.path", "checkout.allowed-repos", "checkout.lfs", "checkout.current", "checkout.wiki", "checkout.submodules"}},
		{"multi expression path refused", "checkout-mode", "checkout: {multi: true, path: '${{ vars.PREFIX }}', allowed-repos: 'owner/*'}", []string{"checkout.path"}},
		{"multi empty allowlist", "checkout-mode", "checkout: {multi: true, path: repos, allowed-repos: []}", []string{"checkout.allowed-repos"}},
		{"multi scoped", "checkout-mode", "checkout: {multi: true, path: repos, allowed-repos: 'owner/*', submodules: false}", nil},
		{"space in literal prefix", "checkout-mode", "checkout: {multi: true, path: 'my repos', allowed-repos: 'owner/*'}", nil},
		{"container option", "image-argument", "jobs: {test: {container: '--option'}}", []string{"jobs.test.container"}},
		{"service option", "image-argument", "safe-jobs: {test: {services: {db: {image: '--option'}}}}", []string{"safe-jobs.test.services.db.image"}},
		{"mcp image option", "image-argument", "mcp-servers: {server: {container: '--option'}}", []string{"mcp-servers.server.container"}},
		{"normal image", "image-argument", "mcp-servers: {server: {container: 'registry.example/server:1'}}", nil},
	} {
		t.Run(tt.name, func(t *testing.T) {
			document, err := aw.Parse("---\n" + tt.fields + "\n---\n")
			require.NoError(t, err)
			before, err := aw.Emit(document)
			require.NoError(t, err)
			runner, err := Default().Select(tt.rule)
			require.NoError(t, err)
			diagnostics, err := runner.Run(context.Background(), Input{Document: document})
			require.NoError(t, err)
			var paths []string
			for _, diagnostic := range diagnostics {
				paths = append(paths, diagnostic.Path)
			}
			assert.ElementsMatch(t, tt.paths, paths)
			after, err := aw.Emit(document)
			require.NoError(t, err)
			assert.Equal(t, before, after)
		})
	}
}

func TestAdditionalASTPolicyFailures(t *testing.T) {
	for _, tt := range []struct{ rule, fields string }{
		{"tool-scope", "tools: {cache-memory: PRIVATE_VALUE}"},
		{"tool-scope", "tools: {cache-memory: [{scope: repo}, PRIVATE_VALUE]}"},
		{"sandbox-config", "sandbox: {agent: {args: PRIVATE_VALUE}}"},
		{"sandbox-config", "sandbox: PRIVATE_VALUE"},
		{"sandbox-config", "sandbox: true"},
		{"sandbox-config", "sandbox: [PRIVATE_VALUE]"},
		{"sandbox-config", "sandbox: {agent: 123}"},
		{"sandbox-config", "sandbox: {agent: {env: 123}}"},
		{"tool-scope", "imports: {aw: PRIVATE_VALUE}\ntools: {}"},
		{"fork-association", "on: {pull_request: {forks: {author-association: PRIVATE_VALUE}}}"},
		{"checkout-mode", "checkout: PRIVATE_VALUE"},
		{"image-argument", "jobs: {test: {container: [PRIVATE_VALUE]}}"},
	} {
		t.Run(tt.rule+"/"+tt.fields, func(t *testing.T) {
			document, err := aw.Parse("---\n" + tt.fields + "\n---\n")
			require.NoError(t, err)
			runner, err := Default().Select(tt.rule)
			require.NoError(t, err)
			_, err = runner.Run(context.Background(), Input{Document: document})
			require.Error(t, err)
			assert.NotContains(t, err.Error(), "PRIVATE_VALUE")
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = runner.Run(ctx, Input{Document: document})
			assert.ErrorIs(t, err, context.Canceled)
		})
	}
}

func TestImageArgumentActionsAST(t *testing.T) {
	workflow, err := actions.Parse([]byte("jobs:\n  test:\n    runs-on: ubuntu-latest\n    container: {image: '--option'}\n    services:\n      db: {image: '-option'}\n    steps:\n      - run: echo public\n"))
	require.NoError(t, err)
	runner, err := New(ImageArgumentRule())
	require.NoError(t, err)
	diagnostics, err := runner.Run(context.Background(), Input{Workflow: workflow})
	require.NoError(t, err)
	require.Len(t, diagnostics, 2)
	assert.Equal(t, "jobs.test.container.image", diagnostics[0].Path)
	assert.Equal(t, "jobs.test.services.db.image", diagnostics[1].Path)
}

func TestAdditionalASTStrictSeverity(t *testing.T) {
	for _, tt := range []struct{ rule, fields string }{
		{"tool-scope", "tools: {cache-memory: {scope: repo}}"},
		{"sandbox-config", "sandbox: {agent: false}"},
	} {
		document, err := aw.Parse("---\nstrict: false\n" + tt.fields + "\n---\n")
		require.NoError(t, err)
		runner, err := Default().Select(tt.rule)
		require.NoError(t, err)
		diagnostics, err := runner.Run(context.Background(), Input{Document: document})
		require.NoError(t, err)
		require.Len(t, diagnostics, 1)
		assert.Equal(t, Warning, diagnostics[0].Severity)
	}
}
