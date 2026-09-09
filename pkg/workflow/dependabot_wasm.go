//go:build js || wasm

package workflow

import "context"

func (c *Compiler) GenerateDependabotManifests(ctx context.Context, workflowDataList []*WorkflowData, workflowDir string, forceOverwrite bool) error {
	return nil
}
