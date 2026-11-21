package taint

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func summaryTestFiles(t testing.TB) fstest.MapFS {
	t.Helper()
	catalog, err := javaScriptSummaries()
	require.NoError(t, err)
	files := make(fstest.MapFS)
	for name, contract := range catalog.Modules {
		source, err := os.ReadFile(filepath.Join("..", "..", "..", filepath.FromSlash(name)))
		require.NoError(t, err)
		hash := sha256.Sum256(source)
		require.Equal(t, contract.SHA256, hex.EncodeToString(hash[:]), "runtime API changed: review the transfer function and transitive dependencies before updating its hash")
		files[name] = &fstest.MapFile{Data: source}
		for _, dependency := range contract.Dependencies {
			_, exists := catalog.Modules[dependency]
			require.True(t, exists, "every transitive dependency must have a versioned contract")
		}
	}
	return files
}

func summaryTestOptions(t testing.TB) *JavaScriptOptions {
	t.Helper()
	options := nativeJavaScriptOptions(t)
	options.Mode, options.Files = JavaScriptFast, summaryTestFiles(t)
	options.RuntimePaths = map[string]string{"./actions/setup/js": "actions/setup/js"}
	return options
}

func TestJavaScriptSummaryCatalogSourceIdentity(t *testing.T) {
	files := summaryTestFiles(t)
	require.NotEmpty(t, files)
}

func TestJavaScriptFastSummaryEffects(t *testing.T) {
	options := summaryTestOptions(t)
	tests := []struct {
		name, source string
		leak         bool
		captured     bool
		stdout       bool
	}{
		{"mask registration", `require("./actions/setup/js/actions_secret_masking.cjs").maskSecret(process.env.TOKEN)`, false, false, false},
		{"mask not sanitizer", `const {maskSecret}=require("./actions/setup/js/actions_secret_masking.cjs"); maskSecret(process.env.TOKEN); console.log(process.env.TOKEN)`, true, false, true},
		{"mask custom callback", `global.core={setSecret: token=>console.error(token)}; require("./actions/setup/js/actions_secret_masking.cjs").maskSecret(process.env.TOKEN)`, true, false, false},
		{"captured mask", `require("./actions/setup/js/actions_secret_masking.cjs").maskSecret(process.env.TOKEN)`, false, true, true},
		{"escaping not sanitizer", `console.log(require("./actions/setup/js/actions_secret_masking.cjs").escapeWorkflowCommandValue(process.env.TOKEN))`, true, false, true},
		{"escaped command", "const {escapeWorkflowCommandValue}=require('./actions/setup/js/actions_secret_masking.cjs'); process.stdout.write('::add-mask::'+escapeWorkflowCommandValue(process.env.TOKEN)+'\\n')", false, false, false},
		{"git public field", `console.log(require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(process.env.TOKEN).GIT_CONFIG_COUNT)`, false, false, false},
		{"git auth field", `console.log(require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(process.env.TOKEN).GIT_CONFIG_VALUE_0)`, true, false, true},
		{"git callback effect", `require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(process.env.TOKEN, token=>console.error(token))`, true, false, false},
		{"git builtin callback", `require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(process.env.TOKEN, console.error)`, true, false, false},
		{"git callback mutation", `let captured; require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(process.env.TOKEN, token => {captured=token}); console.log(captured)`, true, false, true},
		{"git mask callback", `require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(process.env.TOKEN, core.setSecret)`, false, false, false},
		{"error retains source", `console.log(require("./actions/setup/js/error_helpers.cjs").getErrorMessage({message:process.env.TOKEN}))`, true, false, true},
		{"error request id retains source", `console.log(require("./actions/setup/js/error_helpers.cjs").getErrorMessage({message:"",response:{headers:{"x-github-request-id":process.env.TOKEN}}}))`, true, false, true},
		{"error status retains source", `console.log(require("./actions/setup/js/error_helpers.cjs").getErrorStatus({response:{status:process.env.TOKEN}}))`, true, false, true},
		{"error predicate", `console.log(require("./actions/setup/js/error_helpers.cjs").isLockedError({message:process.env.TOKEN,status:403}))`, false, false, false},
		{"comment limit predicate", `console.log(require("./actions/setup/js/error_helpers.cjs").isCommentLimitError({message:process.env.TOKEN,status:403}))`, false, false, false},
		{"OTLP returns credentials", `console.log(require("./actions/setup/js/otlp_header_helpers.cjs").getOTLPSecretValues())`, true, false, true},
		{"OTLP runner masks retain credentials", `console.log(require("./actions/setup/js/otlp_header_helpers.cjs").getOTLPRunnerMaskValues())`, true, false, true},
		{"OTLP reexported runner masks retain credentials", `console.log(require("./actions/setup/js/actions_secret_masking.cjs").getOTLPRunnerMaskValues())`, true, false, true},
		{"OTLP warning callback", `global.core={warning: ()=>console.error(process.env.TOKEN)}; require("./actions/setup/js/otlp_header_helpers.cjs").getOTLPSecretValues()`, true, false, false},
		{"OTLP runner masks warning callback", `global.core={warning: ()=>console.error(process.env.TOKEN)}; require("./actions/setup/js/otlp_header_helpers.cjs").getOTLPRunnerMaskValues()`, true, false, false},
		{"OTLP excludes unrelated env", `console.log(require("./actions/setup/js/otlp_header_helpers.cjs").getAuthorizationSecretValues("public"))`, false, false, false},
	}
	sources := make([]string, len(tests))
	for i, tt := range tests {
		sources[i] = tt.source
	}
	_, err := options.Parser.(JavaScriptBatchParser).ParseBatch(t.Context(), sources)
	require.NoError(t, err)
	before := len(options.Parser.(*TypeScriptParser).cache)
	secret := []Source{{Reference: "secrets.token", Path: "env.TOKEN"}}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result, err := AnalyzeJavaScript(JavaScriptInput{
				Name: "entry.cjs", Source: tt.source, CaptureStdout: tt.captured,
				Env: map[string][]Source{"TOKEN": secret, "OTEL_EXPORTER_OTLP_HEADERS": secret},
			}, SecretLeakRule(), *options)
			require.NoError(t, err)
			assert.Empty(t, result.Gaps)
			assert.Equal(t, tt.leak, len(result.Findings) > 0, "%+v", result)
			assert.Equal(t, tt.stdout, len(result.Stdout) > 0)
		})
	}
	assert.Len(t, options.Parser.(*TypeScriptParser).cache, before, "summaries never parse runtime helper bodies")
}

