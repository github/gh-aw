package workflow

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var ghCLIPermissionsLog = logger.New("workflow:gh_cli_permissions")

//go:embed data/gh_cli_permissions.json
var ghCLIPermissionsJSON []byte

// ghCLISubcommandGroup maps a gh subcommand group (e.g. "pr", "issue") to its permissions.
type ghCLISubcommandGroup struct {
	Description         string   `json:"description"`
	ReadSubcommands     []string `json:"read_subcommands"`
	WriteSubcommands    []string `json:"write_subcommands"`
	ReadPermissions     []string `json:"read_permissions"`
	WritePermissions    []string `json:"write_permissions"`
	AppReadPermissions  []string `json:"app_read_permissions"`
	AppWritePermissions []string `json:"app_write_permissions"`
}

// ghCLIAPIPathPattern maps a REST API path pattern to the required permissions.
type ghCLIAPIPathPattern struct {
	Pattern        string   `json:"pattern"`
	Description    string   `json:"description"`
	Permissions    []string `json:"permissions"`
	AppPermissions []string `json:"app_permissions"`
}

// ghCLIPermissionsData is the top-level structure of gh_cli_permissions.json.
type ghCLIPermissionsData struct {
	Version          string                          `json:"version"`
	Description      string                          `json:"description"`
	SubcommandGroups map[string]ghCLISubcommandGroup `json:"subcommand_groups"`
	APIPathPatterns  []ghCLIAPIPathPattern           `json:"api_path_patterns"`
}

// compiledGHCLIPermissions holds pre-compiled lookup data built from the JSON.
type compiledGHCLIPermissions struct {
	// subcommandRE is dynamically compiled from the subcommand_groups keys so that adding
	// a new group to the JSON automatically extends the pattern without a code change.
	// Capture groups: (1) subcommand group, (2) action word.
	subcommandRE *regexp.Regexp
	// readCommands maps "group action" (e.g. "pr diff") to read permission scopes.
	readCommands map[string][]PermissionScope
	// writeCommands maps "group action" (e.g. "pr create") to write permission scopes.
	writeCommands map[string][]PermissionScope
	// groupReadPermissions maps a subcommand group name (e.g. "pr") to read permission scopes
	// used as fallback when the specific action is not recognised.
	groupReadPermissions map[string][]PermissionScope
	// appReadCommands maps "group action" to GitHub App read permission scopes.
	appReadCommands map[string][]PermissionScope
	// appWriteCommands maps "group action" to GitHub App write permission scopes.
	appWriteCommands map[string][]PermissionScope
	// groupAppReadPermissions maps a subcommand group name to GitHub App read permission scopes
	// used as fallback when the specific action is not recognised.
	groupAppReadPermissions map[string][]PermissionScope
	// apiPathPatterns holds compiled regexps paired with required permission scopes.
	apiPathPatterns []compiledAPIPathPattern
}

type compiledAPIPathPattern struct {
	re             *regexp.Regexp
	permissions    []PermissionScope
	appPermissions []PermissionScope
}

var getCompiledGHCLIPermissions = sync.OnceValues(compileGHCLIPermissions)

func compileGHCLIPermissions() (compiledGHCLIPermissions, error) {
	var data ghCLIPermissionsData
	if err := json.Unmarshal(ghCLIPermissionsJSON, &data); err != nil {
		return compiledGHCLIPermissions{}, fmt.Errorf("failed to load gh CLI permissions from JSON: %w", err)
	}

	cp := compiledGHCLIPermissions{
		readCommands:            make(map[string][]PermissionScope),
		writeCommands:           make(map[string][]PermissionScope),
		groupReadPermissions:    make(map[string][]PermissionScope),
		appReadCommands:         make(map[string][]PermissionScope),
		appWriteCommands:        make(map[string][]PermissionScope),
		groupAppReadPermissions: make(map[string][]PermissionScope),
	}

	var err error
	cp.subcommandRE, err = compileGHCLISubcommandPattern(data.SubcommandGroups)
	if err != nil {
		return compiledGHCLIPermissions{}, err
	}
	compileGHCLISubcommandPermissions(data.SubcommandGroups, &cp)
	if err := compileGHCLIAPIPathPatterns(data.APIPathPatterns, &cp); err != nil {
		return compiledGHCLIPermissions{}, err
	}

	ghCLIPermissionsLog.Printf("Loaded gh CLI permissions: version=%s, subcommand_groups=%d, api_path_patterns=%d", data.Version, len(data.SubcommandGroups), len(data.APIPathPatterns))
	return cp, nil
}

