//go:build !integration

package workflow

import (
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	yamlv3 "go.yaml.in/yaml/v3"
)

// Guard-policy error codes, aligned with pkg/cli/gateway_logs_types.go and the normative
// GitHub MCP access-control specification (Appendix B).
const (
	formalErrorToolNotAllowed    = -32001 // tool not in allowed-tools (general access denied)
	formalErrorRepoNotAllowed    = -32002 // repos guard failed
	formalErrorInsufficientRole  = -32003 // roles guard failed
	formalErrorPrivateRepoDenied = -32004 // private-repos: false guard failed
	formalErrorBlockedUser       = -32005 // blocked-users guard failed (within integrity management)
	formalErrorIntegrityTooLow   = -32006 // min-integrity guard failed
)

type formalToolConfig struct {
	Repos        []string
	Roles        []string
	PrivateRepos *bool
	AllowedTools []string
	BlockedUsers []string
	MinIntegrity string
}

type formalAccessRequest struct {
	Repository       string
	UserRole         string
	IsPrivate        bool
	ToolName         string
	UserLogin        string
	ContentIntegrity string
}

func TestFormal_ExactMatchAllow(t *testing.T) {
	allowed := formalEvaluateAccess(formalToolConfig{Repos: []string{"github/gh-aw"}}, formalAccessRequest{Repository: "github/gh-aw"})
	denied := formalEvaluateAccess(formalToolConfig{Repos: []string{"github/gh-aw"}}, formalAccessRequest{Repository: "github/other"})

	assert.True(t, allowed.allow)
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorRepoNotAllowed, denied.errorCode)
}

func TestFormal_WildcardMatch(t *testing.T) {
	// owner/* — all repos under an owner
	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"github/*"}}, formalAccessRequest{Repository: "github/gh-aw"}).allow)
	assert.False(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"github/*"}}, formalAccessRequest{Repository: "microsoft/vscode"}).allow)
	// */repo — exact repo name under any owner
	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"*/gh-aw"}}, formalAccessRequest{Repository: "github/gh-aw"}).allow)
	assert.False(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"*/gh-aw"}}, formalAccessRequest{Repository: "github/other"}).allow)
	// */* — full wildcard (equivalent to no repos restriction)
	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"*/*"}}, formalAccessRequest{Repository: "any/repo"}).allow)
}

func TestFormal_OmittedReposAllowAll(t *testing.T) {
	// P2_RepoMatch: omitted repos field means "no restriction" — all accessible repos are allowed (spec §4.4.1).
	assert.True(t, formalEvaluateAccess(formalToolConfig{}, formalAccessRequest{Repository: "github/gh-aw"}).allow)
	assert.True(t, formalEvaluateAccess(formalToolConfig{}, formalAccessRequest{Repository: "microsoft/vscode"}).allow)

	// An empty slice is an invalid configuration (rejected at compilation; see TestValidateGitHubGuardPolicy
	// in tools_validation_test.go). In the runtime formal model it is treated as no-match.
	assert.False(t, formalEvaluateAccess(formalToolConfig{Repos: []string{}}, formalAccessRequest{Repository: "github/gh-aw"}).allow)
}

func TestFormal_RoleFilter(t *testing.T) {
	cfg := formalToolConfig{Repos: []string{"*/*"}, Roles: []string{"write", "admin"}}
	assert.True(t, formalEvaluateAccess(cfg, formalAccessRequest{Repository: "github/gh-aw", UserRole: "write"}).allow)
	denied := formalEvaluateAccess(cfg, formalAccessRequest{Repository: "github/gh-aw", UserRole: "read"})
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorInsufficientRole, denied.errorCode)
}

func TestFormal_PrivateRepoControl(t *testing.T) {
	allowPrivate := true
	denyPrivate := false

	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"myorg/*"}, PrivateRepos: &allowPrivate}, formalAccessRequest{Repository: "myorg/private", IsPrivate: true}).allow)
	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"myorg/*"}, PrivateRepos: &denyPrivate}, formalAccessRequest{Repository: "myorg/public", IsPrivate: false}).allow)
	denied := formalEvaluateAccess(formalToolConfig{Repos: []string{"myorg/*"}, PrivateRepos: &denyPrivate}, formalAccessRequest{Repository: "myorg/private", IsPrivate: true})
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorPrivateRepoDenied, denied.errorCode)
}

