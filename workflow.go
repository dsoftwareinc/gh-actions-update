package main

import (
	"fmt"
	"regexp"
	"strings"

	"go.yaml.in/yaml/v3"
)

// workflow is a parsed workflow file.
type workflow struct {
	Path    string   // slash-separated, relative to the repository root
	Name    string   // the workflow's name, or Path when it has none
	Uses    []string // `uses` values of its steps, without duplicates, in order of appearance
	Content []byte
}

// parseWorkflow returns the name of a workflow and the `uses` values of its steps.
func parseWorkflow(content []byte) (name string, uses []string, err error) {
	var doc struct {
		Name string    `yaml:"name"`
		Jobs yaml.Node `yaml:"jobs"`
	}
	if err := yaml.Unmarshal(content, &doc); err != nil {
		return "", nil, err
	}
	seen := make(map[string]bool)
	if doc.Jobs.Kind == yaml.MappingNode {
		// Walk the node instead of decoding into a map, to keep the jobs in file order.
		for i := 0; i+1 < len(doc.Jobs.Content); i += 2 {
			var job struct {
				Steps []struct {
					Uses string `yaml:"uses"`
				} `yaml:"steps"`
			}
			if err := doc.Jobs.Content[i+1].Decode(&job); err != nil {
				return "", nil, fmt.Errorf("job %s: %w", doc.Jobs.Content[i].Value, err)
			}
			for _, step := range job.Steps {
				if step.Uses != "" && !seen[step.Uses] {
					seen[step.Uses] = true
					uses = append(uses, step.Uses)
				}
			}
		}
	}
	return doc.Name, uses, nil
}

// isVersionedAction reports whether a `uses` value references an action release that can be checked for
// updates, i.e. OWNER/REPO[/PATH]@REF, as opposed to a local action (./path) or a Docker image.
func isVersionedAction(uses string) bool {
	return strings.Contains(uses, "@") && !strings.HasPrefix(uses, "./") && !strings.HasPrefix(uses, "docker://")
}

// applyUpdates rewrites `name@current` references to `name@latest` for every update with a latest version.
// References are only replaced as a whole: updating actions/checkout@v4 leaves actions/checkout@v4.1.0 alone.
func applyUpdates(content string, updates []actionVersion) string {
	for _, u := range updates {
		if u.Latest == "" {
			continue
		}
		re := regexp.MustCompile(`(?m)(^|[\s"'])` + regexp.QuoteMeta(u.Name+"@"+u.Current) + `($|[\s"',}\]])`)
		replacement := "${1}" + strings.ReplaceAll(u.Name+"@"+u.Latest, "$", "$$") + "${2}"
		content = re.ReplaceAllString(content, replacement)
	}
	return content
}
