// Package parser provides functions for parsing and processing workflow markdown files.
// import_topological.go implements topological ordering of imports using Kahn's algorithm,
// ensuring dependencies are processed before the files that depend on them.
package parser

import (
	"errors"
	"slices"

	"github.com/github/gh-aw/pkg/setutil"
)

// topologicalSortImports sorts imports in topological order using Kahn's algorithm.
// Returns imports sorted such that roots (files with no imports) come first,
// and each import has all its dependencies listed before it.
// workflowFile is the path to the top-level workflow file, used for error context
// when a circular import is detected.
// Returns an error if a circular import is detected.
func topologicalSortImports(imports []string, dependencies map[string][]string, importPaths map[string]string, priorities map[string]int, workflowFile string) ([]string, error) {
	importLog.Printf("Starting topological sort of %d imports", len(imports))
	allImportsSet := toImportSet(imports)
	inDegree := calculateInDegree(imports, dependencies, allImportsSet)
	importLog.Printf("Calculated in-degrees: %v", inDegree)
	queue := collectRootImports(imports, inDegree)
	result := runKahnTopologicalSort(imports, dependencies, allImportsSet, inDegree, queue, priorities)

	importLog.Printf("Topological sort complete: %v", result)
	if len(result) < len(imports) {
		importLog.Printf("Cycle detected: processed %d/%d imports", len(result), len(imports))
		cycleNodes := findCycleNodes(imports, result)
		cyclePath := findCyclePath(cycleNodes, dependencies)
		if len(cyclePath) > 0 {
			return nil, &ImportCycleError{
				Chain:        displayImportPaths(cyclePath, importPaths),
				WorkflowFile: workflowFile,
			}
		}

		// Fallback error if we couldn't construct the path (shouldn't happen)
		return nil, errors.New("circular import detected but could not determine cycle path")
	}

	return result, nil
}

func toImportSet(imports []string) map[string]struct {
} {
	allImportsSet := make(map[string]struct {
	}, len(imports))
	for _, imp := range imports {
		allImportsSet[imp] = struct {
		}{}
	}
	return allImportsSet
}

func calculateInDegree(imports []string, dependencies map[string][]string, allImportsSet map[string]struct {
}) map[string]int {
	inDegree := make(map[string]int, len(imports))
	for _, imp := range imports {
		inDegree[imp] = 0
	}
	for _, imp := range imports {
		for _, dep := range dependencies[imp] {
			if setutil.Contains(allImportsSet, dep) {
				inDegree[imp]++
			}
		}
	}
	return inDegree
}

func collectRootImports(imports []string, inDegree map[string]int) []string {
	var queue []string
	for _, imp := range imports {
		if inDegree[imp] == 0 {
			queue = append(queue, imp)
			importLog.Printf("Root import (no dependencies): %s", imp)
		}
	}
	return queue
}

func runKahnTopologicalSort(
	imports []string,
	dependencies map[string][]string,
	allImportsSet map[string]struct {
	}, inDegree map[string]int,
	queue []string,
	priorities map[string]int,
) []string {
	result := make([]string, 0, len(imports))
	declarationOrder := make(map[string]int, len(imports))
	for index, imp := range imports {
		declarationOrder[imp] = index
	}
	for len(queue) > 0 {
		slices.SortStableFunc(queue, func(a, b string) int {
			if diff := priorities[a] - priorities[b]; diff != 0 {
				return diff
			}
			return declarationOrder[a] - declarationOrder[b]
		})
		current := queue[0]
		queue = queue[1:]
		result = append(result, current)
		importLog.Printf("Processing import %s (in-degree was 0)", current)
		queue = reduceDependentInDegrees(current, imports, dependencies, allImportsSet, inDegree, queue)
	}
	return result
}

func reduceDependentInDegrees(
	current string,
	imports []string,
	dependencies map[string][]string,
	allImportsSet map[string]struct {
	}, inDegree map[string]int,
	queue []string,
) []string {
	for _, imp := range imports {
		for _, dep := range dependencies[imp] {
			if dep == current && setutil.Contains(allImportsSet, imp) {
				inDegree[imp]--
				importLog.Printf("Reduced in-degree of %s to %d (resolved dependency on %s)", imp, inDegree[imp], current)
				if inDegree[imp] == 0 {
					queue = append(queue, imp)
					importLog.Printf("Added %s to queue (in-degree reached 0)", imp)
				}
			}
		}
	}
	return queue
}

func displayImportPaths(paths []string, importPaths map[string]string) []string {
	displayPaths := make([]string, 0, len(paths))
	for _, fullPath := range paths {
		if importPath, ok := importPaths[fullPath]; ok {
			displayPaths = append(displayPaths, importPath)
		} else {
			displayPaths = append(displayPaths, fullPath)
		}
	}
	return displayPaths
}

func findCycleNodes(imports, result []string) map[string]struct {
} {
	cycleNodes := make(map[string]struct {
	})
	for _, imp := range imports {
		if !slices.Contains(result, imp) {
			cycleNodes[imp] = struct {
			}{}
		}
	}
	return cycleNodes
}
