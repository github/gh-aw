package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/github/gh-aw/pkg/parser"
	"github.com/github/gh-aw/pkg/workflow"
	"go.yaml.in/yaml/v3"
)

type updatePRFile struct {
	hash         [32]byte
	source       string
	sourceSHA    string
	compiler     string
	pins         map[string]updatePRPin
	metadataOnly bool
}

type updatePRPin struct {
	kind, repo, version, sha string
}

type updatePRCompilerTransition struct {
	before, after string
}

type updatePRSummary struct {
	before  map[string]updatePRFile
	dir     string
	applied []workflowFixInfo
}

var summaryReleaseVersion = regexp.MustCompile(`^v?[0-9]+\.[0-9]+\.[0-9]+(?:-[0-9A-Za-z.-]+)?(?:\+[0-9A-Za-z.-]+)?$`)
var summaryDescribeVersion = regexp.MustCompile(`-[0-9]+-g[0-9a-f]+(?:-dirty)?$`)
var summarySafeRef = regexp.MustCompile(`^[0-9A-Za-z._/@:+-]+$`)

func newUpdatePRSummary(ctx context.Context, dir string) (*updatePRSummary, error) {
	if dir == "" {
		dir = constants.GetWorkflowDir()
	}

	before, err := captureUpdatePRFiles(ctx, dir)
	if err != nil {
		return nil, err
	}
	return &updatePRSummary{before: before, dir: dir}, nil
}

func updatePRBody(ctx context.Context, summary *updatePRSummary, opts UpdateWorkflowsOptions) (string, error) {
	body, err := summary.body(ctx, "update")
	if err != nil {
		return "", err
	}
	return appendPRMessage(body, opts.PRMessage), nil
}

// Snapshot file hashes rather than diffs, so pre-existing local edits aren't
// attributed to this run. Only structured, allowlisted metadata enters the body.
func captureUpdatePRFiles(ctx context.Context, dir string) (map[string]updatePRFile, error) {
	rootBytes, err := exec.CommandContext(ctx, "git", "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return nil, fmt.Errorf("cannot locate repository for PR summary: %w", err)
	}
	root, err := filepath.EvalSymlinks(strings.TrimSpace(string(rootBytes)))
	if err != nil {
		return nil, err
	}
	workflowDir, err := summaryPhysicalPath(dir)
	if err != nil {
		return nil, err
	}
	args := []string{"-C", root, "ls-files", "-z", "--cached", "--others", "--exclude-standard", "--",
		".github", ".gitattributes", ".gitignore", "AGENTS.md", "SKILL.md"}
	skillDirs, agentDirs := summaryEngineDirs()
	args = append(args, skillDirs...)
	args = append(args, agentDirs...)
	args = append(args, summaryWorkflowPathspec(root, workflowDir)...)
	paths, err := exec.CommandContext(ctx, "git", args...).Output()
	if err != nil {
		return nil, fmt.Errorf("cannot list files for PR summary: %w", err)
	}
	files := make(map[string]updatePRFile)
	for path := range strings.SplitSeq(string(paths), "\x00") {
		if path == "" {
			continue
		}
		full := filepath.Join(root, filepath.FromSlash(path))
		info, err := os.Lstat(full)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("cannot inspect PR summary file %s: %w", path, err)
		}
		// Do not follow links outside the checkout or materialize sparse files.
		if !info.Mode().IsRegular() {
			continue
		}
		if slices.ContainsFunc(agentDirs, func(agentDir string) bool {
			return strings.HasPrefix(path, strings.TrimSuffix(agentDir, "/")+"/")
		}) {
			// Agent contents aren't needed to report that generated artifacts
			// changed. Metadata-only entries are explicitly labeled in the body.
			fingerprint := fmt.Sprintf("%d:%d:%d", info.Size(), info.ModTime().UnixNano(), info.Mode())
			files[path] = updatePRFile{hash: sha256.Sum256([]byte(fingerprint)), metadataOnly: true}
			continue
		}
		state, err := readUpdatePRFile(full, path, workflowDir)
		if err != nil {
			return nil, fmt.Errorf("cannot inspect PR summary metadata %s: %w", path, err)
		}
		files[path] = state
	}
	return files, nil
}

