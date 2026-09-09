// Package parser provides functions for parsing and processing workflow markdown files.
// import_cycle.go implements cycle detection in the import dependency graph using
// depth-first search to find and report circular import chains.
package parser

import (
	"sort"

	"github.com/github/gh-aw/pkg/setutil"
	"github.com/github/gh-aw/pkg/sliceutil"
)

// findCyclePath uses DFS to find a complete cycle path in the dependency graph.
// Returns a path showing the full chain including the back-edge (e.g., ["b.md", "c.md", "d.md", "b.md"]).
func findCyclePath(cycleNodes map[string]struct {
}, dependencies map[string][]string) []string {
	importLog.Printf("Finding cycle path among %d cycle nodes", len(cycleNodes))

	// Try nodes in sorted order for determinism. Kahn's unprocessed set can also
	// contain files that depend on a cycle without participating in it.
	sortedNodes := sliceutil.SortedKeys(cycleNodes)
	if len(sortedNodes) == 0 {
		importLog.Print("No cycle nodes found, cannot determine cycle path")
		return nil
	}

	for _, startNode := range sortedNodes {
		importLog.Printf("Starting DFS cycle detection from node: %s", startNode)
		visited := make(map[string]struct {
		})
		path := []string{}
		if dfsForCycle(startNode, startNode, cycleNodes, dependencies, visited, &path, true) {
			importLog.Printf("Cycle path found: %v", path)
			return path
		}
	}

	importLog.Print("DFS completed but no cycle path could be constructed")
	return nil
}

// dfsForCycle performs DFS to find a cycle path.
// isFirst tracks if this is the first call (starting point).
func dfsForCycle(current, target string, cycleNodes map[string]struct {
}, dependencies map[string][]string, visited map[string]struct {
}, path *[]string, isFirst bool) bool {
	// Add current node to path
	*path = append(*path, current)
	visited[current] = struct{}{}

	// Get dependencies of current node, sorted for determinism
	deps := dependencies[current]
	sortedDeps := make([]string, 0, len(deps))
	for _, dep := range deps {
		// Only follow edges within the cycle subgraph
		if setutil.Contains(cycleNodes, dep) {
			sortedDeps = append(sortedDeps, dep)
		}
	}
	sort.Strings(sortedDeps)

	// Explore each dependency
	for _, dep := range sortedDeps {
		// Found the cycle - we've reached the target again. A direct self-edge
		// (dep == current) always counts as a cycle, even on the first call,
		// so that self-imports (e.g. a.md importing a.md) are detected.
		if dep == target && (!isFirst || dep == current) {
			importLog.Printf("Cycle back-edge found: %s -> %s", current, dep)
			*path = append(*path, dep) // Add the back-edge
			return true
		}

		// Continue DFS if not visited
		if !setutil.Contains(visited, dep) {
			if dfsForCycle(dep, target, cycleNodes, dependencies, visited, path, false) {
				return true
			}
		}
	}

	// Backtrack
	*path = (*path)[:len(*path)-1]
	return false
}
