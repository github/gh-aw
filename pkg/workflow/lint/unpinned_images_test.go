package lint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnpinnedImages(t *testing.T) {
	for _, tt := range []struct {
		image string
		want  bool
	}{
		{"node", true},
		{"node:latest", true},
		{"node:22", true},
		{"registry.example:5000/org/node@sha256:" + strings.Repeat("a", 64), false},
		{"node:22@sha256:" + strings.Repeat("b", 64), false},
		{"node@sha256:abc", true},
		{"node@sha256:" + strings.Repeat("z", 64), true},
		{"${{ vars.IMAGE }}", true},
	} {
		for _, field := range []string{"container: '%s'", "services: {db: {image: '%s'}}"} {
			t.Run(tt.image+"/"+field, func(t *testing.T) {
				declaration := strings.Replace(field, "%s", tt.image, 1)
				source := contractJob + "    " + declaration + "\n" + contractSafeSteps
				for _, target := range []ASTTargets{ActionsAST, AWAST} {
					diagnostics := integrityTestRun(t, UnpinnedImagesRule(), ruleContractInput(t, source, target, false))
					if tt.want {
						require.Len(t, diagnostics, 1)
						assert.Equal(t, Warning, diagnostics[0].Severity)
					} else {
						assert.Empty(t, diagnostics)
					}
				}

			})
		}
	}
}

func TestDisabledServiceImage(t *testing.T) {
	for _, target := range []ASTTargets{ActionsAST, AWAST} {
		source := contractJob + "    services: {db: {image: ''}}\n" + contractSafeSteps
		assert.Empty(t, integrityTestRun(t, UnpinnedImagesRule(), ruleContractInput(t, source, target, false)))
		source = contractJob + "    container: {image: ''}\n" + contractSafeSteps
		require.Len(t, integrityTestRun(t, UnpinnedImagesRule(), ruleContractInput(t, source, target, false)), 1)
	}
}

func TestUnpinnedMatrixImages(t *testing.T) {
	pinned := "node@sha256:" + strings.Repeat("a", 64)
	for _, tt := range []struct {
		matrix string
		want   bool
	}{
		{"{image: ['" + pinned + "']}", false},
		{"{image: ['" + pinned + "', 'node:22']}", true},
		{"{include: [{image: '" + pinned + "'}]}", false},
		{"{image: ['" + pinned + "'], include: [{image: 'node:latest'}]}", true},
		{"{image: ['" + pinned + "', 'node:latest'], exclude: [{image: 'node:latest'}]}", false},
		{"{image: ['" + pinned + "', 'node:latest'], exclude: [{image: 'node:latest'}], include: [{image: 'node:latest'}]}", true},
		{"{image: ['" + pinned + "', 'node:latest'], os: [linux, windows], exclude: [{image: 'node:latest', os: linux}]}", true},
		{"${{ fromJSON(inputs.matrix) }}", true},
		{"{image: ['${{ vars.IMAGE }}']}", true},
	} {
		t.Run(tt.matrix, func(t *testing.T) {
			source := contractJob + "    strategy:\n      matrix: " + tt.matrix + "\n    container: '${{ matrix.image }}'\n" + contractSafeSteps
			for _, target := range []ASTTargets{ActionsAST, AWAST} {
				diagnostics := integrityTestRun(t, UnpinnedImagesRule(), ruleContractInput(t, source, target, false))
				assert.Equal(t, tt.want, len(diagnostics) > 0)
			}
		})
	}
}