func compileGHCLISubcommandPattern(groupsByName map[string]ghCLISubcommandGroup) (*regexp.Regexp, error) {
	groups := make([]string, 0, len(groupsByName))
	for group := range groupsByName {
		groups = append(groups, regexp.QuoteMeta(group))
	}
	sort.Strings(groups)
	pattern := `(?m)(?:^|[\s|;])gh\s+(` + strings.Join(groups, "|") + `)\s+([\w][\w-]*)\b`
	//nolint:regexpdynamicpattern // Embedded group names are quoted and compilation errors are returned.
	re, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("invalid gh subcommand pattern %q: %w", pattern, err)
	}
	return re, nil
}

func compileGHCLISubcommandPermissions(groups map[string]ghCLISubcommandGroup, cp *compiledGHCLIPermissions) {
	for group, sg := range groups {
		readPerms := convertPermissionScopes(sg.ReadPermissions)
		writePerms := convertPermissionScopes(sg.WritePermissions)
		appReadPerms := convertPermissionScopes(sg.AppReadPermissions)
		appWritePerms := convertPermissionScopes(sg.AppWritePermissions)
		// Store group-level fallback (used when specific action is unknown).
		cp.groupReadPermissions[group] = readPerms
		cp.groupAppReadPermissions[group] = appReadPerms

		for _, action := range sg.ReadSubcommands {
			key := group + " " + action
			cp.readCommands[key] = readPerms
			cp.appReadCommands[key] = appReadPerms
		}
		for _, action := range sg.WriteSubcommands {
			key := group + " " + action
			cp.writeCommands[key] = writePerms
			cp.appWriteCommands[key] = appWritePerms
		}
	}
}

func convertPermissionScopes(values []string) []PermissionScope {
	scopes := make([]PermissionScope, 0, len(values))
	for _, value := range values {
		scopes = append(scopes, PermissionScope(value))
	}
	return scopes
}

func compileGHCLIAPIPathPatterns(patterns []ghCLIAPIPathPattern, cp *compiledGHCLIPermissions) error {
	for _, ap := range patterns {
		//nolint:regexpdynamicpattern // Embedded permission patterns are validated and compilation errors are returned.
		re, err := regexp.Compile(ap.Pattern)
		if err != nil {
			return fmt.Errorf("invalid gh API path pattern %q in gh_cli_permissions.json: %w", ap.Pattern, err)
		}
		cp.apiPathPatterns = append(cp.apiPathPatterns, compiledAPIPathPattern{
			re:             re,
			permissions:    convertPermissionScopes(ap.Permissions),
			appPermissions: convertPermissionScopes(ap.AppPermissions),
		})
	}
	return nil
}

// ghAPICmdRE matches `gh api` at a command boundary, capturing the rest of the line.
var ghAPICmdRE = regexp.MustCompile(`(?m)(?:^|[\s|;])gh\s+api\s+(.+)`)

// ghAPIValueFlags is the set of `gh api` flags that consume the next token as their value.
// These are skipped so that the API endpoint path can be located even when flags precede it.
var ghAPIValueFlags = map[string]bool{
	"-X": true, "--method": true,
	"-H": true, "--header": true,
	"-f": true, "--field": true,
	"-F": true, "--raw-field": true,
	"-q": true, "--jq": true,
	"-t": true, "--template": true,
	"--cache": true, "--paginate-limit": true,
}