func TestFormal_BlockedUserDeny(t *testing.T) {
	cfg := formalToolConfig{Repos: []string{"github/gh-aw"}, Roles: []string{"write"}, BlockedUsers: []string{"bad-actor"}}
	assert.True(t, formalEvaluateAccess(cfg, formalAccessRequest{
		Repository: "github/gh-aw", UserRole: "write", UserLogin: "good-actor", ContentIntegrity: "approved",
	}).allow)
	denied := formalEvaluateAccess(cfg, formalAccessRequest{
		Repository: "github/gh-aw", UserRole: "write", UserLogin: "bad-actor", ContentIntegrity: "approved",
	})
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorBlockedUser, denied.errorCode)
}

func TestFormal_ToolNameFilter(t *testing.T) {
	cfg := formalToolConfig{Repos: []string{"*/*"}, AllowedTools: []string{"issue_read"}}
	assert.True(t, formalEvaluateAccess(cfg, formalAccessRequest{Repository: "github/gh-aw", ToolName: "issue_read"}).allow)
	// no AllowedTools configured → any tool is allowed
	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"*/*"}}, formalAccessRequest{Repository: "github/gh-aw", ToolName: "delete_repo"}).allow)
	// tool not in allowlist
	denied := formalEvaluateAccess(cfg, formalAccessRequest{Repository: "github/gh-aw", ToolName: "delete_repo"})
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorToolNotAllowed, denied.errorCode)
	// empty tool name with a non-empty allowlist must also deny (the tool is not present)
	deniedEmpty := formalEvaluateAccess(cfg, formalAccessRequest{Repository: "github/gh-aw", ToolName: ""})
	assert.False(t, deniedEmpty.allow)
	assert.Equal(t, formalErrorToolNotAllowed, deniedEmpty.errorCode)
}

func TestFormal_IntegrityLevelOrder(t *testing.T) {
	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"*/*"}, MinIntegrity: "approved"}, formalAccessRequest{Repository: "github/gh-aw", ContentIntegrity: "approved"}).allow)
	assert.True(t, formalEvaluateAccess(formalToolConfig{Repos: []string{"*/*"}, MinIntegrity: "approved"}, formalAccessRequest{Repository: "github/gh-aw", ContentIntegrity: "merged"}).allow)
	denied := formalEvaluateAccess(formalToolConfig{Repos: []string{"*/*"}, MinIntegrity: "approved"}, formalAccessRequest{Repository: "github/gh-aw", ContentIntegrity: "unapproved"})
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorIntegrityTooLow, denied.errorCode)
}

func TestFormal_UnknownContentIntegrityDenied(t *testing.T) {
	// An unknown ContentIntegrity value (rank -1) is below any valid minimum threshold.
	denied := formalEvaluateAccess(
		formalToolConfig{Repos: []string{"*/*"}, MinIntegrity: "approved"},
		formalAccessRequest{Repository: "github/gh-aw", ContentIntegrity: "unknown-level"},
	)
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorIntegrityTooLow, denied.errorCode)
}

func TestFormal_InvalidMinIntegrityConfigDenied(t *testing.T) {
	// An unrecognized MinIntegrity configuration is fail-safe: denies all requests.
	denied := formalEvaluateAccess(
		formalToolConfig{Repos: []string{"*/*"}, MinIntegrity: "invalid"},
		formalAccessRequest{Repository: "github/gh-aw", ContentIntegrity: "merged"},
	)
	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorIntegrityTooLow, denied.errorCode)
}

