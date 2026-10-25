package workflow

import (
	"fmt"
	"maps"
	"slices"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var permissionsOpsLog = logger.New("workflow:permissions_operations")

// SortPermissionScopes sorts a slice of PermissionScope in place using Go's standard library sort
func SortPermissionScopes(s []PermissionScope) {
	slices.SortFunc(s, func(a, b PermissionScope) int {
		switch {
		case string(a) < string(b):
			return -1
		case string(a) > string(b):
			return 1
		default:
			return 0
		}
	})
}

// HasContentsReadAccess returns true if the permissions allow reading the repository contents.
// This is equivalent to PermissionsParser.HasContentsReadAccess but operates directly on the
// parsed Permissions struct to avoid redundant YAML parsing when CachedPermissions is available.
func (p *Permissions) HasContentsReadAccess() bool {
	if p == nil {
		return false
	}

	if p.shorthand != "" {
		switch p.shorthand {
		case "read-all", "write-all":
			return true
		// "none" shorthand denies all access; any other unexpected value is also denied.
		default:
			return false
		}
	}
	// all: write implies write-level access on every scope, which includes read access.
	if p.hasAll && (p.allLevel == PermissionRead || p.allLevel == PermissionWrite) {
		if contentsLevel, exists := p.permissions[PermissionContents]; exists {
			return contentsLevel == PermissionRead || contentsLevel == PermissionWrite
		}
		return true
	}
	if contentsLevel, exists := p.permissions[PermissionContents]; exists {
		return contentsLevel == PermissionRead || contentsLevel == PermissionWrite
	}
	return false
}

// HasCopilotRequestsWrite returns true if the permissions grant copilot-requests: write.
func (p *Permissions) HasCopilotRequestsWrite() bool {
	if p == nil {
		return false
	}

	level, ok := p.Get(PermissionCopilotRequests)
	return ok && level == PermissionWrite
}

// HasAnyWriteScope returns true if the permissions grant write access to any
// GITHUB_TOKEN scope. This is useful for jobs that only need to avoid an
// otherwise read-only token, regardless of which specific writable scope
// provides that property.
func (p *Permissions) HasAnyWriteScope() bool {
	if p == nil {
		return false
	}

	if p.shorthand == "write-all" {
		return true
	}

	if p.hasAll && p.allLevel == PermissionWrite {
		return true
	}

	for _, level := range p.permissions {
		if level == PermissionWrite {
			return true
		}
	}

	return false
}

// hasCopilotRequestsWritePermission returns true when workflow permissions include
// copilot-requests: write. This controls whether engines should use ${{ github.token }}
// for Copilot authentication instead of requiring COPILOT_GITHUB_TOKEN.
func hasCopilotRequestsWritePermission(workflowData *WorkflowData) bool {
	if workflowData == nil {
		return false
	}
	perms := workflowData.CachedPermissions
	if perms == nil {
		perms = NewPermissionsParser(workflowData.Permissions).ToPermissions()
	}
	if perms == nil {
		return false
	}
	return perms.HasCopilotRequestsWrite()
}

// HasCopilotRequestsWriteFromFrontmatter returns true when the frontmatter permissions map
// includes copilot-requests: write. It is the frontmatter-map counterpart of the unexported
// hasCopilotRequestsWritePermission in this file (which operates on *WorkflowData) and exists
// so that callers outside this package (e.g. pkg/cli) can perform the same check without
// duplicating the frontmatter-to-Permissions conversion logic.
func HasCopilotRequestsWriteFromFrontmatter(frontmatter map[string]any) bool {
	if frontmatter == nil {
		return false
	}
	permissionsValue, ok := frontmatter["permissions"]
	if !ok {
		return false
	}
	return NewPermissionsParserFromValue(permissionsValue).ToPermissions().HasCopilotRequestsWrite()
}

// filterJobLevelPermissions takes a raw permissions YAML string (as stored in WorkflowData.Permissions)
// and returns a version suitable for use in a GitHub Actions job-level permissions block.
//
// GitHub App-only permission scopes (e.g., members, administration) are not
// valid GitHub Actions workflow permissions and cause a parse error when GitHub Actions tries to
// queue the workflow. Those scopes must only appear as permission-* inputs when minting GitHub App
// installation access tokens via actions/create-github-app-token, not in the job-level block.
//
// RenderToYAML already skips App-only scopes; this function converts the raw YAML string through
// the Permissions struct so that filtering is applied before job-level rendering.
// The returned string uses 2-space indentation so that the caller's subsequent
// indentYAMLLines("    ") call adds 4 spaces, producing the correct 6-space job-level
// indentation in the final YAML (matching the renderJob format).
//
// If cachedPerms is provided and non-nil, the YAML parsing step is skipped and cachedPerms is used
// directly, avoiding the overhead of re-parsing the YAML string on every call.
//
// If the input YAML is malformed or contains only App-only scopes, an empty string is returned
// so the caller omits the permissions block entirely rather than emitting invalid YAML.
func filterJobLevelPermissions(rawPermissionsYAML string, cachedPerms ...*Permissions) string {
	if rawPermissionsYAML == "" {
		return ""
	}

	var filtered *Permissions
	if len(cachedPerms) > 0 && cachedPerms[0] != nil {
		filtered = cachedPerms[0]
	} else {
		filtered = NewPermissionsParser(rawPermissionsYAML).ToPermissions()
	}
	rendered := filtered.RenderToYAML()
	if rendered == "" {
		// If the raw permissions YAML was an explicit empty block (permissions: {}), preserve
		// it at the job level. Without this check, "permissions: {}" would be silently dropped,
		// leaving the job without any permissions block and causing it to inherit the workflow-
		// level permissions instead of having its own explicit empty block.
		if strings.TrimSpace(rawPermissionsYAML) == "permissions: {}" {
			return "permissions: {}"
		}
		return ""
	}

	// RenderToYAML hard-codes 6-space indentation for permission values so that shorthand
	// callers that embed the output directly into a job block get the right alignment:
	//   permissions:        ← first line, 4 spaces added by renderJob's fmt.Fprintf
	//         contents: read  ← 6 spaces from RenderToYAML → total 10 would be wrong
	// Here we normalise back to 2-space indentation. The caller will then run
	// indentYAMLLines("    "), adding 4 spaces to lines 1+, yielding 6 spaces total.
	const renderYAMLIndent = 6 // spaces used by RenderToYAML for permission value lines
	const targetIndent = 2     // spaces we want here so indentYAMLLines("    ") gives 6
	prefix := strings.Repeat(" ", renderYAMLIndent)
	replacement := strings.Repeat(" ", targetIndent)
	lines := strings.Split(rendered, "\n")
	for i := 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], prefix) {
			lines[i] = replacement + lines[i][renderYAMLIndent:]
		}
	}
	return strings.Join(lines, "\n")
}

