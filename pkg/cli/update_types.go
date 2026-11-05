package cli

// workflowWithSource represents a workflow with its source information
type workflowWithSource struct {
	Name       string
	Path       string
	SourceSpec string // e.g., "owner/repo/path@ref"
	SourceSHA  string // installed commit used as the three-way merge base
}

// updateFailure represents a failed workflow update
type updateFailure struct {
	Name  string
	Error string
}

// actionUpdateFailure represents a failed GitHub Action update check
type actionUpdateFailure struct {
	name string
	err  string
}