// extractGHAPIEndpoints extracts API endpoint paths from `gh api` invocations in a shell script.
// It handles common invocation patterns such as:
//
//	gh api /repos/owner/repo/pulls
//	gh api -H 'Accept: application/vnd.github+json' /repos/owner/repo/pulls
//	gh api --method GET /repos/owner/repo/pulls
//	gh api "/repos/owner/repo/pulls"
//
// Flags and their arguments are skipped until the first non-flag token is found.
func extractGHAPIEndpoints(script string) []string {
	var endpoints []string
	for _, m := range ghAPICmdRE.FindAllStringSubmatch(script, -1) {
		if len(m) < 2 {
			continue
		}
		if request := parseGHAPIRequest(m[1]); request.endpoint != "" { //nolint:uncheckedsliceindex // The preceding match-length check bounds capture 1.
			endpoints = append(endpoints, request.endpoint)
		}
	}
	return endpoints
}

type parsedGHAPIRequest struct {
	endpoint string
	readOnly bool
}

// parseGHAPIEndpoint returns the API endpoint path from the argument string that follows
// `gh api`, skipping flags and their values. Surrounding quotes are stripped.
func parseGHAPIEndpoint(args string) string {
	return parseGHAPIRequest(args).endpoint
}

func parseGHAPIRequest(args string) parsedGHAPIRequest {
	tokens := splitShellTokens(strings.TrimSpace(args))
	request := parsedGHAPIRequest{readOnly: true}
	i := 0
	for i < len(tokens) {
		tok := strings.Trim(tokens[i], `"'`) //nolint:uncheckedsliceindex // The loop condition bounds i.
		if strings.HasPrefix(tok, "-") {
			switch {
			case tok == "-X" || tok == "--method":
				i++
				if i >= len(tokens) {
					request.readOnly = false
					continue
				}
				if !strings.EqualFold(strings.Trim(tokens[i], `"'`), "GET") { //nolint:uncheckedsliceindex // The preceding i >= len check bounds i.
					request.readOnly = false
				}
			case strings.HasPrefix(tok, "--method="):
				if !strings.EqualFold(strings.Trim(strings.TrimPrefix(tok, "--method="), `"'`), "GET") {
					request.readOnly = false
				}
			case strings.HasPrefix(tok, "-X") && len(tok) > 2:
				if !strings.EqualFold(strings.Trim(strings.TrimPrefix(tok, "-X"), `"'`), "GET") {
					request.readOnly = false
				}
			case tokenIsGHAPIBodyFlag(tok):
				request.readOnly = false
				if !strings.Contains(tok, "=") && (ghAPIValueFlags[tok] || strings.HasPrefix(tok, "-f") || strings.HasPrefix(tok, "-F")) && i+1 < len(tokens) {
					i++
				}
			case strings.Contains(tok, "="):
				// Flags with embedded values consume only one token.
			case ghAPIValueFlags[tok]:
				i++
			default:
				// Other flags are left for gh to interpret; fail closed if one looks
				// like a value-bearing operation we do not understand.
				request.readOnly = false
			}
			i++
			continue
		}
		if request.endpoint == "" {
			request.endpoint = strings.Trim(tok, `"'`)
		}
		i++
	}
	return request
}

func tokenIsGHAPIBodyFlag(token string) bool {
	return token == "--input" || token == "-f" || token == "-F" ||
		token == "--field" || token == "--raw-field" ||
		strings.HasPrefix(token, "--input=") ||
		strings.HasPrefix(token, "--field=") ||
		strings.HasPrefix(token, "--raw-field=") ||
		(strings.HasPrefix(token, "-f") && token != "-f") ||
		(strings.HasPrefix(token, "-F") && token != "-F")
}