// Set sets a permission for a specific scope
func (p *Permissions) Set(scope PermissionScope, level PermissionLevel) {
	permissionsOpsLog.Printf("Setting permission: scope=%s, level=%s", scope, level)
	if p.shorthand != "" {
		// Convert from shorthand to explicit map, preserving all shorthand-implied permissions.
		// This mirrors the hasAll expansion below so that callers adding a single scope to a
		// shorthand (e.g. adding copilot-requests: write to read-all) do not lose the remaining
		// shorthand-implied permissions.
		shorthand := p.shorthand
		permissionsOpsLog.Printf("Converting from shorthand %s to explicit map", shorthand)
		p.shorthand = ""
		if p.permissions == nil {
			p.permissions = make(map[PermissionScope]PermissionLevel)
		}
		var shorthandLevel PermissionLevel
		switch shorthand {
		case "read-all":
			shorthandLevel = PermissionRead
		case "write-all":
			shorthandLevel = PermissionWrite
		case "none":
			shorthandLevel = PermissionNone
		}
		for _, s := range GetAllPermissionScopes() {
			if _, exists := p.permissions[s]; !exists {
				// id-token does not support the read level
				if s == PermissionIdToken && shorthandLevel == PermissionRead {
					continue
				}
				p.permissions[s] = shorthandLevel
			}
		}
	}
	if p.hasAll {
		// Convert from all to explicit map
		permissionsOpsLog.Printf("Converting from all:%s to explicit map", p.allLevel)
		if p.permissions == nil {
			p.permissions = make(map[PermissionScope]PermissionLevel)
		}
		// Expand all permissions to explicit permissions first
		for _, s := range GetAllPermissionScopes() {
			if _, exists := p.permissions[s]; !exists {
				// id-token does not support the read level
				if s == PermissionIdToken && p.allLevel == PermissionRead {
					continue
				}
				p.permissions[s] = p.allLevel
			}
		}
		p.hasAll = false
		p.allLevel = ""
	}
	p.permissions[scope] = level
}

// GetExplicit returns the permission level only if the scope was explicitly declared in the
// permissions map. Unlike Get, it never returns a level derived from shorthand (read-all /
// write-all) or "all: read" defaults. Use this when you need to know what the user explicitly
// specified — for example, when deciding which GitHub App-only scopes to forward to
// actions/create-github-app-token, or when validating that App-only scopes are present.
func (p *Permissions) GetExplicit(scope PermissionScope) (PermissionLevel, bool) {
	if p == nil {
		return "", false
	}
	level, exists := p.permissions[scope]
	return level, exists
}

