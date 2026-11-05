//go:build js || wasm

package workflow

import "fmt"

func (c *Compiler) resolveAPMManifestSkills(data *WorkflowData, markdownPath string) error {
	if _, exists := data.RawFrontmatter["apm"]; exists {
		return fmt.Errorf("manifest-backed APM compilation is not available in Wasm")
	}
	return nil
}
