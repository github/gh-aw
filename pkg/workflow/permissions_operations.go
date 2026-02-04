package workflow

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/logger"
)

var permissionsOpsLog = logger.New("workflow:permissions_operations")

// Set sets a permission for a specific scope
func (p *Permissions) Set(scope PermissionScope, level PermissionLevel) {
	permissionsOpsLog.Printf("Setting permission: scope=%s, level=%s", scope, level)
	if p.shorthand != "" {
		// Convert from shorthand to map
		permissionsOpsLog.Printf("Converting from shorthand %s to explicit map", p.shorthand)
		p.shorthand = ""
		if p.permissions == nil {
			p.permissions = make(map[PermissionScope]PermissionLevel)
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
				p.permissions[s] = p.allLevel
			}
		}
		p.hasAll = false
		p.allLevel = ""
	}
	p.permissions[scope] = level
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
			if p.permissions == nil {
				p.permissions = make(map[PermissionScope]PermissionLevel)
			}
			for _, scope := range GetAllPermissionScopes() {
				if _, exists := p.permissions[scope]; !exists {
					// Skip id-token when level is read since it doesn't support read
					if scope == PermissionIdToken && p.allLevel == PermissionRead {
						continue
					}
					p.permissions[scope] = p.allLevel
				}
			}
			p.hasAll = false
			p.allLevel = ""
		}
		if other.hasAll {
			if other.permissions == nil {
				// Create a temporary map for merging
				tempPerms := make(map[PermissionScope]PermissionLevel)
				for _, scope := range GetAllPermissionScopes() {
					// Skip id-token when level is read since it doesn't support read
					if scope == PermissionIdToken && other.allLevel == PermissionRead {
						continue
					}
					tempPerms[scope] = other.allLevel
				}
				// Merge the temporary map
				p.mergePermissionMaps(tempPerms)
				// Also merge explicit permissions from other if any
				p.mergePermissionMaps(other.permissions)
				return
			}
		}
	}

	// If other has shorthand, we need to handle it specially
	if other.shorthand != "" {
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

// RenderToYAML renders the Permissions to GitHub Actions YAML format
func (p *Permissions) RenderToYAML() string {
	if p == nil {
		return ""
	}

	if p.shorthand != "" {
		return fmt.Sprintf("permissions: %s", p.shorthand)
	}

	// Collect all permissions to render
	allPerms := make(map[PermissionScope]PermissionLevel)

	if p.hasAll {
		// Expand all: read/write to individual permissions
		for _, scope := range GetAllPermissionScopes() {
			// Skip id-token when expanding all: read since id-token doesn't support read level
			if scope == PermissionIdToken && p.allLevel == PermissionRead {
				continue
			}
			allPerms[scope] = p.allLevel
		}
	}

	// Override with explicit permissions
	for scope, level := range p.permissions {
		allPerms[scope] = level
	}

	if len(allPerms) == 0 {
		// If explicitEmpty is true, render "permissions: {}"
		if p.explicitEmpty {
			return "permissions: {}"
		}
		return ""
	}

	// Sort scopes for consistent output
	var scopes []string
	for scope := range allPerms {
		scopes = append(scopes, string(scope))
	}
	sort.Strings(scopes)

	var lines []string
	lines = append(lines, "permissions:")
	for _, scopeStr := range scopes {
		scope := PermissionScope(scopeStr)
		level := allPerms[scope]

		// Skip organization-projects - it's only valid for GitHub App tokens, not workflow permissions
		if scope == PermissionOrganizationProj {
			continue
		}

		// Skip metadata - it's a built-in permission that is always available with read access
		if scope == PermissionMetadata {
			continue
		}

		// Add 2 spaces for proper indentation under permissions:
		// When rendered in a job, the job renderer adds 4 spaces to the first line only,
		// so we need to pre-indent continuation lines with 4 additional spaces
		// to get 6 total spaces (4 from job + 2 for being under permissions)
		lines = append(lines, fmt.Sprintf("      %s: %s", scope, level))
	}

	return strings.Join(lines, "\n")
}
