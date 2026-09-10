//go:build !integration

package cli

import (
	"context"
	"math/rand"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUpdateActionRefsInContent_VersionTagReplacement(t *testing.T) {
	t.Parallel()
	// Stub latest release lookup so the test doesn't hit the network.
	deps := newActionUpdateDepsWithLatestRelease(func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		switch repo {
		case "actions/checkout":
			return "v6", "de0fac2e4500dabe0009e67214ff5f5447ce83dd", nil
		case "actions/setup-go":
			return "v6", "4b73464bb391a5985ede5d7fd8a6c0c9c59c4c4e", nil
		default:
			return currentVersion, "", nil
		}
	})

	input := `steps:
  - uses: actions/checkout@v4
  - uses: actions/setup-go@v5
  - run: echo hello`

	want := `steps:
  - uses: actions/checkout@v6
  - uses: actions/setup-go@v6
  - run: echo hello`

	cache := make(map[string]latestReleaseResult)
	changed, got, err := updateActionRefsInContentWithDeps(context.Background(), deps, input, cache, make(map[string]coolDownCheckResult), true, false, 0)
	if err != nil {
		t.Fatalf("updateActionRefsInContent() error = %v", err)
	}
	if !changed {
		t.Error("updateActionRefsInContent() changed = false, want true")
	}
	if got != want {
		t.Errorf("updateActionRefsInContent() output mismatch\nGot:\n%s\nWant:\n%s", got, want)
	}
}
func TestUpdateActionRefsInContent_SHAPinnedReplacement(t *testing.T) {
	t.Parallel()
	newSHA := "de0fac2e4500dabe0009e67214ff5f5447ce83dd"
	deps := newActionUpdateDepsWithLatestRelease(func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		return "v6.0.2", newSHA, nil
	})

	oldSHA := "11bd71901bbe5b1630ceea73d27597364c9af683"
	input := "        uses: actions/checkout@" + oldSHA + " # v5.0.0"
	want := "        uses: actions/checkout@" + newSHA + "  # v6.0.2"

	cache := make(map[string]latestReleaseResult)
	changed, got, err := updateActionRefsInContentWithDeps(context.Background(), deps, input, cache, make(map[string]coolDownCheckResult), true, false, 0)
	if err != nil {
		t.Fatalf("updateActionRefsInContent() error = %v", err)
	}
	if !changed {
		t.Error("updateActionRefsInContent() changed = false, want true")
	}
	if got != want {
		t.Errorf("updateActionRefsInContent() output mismatch\nGot:  %s\nWant: %s", got, want)
	}
}
func TestUpdateActionRefsInContent_CacheReusedAcrossLines(t *testing.T) {
	t.Parallel()
	// Verify that the cache prevents duplicate calls to latest-release resolution.
	callCount := 0
	deps := newActionUpdateDepsWithLatestRelease(func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		callCount++
		return "v8", "ed597411d8f9245be5a6f5b7f5d52e63b7e62e96", nil
	})

	// Two lines referencing the same repo@version: should resolve via cache after first call
	input := `steps:
  - uses: actions/github-script@v7
  - uses: actions/github-script@v7`

	cache := make(map[string]latestReleaseResult)
	changed, _, err := updateActionRefsInContentWithDeps(context.Background(), deps, input, cache, make(map[string]coolDownCheckResult), true, false, 0)
	if err != nil {
		t.Fatalf("updateActionRefsInContent() error = %v", err)
	}
	if !changed {
		t.Error("updateActionRefsInContent() changed = false, want true")
	}
	if callCount != 1 {
		t.Errorf("latest release resolver called %d times, want 1 (cache should prevent second call)", callCount)
	}
}
func TestUpdateActionRefsInContent_AllOrgsUpdatedWhenAllowMajor(t *testing.T) {
	t.Parallel()
	// With allowMajor=true (default behaviour), non-actions/* org references should
	// also be updated to the latest major version.
	deps := newActionUpdateDepsWithLatestRelease(func(_ context.Context, repo, currentVersion string, allowMajor, verbose bool) (string, string, error) {
		switch repo {
		case "docker/login-action":
			return "v4", "newsha11234567890123456789012345678901234", nil
		case "github/codeql-action":
			return "v4", "newsha21234567890123456789012345678901234", nil
		default:
			return currentVersion, "", nil
		}
	})

	input := `steps:
  - uses: docker/login-action@v3
  - uses: github/codeql-action@v3`

	want := `steps:
  - uses: docker/login-action@v4
  - uses: github/codeql-action@v4`

	cache := make(map[string]latestReleaseResult)
	changed, got, err := updateActionRefsInContentWithDeps(context.Background(), deps, input, cache, make(map[string]coolDownCheckResult), true, false, 0)
	if err != nil {
		t.Fatalf("updateActionRefsInContent() error = %v", err)
	}
	if !changed {
		t.Error("updateActionRefsInContent() changed = false, want true")
	}
	if got != want {
		t.Errorf("updateActionRefsInContent() output mismatch\nGot:\n%s\nWant:\n%s", got, want)
	}
}
func TestUpdateSkillRefsInContentWithResolver_UpdatesStringAndObjectSkillRefs(t *testing.T) {
	t.Parallel()
	oldRepoSkillSHA := "1111111111111111111111111111111111111111"
	oldPathSkillSHA := "2222222222222222222222222222222222222222"
	newRepoSkillSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newPathSkillSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	input := `---
name: test
skills:
  - githubnext/skills@` + oldRepoSkillSHA + `
  - skill: githubnext/skills/review/security@` + oldPathSkillSHA + `
  - ${{ inputs.dynamic_skill }}
---
body
`

	resolver := func(_ context.Context, repo, currentRef string, allowMajor, verbose bool, coolDown time.Duration) (string, error) {
		if repo != "githubnext/skills" {
			t.Fatalf("resolver called with repo %q, want githubnext/skills", repo)
		}
		switch currentRef {
		case oldRepoSkillSHA:
			return newRepoSkillSHA, nil
		case oldPathSkillSHA:
			return newPathSkillSHA, nil
		default:
			return currentRef, nil
		}
	}

	changed, got, err := updateSkillRefsInContentWithResolver(context.Background(), input, true, false, 0, resolver)
	if err != nil {
		t.Fatalf("updateSkillRefsInContentWithResolver() error = %v", err)
	}
	if !changed {
		t.Fatal("updateSkillRefsInContentWithResolver() changed = false, want true")
	}
	if !strings.Contains(got, "githubnext/skills@"+newRepoSkillSHA) {
		t.Fatalf("updated content missing updated repo skill ref:\n%s", got)
	}
	if !strings.Contains(got, "githubnext/skills/review/security@"+newPathSkillSHA) {
		t.Fatalf("updated content missing updated path skill ref:\n%s", got)
	}
	if !strings.Contains(got, "- ${{ inputs.dynamic_skill }}") {
		t.Fatalf("updated content unexpectedly modified expression skill ref:\n%s", got)
	}
}
func TestUpdateSkillRefsInContentWithResolver_PreservesObjectAuthFields(t *testing.T) {
	t.Parallel()
	oldSHA := "1111111111111111111111111111111111111111"
	newSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	input := `---
name: test
skills:
  - skill: githubnext/skills/review/security@` + oldSHA + `
    github-token: ${{ secrets.SOME_TOKEN }}
---
body
`
	resolver := func(_ context.Context, repo, currentRef string, allowMajor, verbose bool, coolDown time.Duration) (string, error) {
		if repo != "githubnext/skills" {
			t.Fatalf("resolver called with repo %q, want githubnext/skills", repo)
		}
		if currentRef != oldSHA {
			t.Fatalf("resolver called with ref %q, want %q", currentRef, oldSHA)
		}
		return newSHA, nil
	}

	changed, got, err := updateSkillRefsInContentWithResolver(context.Background(), input, true, false, 0, resolver)
	if err != nil {
		t.Fatalf("updateSkillRefsInContentWithResolver() error = %v", err)
	}
	if !changed {
		t.Fatal("updateSkillRefsInContentWithResolver() changed = false, want true")
	}
	if !strings.Contains(got, "skill: githubnext/skills/review/security@"+newSHA) {
		t.Fatalf("updated content missing updated object skill ref:\n%s", got)
	}
	if !strings.Contains(got, "github-token: ${{ secrets.SOME_TOKEN }}") {
		t.Fatalf("updated content dropped github-token object field:\n%s", got)
	}
}
func TestUpdateSkillRefsInContentWithResolver_NoFrontmatterNoChange(t *testing.T) {
	t.Parallel()
	input := "steps:\n  - run: echo hello\n"
	changed, got, err := updateSkillRefsInContentWithResolver(context.Background(), input, true, false, 0, resolveLatestRef)
	if err != nil {
		t.Fatalf("updateSkillRefsInContentWithResolver() error = %v", err)
	}
	if changed {
		t.Fatal("updateSkillRefsInContentWithResolver() changed = true, want false")
	}
	if got != input {
		t.Fatalf("content changed unexpectedly:\n got: %q\nwant: %q", got, input)
	}
}