func TestFormal_CombinedFiltersAllAllow(t *testing.T) {
	allowPrivate := true
	cfg := formalToolConfig{
		Repos:        []string{"github/gh-aw"},
		Roles:        []string{"write"},
		PrivateRepos: &allowPrivate,
		AllowedTools: []string{"issue_read"},
		MinIntegrity: "approved",
	}
	assert.True(t, formalEvaluateAccess(cfg, formalAccessRequest{
		Repository: "github/gh-aw", UserRole: "write", IsPrivate: true, ToolName: "issue_read", UserLogin: "good-user", ContentIntegrity: "approved",
	}).allow)
}

func TestFormal_ErrorCodeFirstFailingGuard(t *testing.T) {
	// INV2: the denial code is selected by the FIRST failing guard in evaluation order:
	//   tool → repo → role → private-repo → blocked-user → integrity
	//
	// Each case makes one guard the first failure while one or more later guards also fail,
	// ensuring that only the first guard's code is returned.
	denyPrivate := false
	cfg := formalToolConfig{
		Repos:        []string{"github/gh-aw"},
		Roles:        []string{"write"},
		PrivateRepos: &denyPrivate,
		AllowedTools: []string{"issue_read"},
		BlockedUsers: []string{"bad-actor"},
		MinIntegrity: "approved",
	}

	cases := []struct {
		name     string
		req      formalAccessRequest
		wantCode int
	}{
		{
			name: "tool guard fails first (repo/role/private/blocked/integrity also fail)",
			req: formalAccessRequest{
				Repository: "github/other", UserRole: "read", IsPrivate: true,
				ToolName: "delete_repo", UserLogin: "bad-actor", ContentIntegrity: "none",
			},
			wantCode: formalErrorToolNotAllowed,
		},
		{
			name: "repo guard fails first (role/private/blocked/integrity also fail)",
			req: formalAccessRequest{
				Repository: "github/other", UserRole: "read", IsPrivate: true,
				ToolName: "issue_read", UserLogin: "bad-actor", ContentIntegrity: "none",
			},
			wantCode: formalErrorRepoNotAllowed,
		},
		{
			name: "role guard fails first (private/blocked/integrity also fail)",
			req: formalAccessRequest{
				Repository: "github/gh-aw", UserRole: "read", IsPrivate: true,
				ToolName: "issue_read", UserLogin: "bad-actor", ContentIntegrity: "none",
			},
			wantCode: formalErrorInsufficientRole,
		},
		{
			name: "private-repo guard fails first (blocked/integrity also fail)",
			req: formalAccessRequest{
				Repository: "github/gh-aw", UserRole: "write", IsPrivate: true,
				ToolName: "issue_read", UserLogin: "bad-actor", ContentIntegrity: "none",
			},
			wantCode: formalErrorPrivateRepoDenied,
		},
		{
			name: "blocked-user guard fails first within integrity (integrity also fails)",
			// PrivateRepos must allow private so private check passes.
			// Use a separate cfg that allows private repos so private guard is not the first failure.
			req: formalAccessRequest{
				Repository: "github/gh-aw", UserRole: "write", IsPrivate: false,
				ToolName: "issue_read", UserLogin: "bad-actor", ContentIntegrity: "none",
			},
			wantCode: formalErrorBlockedUser,
		},
		{
			name: "integrity guard fails (all earlier guards pass)",
			req: formalAccessRequest{
				Repository: "github/gh-aw", UserRole: "write", IsPrivate: false,
				ToolName: "issue_read", UserLogin: "good-actor", ContentIntegrity: "none",
			},
			wantCode: formalErrorIntegrityTooLow,
		},
	}

	// The cfg uses denyPrivate=false; IsPrivate=true triggers the private-repo guard, and
	// IsPrivate=false bypasses it — so blocked-user and integrity guards can be the first failure.

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := formalEvaluateAccess(cfg, tc.req)
			assert.False(t, d.allow)
			assert.Equal(t, tc.wantCode, d.errorCode)
		})
	}
}

