package deploy

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// TranslateCompose reads the docker-compose.yml file from the unit's
// Cwd, removes 'build' sections, and injects the registry-pinned image
// using the ${IMAGE_TAG} environment variable. It writes the result
// to docker-compose.mctf.yml in the same directory.
func TranslateCompose(u Unit, registryURL string) error {
	raw, err := os.ReadFile(u.ComposeFile)
	if err != nil {
		return fmt.Errorf("read compose file: %w", err)
	}

	var node yaml.Node
	if err := yaml.Unmarshal(raw, &node); err != nil {
		return fmt.Errorf("parse compose file: %w", err)
	}

	if len(node.Content) == 0 || node.Content[0].Kind != yaml.MappingNode {
		return fmt.Errorf("invalid compose file: root must be a mapping")
	}

	rootMap := node.Content[0]
	for i := 0; i < len(rootMap.Content); i += 2 {
		key := rootMap.Content[i]
		val := rootMap.Content[i+1]
		if key.Value == "services" && val.Kind == yaml.MappingNode {
			// Iterate over all services
			for j := 0; j < len(val.Content); j += 2 {
				// serviceKey := val.Content[j]
				serviceMap := val.Content[j+1]
				if serviceMap.Kind != yaml.MappingNode {
					continue
				}

				var newServiceContent []*yaml.Node
				for k := 0; k < len(serviceMap.Content); k += 2 {
					sKey := serviceMap.Content[k]
					sVal := serviceMap.Content[k+1]
					if sKey.Value == "build" {
						// Skip build block
						continue
					}
					newServiceContent = append(newServiceContent, sKey, sVal)
				}

				// Inject image: <registryURL>/<project_name>:${IMAGE_TAG}
				imageNode := &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: fmt.Sprintf("%s/%s:${IMAGE_TAG}", registryURL, u.ProjectName),
				}
				imageKey := &yaml.Node{
					Kind:  yaml.ScalarNode,
					Value: "image",
				}
				newServiceContent = append(newServiceContent, imageKey, imageNode)
				serviceMap.Content = newServiceContent
			}
			break
		}
	}

	outBytes, err := yaml.Marshal(&node)
	if err != nil {
		return fmt.Errorf("marshal modified compose file: %w", err)
	}

	outFile := filepath.Join(u.Cwd, "docker-compose.mctf.yml")
	if err := os.WriteFile(outFile, outBytes, 0644); err != nil {
		return fmt.Errorf("write mctf compose file: %w", err)
	}

	return nil
}