// Get gets the permission level for a specific scope
func (p *Permissions) Get(scope PermissionScope) (PermissionLevel, bool) {
	if p.shorthand != "" {
		// Shorthand permissions apply to all scopes
		switch p.shorthand {
		case "read-all":
			return PermissionRead, true
		case "write-all":
			return PermissionWrite, true
		case "none":
			return PermissionNone, true
		}
		return "", false
	}

	// Check explicit permission first
	if level, exists := p.permissions[scope]; exists {
		return level, true
	}

	// If we have all: read, return that as default for any scope not explicitly set
	if p.hasAll {
		// Special case: id-token doesn't support read level
		if scope == PermissionIdToken && p.allLevel == PermissionRead {
			return "", false
		}
		return p.allLevel, true
	}

	return "", false
}

// mergePermissionMaps merges a map of permissions into the current permissions
// Write permission takes precedence over read
func (p *Permissions) mergePermissionMaps(otherPerms map[PermissionScope]PermissionLevel) {
	permissionsOpsLog.Printf("Merging %d permission entries into permissions map", len(otherPerms))
	for scope, otherLevel := range otherPerms {
		currentLevel, exists := p.permissions[scope]
		if !exists {
			p.permissions[scope] = otherLevel
		} else {
			// Write takes precedence
			if otherLevel == PermissionWrite || currentLevel == PermissionWrite {
				p.permissions[scope] = PermissionWrite
			} else if otherLevel == PermissionRead || currentLevel == PermissionRead {
				p.permissions[scope] = PermissionRead
			} else {
				p.permissions[scope] = PermissionNone
			}
		}
	}
}

// Merge merges another Permissions into this one
// Write permission takes precedence over read (write implies read)
// Individual scope permissions override shorthand
func (p *Permissions) Merge(other *Permissions) {
	if other == nil {
		return
	}

	if permissionsOpsLog.Enabled() {
		permissionsOpsLog.Printf("Merging permissions: current_perms_count=%d, other_perms_count=%d", len(p.permissions), len(other.permissions))
	}

	// Handle all permissions - convert to explicit first if needed
	if p.hasAll || other.hasAll {
		// Convert both to explicit maps
		if p.hasAll {
			p.expandAllForMerge()
		}
		if other.hasAll {
			if other.permissions == nil {
				p.mergeAllPermissions(other)
				return
			}
		}
	}

	// If other has shorthand, we need to handle it specially
	if other.shorthand != "" {
		p.mergeShorthandPermissions(other)
		return
	}

	// Both have maps, merge them
	if p.shorthand != "" {
		// We have shorthand, other has map - convert to map first
		p.shorthand = ""
		if p.permissions == nil {
			p.permissions = make(map[PermissionScope]PermissionLevel)
		}
	}

	// Merge permissions - write overrides read
	p.mergePermissionMaps(other.permissions)
}

func (p *Permissions) expandAllForMerge() {
	if p.permissions == nil {
		p.permissions = make(map[PermissionScope]PermissionLevel)
	}
	for _, scope := range GetAllPermissionScopes() {
		if _, exists := p.permissions[scope]; !exists {
			// id-token does not support read.
			if scope == PermissionIdToken && p.allLevel == PermissionRead {
				continue
			}
			p.permissions[scope] = p.allLevel
		}
	}
	p.hasAll = false
	p.allLevel = ""
}

func (p *Permissions) mergeAllPermissions(other *Permissions) {
	tempPerms := make(map[PermissionScope]PermissionLevel)
	for _, scope := range GetAllPermissionScopes() {
		if scope == PermissionIdToken && other.allLevel == PermissionRead {
			continue
		}
		tempPerms[scope] = other.allLevel
	}
	p.mergePermissionMaps(tempPerms)
	p.mergePermissionMaps(other.permissions)
}

func (p *Permissions) mergeShorthandPermissions(other *Permissions) {
	// If we also have shorthand, resolve the conflict
	if p.shorthand != "" {
		// Promote to the higher permission level
		if other.shorthand == "write-all" || p.shorthand == "write-all" {
			p.shorthand = "write-all"
		} else if other.shorthand == "read-all" || p.shorthand == "read-all" {
			p.shorthand = "read-all"
		}
		// none is lowest, so only keep if both are none
		return
	}
	// We have map, other has shorthand - expand our map
	// Apply other's shorthand as baseline, then our specific permissions override
	otherLevel := PermissionNone
	switch other.shorthand {
	case "read-all":
		otherLevel = PermissionRead
	case "write-all":
		otherLevel = PermissionWrite
	}

	// For all scopes we don't have, set to other's shorthand level
	allScopes := GetAllPermissionScopes()
	for _, scope := range allScopes {
		if _, exists := p.permissions[scope]; !exists && otherLevel != PermissionNone {
			// Skip id-token when level is read since it doesn't support read
			if scope == PermissionIdToken && otherLevel == PermissionRead {
				continue
			}
			p.permissions[scope] = otherLevel
		}
	}
	return
}