// splitShellTokens splits a shell argument string by whitespace while respecting
// single and double quoted regions. Quotes are preserved in the returned tokens so
// that the caller can strip them as needed.
//
// Backslash escape sequences (e.g. \" inside a double-quoted string) are treated as
// opaque two-character sequences and passed through unchanged; the caller is
// responsible for any further unescaping. This is sufficient for the practical
// `gh api` invocation patterns handled here.
func splitShellTokens(s string) []string {
	var tokens []string
	var cur strings.Builder
	inSingle := false
	inDouble := false
	for i := 0; i < len(s); i++ {
		c := s[i] //nolint:uncheckedsliceindex // The loop condition bounds i.
		switch {
		case c == '\\' && !inSingle && i+1 < len(s):
			// Escape sequence outside single quotes: consume the backslash and the
			// next character as a single unit so that \" does not close a quoted string.
			cur.WriteByte(c)
			i++
			cur.WriteByte(s[i]) //nolint:uncheckedsliceindex // The preceding i+1 < len(s) check bounds i after increment.
		case c == '\'' && !inDouble:
			inSingle = !inSingle
			cur.WriteByte(c)
		case c == '"' && !inSingle:
			inDouble = !inDouble
			cur.WriteByte(c)
		case (c == ' ' || c == '\t') && !inSingle && !inDouble:
			if cur.Len() > 0 {
				tokens = append(tokens, cur.String())
				cur.Reset()
			}
		default:
			cur.WriteByte(c)
		}
	}
	if cur.Len() > 0 {
		tokens = append(tokens, cur.String())
	}
	return tokens
}

// inferPermissionsFromShellScripts scans one or more shell script strings for
// gh CLI invocations and returns the minimum set of GitHub Actions and GitHub App
// permissions required to run those commands.
//
// The returned map includes both GitHub Actions (GITHUB_TOKEN) scopes and GitHub
// App-only scopes. Callers should use IsGitHubAppOnlyScope to distinguish them:
// App-only scopes are skipped when rendering GITHUB_TOKEN permissions but are passed
// to the GitHub App token minting step when a GitHub App is configured.
//
// Only read-level permissions are inferred here; write-level operations are
// intentionally not auto-escalated. Use detectWriteCommandsInShellScripts to
// surface write commands as validation errors.
func inferPermissionsFromShellScripts(scripts []string) (map[PermissionScope]PermissionLevel, error) {
	ghCLIPermissionsLog.Printf("Inferring permissions from %d shell script(s)", len(scripts))
	perms := make(map[PermissionScope]PermissionLevel)
	ghCLIPermissions, err := getCompiledGHCLIPermissions()
	if err != nil {
		return nil, fmt.Errorf("load gh CLI permissions: %w", err)
	}

	addScopes := func(scopes []PermissionScope) {
		for _, scope := range scopes {
			if _, exists := perms[scope]; !exists {
				perms[scope] = PermissionRead
			}
		}
	}

	for _, script := range scripts {
		inferSubcommandPermissions(script, ghCLIPermissions, addScopes)
		inferAPIPermissions(script, ghCLIPermissions, addScopes)
	}

	ghCLIPermissionsLog.Printf("Inferred %d permission scope(s) from shell scripts", len(perms))
	return perms, nil
}

func inferSubcommandPermissions(script string, permissions compiledGHCLIPermissions, addScopes func([]PermissionScope)) {
	for _, match := range permissions.subcommandRE.FindAllStringSubmatch(script, -1) {
		group := strings.ToLower(match[1])  //nolint:uncheckedsliceindex // The subcommand regex always returns two captures.
		action := strings.ToLower(match[2]) //nolint:uncheckedsliceindex // The subcommand regex always returns two captures.
		key := group + " " + action
		matched := false
		if scopes, ok := permissions.readCommands[key]; ok {
			addScopes(scopes)
			matched = true
		}
		if scopes, ok := permissions.appReadCommands[key]; ok {
			addScopes(scopes)
			matched = true
		}
		if matched {
			continue
		}
		// Write commands only need read-level permissions in the activation job context.
		// (Full write escalation is rejected by detectWriteCommandsInShellScripts instead.)
		if scopes, ok := permissions.writeCommands[key]; ok {
			addScopes(scopes)
			matched = true
		}
		if scopes, ok := permissions.appWriteCommands[key]; ok {
			addScopes(scopes)
			matched = true
		}
		if !matched {
			addScopes(permissions.groupReadPermissions[group])
			addScopes(permissions.groupAppReadPermissions[group])
		}
	}
}