func summaryWorkflowPathspec(root, dir string) []string {
	relative, err := filepath.Rel(root, dir)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return nil
	}
	if relative == "." {
		return []string{":(glob)*.md", ":(glob)*.lock.yml"}
	}
	return []string{filepath.ToSlash(relative)}
}

func summaryPhysicalPath(path string) (string, error) {
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if !os.IsNotExist(err) {
		return resolved, err
	}
	// A default workflow directory may not have been created yet.
	parent, err := summaryPhysicalPath(filepath.Dir(absolute))
	if err != nil {
		return "", err
	}
	return filepath.Join(parent, filepath.Base(absolute)), nil
}

func summaryEngineDirs() (skills, agents []string) {
	agents = append(agents, constants.AgentsDir)
	for _, engine := range workflow.GetGlobalEngineRegistry().GetSupportedEngines() {
		skills = append(skills, workflow.GetEngineSkillDir(engine))
		agents = append(agents, workflow.GetEngineSubAgentDir(engine))
	}
	return skills, agents
}

func hashUpdatePRFile(full string) (hash [32]byte, err error) {
	file, err := os.Open(full)
	if err != nil {
		return hash, err
	}
	defer func() {
		if closeErr := file.Close(); closeErr != nil && err == nil {
			err = closeErr
		}
	}()
	hasher := sha256.New()
	if _, err = io.Copy(hasher, file); err != nil {
		return hash, err
	}
	copy(hash[:], hasher.Sum(nil))
	return hash, nil
}

func readUpdatePRFile(full, path, workflowDir string) (updatePRFile, error) {
	var state updatePRFile
	var err error
	state.hash, err = hashUpdatePRFile(full)
	if err != nil {
		return state, err
	}
	isWorkflow := strings.HasPrefix(full, workflowDir+string(filepath.Separator))
	isYAML := strings.HasSuffix(path, ".yml") || strings.HasSuffix(path, ".yaml")
	isWorkflowYAML := isYAML && (isWorkflow || strings.HasPrefix(path, constants.WorkflowsDirSlash))
	if filepath.Base(path) == "actions-lock.json" || (isWorkflow && strings.HasSuffix(path, ".md")) || isWorkflowYAML {
		content, err := os.ReadFile(full)
		if err != nil {
			return state, err
		}
		switch {
		case filepath.Base(path) == "actions-lock.json":
			state.pins = summaryLockPins(content)
		case strings.HasSuffix(path, ".md"):
			if parsed, err := parser.ExtractFrontmatterFromContent(string(content)); err == nil {
				state.pins = summaryYAMLPins([]byte(strings.Join(parsed.FrontmatterLines, "\n")))
				if source, ok := parsed.Frontmatter["source"].(string); ok {
					state.source = source
				}
				if sourceSHA, ok := parsed.Frontmatter["source-sha"].(string); ok {
					state.sourceSHA = sourceSHA
				}
				state.pins = summaryYAMLPins([]byte(strings.Join(parsed.FrontmatterLines, "\n")))
			}
		case strings.HasSuffix(path, ".lock.yml"):
			state.compiler = extractCompilerVersionFromLockContent(string(content))
			state.pins = summaryYAMLPins(content)
		case isWorkflowYAML:
			state.pins = summaryYAMLPins(content)
		}
	}
	return state, nil
}

// Parse YAML nodes so a uses: string inside a shell script cannot become a pin.
func summaryYAMLPins(content []byte) map[string]updatePRPin {
	var document yaml.Node
	pins := make(map[string]updatePRPin)
	if yaml.Unmarshal(content, &document) != nil {
		return pins
	}
	var visit func(*yaml.Node)
	visit = func(node *yaml.Node) {
		if node.Kind == yaml.MappingNode {
			for i, key := range node.Content {
				valueIndex := i + 1
				if i%2 == 0 && valueIndex >= 0 && valueIndex < len(node.Content) && key.Value == "uses" {
					value := node.Content[valueIndex]
					if pin, ok := summaryYAMLActionPin(value); ok {
						pins["action:"+value.Value] = pin
					}
				}
			}
		}
		for _, child := range node.Content {
			visit(child)
		}
	}
	visit(&document)
	return pins
}

