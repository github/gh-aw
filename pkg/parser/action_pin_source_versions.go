package parser

import (
	"fmt"
	"sort"
	"strings"

	"github.com/github/gh-aw/pkg/gitutil"
	"github.com/goccy/go-yaml"
)

// ActionPinSourceVersions collects inline labels for SHA-pinned action references in YAML.
func ActionPinSourceVersions(content []byte) map[string]string {
	comments := yaml.CommentMap{}
	var document any
	if err := yaml.UnmarshalWithOptions(content, &document, yaml.CommentToMap(comments)); err != nil {
		return nil
	}

	versions := make(map[string]string)
	collectActionPinSourceVersions(document, "$", comments, versions)
	return versions
}

func collectActionPinSourceVersions(value any, path string, comments yaml.CommentMap, versions map[string]string) {
	switch value := value.(type) {
	case map[string]any:
		keys := make([]string, 0, len(value))
		for key := range value {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			childPath := path + "." + key
			if key == "uses" {
				uses, ok := value[key].(string)
				if ok {
					repo, sha, found := strings.Cut(uses, "@")
					if found && gitutil.IsValidFullSHA(sha) {
						for _, comment := range comments[childPath] {
							if comment.Position == yaml.CommentLinePosition {
								for _, text := range comment.Texts {
									if label := strings.TrimSpace(text); label != "" {
										versions[repo+"@"+sha] = label
									}
									break
								}
							}
						}
					}
				}
			}
			collectActionPinSourceVersions(value[key], childPath, comments, versions)
		}
	case []any:
		for i, item := range value {
			collectActionPinSourceVersions(item, fmt.Sprintf("%s[%d]", path, i), comments, versions)
		}
	}
}
