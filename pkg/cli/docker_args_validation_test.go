//go:build !integration

package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestBuildDockerVolumeMount(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()

	mount, err := buildDockerVolumeMount(tmpDir, "/workdir")
	require.NoError(t, err)
	require.Equal(t, tmpDir+":/workdir", mount)

	_, err = buildDockerVolumeMount("relative/path", "/workdir")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid host path")

	_, err = buildDockerVolumeMount(tmpDir, "workdir")
	require.Error(t, err)
	require.ErrorContains(t, err, "container path must be absolute")

	_, err = buildDockerVolumeMount(tmpDir, "/work:dir")
	require.Error(t, err)
	require.ErrorContains(t, err, "reserved characters")

	_, err = buildDockerVolumeMount(tmpDir, "/work\x00dir")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid control characters")

	_, err = buildDockerVolumeMount(tmpDir, "/work\u202edir")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid control characters")

	_, err = buildDockerVolumeMount(tmpDir+"\nrepo", "/workdir")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid control characters")

	if runtime.GOOS != "windows" {
		_, err = buildDockerVolumeMount("/", "/workdir")
		require.ErrorContains(t, err, "cannot be a filesystem root")

		_, err = buildDockerVolumeMount("/tmp/repo:fixture", "/workdir")
		require.Error(t, err)
		require.ErrorContains(t, err, "unsupported ':'")
	}
}

func TestBuildDockerReadonlyFileMount(t *testing.T) {
	t.Parallel()
	tmpDir := t.TempDir()
	policyFile := filepath.Join(tmpDir, ".grant.yaml")
	require.NoError(t, os.WriteFile(policyFile, []byte("policy: true\n"), 0o644))

	mount, err := buildDockerReadonlyFileMount(policyFile, "/tmp/policy.yaml")
	require.NoError(t, err)
	require.Equal(t, policyFile+":/tmp/policy.yaml:ro", mount)

	_, err = buildDockerReadonlyFileMount(tmpDir, "/tmp/policy.yaml")
	require.Error(t, err)
	require.ErrorContains(t, err, "not a regular file")

	_, err = buildDockerReadonlyFileMount(policyFile, "/tmp/policy:yaml")
	require.Error(t, err)
	require.ErrorContains(t, err, "reserved characters")

	_, err = buildDockerReadonlyFileMount(policyFile, "/tmp/policy\x00yaml")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid control characters")

	_, err = buildDockerReadonlyFileMount(policyFile, "/tmp/policy\u202eyaml")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid control characters")

	_, err = buildDockerReadonlyFileMount(policyFile+"\n", "/tmp/policy.yaml")
	require.Error(t, err)
	require.ErrorContains(t, err, "invalid control characters")
}

func TestValidateDockerImageRefRejectsUnsafeCharacters(t *testing.T) {
	t.Parallel()
	testCases := []struct {
		name     string
		imageRef string
		wantErr  string
	}{
		{name: "trailing newline", imageRef: "alpine:latest\n", wantErr: "invalid whitespace/control characters"},
		{name: "unicode line separator", imageRef: "alpine\u2028latest", wantErr: "invalid whitespace/control characters"},
		{name: "unicode bidi override", imageRef: "alpine\u202elatest", wantErr: "invalid whitespace/control characters"},
		{name: "multiple digests", imageRef: "ghcr.io/org/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa@sha256:bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb", wantErr: "multiple digest separators"},
		{name: "invalid digest", imageRef: "ghcr.io/org/image@sha256:nothex", wantErr: "invalid digest format"},
		{name: "invalid digest algorithm", imageRef: "ghcr.io/org/image@sha1:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", wantErr: "invalid digest format"},
		{name: "invalid sha256 digest length", imageRef: "ghcr.io/org/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", wantErr: "invalid digest format"},
		{name: "invalid tag", imageRef: "ghcr.io/org/image:-tag", wantErr: "invalid tag format"},
		{name: "invalid image name characters", imageRef: "ghcr.io/org/im;age:latest", wantErr: "allow-listed image pattern"},
		{name: "shell command injection", imageRef: "image; rm -rf /", wantErr: "invalid whitespace/control characters"},
		{name: "command substitution", imageRef: "image$(malicious)", wantErr: "allow-listed image pattern"},
		{name: "empty string", imageRef: "", wantErr: "cannot be empty"},
	}

	for _, tt := range testCases {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := validateDockerImageRef(tt.imageRef)
			require.Error(t, err)
			require.ErrorContains(t, err, tt.wantErr)
		})
	}
}

func TestValidateDockerImageRefAcceptsCommonReferences(t *testing.T) {
	t.Parallel()
	testCases := []string{
		"alpine:latest",
		"ghcr.io/github/gh-aw:1.2.3",
		"localhost:5000/org/image_name:tag-1",
		"registry.example.com/team/my__image:latest",
		"team/my--image:latest",
		"ghcr.io/org/image@sha256:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
		"ghcr.io/org/image@sha512:aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa",
	}

	for _, imageRef := range testCases {
		t.Run(imageRef, func(t *testing.T) {
			t.Parallel()
			validated, err := validateDockerImageRef(imageRef)
			require.NoError(t, err)
			require.Equal(t, imageRef, validated)
		})
	}
}

func TestPinnedScannerImagesUseValidDockerReferences(t *testing.T) {
	t.Parallel()
	for _, imageRef := range []string{
		PoutineImage,
		RunnerGuardImage,
		GrantImage,
	} {
		t.Run(imageRef, func(t *testing.T) {
			t.Parallel()
			validated, err := validateDockerImageRef(imageRef)
			require.NoError(t, err)
			require.Equal(t, imageRef, validated)
		})
	}
}

func TestGrantContainerPolicyPathIsValidContainerMountPath(t *testing.T) {
	t.Parallel()
	validated, err := validateContainerMountPath(grantContainerPolicyPath)
	require.NoError(t, err)
	require.Equal(t, grantContainerPolicyPath, validated)
}
