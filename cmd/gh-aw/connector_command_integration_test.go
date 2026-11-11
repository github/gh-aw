//go:build integration

package main

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/github/gh-aw/pkg/connectors"
	"github.com/stretchr/testify/require"
)

func TestConnectorsValidateCLI(t *testing.T) {
	directory := t.TempDir()
	name := "gh-aw"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	binary := filepath.Join(directory, name)
	build := exec.CommandContext(t.Context(), "go", "build", "-o", binary, ".")
	output, err := build.CombinedOutput()
	require.NoError(t, err, "%s", output)

	example, err := filepath.Abs("../../pkg/connectors/testdata/example.yaml")
	require.NoError(t, err)
	spec, err := connectors.LoadSpec(example)
	require.NoError(t, err)
	data, err := json.Marshal(spec)
	require.NoError(t, err)
	jsonSpec := filepath.Join(directory, "definition with spaces.json")
	require.NoError(t, os.WriteFile(jsonSpec, data, 0600))
	invalid := filepath.Join(directory, "unknown-field.yaml")
	source, err := os.ReadFile(example)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(invalid, append(source, []byte("\nrun: arbitrary-code\n")...), 0600))
	missing := filepath.Join(directory, "missing.yaml")

	for _, test := range []struct {
		name     string
		args     []string
		fail     bool
		json     bool
		valid    []bool
		files    []string
		messages []string
	}{
		{name: "YAML text", args: []string{example}, messages: []string{"valid ConnectorSpec example.com/issues", "no provider access or webhook activation"}},
		{name: "JSON file", args: []string{jsonSpec, "--json"}, json: true, files: []string{jsonSpec}, valid: []bool{true}},
		{name: "mixed JSON checks every file", args: []string{invalid, example, missing, jsonSpec, "--json"}, fail: true, json: true,
			files: []string{invalid, example, missing, jsonSpec}, valid: []bool{false, true, false, true}, messages: []string{"connector validation failed for 2 of 4"}},
		{name: "mixed text checks every file", args: []string{invalid, example, missing}, fail: true, files: []string{invalid, example, missing},
			messages: []string{"unknown field", "valid ConnectorSpec example.com/issues", "connector validation failed for 2 of 3"}},
		{name: "missing file", args: []string{missing}, fail: true, messages: []string{"open connector definition", "connector validation failed for 1 of 1"}},
		{name: "requires files", fail: true, messages: []string{"requires at least 1 arg"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			args := append([]string{"connectors", "validate"}, test.args...)
			command := exec.CommandContext(t.Context(), binary, args...)
			command.Dir = directory
			var stdout, stderr bytes.Buffer
			command.Stdout, command.Stderr = &stdout, &stderr
			err := command.Run()
			if test.fail {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit, "%s\n%s", stdout.String(), stderr.String())
				require.Equal(t, 1, exit.ExitCode())
				require.NotEmpty(t, stderr.String())
			} else {
				require.NoError(t, err, "%s\n%s", stdout.String(), stderr.String())
				require.Empty(t, stderr.String())
			}
			for _, message := range test.messages {
				require.Contains(t, stdout.String()+stderr.String(), message)
			}
			if test.json {
				var results []struct {
					File  string `json:"file"`
					ID    string `json:"id"`
					Valid bool   `json:"valid"`
					Error string `json:"error"`
				}
				require.NoError(t, json.Unmarshal(stdout.Bytes(), &results), "stdout must remain JSON-only even on failure")
				require.Len(t, results, len(test.files))
				for index, result := range results {
					require.Equal(t, test.files[index], result.File)
					require.Equal(t, test.valid[index], result.Valid)
					if result.Valid {
						require.Equal(t, "example.com/issues", result.ID)
						require.Empty(t, result.Error)
					} else {
						require.Empty(t, result.ID)
						require.NotEmpty(t, result.Error)
					}
				}
			} else {
				for _, path := range test.files {
					require.Contains(t, stdout.String(), path+":")
				}
			}
		})
	}
}