func TestFormal_BlockedUserSafetyProperty(t *testing.T) {
	// SAFETY_BlockedUserAlwaysDenied: a blocked user is always denied, even when all other guards
	// (tool, repo, role, private-repo) pass. Blocked-user is the first integrity-management guard.
	allowPrivate := true
	cfg := formalToolConfig{
		Repos:        []string{"*/*"},
		Roles:        []string{"admin"},
		PrivateRepos: &allowPrivate,
		AllowedTools: []string{"issue_read"},
		BlockedUsers: []string{"blocked"},
		MinIntegrity: "merged",
	}

	denied := formalEvaluateAccess(cfg, formalAccessRequest{
		Repository: "any/repo", UserRole: "admin", IsPrivate: false,
		ToolName: "issue_read", UserLogin: "blocked", ContentIntegrity: "merged",
	})

	assert.False(t, denied.allow)
	assert.Equal(t, formalErrorBlockedUser, denied.errorCode)
}

func TestFormal_NoSpuriousAllowInvariant(t *testing.T) {
	allowPrivate := true
	cfg := formalToolConfig{
		Repos:        []string{"github/gh-aw"},
		Roles:        []string{"write"},
		PrivateRepos: &allowPrivate,
		AllowedTools: []string{"issue_read"},
		BlockedUsers: []string{"blocked"},
		MinIntegrity: "approved",
	}

	cases := []formalAccessRequest{
		{Repository: "github/other", UserRole: "write", ToolName: "issue_read", ContentIntegrity: "approved"},
		{Repository: "github/gh-aw", UserRole: "read", ToolName: "issue_read", ContentIntegrity: "approved"},
		{Repository: "github/gh-aw", UserRole: "write", ToolName: "delete_repo", ContentIntegrity: "approved"},
		{Repository: "github/gh-aw", UserRole: "write", ToolName: "issue_read", ContentIntegrity: "none"},
		{Repository: "github/gh-aw", UserRole: "write", UserLogin: "blocked", ToolName: "issue_read", ContentIntegrity: "approved"},
	}

	for _, req := range cases {
		assert.False(t, formalEvaluateAccess(cfg, req).allow)
	}
}

type formalDecision struct {
	allow     bool
	errorCode int
}

func formalEvaluateAccess(cfg formalToolConfig, req formalAccessRequest) formalDecision {
	// Guard evaluation order (spec §4.5.3):
	// 1. Tool selection (allowed-tools)
	// 2. Repository access control: repo → role → private-repo visibility
	// 3. Integrity management: blocked-user → min-integrity threshold
	if len(cfg.AllowedTools) > 0 && !containsExact(cfg.AllowedTools, req.ToolName) {
		return formalDecision{errorCode: formalErrorToolNotAllowed}
	}
	if !formalRepositoryAllowed(cfg.Repos, req.Repository) {
		return formalDecision{errorCode: formalErrorRepoNotAllowed}
	}
	if len(cfg.Roles) > 0 && !containsExact(cfg.Roles, req.UserRole) {
		return formalDecision{errorCode: formalErrorInsufficientRole}
	}
	if cfg.PrivateRepos != nil && !*cfg.PrivateRepos && req.IsPrivate {
		return formalDecision{errorCode: formalErrorPrivateRepoDenied}
	}
	if containsExact(cfg.BlockedUsers, req.UserLogin) {
		return formalDecision{errorCode: formalErrorBlockedUser}
	}
	if cfg.MinIntegrity != "" {
		cfgRank := formalIntegrityRank(cfg.MinIntegrity)
		reqRank := formalIntegrityRank(req.ContentIntegrity)
		// Fail-safe: an unrecognized MinIntegrity configuration value is treated as deny-all.
		if cfgRank < 0 {
			return formalDecision{errorCode: formalErrorIntegrityTooLow}
		}
		// Deny if content integrity is below (or unrecognized — rank -1) the configured minimum.
		if reqRank < cfgRank {
			return formalDecision{errorCode: formalErrorIntegrityTooLow}
		}
	}
	return formalDecision{allow: true}
}