func summaryYAMLActionPin(value *yaml.Node) (updatePRPin, bool) {
	at := strings.LastIndex(value.Value, "@")
	if value.Kind != yaml.ScalarNode || at <= 0 {
		return updatePRPin{}, false
	}
	repo, ref := value.Value[:at], value.Value[at+1:]
	if !summarySafeRef.MatchString(repo) || !summarySafeRef.MatchString(ref) || !strings.Contains(repo, "/") || strings.Contains(repo, ":") || strings.HasPrefix(repo, ".") {
		return updatePRPin{}, false
	}
	pin := updatePRPin{kind: "Action", repo: repo, version: ref}
	if gitutil.IsValidFullSHA(ref) {
		pin.version, pin.sha = "", ref
		for word := range strings.FieldsSeq(strings.TrimPrefix(value.LineComment, "#")) {
			if summaryReleaseVersion.MatchString(word) {
				pin.version = word
				break
			}
		}
	}
	return pin, true
}

func summaryLockPins(content []byte) map[string]updatePRPin {
	var lock struct {
		Entries map[string]struct {
			Repo    string `json:"repo"`
			Version string `json:"version"`
			SHA     string `json:"sha"`
		} `json:"entries"`
		Containers map[string]struct {
			Image  string `json:"image"`
			Digest string `json:"digest"`
		} `json:"containers"`
	}
	pins := make(map[string]updatePRPin)
	if json.Unmarshal(content, &lock) != nil {
		return pins
	}
	for key, entry := range lock.Entries {
		pins["action:"+key] = updatePRPin{"Action", entry.Repo, entry.Version, entry.SHA}
	}
	for key, entry := range lock.Containers {
		image := entry.Image
		if image == "" {
			image = key
		}
		repo, version := image, ""
		if colon := strings.LastIndex(image, ":"); colon > strings.LastIndex(image, "/") {
			repo, version = image[:colon], image[colon+1:]
		}
		pins["container:"+key] = updatePRPin{"Container", repo, version, entry.Digest}
	}
	return pins
}

func summaryCode(text string) string {
	text = strings.NewReplacer("\r\n", " ", "\r", " ", "\n", " ").Replace(text)
	if text == "" {
		return "—"
	}
	longest, run := 0, 0
	for _, char := range text {
		if char == '`' {
			run++
			longest = max(longest, run)
		} else {
			run = 0
		}
	}
	delimiter := strings.Repeat("`", longest+1)
	if strings.HasPrefix(text, "`") || strings.HasSuffix(text, "`") || (strings.HasPrefix(text, " ") && strings.HasSuffix(text, " ") && strings.TrimSpace(text) != "") {
		text = " " + text + " "
	}
	return delimiter + text + delimiter
}

func summaryPinValue(pin updatePRPin) string {
	if pin.repo == "" {
		return "—"
	}
	if !summarySafeRef.MatchString(pin.repo) || (pin.version != "" && !summarySafeRef.MatchString(pin.version)) || (pin.sha != "" && !summarySafeRef.MatchString(pin.sha)) {
		return "(non-literal reference)"
	}
	value := pin.version
	if pin.sha != "" {
		value += " @ " + pin.sha
	}
	return summaryCode(value)
}

func summaryReleaseLink(repo, version string) string {
	parts := strings.Split(repo, "/")
	if len(parts) < 2 || !summarySafeRef.MatchString(repo) || !summaryReleaseVersion.MatchString(version) || summaryDescribeVersion.MatchString(version) || strings.HasSuffix(version, "-dirty") {
		return ""
	}
	repo = strings.Join(parts[:2], "/")
	if repo == "github/gh-aw-actions" {
		repo = ghawReleaseRepo
	}
	return fmt.Sprintf("[%s %s](https://github.com/%s/releases/tag/%s)", repo, version, repo, url.PathEscape(version))
}

