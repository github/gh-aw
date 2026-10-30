//go:build !js && !wasm

package workflow

import (
	"crypto/sha256"
	"fmt"

	"github.com/github/gh-aw/pkg/sliceutil"
)

// Preserve each installed engine version, even when other workflows or MCP
// tools use another version of the same npm package.
func (c *Compiler) mergeNpmEngineDependencies(deps []NpmDependency, workflows []*WorkflowData) []NpmDependency {
	byName := make(map[string]NpmDependency)
	for _, dep := range deps {
		byName[dep.Name] = dep
	}
	engines := collectDependencies(workflows, func(data *WorkflowData) []string {
		return c.enginePackages(data, "npm")
	}, parseNpmPackage, func(dep NpmDependency) string {
		return dep.Name + "@" + dep.Version
	})
	for _, dep := range engines {
		if existing, ok := byName[dep.Name]; ok {
			if existing.Version == dep.Version {
				continue
			}
			alias := fmt.Sprintf("gh-aw-engine-%x", sha256.Sum256([]byte(dep.Name+"@"+dep.Version)))
			baseAlias := alias
			// Do not overwrite an authored dependency that happens to use our alias.
			for suffix := 1; byName[alias].Name != "" && byName[alias].Version != "npm:"+dep.Name+"@"+dep.Version; suffix++ {
				alias = fmt.Sprintf("%s-%d", baseAlias, suffix)
			}
			dep = NpmDependency{Name: alias, Version: "npm:" + dep.Name + "@" + dep.Version}
		}
		byName[dep.Name] = dep
	}
	var result []NpmDependency
	for _, name := range sliceutil.SortedKeys(byName) {
		result = append(result, byName[name])
	}
	return result
}