func formalRepositoryAllowed(patterns []string, repository string) bool {
	// nil patterns means the repos field is omitted: all accessible repositories are allowed (spec §4.4.1).
	if patterns == nil {
		return true
	}
	// Empty slice is an invalid configuration (rejected at compilation); runtime treats as no-match.
	if len(patterns) == 0 {
		return false
	}
	repoOwner, repoName, ok := strings.Cut(repository, "/")
	if !ok || repoOwner == "" || repoName == "" {
		return false
	}

	for _, pattern := range patterns {
		patternOwner, patternRepo, ok := strings.Cut(pattern, "/")
		if !ok {
			continue
		}
		switch {
		case patternOwner == "*" && patternRepo == "*":
			return true
		case patternOwner == "*" && patternRepo == repoName:
			// */repo — matches exact repo name under any owner
			return true
		case patternOwner == repoOwner && patternRepo == "*":
			return true
		case patternOwner == repoOwner && patternRepo == repoName:
			return true
		}
	}
	return false
}

func formalIntegrityRank(level string) int {
	switch strings.ToLower(level) {
	case "none":
		return 0
	case "unapproved":
		return 1
	case "approved":
		return 2
	case "merged":
		return 3
	default:
		return -1
	}
}

func containsExact(values []string, needle string) bool {
	return slices.Contains(values, needle)
}

// ---------------------------------------------------------------------------
// Compliance fixture runner
// ---------------------------------------------------------------------------
// fixtureFile mirrors the top-level YAML structure for the compliance fixture files
// located at specs/github-mcp-access-control-compliance/*.yaml.
type fixtureFile struct {
	FixtureID   string            `yaml:"fixture_id"`
	Description string            `yaml:"description"`
	Scenarios   []fixtureScenario `yaml:"scenarios"`
}

type fixtureScenario struct {
	ScenarioID  string          `yaml:"scenario_id"`
	Description string          `yaml:"description"`
	Input       fixtureInput    `yaml:"input"`
	Expected    fixtureExpected `yaml:"expected"`
}

type fixtureInput struct {
	ToolConfig fixtureToolConfig `yaml:"tool_config"`
	Request    fixtureRequest    `yaml:"request"`
}

type fixtureToolConfig struct {
	AllowedRepos []string `yaml:"allowed-repos"`
	Repos        []string `yaml:"repos"`
	Roles        []string `yaml:"roles"`
	PrivateRepos *bool    `yaml:"private-repos"`
	AllowedTools []string `yaml:"allowed-tools"`
	BlockedUsers []string `yaml:"blocked-users"`
	MinIntegrity string   `yaml:"min-integrity"`
}

type fixtureRequest struct {
	Repository       string `yaml:"repository"`
	UserRole         string `yaml:"user_role"`
	IsPrivate        bool   `yaml:"is_private"`
	ToolName         string `yaml:"tool_name"`
	UserLogin        string `yaml:"user_login"`
	ContentIntegrity string `yaml:"content_integrity"`
}

type fixtureExpected struct {
	Decision  string `yaml:"decision"`
	ErrorCode *int   `yaml:"error_code"`
	Reason    string `yaml:"reason"`
}