func inferAPIPermissions(script string, permissions compiledGHCLIPermissions, addScopes func([]PermissionScope)) {
	for _, path := range extractGHAPIEndpoints(script) {
		for _, pattern := range permissions.apiPathPatterns {
			if pattern.re.MatchString(path) {
				addScopes(pattern.permissions)
				addScopes(pattern.appPermissions)
			}
		}
	}
}

// InferGitHubAppTokenPermissionsFromShellScripts returns permission-* inputs
// for scripts that contain only GitHub CLI read operations whose scopes are
// explicitly known. It declines to infer permissions for write, unknown, or
// unrecognized API operations so callers can require manual review.
func InferGitHubAppTokenPermissionsFromShellScripts(scripts []string) (map[string]string, bool) {
	if len(scripts) == 0 {
		return nil, false
	}
	ghCLIPermissions, err := getCompiledGHCLIPermissions()
	if err != nil {
		return nil, false
	}
	inferred := make(map[PermissionScope]PermissionLevel)
	matched := false
	for _, originalScript := range scripts {
		script := removeCommentOnlyLines(originalScript)
		scopes, scriptMatched, ok := inferExplicitReadOnlyGHScript(script, ghCLIPermissions)
		if !ok {
			return nil, false
		}
		for scope := range scopes {
			inferred[scope] = PermissionRead
		}
		matched = matched || scriptMatched
	}

	if !matched || len(inferred) == 0 {
		return nil, false
	}
	fields := convertPermissionsToAppTokenFields(NewPermissionsFromMap(inferred))
	for scope := range inferred {
		if scope == PermissionScope("secrets") || scope == PermissionScope("variables") {
			fields["permission-"+string(scope)] = "read"
		}
	}
	if len(fields) == 0 {
		return nil, false
	}
	return fields, true
}

