package parser

// SetLocalOnlyRoot rejects remote dependencies and local imports outside root.
// Builtin virtual documents remain available from the trusted compiler.
func (c *ImportCache) SetLocalOnlyRoot(root string) {
	c.localOnlyRoot = root
	c.SetReadOnly(true)
}
