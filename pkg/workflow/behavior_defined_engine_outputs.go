package workflow

import (
	"fmt"
	"path"
	"regexp"
	"slices"
)

var behaviorOutputFilePath = regexp.MustCompile(`^/tmp/gh-aw/agent/[A-Za-z0-9._/-]+$`)

func validateBehaviorOutputFiles(files []string) error {
	for _, file := range files {
		if !behaviorOutputFilePath.MatchString(file) || path.Clean(file) != file ||
			!slices.Contains(secretRedactionScannedExtensions[:], path.Ext(file)) {
			return fmt.Errorf("invalid output file %q: use a literal file path under /tmp/gh-aw/agent/ with a secret-redactable extension and no traversal or wildcards", file)
		}
	}
	return nil
}

func (e *BehaviorDefinedEngine) GetDeclaredOutputFiles() []string {
	if behavior := e.behavior(); behavior != nil {
		return slices.Clone(behavior.OutputFiles)
	}
	return nil
}