// RenderToYAML renders the Permissions to GitHub Actions YAML format
func (p *Permissions) RenderToYAML() string {
	if p == nil {
		return ""
	}
	if permissionsOpsLog.Enabled() {
		permissionsOpsLog.Printf("Rendering permissions to YAML: shorthand=%s, hasAll=%t, perms_count=%d", p.shorthand, p.hasAll, len(p.permissions))
	}

	if p.shorthand != "" {
		return "permissions: " + p.shorthand
	}

	// Collect all permissions to render
	allPerms := p.permissionsForRendering()

	if len(allPerms) == 0 {
		// If explicitEmpty is true, render "permissions: {}"
		if p.explicitEmpty {
			return "permissions: {}"
		}
		return ""
	}

	return p.renderPermissionMap(allPerms)
}

func (p *Permissions) permissionsForRendering() map[PermissionScope]PermissionLevel {
	allPerms := make(map[PermissionScope]PermissionLevel)

	if p.hasAll {
		// Expand all: read/write to individual permissions
		for _, scope := range GetAllPermissionScopes() {
			// Skip id-token when expanding all: read since id-token doesn't support read level
			if scope == PermissionIdToken && p.allLevel == PermissionRead {
				continue
			}
			// Skip discussions when expanding all: read unless explicitly set
			// This prevents issues in GitHub Enterprise where discussions might not be available
			// Discussions permission should be added explicitly or via safe-outputs that need it
			if scope == PermissionDiscussions && p.allLevel == PermissionRead {
				// Only include if explicitly set in permissions map
				if _, explicitlySet := p.permissions[PermissionDiscussions]; !explicitlySet {
					continue
				}
			}
			allPerms[scope] = p.allLevel
		}
	}

	// Override with explicit permissions
	maps.Copy(allPerms, p.permissions)

	return allPerms
}

func (p *Permissions) renderPermissionMap(allPerms map[PermissionScope]PermissionLevel) string {
	// Sort scopes for consistent output
	var scopes []string
	for scope := range allPerms {
		scopes = append(scopes, string(scope))
	}
	sort.Strings(scopes)

	var lines []string
	lines = append(lines, "permissions:")
	hasRenderable := false
	for _, scopeStr := range scopes {
		scope := PermissionScope(scopeStr)
		level := allPerms[scope]

		// Skip GitHub App-only permissions - they are not valid GitHub Actions workflow permissions
		// and cannot be set on the GITHUB_TOKEN. They are handled separately when minting
		// GitHub App installation access tokens.
		if IsGitHubAppOnlyScope(scope) {
			continue
		}

		// Skip metadata - it's a built-in permission that is always available with read access
		if scope == PermissionMetadata {
			continue
		}

		hasRenderable = true
		// Add 2 spaces for proper indentation under permissions:
		// When rendered in a job, the job renderer adds 4 spaces to the first line only,
		// so we need to pre-indent continuation lines with 4 additional spaces
		// to get 6 total spaces (4 from job + 2 for being under permissions)
		lines = append(lines, fmt.Sprintf("      %s: %s", scope, level))
	}

	// If everything was skipped (all App-only or metadata), return as if empty
	if !hasRenderable {
		if p.explicitEmpty {
			return "permissions: {}"
		}
		return ""
	}

	return strings.Join(lines, "\n")
}

// mergeInferredIntoPermissionsYAML merges a map of inferred permissions into an existing
// permissions YAML string and returns the updated YAML string (2-space indented, suitable
// for filterJobLevelPermissions / indentYAMLLines callers).
//
// Rules:
//   - GitHub App-only scopes are skipped (they are not valid job-level permissions).
//   - An inferred scope is added only when not already declared by the user.
//   - An inferred scope at PermissionNone is always ignored.
//
// If permissionsYAML is empty the function returns an empty string unchanged, because
// adding a new explicit block to a job that currently inherits workflow-level permissions
// would unintentionally restrict those permissions.
func mergeInferredIntoPermissionsYAML(permissionsYAML string, inferred map[PermissionScope]PermissionLevel) string {
	if permissionsYAML == "" {
		// No existing permissions block: adding one would unintentionally narrow the
		// workflow-level permissions that the job currently inherits.
		return permissionsYAML
	}
	if len(inferred) == 0 {
		return permissionsYAML
	}

	parsedPerms := NewPermissionsParser(permissionsYAML).ToPermissions()

	changed := false
	for scope, level := range inferred {
		if IsGitHubAppOnlyScope(scope) {
			continue
		}
		if level == PermissionNone {
			continue
		}
		if _, exists := parsedPerms.Get(scope); !exists {
			parsedPerms.Set(scope, level)
			changed = true
		}
	}

	if !changed {
		return permissionsYAML
	}

	return filterJobLevelPermissions(parsedPerms.RenderToYAML())
}
