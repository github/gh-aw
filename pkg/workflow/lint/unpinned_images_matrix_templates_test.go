package lint

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUnpinnedMatrixImageTemplates(t *testing.T) {
	digest := strings.Repeat("a", 64)
	for _, tt := range []struct {
		name, matrix, image, message string
	}{
		{"numeric tags", "{tag: [18, 20]}", "node:${{ matrix.tag }}", "mutable or malformed"},
		{"digest suffix", "{digest: ['" + digest + "']}", "node@sha256:${{ matrix.digest }}", ""},
		{"numeric pinned tags", "{tag: [18, 20]}", "node:${{ matrix.tag }}@sha256:" + digest, ""},
		{"include digest", "{include: [{digest: '" + digest + "'}]}", "node@sha256:${{ matrix.digest }}", ""},
		{"excluded malformed digest", "{digest: ['" + digest + "', bad], exclude: [{digest: bad}]}", "node@sha256:${{ matrix.digest }}", ""},
		{"include restores malformed digest", "{digest: ['" + digest + "', bad], exclude: [{digest: bad}], include: [{digest: bad}]}", "node@sha256:${{ matrix.digest }}", "mutable or malformed"},
		{"numeric exclusion", "{digest: ['" + digest + "', 18], exclude: [{digest: 18}]}", "node@sha256:${{ matrix.digest }}", ""},
		{"repeated axis", "{name: [node]}", "${{ matrix.name }}/${{ matrix.name }}@sha256:" + digest, ""},
		{"unknown expression", "{tag: [18, 20]}", "node:${{ matrix.tag }}@${{ vars.DIGEST }}", "computed image cannot be verified"},
		{"multiple axes", "{name: [node], digest: ['" + digest + "']}", "${{ matrix.name }}@sha256:${{ matrix.digest }}", "computed image cannot be verified"},
		{"computed alternative", "{digest: ['${{ vars.DIGEST }}']}", "node@sha256:${{ matrix.digest }}", "computed image cannot be verified"},
		{"object alternative", "{tag: [{version: 18}]}", "node:${{ matrix.tag }}", "computed image cannot be verified"},
		{"structural exclusion stays incomplete", "{tag: [node, {version: 18}], exclude: [{tag: {version: 18}}]}", "node:${{ matrix.tag }}", "computed image cannot be verified"},
		{"scalar exclusion with structural alternative", "{tag: [node, {version: 18}], exclude: [{tag: node}]}", "node:${{ matrix.tag }}", "computed image cannot be verified"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			source := contractJob + "    strategy:\n      matrix: " + tt.matrix + "\n    container: '" + tt.image + "'\n" + contractSafeSteps
			for _, target := range []ASTTargets{ActionsAST, AWAST} {
				diagnostics := integrityTestRun(t, UnpinnedImagesRule(), ruleContractInput(t, source, target, false))
				if tt.message == "" {
					assert.Empty(t, diagnostics)
				} else {
					require.Len(t, diagnostics, 1)
					assert.Contains(t, diagnostics[0].Message, tt.message)
				}
			}
		})
	}
}

func TestUnpinnedMatrixDisabledServiceImages(t *testing.T) {
	pinned := "node@sha256:" + strings.Repeat("a", 64)
	for _, tt := range []struct {
		name, matrix, image string
		serviceFindings     int
	}{
		{"axis disables service", "{image: ['', '" + pinned + "']}", "${{ matrix.image }}", 0},
		{"include disables service", "{image: ['" + pinned + "'], include: [{image: ''}]}", "${{ matrix.image }}", 0},
		{"include only", "{include: [{image: ''}, {image: '" + pinned + "'}]}", "${{ matrix.image }}", 0},
		{"mutable alternative remains", "{image: ['', 'node:22']}", "${{ matrix.image }}", 1},
		{"empty partial value is not disabled", "{image: ['']}", "node:${{ matrix.image }}", 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			for _, target := range []ASTTargets{ActionsAST, AWAST} {
				prefix := contractJob + "    strategy:\n      matrix: " + tt.matrix + "\n"
				service := prefix + "    services: {db: {image: '" + tt.image + "'}}\n" + contractSafeSteps
				assert.Len(t, integrityTestRun(t, UnpinnedImagesRule(), ruleContractInput(t, service, target, false)), tt.serviceFindings)
				container := prefix + "    container: {image: '" + tt.image + "'}\n" + contractSafeSteps
				require.Len(t, integrityTestRun(t, UnpinnedImagesRule(), ruleContractInput(t, container, target, false)), 1)
			}
		})
	}
}
