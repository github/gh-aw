package workflow

import (
	"errors"
	"path/filepath"

	"github.com/github/gh-aw/pkg/parser"
)

// NewAuthorizationVerificationCompiler compiles candidate data without emitting
// files, fetching dependencies, or consulting candidate-owned resolution caches.
// The caller must provide an isolated snapshot and deny network/process execution
// outside the trusted compiler. Only local imports and embedded action pins resolve.
func NewAuthorizationVerificationCompiler(root string) *Compiler {
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
	if !c.verifyAuthorization || !c.noEmit || !c.readOnly {
		return nil, "", errors.New("authorization snapshot compilation requires the read-only verification compiler")
	}
	relative, err := filepath.Rel(c.gitRoot, path)
	if err != nil || !filepath.IsLocal(relative) {
		return nil, "", errors.New("authorization source must be contained in the candidate snapshot")
	}
	c.SetWorkflowIdentifier(filepath.ToSlash(relative))
	data, err := c.ParseWorkflowFile(path)
	if err != nil {
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
	return data, generated, err
}
