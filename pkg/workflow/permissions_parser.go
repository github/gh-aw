package workflow

import (
	"strings"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/goccy/go-yaml"
)

var permissionsParserLog = logger.New("workflow:permissions_parser")

// PermissionsParser provides functionality to parse and analyze GitHub Actions permissions
type PermissionsParser struct {
	rawPermissions string
	parsedPerms    map[string]string
	isShorthand    bool
	shorthandValue string
	hasAll         bool
	allLevel       string
}

// NewPermissionsParser creates a new PermissionsParser instance
func NewPermissionsParser(permissionsYAML string) *PermissionsParser {
	permissionsParserLog.Print("Creating new permissions parser")

	parser := &PermissionsParser{
		rawPermissions: permissionsYAML,
		parsedPerms:    make(map[string]string),
	}
	parser.parse()
	return parser
}

// parse parses the permissions YAML and populates the internal structures
func (p *PermissionsParser) parse() {
	if p.rawPermissions == "" {
		permissionsParserLog.Print("No permissions to parse")
		return
	}

	permissionsParserLog.Printf("Parsing permissions YAML: length=%d", len(p.rawPermissions))

	// Remove the "permissions:" prefix if present and get just the YAML content
	yamlContent := strings.TrimSpace(p.rawPermissions)
	if strings.HasPrefix(yamlContent, "permissions:") {
		// Extract everything after "permissions:"
		lines := strings.Split(yamlContent, "\n")
		if len(lines) > 1 {
			// Get the lines after the first, and normalize indentation
			contentLines := lines[1:]
			var normalizedLines []string

			// Find the common indentation to remove
			minIndent := -1
			for _, line := range contentLines {
				if strings.TrimSpace(line) == "" {
					continue // Skip empty lines
				}
				indent := 0
				for _, r := range line {
					if r == ' ' || r == '\t' {
						indent++
					} else {
						break
					}
				}
				if minIndent == -1 || indent < minIndent {
					minIndent = indent
				}
			}

			// Remove common indentation from all lines
			if minIndent > 0 {
				for _, line := range contentLines {
					if strings.TrimSpace(line) == "" {
						normalizedLines = append(normalizedLines, "")
					} else if len(line) > minIndent {
						normalizedLines = append(normalizedLines, line[minIndent:])
					} else {
						normalizedLines = append(normalizedLines, line)
					}
				}
			} else {
				normalizedLines = contentLines
			}

			yamlContent = strings.Join(normalizedLines, "\n")
		} else {
			// Single line format like "permissions: read-all"
			parts := strings.SplitN(lines[0], ":", 2)
			if len(parts) == 2 {
				yamlContent = strings.TrimSpace(parts[1])
			}
		}
	}

	yamlContent = strings.TrimSpace(yamlContent)
	if yamlContent == "" {
		return
	}

	// Check if it's a shorthand permission (read-all, write-all, none)
	// Note: "read" and "write" are no longer valid shorthands as they create invalid GitHub Actions YAML
	shorthandPerms := []string{"read-all", "write-all", "none"}
	for _, shorthand := range shorthandPerms {
		if yamlContent == shorthand {
			p.isShorthand = true
			p.shorthandValue = shorthand
			return
		}
	}

	// Try to parse as YAML map
	var perms map[string]any
	if err := yaml.Unmarshal([]byte(yamlContent), &perms); err == nil {
		permissionsParserLog.Printf("Successfully parsed permissions map with %d keys", len(perms))

		// Handle 'all' key specially
		if allValue, exists := perms["all"]; exists {
			if strValue, ok := allValue.(string); ok {
				permissionsParserLog.Printf("Found 'all' permission with value: %s", strValue)
				if strValue == "write" {
					permissionsParserLog.Print("Invalid 'all: write' not allowed, ignoring permissions")
					// all: write is not allowed - don't set any permissions
					return
				}
				if strValue == "read" {
					// Check that no other permissions are set to 'none' when all: read is used
					for key, value := range perms {
						if key != "all" {
							if permValue, ok := value.(string); ok && permValue == "none" {
								permissionsParserLog.Printf("Invalid combination: all: read with %s: none", key)
								// all: read cannot be combined with : none - don't set any permissions
								return
							}
						}
					}
					p.hasAll = true
					p.allLevel = strValue
					permissionsParserLog.Print("Set hasAll=true with level=read")
				}
			}
		}
		// Convert any values to strings
		for key, value := range perms {
			if strValue, ok := value.(string); ok {
				p.parsedPerms[key] = strValue
			}
		}
		permissionsParserLog.Printf("Parsed %d permission entries", len(p.parsedPerms))
	} else {
		permissionsParserLog.Printf("Failed to parse permissions as YAML: %v", err)
	}
}

