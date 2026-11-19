package parser

import "slices"

// SetLocalOnlyRoot rejects remote dependencies and local imports outside root.
// Builtin virtual documents remain available from the trusted compiler.
func (c *ImportCache) SetLocalOnlyRoot(root string) {
	c.localOnlyRoot = root
	c.resolvedLocalFiles = make(map[string]struct{})
	c.SetReadOnly(true)
}

// ResolvedLocalFiles returns canonical filenames resolved in local-only mode,
// independently of the display paths and section selectors in manifests.
func (c *ImportCache) ResolvedLocalFiles() []string {
	files := make([]string, 0, len(c.resolvedLocalFiles))
	for file := range c.resolvedLocalFiles {
		files = append(files, file)
	}
	slices.Sort(files)
	return files
}
