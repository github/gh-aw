//go:build !integration

package workflow

import (
	"encoding/xml"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/github/gh-aw/pkg/parser"
	"github.com/goccy/go-yaml"
	"github.com/stretchr/testify/require"
)

type providerDocumentationElement struct {
	XMLName  xml.Name
	ID       string                         `xml:"id,attr"`
	Version  string                         `xml:"version,attr"`
	Text     string                         `xml:",chardata"`
	Children []providerDocumentationElement `xml:",any"`
}

func TestProviderDocumentationXMLConsumers(t *testing.T) {
	files, err := filepath.Glob("../../.github/workflows/connectors/*.md")
	require.NoError(t, err)
	require.NotEmpty(t, files)
	for _, file := range files {
		provider := strings.TrimSuffix(filepath.Base(file), ".md")
		t.Run(provider, func(t *testing.T) {
			source, err := os.ReadFile(file)
			require.NoError(t, err)
			var declaration struct {
				Connectors map[string]struct {
					Experimental bool     `yaml:"experimental"`
					Docs         []string `yaml:"docs"`
				} `yaml:"connectors"`
			}
			frontmatter, _, found := strings.Cut(strings.TrimPrefix(string(source), "---\n"), "\n---\n")
			require.True(t, found)
			require.NoError(t, yaml.Unmarshal([]byte(frontmatter), &declaration))
			require.True(t, declaration.Connectors[provider].Experimental, "all bundled connectors are experimental")
			require.NotEmpty(t, declaration.Connectors[provider].Docs, "cite official documentation used to author the connector")
			for _, reference := range declaration.Connectors[provider].Docs {
				require.Contains(t, string(source), `"`+reference+`"`, "each docs reference must also occur in the self-contained XML sources")
			}
			_, body, found := strings.Cut(strings.TrimPrefix(string(source), "---\n"), "\n---\n")
			require.True(t, found)
			body = strings.TrimSpace(body)
			require.True(t, strings.HasPrefix(body, "<!--\n"))
			require.Equal(t, 1, strings.Count(body, "<!--"))
			require.Equal(t, 1, strings.Count(body, "-->"))
			require.Equal(t, 1, strings.Count(body, "<connector-documentation "))
			document, _, found := strings.Cut(strings.TrimPrefix(body, "<!--\n"), "\n-->")
			require.True(t, found)
			require.NotContains(t, document, "--")
			var root providerDocumentationElement
			decoder := xml.NewDecoder(strings.NewReader(document))
			require.NoError(t, decoder.Decode(&root))
			_, err = decoder.Token()
			require.ErrorIs(t, err, io.EOF)
			require.Equal(t, "connector-documentation", root.XMLName.Local)
			require.Equal(t, provider, root.ID)
			require.Equal(t, "1", root.Version)

			var samples [][2]string
			var visit func(providerDocumentationElement)
			visit = func(element providerDocumentationElement) {
				var frontmatter, prompt string
				var frontmatterCount, bodyCount int
				for _, child := range element.Children {
					switch child.XMLName.Local {
					case "frontmatter", "workflow-yaml", "yaml":
						frontmatter = child.Text
						frontmatterCount++
					case "body", "workflow-body", "prompt":
						prompt = child.Text
						bodyCount++
					default:
						visit(child)
					}
				}
				if frontmatterCount != 0 || bodyCount != 0 {
					require.Equal(t, 1, frontmatterCount)
					require.Equal(t, 1, bodyCount)
					require.NotEmpty(t, strings.TrimSpace(frontmatter))
					require.NotEmpty(t, strings.TrimSpace(prompt))
					samples = append(samples, [2]string{frontmatter, prompt})
				}
			}
			visit(root)
			require.NotEmpty(t, samples)
			require.Equal(t, len(samples), strings.Count(document, "<frontmatter><![CDATA[")+strings.Count(document, "<workflow-yaml><![CDATA[")+strings.Count(document, "<yaml><![CDATA["))
			bodyCDATA := strings.Count(document, "<body><![CDATA[") + strings.Count(document, "<workflow-body><![CDATA[") + strings.Count(document, "<prompt><![CDATA[")
			require.Equal(t, len(samples)+bodyCDATA, strings.Count(document, "<![CDATA["))
			for index, sample := range samples {
				t.Run(fmt.Sprintf("consumer-%d", index+1), func(t *testing.T) {
					for _, remote := range []bool{false, true} {
						t.Run(fmt.Sprintf("remote-%t", remote), func(t *testing.T) {
							dir := t.TempDir()
							path := filepath.Join(dir, ".github", "workflows", provider+"-review.md")
							require.NoError(t, os.MkdirAll(filepath.Dir(path), 0700))
							frontmatter := sample[0]
							localImport := ".github/workflows/connectors/" + provider + ".md"
							if remote {
								const revision = "1111111111111111111111111111111111111111"
								cache := parser.NewImportCache(dir)
								_, err := cache.Set("github", "gh-aw", localImport, revision, source)
								require.NoError(t, err)
								frontmatter = strings.ReplaceAll(frontmatter, localImport, "github/gh-aw/"+localImport+"@"+revision)
							} else {
								declaration := filepath.Join(dir, ".github", "workflows", "connectors", provider+".md")
								require.NoError(t, os.MkdirAll(filepath.Dir(declaration), 0700))
								require.NoError(t, os.WriteFile(declaration, source, 0600))
							}
							consumer := "---\n" + strings.TrimSpace(frontmatter) + "\n---\n" + strings.TrimSpace(sample[1]) + "\n"
							require.NoError(t, os.WriteFile(path, []byte(consumer), 0600))
							compiler := NewCompiler(WithVersion("v0.0.0-1-g72c8a3ff80e"))
							compiler.gitRoot = dir
							compiler.SetActionMode(ActionModeDev)
							require.NoError(t, compiler.CompileWorkflow(path))
							output, err := os.ReadFile(strings.TrimSuffix(path, ".md") + ".lock.yml")
							require.NoError(t, err)
							require.Contains(t, string(output), "workflow_dispatch:")
							require.Contains(t, string(output), "connector_runtime.cjs")
							require.NotContains(t, string(output), "go build")
							require.NotContains(t, string(output), "gh extension install")
							if remote {
								_, err := os.Stat(filepath.Join(dir, ".github", "workflows", "connectors"))
								require.True(t, os.IsNotExist(err), "URL imports must not materialize local declarations")
							}
						})
					}
				})
			}
		})
	}
}