// HasContentsReadAccess returns true if the permissions allow reading contents
func (p *PermissionsParser) HasContentsReadAccess() bool {
	permissionsParserLog.Print("Checking contents read access")

	// Handle shorthand permissions
	if p.isShorthand {
		switch p.shorthandValue {
		case "read-all", "write-all":
			permissionsParserLog.Printf("Shorthand permissions grant contents read: %s", p.shorthandValue)
			return true
		case "none":
			permissionsParserLog.Print("Shorthand 'none' denies contents read")
			return false
		}
		return false
	}

	// Handle all: read case
	if p.hasAll && p.allLevel == "read" {
		// all: read grants contents access unless explicitly overridden
		if contentsLevel, exists := p.parsedPerms["contents"]; exists {
			return contentsLevel == "read" || contentsLevel == "write"
		}
		return true
	}

	// Handle explicit permissions map
	if contentsLevel, exists := p.parsedPerms["contents"]; exists {
		return contentsLevel == "read" || contentsLevel == "write"
	}

	// Default: if no contents permission is specified, assume no access
	return false
}

// IsAllowed checks if a specific permission scope has the specified access level
// scope: "contents", "issues", "pull-requests", etc.
// level: "read", "write", "none"
func (p *PermissionsParser) IsAllowed(scope, level string) bool {
	permissionsParserLog.Printf("Checking if scope=%s has level=%s", scope, level)

	// Handle shorthand permissions
	if p.isShorthand {
		permissionsParserLog.Printf("Using shorthand permission: %s", p.shorthandValue)
		switch p.shorthandValue {
		case "read-all":
			return level == "read"
		case "write-all":
			return level == "read" || level == "write"
		case "none":
			return false
		default:
			return false
		}
	}

	// Handle all: read case
	if p.hasAll && p.allLevel == "read" {
		// Check if there's an explicit permission for this scope
		if permLevel, exists := p.parsedPerms[scope]; exists {
			if level == "read" {
				// Read access is allowed if permission is "read" or "write"
				return permLevel == "read" || permLevel == "write"
			}
			return permLevel == level
		}
		// No explicit permission, use the "all" default
		// Special case: id-token doesn't support read level
		if scope == "id-token" && level == "read" {
			return false
		}
		return level == "read"
	}

	// Handle explicit permissions map
	if permLevel, exists := p.parsedPerms[scope]; exists {
		if level == "read" {
			// Read access is allowed if permission is "read" or "write"
			return permLevel == "read" || permLevel == "write"
		}
		return permLevel == level
	}

	// Default: permission not specified means no access
	return false
}

// NewPermissionsParserFromValue creates a PermissionsParser from a frontmatter value (any type)
func NewPermissionsParserFromValue(permissionsValue any) *PermissionsParser {
	parser := &PermissionsParser{
		parsedPerms: make(map[string]string),
	}

	if permissionsValue == nil {
		return parser
	}

	// Handle string shorthand (read-all, write-all, etc.)
	if strValue, ok := permissionsValue.(string); ok {
		parser.isShorthand = true
		parser.shorthandValue = strValue
		return parser
	}

	// Handle map format
	if mapValue, ok := permissionsValue.(map[string]any); ok {
		// Handle 'all' key specially
		if allValue, exists := mapValue["all"]; exists {
			if strValue, ok := allValue.(string); ok {
				if strValue == "write" {
					// all: write is not allowed, return empty parser
					return parser
				}
				if strValue == "read" {
					// Check that no other permissions are set to 'none' when all: read is used
					for key, value := range mapValue {
						if key != "all" {
							if permValue, ok := value.(string); ok && permValue == "none" {
								// all: read cannot be combined with : none, return empty parser
								return parser
							}
						}
					}
					parser.hasAll = true
					parser.allLevel = strValue
				}
			}
		}

		for key, value := range mapValue {
			if strValue, ok := value.(string); ok {
				parser.parsedPerms[key] = strValue
			}
		}
	}

	return parser
}

// ToPermissions converts a PermissionsParser to a Permissions object
func (p *PermissionsParser) ToPermissions() *Permissions {
	if p == nil {
		return NewPermissions()
	}

	// Handle shorthand permissions
	if p.isShorthand {
		switch p.shorthandValue {
		case "read-all":
			return NewPermissionsReadAll()
		case "write-all":
			return NewPermissionsWriteAll()
		case "none":
			return NewPermissionsNone()
		default:
			return NewPermissions()
		}
	}

	// Handle all: read case
	if p.hasAll && p.allLevel == "read" {
		perms := NewPermissionsAllRead()

		// Apply explicit overrides from parsedPerms
		for key, value := range p.parsedPerms {
			if key == "all" {
				continue // Skip the "all" key itself
			}
			scope := convertStringToPermissionScope(key)
			if scope != "" {
				perms.Set(scope, PermissionLevel(value))
			}
		}

		return perms
	}

	// Handle explicit permissions map
	permsMap := make(map[PermissionScope]PermissionLevel)
	for key, value := range p.parsedPerms {
		if key == "all" {
			continue // Skip the "all" key
		}
		scope := convertStringToPermissionScope(key)
		if scope != "" {
			permsMap[scope] = PermissionLevel(value)
		}
	}

	return NewPermissionsFromMap(permsMap)
}