func TestUpdatePluginRefsInContentWithResolver_UpdatesPluginRefs(t *testing.T) {
	t.Parallel()
	oldRepoPluginSHA := "1111111111111111111111111111111111111111"
	oldPathPluginSHA := "2222222222222222222222222222222222222222"
	newRepoPluginSHA := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	newPathPluginSHA := "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	input := `---
name: test
plugins:
  - githubnext/plugins@` + oldRepoPluginSHA + `
  - githubnext/plugins/review/security@` + oldPathPluginSHA + `
  - ${{ inputs.dynamic_plugin }}
---
body
`

	resolver := func(_ context.Context, repo, currentRef string, allowMajor, verbose bool, coolDown time.Duration) (string, error) {
		if repo != "githubnext/plugins" {
			t.Fatalf("resolver called with repo %q, want githubnext/plugins", repo)
		}
		switch currentRef {
		case oldRepoPluginSHA:
			return newRepoPluginSHA, nil
		case oldPathPluginSHA:
			return newPathPluginSHA, nil
		default:
			return currentRef, nil
		}
	}

	changed, got, err := updatePluginRefsInContentWithResolver(context.Background(), input, true, false, 0, resolver)
	if err != nil {
		t.Fatalf("updatePluginRefsInContentWithResolver() error = %v", err)
	}
	if !changed {
		t.Fatal("updatePluginRefsInContentWithResolver() changed = false, want true")
	}
	if !strings.Contains(got, "githubnext/plugins@"+newRepoPluginSHA) {
		t.Fatalf("updated content missing updated repo plugin ref:\n%s", got)
	}
	if !strings.Contains(got, "githubnext/plugins/review/security@"+newPathPluginSHA) {
		t.Fatalf("updated content missing updated path plugin ref:\n%s", got)
	}
	if !strings.Contains(got, "- ${{ inputs.dynamic_plugin }}") {
		t.Fatalf("updated content unexpectedly modified expression plugin ref:\n%s", got)
	}
}

func TestUpgradeTransformsPreserveRandomizedFrontmatterFormatting(t *testing.T) {
	t.Parallel()

	const (
		oldSHA = "1111111111111111111111111111111111111111"
		newSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	resolver := func(_ context.Context, repo, currentRef string, allowMajor, verbose bool, coolDown time.Duration) (string, error) {
		if currentRef != oldSHA {
			t.Fatalf("resolver called with ref %q, want %q", currentRef, oldSHA)
		}
		return newSHA, nil
	}
	rng := rand.New(rand.NewSource(42)) //nolint:gosec

	for sample := range 100 {
		indent := strings.Repeat(" ", 2+2*rng.Intn(2))
		fieldName := "skills"
		objectKey := "skill"
		ref := "githubnext/skills/review/security@" + oldSHA
		newRef := "githubnext/skills/review/security@" + newSHA
		refBlock := []string{
			"skills: # preserve list comment",
			indent + `- skill: "` + ref + `" # update only this value`,
			indent + "  github-token: ${{ secrets.SOME_TOKEN }}",
			indent + "# keep mentioned ref " + ref,
		}
		if sample%2 == 1 {
			fieldName = "plugins"
			objectKey = noObjectKey
			ref = "githubnext/plugins/review/security@" + oldSHA
			newRef = "githubnext/plugins/review/security@" + newSHA
			refBlock = []string{
				"plugins: # preserve list comment",
				indent + `- '` + ref + `' # update only this value`,
				indent + "# keep mentioned ref " + ref,
			}
		}

		blocks := [][]string{
			{"# sample comment " + strconv.Itoa(sample), `description: "quoted # value"`},
			{"timeout_minutes: 30 # deprecated spelling"},
			{"permissions:", indent + "contents: read # preserve permission comment"},
			refBlock,
			{"engine: copilot"},
		}
		order := rng.Perm(len(blocks))
		var frontmatter []string
		for i, blockIndex := range order {
			if i > 0 && rng.Intn(2) == 0 {
				frontmatter = append(frontmatter, "")
			}
			frontmatter = append(frontmatter, blocks[blockIndex]...)
		}

		input := "---\n" + strings.Join(frontmatter, "\n") + "\n---\n\n# Generated workflow\n\nBody."
		expected := strings.Replace(input, "timeout_minutes:", "timeout-minutes:", 1)
		expected = strings.Replace(expected, ref, newRef, 1)

		fixed, applied, err := getTimeoutMinutesCodemod().Apply(input, map[string]any{"timeout_minutes": 30})
		if err != nil {
			t.Fatalf("sample %d timeout codemod failed: %v", sample, err)
		}
		if !applied {
			t.Fatalf("sample %d timeout codemod was not applied", sample)
		}

		changed, got, err := updateFrontmatterRepoRefsInContentWithResolver(
			context.Background(), fixed, fieldName, objectKey, true, false, 0, resolver,
		)
		if err != nil {
			t.Fatalf("sample %d ref update failed: %v", sample, err)
		}
		if !changed {
			t.Fatalf("sample %d ref update was not applied", sample)
		}
		if got != expected {
			t.Fatalf("sample %d reformatted frontmatter\n--- got ---\n%s\n--- want ---\n%s", sample, got, expected)
		}
	}
}

