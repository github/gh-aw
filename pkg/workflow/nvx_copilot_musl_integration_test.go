//go:build integration

package workflow

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/github/gh-aw/pkg/constants"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNVXStagedMuslCopilotInvokesAPIProxy(t *testing.T) {
	if _, err := exec.LookPath("docker"); err != nil {
		t.Skip("docker is required")
	}
	require.NoError(t, exec.Command("docker", "version").Run())
	if runtime.GOOS == "linux" {
		if _, err := os.Stat("/usr/bin/patchelf"); err != nil {
			t.Skip("/usr/bin/patchelf is required to stage the portable Node runtime")
		}
	}

	temp := t.TempDir()
	workspace := filepath.Join(temp, "workspace")
	runnerTemp := filepath.Join(temp, "runner")
	hostRoot := filepath.Join(runnerTemp, "gh-aw")
	stateRoot := filepath.Join(temp, "state")
	require.NoError(t, os.MkdirAll(filepath.Join(hostRoot, "actions"), 0o755))
	require.NoError(t, os.MkdirAll(filepath.Join(hostRoot, "nvx-engine"), 0o755))
	require.NoError(t, os.MkdirAll(workspace, 0o755))
	require.NoError(t, os.MkdirAll(stateRoot, 0o755))
	require.NoError(t, copyFile(
		filepath.Join("..", "..", "actions", "setup", "js", "nvx_assets.cjs"),
		filepath.Join(hostRoot, "actions", "nvx_assets.cjs"),
		0o644,
	))
	require.NoError(t, copyFile(
		filepath.Join("..", "..", "actions", "setup", "js", "copilot_sdk_mcp_config_path.cjs"),
		filepath.Join(hostRoot, "actions", "copilot_sdk_mcp_config_path.cjs"),
		0o644,
	))

	version := strings.TrimPrefix(string(constants.DefaultCopilotVersion), "v")
	baseURL := fmt.Sprintf("https://github.com/github/copilot-cli/releases/download/v%s", version)
	assetName := "copilot-linuxmusl-x64.tar.gz"
	archivePath := filepath.Join(temp, assetName)
	client := &http.Client{Timeout: 30 * time.Second}
	require.NoError(t, downloadVerifiedReleaseAsset(client, baseURL, assetName, archivePath))
	archive, err := os.Open(archivePath)
	require.NoError(t, err)
	require.NoError(t, extractCopilotBinary(archive, filepath.Join(hostRoot, "nvx-engine", "copilot")))
	require.NoError(t, archive.Close())

	stage := exec.Command("node", filepath.Join(hostRoot, "actions", "nvx_assets.cjs"), "stage")
	stage.Env = append(os.Environ(), "GITHUB_WORKSPACE="+workspace, "RUNNER_TEMP="+runnerTemp)
	output, err := stage.CombinedOutput()
	require.NoError(t, err, "%s", output)

	requests := make(chan map[string]any, 1)
	serverErrors := make(chan error, 1)
	listener, err := net.Listen("tcp4", "0.0.0.0:0")
	require.NoError(t, err)
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, request *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.HasSuffix(request.URL.Path, "/models") {
			_, _ = io.WriteString(w, `{"object":"list","data":[{"id":"test-model","object":"model","owned_by":"test"}]}`)
			return
		}
		if strings.HasSuffix(request.URL.Path, "/chat/completions") {
			var body map[string]any
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				serverErrors <- err
				http.Error(w, "invalid request", http.StatusBadRequest)
				return
			}
			requests <- body
			_, _ = io.WriteString(w, `{"id":"chatcmpl-test","object":"chat.completion","created":1,"model":"test-model","choices":[{"index":0,"message":{"role":"assistant","content":"ok"},"finish_reason":"stop"}],"usage":{"prompt_tokens":5,"completion_tokens":1,"total_tokens":6}}`)
			return
		}
		http.NotFound(w, request)
	})}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = server.Shutdown(ctx)
	})

	port := listener.Addr().(*net.TCPAddr).Port
	stagedCopilot := "/workspace/.gh-aw-nvx/gh-aw/bin/copilot"
	run := exec.Command(
		"docker", "run", "--rm", "--platform", "linux/amd64",
		"--add-host", "host.docker.internal:host-gateway",
		"-v", workspace+":/workspace",
		"-w", "/workspace",
		"-e", "HOME=/tmp/awf-home",
		"-e", fmt.Sprintf("COPILOT_PROVIDER_BASE_URL=http://host.docker.internal:%d/v1", port),
		"-e", "COPILOT_PROVIDER_API_KEY=test-only",
		"-e", "COPILOT_MODEL=test-model",
		"alpine:3.22",
		stagedCopilot, "-p", "Reply with ok", "--allow-all-tools", "--no-ask-user", "--silent",
	)
	output, err = run.CombinedOutput()
	require.NoError(t, err, "%s", output)
	assert.Contains(t, string(output), "ok")

	select {
	case request := <-requests:
		assert.Equal(t, "test-model", request["model"])
		assert.NotEmpty(t, request["messages"])
	case err := <-serverErrors:
		t.Fatalf("failed to decode API proxy request: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("staged musl Copilot did not invoke the API proxy")
	}
}

