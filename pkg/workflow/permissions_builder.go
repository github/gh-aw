package workflow

import "github.com/github/gh-aw/pkg/logger"

var permissionsBuilderLog = logger.New("workflow:permissions_builder")

// PermissionsBuilder provides a fluent API for building Permissions objects
// Example usage:
//
//	perms := NewPermissionsBuilder().
//	    WithContents(PermissionRead).
//	    WithIssues(PermissionWrite).
//	    WithPullRequests(PermissionWrite).
//	    Build()
type PermissionsBuilder struct {
	perms *Permissions
}

// NewPermissionsBuilder creates a new PermissionsBuilder with an empty permissions map
func NewPermissionsBuilder() *PermissionsBuilder {
	if permissionsBuilderLog.Enabled() {
		permissionsBuilderLog.Print("Creating new PermissionsBuilder")
	}
	return &PermissionsBuilder{
		perms: NewPermissions(),
	}
}

// WithActions sets the actions permission level
func (pb *PermissionsBuilder) WithActions(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionActions, level)
	return pb
}

// WithAttestations sets the attestations permission level
func (pb *PermissionsBuilder) WithAttestations(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionAttestations, level)
	return pb
}

// WithChecks sets the checks permission level
func (pb *PermissionsBuilder) WithChecks(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionChecks, level)
	return pb
}

// WithContents sets the contents permission level
func (pb *PermissionsBuilder) WithContents(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionContents, level)
	return pb
}

// WithDeployments sets the deployments permission level
func (pb *PermissionsBuilder) WithDeployments(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionDeployments, level)
	return pb
}

// WithDiscussions sets the discussions permission level
func (pb *PermissionsBuilder) WithDiscussions(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionDiscussions, level)
	return pb
}

// WithIdToken sets the id-token permission level
func (pb *PermissionsBuilder) WithIdToken(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionIdToken, level)
	return pb
}

// WithIssues sets the issues permission level
func (pb *PermissionsBuilder) WithIssues(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionIssues, level)
	return pb
}

// WithMetadata sets the metadata permission level
func (pb *PermissionsBuilder) WithMetadata(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionMetadata, level)
	return pb
}

// WithModels sets the models permission level
func (pb *PermissionsBuilder) WithModels(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionModels, level)
	return pb
}

// WithPackages sets the packages permission level
func (pb *PermissionsBuilder) WithPackages(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionPackages, level)
	return pb
}

// WithPages sets the pages permission level
func (pb *PermissionsBuilder) WithPages(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionPages, level)
	return pb
}

// WithPullRequests sets the pull-requests permission level
func (pb *PermissionsBuilder) WithPullRequests(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionPullRequests, level)
	return pb
}

// WithRepositoryProjects sets the repository-projects permission level
func (pb *PermissionsBuilder) WithRepositoryProjects(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionRepositoryProj, level)
	return pb
}

// WithOrganizationProjects sets the organization-projects permission level
// Note: organization-projects is only valid for GitHub App tokens, not workflow permissions
func (pb *PermissionsBuilder) WithOrganizationProjects(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionOrganizationProj, level)
	return pb
}

// WithSecurityEvents sets the security-events permission level
func (pb *PermissionsBuilder) WithSecurityEvents(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionSecurityEvents, level)
	return pb
}

// WithStatuses sets the statuses permission level
func (pb *PermissionsBuilder) WithStatuses(level PermissionLevel) *PermissionsBuilder {
	pb.perms.Set(PermissionStatuses, level)
	return pb
}

// Build returns the constructed Permissions object
func (pb *PermissionsBuilder) Build() *Permissions {
	if permissionsBuilderLog.Enabled() {
		permissionsBuilderLog.Printf("Building permissions: scope_count=%d", len(pb.perms.permissions))
	}
	return pb.perms
}
