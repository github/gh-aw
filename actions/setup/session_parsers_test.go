package setup

import (
	"io/fs"
	"path"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionParserSourcesIncludeLocalDependencies(t *testing.T) {
	t.Parallel()
	localRequire := regexp.MustCompile(`require\(["'](\./[^"']+\.cjs)["']\)`)
	err := fs.WalkDir(SessionParserSources, "js", func(name string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() {
			return nil
		}
		content, err := SessionParserSources.ReadFile(name)
		if err != nil {
			return err
		}
		for _, match := range localRequire.FindAllSubmatch(content, -1) {
			dependency := path.Join(path.Dir(name), string(match[1]))
			_, err := SessionParserSources.ReadFile(dependency)
			require.NoError(t, err, "%s requires missing embedded dependency %s", name, dependency)
		}
		return nil
	})
	require.NoError(t, err)
}