// TestFormal_FixtureRunner loads every YAML fixture from
// specs/github-mcp-access-control-compliance/ and verifies each scenario against
// formalEvaluateAccess. This binds the executable formal model to the YAML spec
// artifacts so that a divergence between the fixture and the evaluator is caught
// immediately rather than silently.
func TestFormal_FixtureRunner(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "specs", "github-mcp-access-control-compliance")
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err, "failed to read compliance fixture directory")

	var totalScenarios int
	var fixtureFilesLoaded int
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		fixtureFilesLoaded++
		require.Truef(t, slices.Contains(documentedComplianceFixtures, entry.Name()),
			"fixture runner found undocumented fixture file %s; update documentedComplianceFixtures and README table together",
			entry.Name())

		fixturePath := filepath.Join(fixtureDir, entry.Name())
		data, err := os.ReadFile(fixturePath)
		require.NoErrorf(t, err, "failed to read fixture file %s", entry.Name())

		var ff fixtureFile
		require.NoErrorf(t, yamlv3.Unmarshal(data, &ff), "failed to parse fixture file %s", entry.Name())

		for _, sc := range ff.Scenarios {
			totalScenarios++
			t.Run(sc.ScenarioID, func(t *testing.T) {
				// 'repos' is a deprecated alias for 'allowed-repos' in fixtures.
				repos := sc.Input.ToolConfig.AllowedRepos
				if repos == nil {
					repos = sc.Input.ToolConfig.Repos
				}
				cfg := formalToolConfig{
					Repos:        repos,
					Roles:        sc.Input.ToolConfig.Roles,
					PrivateRepos: sc.Input.ToolConfig.PrivateRepos,
					AllowedTools: sc.Input.ToolConfig.AllowedTools,
					BlockedUsers: sc.Input.ToolConfig.BlockedUsers,
					MinIntegrity: sc.Input.ToolConfig.MinIntegrity,
				}
				req := formalAccessRequest{
					Repository:       sc.Input.Request.Repository,
					UserRole:         sc.Input.Request.UserRole,
					IsPrivate:        sc.Input.Request.IsPrivate,
					ToolName:         sc.Input.Request.ToolName,
					UserLogin:        sc.Input.Request.UserLogin,
					ContentIntegrity: sc.Input.Request.ContentIntegrity,
				}

				got := formalEvaluateAccess(cfg, req)

				if sc.Expected.Decision == "allow" {
					assert.True(t, got.allow, "%s: expected allow, got deny (code %d)", sc.ScenarioID, got.errorCode)
					assert.Zero(t, got.errorCode, "%s: allow decision must have zero error code", sc.ScenarioID)
				} else {
					assert.False(t, got.allow, "%s: expected deny, got allow", sc.ScenarioID)
					if sc.Expected.ErrorCode != nil {
						assert.Equal(t, *sc.Expected.ErrorCode, got.errorCode,
							"%s: wrong error code (decision=%s)", sc.ScenarioID, sc.Expected.Decision)
					}
				}
			})
		}
	}

	require.Positive(t, totalScenarios, "fixture runner found no scenarios — check fixture directory path")
	require.Equalf(t, len(documentedComplianceFixtures), fixtureFilesLoaded,
		"fixture runner must load every documented fixture file in %s", fixtureDir)
}

// documentedComplianceFixtures mirrors the "Compliance Fixtures" table in
// specs/github-mcp-access-control-compliance/README.md. This list MUST be kept in
// sync with both the README table and the fixture files present on disk; a
// mismatch here signals that the spec, the fixture directory, or this test has
// drifted out of sync with one of the others.
var documentedComplianceFixtures = []string{
	"exact-match-allow.yaml",
	"wildcard-deny.yaml",
	"empty-repos-block.yaml",
	"role-deny.yaml",
	"tool-name-filter.yaml",
	"empty-tool-name-deny.yaml",
	"empty-tool-name-empty-list-allow.yaml",
	"blocked-user-deny.yaml",
	"private-repo-block.yaml",
	"integrity-level-block.yaml",
	"combined-filter-allow.yaml",
	"combined-blocked-integrity.yaml",
}

// TestFormal_FixtureCountConsistency verifies that the fixture files documented in
// specs/github-mcp-access-control-compliance/README.md's "Compliance Fixtures" table
// exactly match the `.yaml` files present in the fixture directory, so the README
// table cannot silently drift from the fixtures actually exercised by
// TestFormal_FixtureRunner.
func TestFormal_FixtureCountConsistency(t *testing.T) {
	fixtureDir := filepath.Join("..", "..", "specs", "github-mcp-access-control-compliance")
	entries, err := os.ReadDir(fixtureDir)
	require.NoError(t, err, "failed to read compliance fixture directory")

	var onDisk []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".yaml" {
			continue
		}
		onDisk = append(onDisk, entry.Name())
	}

	documented := slices.Clone(documentedComplianceFixtures)
	slices.Sort(documented)
	slices.Sort(onDisk)

	assert.Equal(t, documented, onDisk,
		"fixture files on disk in %s must match the README's documented fixture list exactly "+
			"(add/remove entries in both the README table and documentedComplianceFixtures when fixtures change)",
		fixtureDir)
}
