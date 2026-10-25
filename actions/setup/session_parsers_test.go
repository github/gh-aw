package setup

import (
	"io/fs"
	"os"
	"os/exec"
	"path"
	"regexp"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestSessionParserSourcesIncludeLocalDependencies(t *testing.T) {
	t.Parallel()
	localRequire := regexp.MustCompile(`require\(["'](\./[^"']+\.(?:cjs|json))["']\)`)
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

func TestSessionParserSourcesLoadNativeCreationVerifier(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to load embedded JavaScript sources")
	}

	bundleDir := t.TempDir()
	require.NoError(t, os.CopyFS(bundleDir, SessionParserSources))

	cmd := exec.Command(node, "-e", `
		const assert = require("node:assert/strict");
		const { MAX_CLOSE_COUNT } = require("./js/constants.cjs");
		const delivery = require("./js/work_queue_native_creation_delivery.cjs");
		assert.equal(MAX_CLOSE_COUNT, 10);
		assert.equal(typeof delivery.captureDiscussionDelivery, "function");
	`)
	cmd.Dir = bundleDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}

func TestSessionParserSourcesLoadLogParserBootstrap(t *testing.T) {
	t.Parallel()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is required to load embedded JavaScript sources")
	}

	bundleDir := t.TempDir()
	require.NoError(t, os.CopyFS(bundleDir, SessionParserSources))

	cmd := exec.Command(node, "-e", `
		const assert = require("node:assert/strict");
		const bootstrap = require("./js/log_parser_bootstrap.cjs");
		assert.equal(typeof bootstrap.runLogParser, "function");
		assert.equal(typeof bootstrap.readCopilotSessions, "function");
	`)
	cmd.Dir = bundleDir
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "%s", output)
}
