//go:build !integration

package parser

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActionPinSourceVersions(t *testing.T) {
	const sha = "c19371144df3bb44fab255c43d04cbc2ab54d1c4"
	content := []byte(`jobs:
  build:
    steps:
      - uses: example/labelled@` + sha + ` # v1.2.3
      - uses: example/unlabelled@` + sha + `
      - uses: example/tagged@v1.2.3 # v1.2.3
      - uses: example/short-sha@abc # v4.5.6
  nested:
    setup:
      - uses: example/nested@` + sha + ` #  v4.5.6  
`)

	require.Equal(t, map[string]string{
		"example/labelled@" + sha: "v1.2.3",
		"example/nested@" + sha:   "v4.5.6",
	}, ActionPinSourceVersions(content))
}

func TestActionPinSourceVersionsInvalidYAML(t *testing.T) {
	require.Nil(t, ActionPinSourceVersions([]byte("steps: [")))
}