func removeCommentOnlyLines(script string) string {
	var lines []string
	for line := range strings.SplitSeq(script, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func inferExplicitReadOnlyGHScript(script string, permissions compiledGHCLIPermissions) (map[PermissionScope]struct{}, bool, bool) {
	scopes := make(map[PermissionScope]struct{})
	matched := false
	for line := range strings.SplitSeq(script, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if strings.ContainsAny(line, "|;&<>`#$()\\") {
			return nil, false, false
		}
		fields := strings.Fields(line)
		if len(fields) < 2 || !strings.EqualFold(fields[0], "gh") { //nolint:uncheckedsliceindex // Short-circuiting checks fields has at least two elements.
			return nil, false, false
		}
		if strings.EqualFold(fields[1], "api") { //nolint:uncheckedsliceindex // The preceding guard requires two fields.
			commands := ghAPICmdRE.FindAllStringSubmatch(line, -1)
			if len(commands) != 1 || len(commands[0]) < 2 { //nolint:uncheckedsliceindex // Short-circuiting checks commands has one nonempty match.
				return nil, false, false
			}
			request := parseGHAPIRequest(commands[0][1]) //nolint:uncheckedsliceindex // The preceding guard checks both match indexes.
			apiScopes, known := explicitReadOnlyGHAPIEndpoint(request.endpoint, permissions)
			if !request.readOnly || !known {
				return nil, false, false
			}
			for _, scope := range apiScopes {
				scopes[scope] = struct{}{}
			}
			matched = true
			continue
		}

		commands := permissions.subcommandRE.FindAllStringSubmatch(line, -1)
		if len(commands) != 1 || len(commands[0]) < 3 || //nolint:uncheckedsliceindex // Short-circuiting checks the match indexes below.
			!strings.EqualFold(commands[0][1], fields[1]) || //nolint:uncheckedsliceindex // The preceding conditions and fields guard bound these indexes.
			len(fields) < 3 || !strings.EqualFold(commands[0][2], fields[2]) { //nolint:uncheckedsliceindex // The preceding conditions and fields guard bound these indexes.
			return nil, false, false
		}
		key := strings.ToLower(fields[1]) + " " + strings.ToLower(fields[2]) //nolint:uncheckedsliceindex // The preceding command match requires three fields.
		commandScopes, known := permissions.readCommands[key]
		if !known {
			commandScopes, known = permissions.appReadCommands[key]
		}
		if !known || len(commandScopes) == 0 {
			return nil, false, false
		}
		for _, scope := range commandScopes {
			scopes[scope] = struct{}{}
		}
		matched = true
	}
	return scopes, matched, matched
}

func explicitReadOnlyGHAPIEndpoint(endpoint string, permissions compiledGHCLIPermissions) ([]PermissionScope, bool) {
	path := strings.SplitN(strings.Trim(strings.TrimPrefix(endpoint, "/"), "/"), "?", 2)[0] //nolint:uncheckedsliceindex // SplitN always returns at least one element.
	segments := strings.Split(path, "/")
	if len(segments) >= 4 && strings.EqualFold(segments[0], "repos") && strings.EqualFold(segments[3], "actions") {
		switch {
		case len(segments) == 5 && strings.EqualFold(segments[4], "secrets"): //nolint:uncheckedsliceindex // The length guard bounds segment 4.
			return []PermissionScope{PermissionScope("secrets")}, true
		case len(segments) >= 5 && strings.EqualFold(segments[4], "secrets"): //nolint:uncheckedsliceindex // The length guard bounds segment 4.
			return []PermissionScope{PermissionScope("secrets")}, true
		case len(segments) >= 5 && strings.EqualFold(segments[4], "variables"): //nolint:uncheckedsliceindex // The length guard bounds segment 4.
			return []PermissionScope{PermissionScope("variables")}, true
		case len(segments) == 4:
			return []PermissionScope{PermissionActions}, true
		case len(segments) >= 5 && isKnownActionsEndpoint(segments[4:]): //nolint:uncheckedsliceindex // The length guard bounds segment 4.
			return []PermissionScope{PermissionActions}, true
		default:
			return nil, false
		}
	}
	for _, apiPermission := range permissions.apiPathPatterns {
		if apiPermission.re.MatchString(path) {
			if len(apiPermission.appPermissions) > 0 {
				return apiPermission.appPermissions, true
			}
			if len(apiPermission.permissions) > 0 {
				return apiPermission.permissions, true
			}
			return nil, false
		}
	}
	return nil, false
}

func isKnownActionsEndpoint(path []string) bool {
	if len(path) == 0 {
		return false
	}
	route := strings.ToLower(path[0]) //nolint:uncheckedsliceindex // The preceding empty check bounds index 0.
	suffix := path[1:]
	switch route {
	case "artifacts":
		return len(suffix) == 0 || len(suffix) == 1 || len(suffix) == 2 && strings.EqualFold(suffix[1], "zip") //nolint:uncheckedsliceindex // The final condition requires two suffix segments.
	case "caches":
		return len(suffix) == 0
	case "workflows":
		return len(suffix) == 0 || len(suffix) == 1 ||
			len(suffix) == 2 && (strings.EqualFold(suffix[1], "timing") || strings.EqualFold(suffix[1], "dispatches")) //nolint:uncheckedsliceindex // The final condition requires two suffix segments.
	case "runs":
		if len(suffix) == 0 || len(suffix) == 1 {
			return true
		}
		if len(suffix) == 2 {
			switch strings.ToLower(suffix[1]) { //nolint:uncheckedsliceindex // The preceding length check requires two suffix segments.
			case "jobs", "attempts", "logs", "timing", "pending_deployments":
				return true
			}
		}
		if len(suffix) == 3 && (strings.EqualFold(suffix[1], "jobs") || strings.EqualFold(suffix[1], "attempts")) { //nolint:uncheckedsliceindex // The length check requires three suffix segments.
			return true
		}
		if len(suffix) == 4 && strings.EqualFold(suffix[1], "attempts") { //nolint:uncheckedsliceindex // The length check requires four suffix segments.
			return strings.EqualFold(suffix[3], "jobs") || strings.EqualFold(suffix[3], "logs") //nolint:uncheckedsliceindex // The preceding length check bounds suffix index 3.
		}
	default:
	}
	return false
}

// detectWriteCommandsInShellScripts returns all write gh CLI commands found in the
// given scripts, formatted as "gh <group> <action>" (e.g. "gh pr create").
// The slice contains no duplicates and is sorted deterministically in discovery order.
func detectWriteCommandsInShellScripts(scripts []string) ([]string, error) {
	ghCLIPermissionsLog.Printf("Scanning %d shell script(s) for write gh CLI commands", len(scripts))
	ghCLIPermissions, err := getCompiledGHCLIPermissions()
	if err != nil {
		return nil, fmt.Errorf("load gh CLI permissions: %w", err)
	}
	var found []string
	seen := make(map[string]struct{})

	for _, script := range scripts {
		for _, m := range ghCLIPermissions.subcommandRE.FindAllStringSubmatch(script, -1) {
			group := strings.ToLower(m[1])  //nolint:uncheckedsliceindex // The subcommand regex always returns two captures.
			action := strings.ToLower(m[2]) //nolint:uncheckedsliceindex // The subcommand regex always returns two captures.
			key := group + " " + action

			if _, isWrite := ghCLIPermissions.writeCommands[key]; isWrite {
				cmd := "gh " + key
				if _, already := seen[cmd]; !already {
					seen[cmd] = struct{}{}
					found = append(found, cmd)
				}
			}
		}
	}

	if len(found) > 0 {
		ghCLIPermissionsLog.Printf("Detected %d write gh CLI command(s) in shell scripts", len(found))
	}
	return found, nil
}

// extractRunScriptsFromSectionYAML parses a step-section YAML string (e.g. as stored in
// WorkflowData.PreSteps, PostSteps, PreAgentSteps, or CustomSteps) and returns the `run`
// script text from every step. sectionName must match the top-level key in the YAML
// (e.g. "pre-steps", "post-steps", "pre-agent-steps", "steps").
func extractRunScriptsFromSectionYAML(sectionYAML, sectionName string) []string {
	if sectionYAML == "" {
		return nil
	}
	var wrapper map[string][]map[string]any
	if err := yaml.Unmarshal([]byte(sectionYAML), &wrapper); err != nil {
		return nil
	}
	steps := wrapper[sectionName]
	if len(steps) == 0 {
		return nil
	}
	var scripts []string
	for _, step := range steps {
		if runVal, ok := step["run"].(string); ok && runVal != "" {
			scripts = append(scripts, runVal)
		}
	}
	return scripts
}

// extractRunScriptsFromJobSection returns the `run` script text from every step in the
// named section (e.g. "pre-steps", "steps", "post-steps") of the named job configuration
// inside the frontmatter jobs map.
//
// It is a read-only extraction: it never mutates the jobs map.
func extractRunScriptsFromJobSection(jobs map[string]any, jobName, sectionName string) []string {
	if len(jobs) == 0 {
		return nil
	}

	jobConfig, ok := jobs[jobName]
	if !ok {
		return nil
	}

	configMap, ok := jobConfig.(map[string]any)
	if !ok {
		return nil
	}

	raw, ok := configMap[sectionName]
	if !ok {
		return nil
	}

	stepsList, ok := raw.([]any)
	if !ok {
		return nil
	}

	var scripts []string
	for _, step := range stepsList {
		stepMap, ok := step.(map[string]any)
		if !ok {
			continue
		}
		if runVal, ok := stepMap["run"].(string); ok && runVal != "" {
			scripts = append(scripts, runVal)
		}
	}
	return scripts
}