func TestGetReleaseAssetResponseRetriesServerErrors(t *testing.T) {
	attempts := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		attempts++
		if attempts == 1 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	response, err := getReleaseAssetResponse(server.Client(), server.URL)
	require.NoError(t, err)
	defer response.Body.Close()
	assert.Equal(t, http.StatusOK, response.StatusCode)
	assert.Equal(t, 2, attempts)
}

func downloadVerifiedReleaseAsset(client *http.Client, baseURL, assetName, destination string) error {
	checksumsResponse, err := getReleaseAssetResponse(client, baseURL+"/SHA256SUMS.txt")
	if err != nil {
		return err
	}
	defer checksumsResponse.Body.Close()
	if checksumsResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("checksum download returned HTTP %d", checksumsResponse.StatusCode)
	}
	checksums, err := io.ReadAll(io.LimitReader(checksumsResponse.Body, 1<<20))
	if err != nil {
		return err
	}
	var expected string
	for line := range strings.SplitSeq(string(checksums), "\n") {
		fields := strings.Fields(line)
		if len(fields) == 2 && fields[1] == assetName {
			expected = strings.ToLower(fields[0])
			break
		}
	}
	if expected == "" {
		return fmt.Errorf("checksum for %s not found", assetName)
	}

	assetResponse, err := getReleaseAssetResponse(client, baseURL+"/"+assetName)
	if err != nil {
		return err
	}
	defer assetResponse.Body.Close()
	if assetResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("asset download returned HTTP %d", assetResponse.StatusCode)
	}
	file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	hash := sha256.New()
	_, copyErr := io.Copy(io.MultiWriter(file, hash), assetResponse.Body)
	closeErr := file.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeErr != nil {
		return closeErr
	}
	actual := fmt.Sprintf("%x", hash.Sum(nil))
	if actual != expected {
		return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", assetName, expected, actual)
	}
	return nil
}

func getReleaseAssetResponse(client *http.Client, url string) (*http.Response, error) {
	const maxAttempts = 3
	for attempt := 0; attempt < maxAttempts; attempt++ {
		response, err := client.Get(url)
		if err != nil {
			return nil, err
		}
		if response.StatusCode < http.StatusInternalServerError || attempt == maxAttempts-1 {
			return response, nil
		}
		_ = response.Body.Close()
		time.Sleep(time.Duration(attempt+1) * 250 * time.Millisecond)
	}
	return nil, fmt.Errorf("release download failed after %d attempts", maxAttempts)
}

func copyFile(source, destination string, mode os.FileMode) error {
	content, err := os.ReadFile(source)
	if err != nil {
		return err
	}
	return os.WriteFile(destination, content, mode)
}

func extractCopilotBinary(reader io.Reader, destination string) error {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return err
	}
	defer gzipReader.Close()
	tarReader := tar.NewReader(gzipReader)
	for {
		header, err := tarReader.Next()
		if err == io.EOF {
			return fmt.Errorf("copilot binary not found in archive")
		}
		if err != nil {
			return err
		}
		if header.Name != "copilot" || header.Typeflag != tar.TypeReg {
			continue
		}
		file, err := os.OpenFile(destination, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o755)
		if err != nil {
			return err
		}
		_, copyErr := io.Copy(file, tarReader)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		return closeErr
	}
}
