package workflow

import (
	"errors"
	"fmt"
	"path/filepath"

	"github.com/github/gh-aw/pkg/logger"
	"github.com/github/gh-aw/pkg/parser"
)

var authorizationCompilerLog = logger.New("workflow:authorization_compiler")

// NewAuthorizationVerificationCompiler compiles candidate data without emitting
// files, fetching dependencies, or consulting candidate-owned resolution caches.
// The caller must provide an isolated snapshot and deny network/process execution
// outside the trusted compiler. Only local imports and embedded action pins resolve.
func NewAuthorizationVerificationCompiler(root string) *Compiler {
	authorizationCompilerLog.Printf("Creating authorization verification compiler: root=%s", root)
	c := NewCompiler(WithNoEmit(true))
	c.gitRoot = root
	c.verifyAuthorization = true
	c.engineCatalog.offline = true
	c.SetReadOnly(true)
	c.SetQuiet(true)
	c.SetApprove(true)
	c.SetRefreshStopTime(true)
	c.importCache = parser.NewImportCache(root)
	c.importCache.SetLocalOnlyRoot(root)
	c.actionCache = NewActionCache(root)
	c.actionResolver = NewActionResolver(c.actionCache)
	c.actionResolver.offline = true
	return c
}

// CompileAuthorizationSnapshot uses the ordinary parse/compile pipeline and
// captures the complete generated lock without writing it.
func (c *Compiler) CompileAuthorizationSnapshot(path string) (*WorkflowData, string, error) {
	authorizationCompilerLog.Printf("Compiling authorization snapshot: path=%s", path)
	if !c.verifyAuthorization || !c.noEmit || !c.readOnly {
		authorizationCompilerLog.Printf("Rejecting snapshot compilation: verify_authorization=%t, no_emit=%t, read_only=%t", c.verifyAuthorization, c.noEmit, c.readOnly)
		return nil, "", errors.New("authorization snapshot compilation requires the read-only verification compiler")
	}
	relative, err := filepath.Rel(c.gitRoot, path)
	if err != nil || !filepath.IsLocal(relative) {
		authorizationCompilerLog.Printf("Rejecting snapshot source outside candidate root: path=%s, root=%s", path, c.gitRoot)
		return nil, "", errors.New("authorization source must be contained in the candidate snapshot")
	}
	c.SetWorkflowIdentifier(filepath.ToSlash(relative))
	c.importCache.SetLocalOnlyRoot(c.gitRoot)
	data, err := c.ParseWorkflowFile(path)
	if err != nil {
		return nil, "", err
	}
	if err := validateAuthorizationActionMetadata(data); err != nil {
		return nil, "", err
	}
	stopAfter, err := c.extractStopAfterFromOn(data.RawFrontmatter, data)
	if err != nil {
		return nil, "", err
	}
	if isRelativeStopTime(stopAfter) {
		return nil, "", errors.New("protected verification requires an absolute or expression stop-after value")
	}
	var generated string
	c.verificationYAML = &generated
	defer func() { c.verificationYAML = nil }()
	err = c.CompileWorkflowData(data, path)
	if err != nil {
		return nil, "", err
	}
	if len(data.ActionResolutionFailures) > 0 {
		authorizationCompilerLog.Printf("Rejecting snapshot: unpinned action references=%d", len(data.ActionResolutionFailures))
		return nil, "", errors.New("protected verification requires all action references to be pinned")
	}
	for _, spec := range data.Skills {
		parsed := parseSkillRefSpec(spec)
		if parsed.isRemote && !parsed.isFullSHA {
			return nil, "", fmt.Errorf("protected verification requires a full commit SHA for remote skill %q", spec)
		}
	}
	data.ResolvedDependencyFiles = c.importCache.ResolvedLocalFiles()
	authorizationCompilerLog.Printf("Authorization snapshot compiled: path=%s, lock_bytes=%d, dependency_files=%d", path, len(generated), len(data.ResolvedDependencyFiles))
	return data, generated, nil
}

// Reject missing metadata before job building can consult caches or fetch action.yml.
func validateAuthorizationActionMetadata(data *WorkflowData) error {
	if data.SafeOutputs == nil {
		return nil
	}
	for name, config := range data.SafeOutputs.Actions {
		ref, err := parseActionUsesField(config.Uses)
		if err != nil {
			return fmt.Errorf("protected verification requires a valid safe-output action reference: %w", err)
		}
		if !ref.IsLocal && config.Inputs == nil {
			authorizationCompilerLog.Printf("Rejecting remote safe-output action without inline inputs: name=%s", name)
			return fmt.Errorf("protected verification requires inline inputs for remote safe-output action %q; remote metadata fetching is disabled", name)
		}
	}
	return nil
}
