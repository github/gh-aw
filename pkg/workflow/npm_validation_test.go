//go:build !integration && !js && !wasm

package workflow

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateNpxPackageNames(t *testing.T) {
	for _, packages := range [][]string{nil, {}, {"package-name", "@scope/package_name"}} {
		require.NoError(t, validateNpxPackageNames(packages))
	}

	err := validateNpxPackageNames([]string{"valid-package", "Invalid-Package", "another invalid package"})
	require.ErrorContains(t, err, "invalid npm package name")
	require.ErrorContains(t, err, `Invalid name: "Invalid-Package"`)
	require.NotContains(t, err.Error(), "another invalid package")
}

func TestValidateNpxPackagesWithoutPackages(t *testing.T) {
	require.NoError(t, NewCompiler().validateNpxPackages(&WorkflowData{}))
}