func TestJavaScriptSummaryInvalidationAndUnknownRuntime(t *testing.T) {
	options := summaryTestOptions(t)
	source := `console.log(require("./actions/setup/js/actions_secret_masking.cjs").getOTLPSecretValues())`
	secret := []Source{{Reference: "secrets.token", Path: "env.TOKEN"}}
	for _, changed := range []string{
		"actions/setup/js/actions_secret_masking.cjs",
		"actions/setup/js/otlp_header_helpers.cjs",
		"actions/setup/js/unreviewed.cjs",
	} {
		t.Run(changed, func(t *testing.T) {
			files := summaryTestFiles(t)
			files[changed] = &fstest.MapFile{Data: []byte(`console.log(process.env.TOKEN); module.exports = {getOTLPSecretValues:()=>process.env.TOKEN}`)}
			options.Files = files
			entry := source
			if changed == "actions/setup/js/unreviewed.cjs" {
				entry = `require("./actions/setup/js/unreviewed.cjs").getOTLPSecretValues()`
			}
			result, err := AnalyzeJavaScript(JavaScriptInput{Name: "entry.cjs", Source: entry, Env: map[string][]Source{"TOKEN": secret}}, SecretLeakRule(), *options)
			require.NoError(t, err)
			assert.Empty(t, result.Findings, "unknown boundaries are incomplete, not invented confirmed leaks")
			require.NotEmpty(t, result.Gaps)
			hash := sha256.Sum256(files[changed].Data)
			assert.NotContains(t, options.Parser.(*TypeScriptParser).cache, hash, "fast mode must not fall back to deep traversal")
		})
	}
}

func TestJavaScriptSummaryIdentityIsNotBasename(t *testing.T) {
	options := nativeJavaScriptOptions(t)
	options.Mode = JavaScriptFast
	options.Files = fstest.MapFS{
		"local/actions_secret_masking.cjs": &fstest.MapFile{Data: []byte(`module.exports.maskSecret = x => console.log(x)`)},
	}
	secret := []Source{{Reference: "secrets.token", Path: "env.TOKEN"}}
	result, err := AnalyzeJavaScript(JavaScriptInput{
		Name: "entry.cjs", Source: `require("./local/actions_secret_masking.cjs").maskSecret(process.env.TOKEN)`,
		Env: map[string][]Source{"TOKEN": secret},
	}, SecretLeakRule(), *options)
	require.NoError(t, err)
	require.Len(t, result.Findings, 1)
	assert.Empty(t, result.Gaps)
}