func TestYAMLValueEndHandlesQuotedCommentMarkers(t *testing.T) {
	t.Parallel()

	for _, line := range []string{
		`key: 'it''s # still a value' # comment`,
		`key: "escaped \"# still a value" # comment`,
		`key: "escaped backslash \\" # comment`,
	} {
		comment := strings.LastIndex(line, "# comment")
		if got := yamlValueEnd(line); got != comment {
			t.Errorf("yamlValueEnd(%q) = %d, want %d", line, got, comment)
		}
	}
}

// TestUpdateActionRefsInContent_CooldownFallback verifies that
// updateActionRefsInContentWithDeps falls back to an older cooled-down release
// when the newest candidate is still within the cooldown window.
func TestUpdateActionRefsInContent_CooldownFallback(t *testing.T) {
	t.Parallel()
	deps := newTestActionUpdateDeps()

	// Latest version is v1.321.0 (in cooldown); fallback is v1.320.0.
	deps.getLatestRelease = func(_ context.Context, repo, _ string, _, _ bool) (string, string, error) {
		if repo == "ruby/setup-ruby" {
			return "v1.321.0", "sha321_12345678901234567890123456789012", nil
		}
		return "v1.0.0", "default_1234567890123456789012345678901234", nil
	}
	deps.runGHReleasesAPI = func(_ context.Context, _ string) ([]byte, error) {
		return []byte("v1.321.0\nv1.320.0\nv1.300.0"), nil
	}
	deps.checkCoolDown = func(_ context.Context, repo, tag string, cd time.Duration) coolDownCheckResult {
		switch tag {
		case "v1.321.0":
			return checkReleaseCoolDownWithDate(repo, tag, time.Now().Add(-1*24*time.Hour), cd)
		default:
			return coolDownCheckResult{}
		}
	}
	const fallbackSHA = "sha320_12345678901234567890123456789012"
	deps.getActionSHAForTag = func(_ context.Context, _, tag string) (string, error) {
		if tag == "v1.320.0" {
			return fallbackSHA, nil
		}
		return "sha321_12345678901234567890123456789012", nil
	}

	input := "steps:\n  - uses: ruby/setup-ruby@v1.300.0\n"

	changed, got, err := updateActionRefsInContentWithDeps(
		context.Background(), deps, input,
		make(map[string]latestReleaseResult),
		make(map[string]coolDownCheckResult),
		true, false, 7*24*time.Hour,
	)
	if err != nil {
		t.Fatalf("updateActionRefsInContentWithDeps() error = %v", err)
	}
	if !changed {
		t.Fatal("expected content to be updated, but changed = false")
	}
	if want := "steps:\n  - uses: ruby/setup-ruby@v1.320.0\n"; got != want {
		t.Errorf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestUpdateFrontmatterRefsHandlesFlowMapsAndQuotedKeys(t *testing.T) {
	t.Parallel()

	const (
		oldSHA = "1111111111111111111111111111111111111111"
		newSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	resolver := func(_ context.Context, _, currentRef string, _, _ bool, _ time.Duration) (string, error) {
		if currentRef != oldSHA {
			return currentRef, nil
		}
		return newSHA, nil
	}

	tests := []struct {
		name    string
		update  func(string) (bool, string, error)
		input   string
		want    string
		changed bool
	}{
		{
			name: "skills flow map entries",
			update: func(content string) (bool, string, error) {
				return updateSkillRefsInContentWithResolver(context.Background(), content, true, false, 0, resolver)
			},
			input: "---\n\"skills\": # keep\n  - {skill: githubnext/skills/a@" + oldSHA + ", version: 1}\n---\nbody\n",
			want:  "---\n\"skills\": # keep\n  - {skill: githubnext/skills/a@" + newSHA + ", version: 1}\n---\nbody\n",
		},
		{
			name: "skills inline flow list of maps",
			update: func(content string) (bool, string, error) {
				return updateSkillRefsInContentWithResolver(context.Background(), content, true, false, 0, resolver)
			},
			input: "---\nskills: [{skill: githubnext/skills/a@" + oldSHA + "}]\n---\nbody\n",
			want:  "---\nskills: [{skill: githubnext/skills/a@" + newSHA + "}]\n---\nbody\n",
		},
		{
			name: "plugins quoted key with block list",
			update: func(content string) (bool, string, error) {
				return updatePluginRefsInContentWithResolver(context.Background(), content, true, false, 0, resolver)
			},
			input: "---\n'plugins':\n  - 'githubnext/plugins/a@" + oldSHA + "' # pin\n---\nbody\n",
			want:  "---\n'plugins':\n  - 'githubnext/plugins/a@" + newSHA + "' # pin\n---\nbody\n",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			changed, got, err := tt.update(tt.input)
			if err != nil {
				t.Fatalf("update error = %v", err)
			}
			if !changed {
				t.Fatalf("update changed = false, want true")
			}
			if got != tt.want {
				t.Fatalf("update output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, tt.want)
			}
		})
	}
}

func TestUpdatePluginRefsInContentLeavesFlowMapEntriesUntouched(t *testing.T) {
	t.Parallel()

	const (
		oldSHA = "1111111111111111111111111111111111111111"
		newSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	input := "---\nplugins:\n  - githubnext/plugins/a@" + oldSHA + "\n  - {plugin: githubnext/plugins/b@" + oldSHA + "}\n---\nbody\n"
	want := "---\nplugins:\n  - githubnext/plugins/a@" + newSHA + "\n  - {plugin: githubnext/plugins/b@" + oldSHA + "}\n---\nbody\n"

	resolver := func(_ context.Context, _, currentRef string, _, _ bool, _ time.Duration) (string, error) {
		if currentRef != oldSHA {
			return currentRef, nil
		}
		return newSHA, nil
	}
	changed, got, err := updatePluginRefsInContentWithResolver(context.Background(), input, true, false, 0, resolver)
	if err != nil {
		t.Fatalf("updatePluginRefsInContentWithResolver() error = %v", err)
	}
	if !changed {
		t.Fatal("updatePluginRefsInContentWithResolver() changed = false, want true")
	}
	if got != want {
		t.Fatalf("plugin update output mismatch\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}

func TestParseFrontmatterKey(t *testing.T) {
	t.Parallel()

	tests := []struct {
		line string
		want string
		ok   bool
	}{
		{line: "skills:", want: "skills", ok: true},
		{line: `"skills": []`, want: "skills", ok: true},
		{line: `'plugins' :`, want: "plugins", ok: true},
		{line: `"skills:extra":`, want: "skills:extra", ok: true},
		{line: `""`, want: "", ok: false},
		{line: `"unterminated`, want: "", ok: false},
		{line: "no colon here", want: "", ok: false},
		{line: "", want: "", ok: false},
	}
	for _, tt := range tests {
		got, ok := parseFrontmatterKey(tt.line)
		if ok != tt.ok || got != tt.want {
			t.Fatalf("parseFrontmatterKey(%q) = (%q, %v), want (%q, %v)", tt.line, got, ok, tt.want, tt.ok)
		}
	}
}

func TestUpdateSkillRefsInContentOnlyRewritesFrontmatterBlock(t *testing.T) {
	t.Parallel()

	const (
		oldSHA = "1111111111111111111111111111111111111111"
		newSHA = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	)
	block := "skills:\n  - githubnext/skills/a@" + oldSHA + "\n"
	input := "---\n" + block + "---\n\nExample frontmatter:\n\n```yaml\n" + block + "```\n"
	want := "---\nskills:\n  - githubnext/skills/a@" + newSHA + "\n---\n\nExample frontmatter:\n\n```yaml\n" + block + "```\n"

	resolver := func(_ context.Context, _, currentRef string, _, _ bool, _ time.Duration) (string, error) {
		if currentRef != oldSHA {
			return currentRef, nil
		}
		return newSHA, nil
	}
	changed, got, err := updateSkillRefsInContentWithResolver(context.Background(), input, true, false, 0, resolver)
	if err != nil {
		t.Fatalf("updateSkillRefsInContentWithResolver() error = %v", err)
	}
	if !changed {
		t.Fatal("updateSkillRefsInContentWithResolver() changed = false, want true")
	}
	if got != want {
		t.Fatalf("markdown body was rewritten instead of frontmatter\n--- got ---\n%s\n--- want ---\n%s", got, want)
	}
}