// Match unchanged keys first, then pair remaining versions of the same dependency.
// This preserves multiple pins for one repository, additions, and removals.
func summaryPinChanges(before, after map[string]updatePRPin) []string {
	var rows []string
	removed, added := make(map[string][]updatePRPin), make(map[string][]updatePRPin)
	row := func(old, next updatePRPin) {
		if text := summaryPinChangeRow(old, next); text != "" {
			rows = append(rows, text)
		}
	}
	for key, old := range before {
		if next, ok := after[key]; ok && old.kind == next.kind && old.repo == next.repo {
			if old != next {
				row(old, next)
			}
			continue
		}
		group := old.kind + ":" + old.repo
		removed[group] = append(removed[group], old)
	}
	for key, next := range after {
		if old, ok := before[key]; ok && old.kind == next.kind && old.repo == next.repo {
			continue
		}
		group := next.kind + ":" + next.repo
		added[group] = append(added[group], next)
	}
	groups := make(map[string]struct{})
	for group := range removed {
		groups[group] = struct{}{}
	}
	for group := range added {
		groups[group] = struct{}{}
	}
	compare := func(a, b updatePRPin) int { return strings.Compare(a.version+"@"+a.sha, b.version+"@"+b.sha) }
	for group := range groups {
		old, next := removed[group], added[group]
		slices.SortFunc(old, compare)
		slices.SortFunc(next, compare)
		for i := range max(len(old), len(next)) {
			var a, b updatePRPin
			if i >= 0 && i < len(old) {
				a = old[i]
			}
			if i >= 0 && i < len(next) {
				b = next[i]
			}
			row(a, b)
		}
	}
	slices.Sort(rows)
	return rows
}

func summaryPinChangeRow(old, next updatePRPin) string {
	pin := next
	if pin.repo == "" {
		pin = old
	}
	if !summarySafeRef.MatchString(pin.repo) {
		return ""
	}
	link := ""
	if pin.kind == "Action" && next.repo != "" {
		if release := summaryReleaseLink(next.repo, next.version); release != "" {
			link = " (" + release + ")"
		}
	} else if pin.kind == "Container" && (next.repo == "ghcr.io/github/gh-aw-firewall" || strings.HasPrefix(next.repo, "ghcr.io/github/gh-aw-firewall/")) {
		if release := summaryReleaseLink("github/gh-aw-firewall", normalizeDisplayVersion(next.version)); release != "" {
			link = " (" + release + ")"
		}
	}
	return fmt.Sprintf("- %s %s: %s → %s%s", pin.kind, summaryCode(pin.repo), summaryPinValue(old), summaryPinValue(next), link)
}

func (s *updatePRSummary) recordFix(file string, fixes []string) {
	s.applied = append(s.applied, workflowFixInfo{File: file, Fixes: slices.Clone(fixes)})
}

func (s *updatePRSummary) body(ctx context.Context, command string) (string, error) {
	after, err := captureUpdatePRFiles(ctx, s.dir)
	if err != nil {
		return "", err
	}
	return s.render(command, after), nil
}

func (s *updatePRSummary) changedFiles(after map[string]updatePRFile) (changed, sources, compilers, pins []string) {
	paths := make(map[string]struct{})
	transitions := make(map[updatePRCompilerTransition]int)
	pinFiles := make(map[string][]string)
	for path := range s.before {
		paths[path] = struct{}{}
	}
	for path := range after {
		paths[path] = struct{}{}
	}
	for path := range paths {
		old, hadOld := s.before[path]
		next, hasNext := after[path]
		if hadOld && hasNext && old.hash == next.hash {
			continue
		}
		status := "Modified"
		if !hadOld {
			status = "Added"
		} else if !hasNext {
			status = "Removed"
		}
		artifact := "- " + status + " " + summaryCode(path)
		if old.metadataOnly || next.metadataOnly {
			artifact += " (metadata only)"
		}
		changed = append(changed, artifact)
		if old.source != next.source {
			sources = append(sources, fmt.Sprintf("- %s: %s → %s", summaryCode(path), summarySource(old.source), summarySource(next.source)))
		}
		if old.compiler != next.compiler && (old.compiler != "" || next.compiler != "") {
			transitions[updatePRCompilerTransition{old.compiler, next.compiler}]++
		}
		for _, pin := range slices.Compact(summaryPinChanges(old.pins, next.pins)) {
			pinFiles[pin] = append(pinFiles[pin], path)
		}
	}
	for pin, files := range pinFiles {
		if len(files) == 1 {
			for _, file := range files {
				pins = append(pins, pin+" — "+summaryCode(file))
			}
		} else {
			pins = append(pins, fmt.Sprintf("%s — %d file(s)", pin, len(files)))
		}
	}
	for transition, count := range transitions {
		link := summaryReleaseLink(ghawReleaseRepo, normalizeDisplayVersion(transition.after))
		compilers = append(compilers, fmt.Sprintf("- %s → %s: %d workflow(s). %s", summaryCode(transition.before), summaryCode(transition.after), count, link))
	}
	return changed, sources, compilers, pins
}