func TestJavaScriptSummaryRecursiveCallback(t *testing.T) {
	options := summaryTestOptions(t)
	result, err := AnalyzeJavaScript(JavaScriptInput{
		Name: "entry.cjs",
		Source: `const {maskSecret} = require("./actions/setup/js/actions_secret_masking.cjs");
			global.core = {setSecret: maskSecret}; maskSecret(process.env.TOKEN);`,
		Env: map[string][]Source{"TOKEN": {{Reference: "secrets.token", Path: "env.TOKEN"}}},
	}, SecretLeakRule(), *options)
	require.NoError(t, err)
	require.Contains(t, result.Gaps, CoverageGap{
		Path: "entry.cjs.javascript", Line: 2, Reason: "recursive JavaScript API summary callback is not modeled",
	})
}

func TestJavaScriptSummaryUnresolvedCallbacks(t *testing.T) {
	options := summaryTestOptions(t)
	for name, source := range map[string]string{
		"mask": `global.core={setSecret: process.env.SWITCH ? console.error : ()=>{}};
			require("./actions/setup/js/actions_secret_masking.cjs").maskSecret(process.env.TOKEN);`,
		"git": `const callback = process.env.SWITCH ? console.error : ()=>{};
			require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(process.env.TOKEN, callback);`,
		"git coercion": `const token = {toString: ()=>{console.error(process.env.TOKEN); return "token"}};
			require("./actions/setup/js/git_auth_env.cjs").buildGitAuthEnv(token);`,
		"error coercion": `const error = {toString: ()=>{console.error(process.env.TOKEN); return "error"}};
			require("./actions/setup/js/error_helpers.cjs").getErrorMessage(error);`,
		"nested error coercion": `require("./actions/setup/js/error_helpers.cjs").getErrorMessage({
			message: {toString() {console.error(process.env.TOKEN); return "error"}}
		});`,
		"nested locked predicate coercion": `require("./actions/setup/js/error_helpers.cjs").isLockedError({
			status: 403, message: {toString() {console.error(process.env.TOKEN); return "locked"}}
		});`,
		"nested rate limit predicate coercion": `require("./actions/setup/js/error_helpers.cjs").isRateLimitError({
			message: {toString() {console.error(process.env.TOKEN); return "rate limit exceeded"}}
		});`,
		"nested comment limit predicate coercion": `require("./actions/setup/js/error_helpers.cjs").isCommentLimitError({
			status: 403, message: {toString() {console.error(process.env.TOKEN); return "comment limit"}}
		});`,
	} {
		t.Run(name, func(t *testing.T) {
			result, err := AnalyzeJavaScript(JavaScriptInput{
				Name: "entry.cjs", Source: source,
				Env: map[string][]Source{"TOKEN": {{Reference: "secrets.token", Path: "env.TOKEN"}}},
			}, SecretLeakRule(), *options)
			require.NoError(t, err)
			assert.Empty(t, result.Findings, "unknown callbacks do not invent confirmed leaks")
			assert.NotEmpty(t, result.Gaps, "unknown callback effects must remain incomplete")
		})
	}
}

func BenchmarkJavaScriptFastSummary(b *testing.B) {
	for _, mode := range []JavaScriptMode{JavaScriptDeep, JavaScriptFast} {
		b.Run(string(mode), func(b *testing.B) {
			options := summaryTestOptions(b)
			options.Mode = mode
			input := JavaScriptInput{
				Name: "entry.cjs", Source: `require("./actions/setup/js/actions_secret_masking.cjs").maskSecret(process.env.TOKEN)`,
				Env: map[string][]Source{"TOKEN": {{Reference: "secrets.token", Path: "env.TOKEN"}}},
			}
			_, err := AnalyzeJavaScript(input, SecretLeakRule(), *options)
			require.NoError(b, err)
			b.ResetTimer()
			for b.Loop() {
				result, err := AnalyzeJavaScript(input, SecretLeakRule(), *options)
				require.NoError(b, err)
				if mode == JavaScriptFast {
					require.Empty(b, result.Findings)
					require.Empty(b, result.Gaps)
				}
			}
		})
	}
}