func (s *updatePRSummary) render(command string, after map[string]updatePRFile) string {
	var body strings.Builder
	body.WriteString("## " + strings.ToUpper(command[:1]) + command[1:] + " summary\n\nChanges produced by this run of `gh aw " + command + "`.\n")
	body.WriteString("\n### Tool version\n\n" + summaryCode(GetVersion()))
	if release := summaryReleaseLink(ghawReleaseRepo, normalizeDisplayVersion(GetVersion())); release != "" {
		body.WriteString(" — " + release)
	}
	body.WriteString("\n\n### Changed workflows and artifacts\n\n")
	changed, sources, compilers, pins := s.changedFiles(after)
	writeSummaryChangedFiles(&body, changed)
	body.WriteString("\n### Source updates\n\n")
	writeSummaryList(&body, sources, "No source reference changes.")
	body.WriteString("\n### Action and container pin changes\n\n")
	writeSummaryList(&body, pins, "No action or container pin changes.")
	body.WriteString("\n### Compiler changes\n\n")
	writeSummaryList(&body, compilers, "No compiler version changes in generated workflows.")
	if command == "upgrade" {
		body.WriteString("\n### Applied codemods\n\n")
		writeSummaryList(&body, s.codemodSummary(), "No workflow codemods were applied.")
	}
	return body.String()
}

func (s *updatePRSummary) codemodSummary() []string {
	workflows := make(map[string]map[string]struct{})
	for _, applied := range s.applied {
		for _, name := range applied.Fixes {
			if workflows[name] == nil {
				workflows[name] = make(map[string]struct{})
			}
			workflows[name][applied.File] = struct{}{}
		}
	}
	var fixes []string
	for name, files := range workflows {
		fixes = append(fixes, fmt.Sprintf("- %s: %d workflow(s).", summaryCode(name), len(files)))
	}
	return fixes
}

func writeSummaryChangedFiles(body *strings.Builder, changed []string) {
	const displayLimit = 50
	slices.Sort(changed)
	if len(changed) <= displayLimit {
		writeSummaryList(body, changed, "No file changes detected.")
		return
	}
	writeSummaryList(body, changed[:displayLimit], "")
	body.WriteString("\n" + strconv.Itoa(len(changed)-displayLimit) + " additional changed files. See **Files changed** for the complete list.\n")
}

func summarySource(source string) string {
	if source == "" {
		return "—"
	}
	if !summarySafeRef.MatchString(source) || strings.Count(source, "@") > 1 {
		return "(non-literal source)"
	}
	if strings.Contains(source, "://") {
		parsed, err := url.Parse(source)
		if err != nil || parsed.User != nil {
			return "(non-literal source)"
		}
	}
	rendered := summaryCode(source)
	if at := strings.LastIndex(source, "@"); at > 0 {
		repo, version := source[:at], source[at+1:]
		// Full URLs carry their own host; never misattribute an enterprise
		// release to a repository with the same name on github.com.
		if trimmed, ok := strings.CutPrefix(repo, "https://github.com/"); ok {
			repo = trimmed
		} else if strings.Contains(repo, "://") {
			return rendered
		} else if trimmed, ok := strings.CutPrefix(repo, "github.com/"); ok {
			repo = trimmed
		} else {
			owner, _, _ := strings.Cut(repo, "/")
			if strings.Contains(owner, ".") || (owner != "github" && owner != "githubnext" && owner != "microsoft" && getHostFromOriginRemote() != "github.com") {
				return rendered
			}
		}
		if release := summaryReleaseLink(repo, version); release != "" {
			rendered += " (" + release + ")"
		}
	}
	return rendered
}

func writeSummaryList(body *strings.Builder, lines []string, empty string) {
	if len(lines) == 0 {
		body.WriteString(empty + "\n")
		return
	}
	slices.Sort(lines)
	body.WriteString(strings.Join(lines, "\n") + "\n")
}
