package trim

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chainguard-dev/yam/pkg/yam/formatted"
	"gopkg.in/yaml.v3"
)

// MelangeYAML represents a parsed melange YAML file with AST access
type MelangeYAML struct {
	root     *yaml.Node
	filePath string
}

// PackageLocation identifies where a package was found in the YAML
type PackageLocation struct {
	Path        string // e.g., "environment.contents.packages"
	Index       int    // index in the array
	PackageName string
}

// ParseMelangeYAML parses a melange YAML file preserving formatting
func ParseMelangeYAML(filePath string) (*MelangeYAML, error) {
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	return &MelangeYAML{
		root:     &root,
		filePath: filePath,
	}, nil
}

// GetPackages extracts all package lists from the melange YAML
// Returns map of path -> list of packages
func (m *MelangeYAML) GetPackages() map[string][]string {
	result := make(map[string][]string)

	if m.root.Kind != yaml.DocumentNode || len(m.root.Content) == 0 {
		return result
	}
	doc := m.root.Content[0]

	// Build-time: environment.contents.packages
	if pkgs := m.getSequenceAt(doc, "environment", "contents", "packages"); pkgs != nil {
		result["environment.contents.packages"] = nodeToStrings(pkgs)
	}

	// Runtime (main package): package.dependencies.runtime
	if pkgs := m.getSequenceAt(doc, "package", "dependencies", "runtime"); pkgs != nil {
		result["package.dependencies.runtime"] = nodeToStrings(pkgs)
	}

	// Test (top-level): test.environment.contents.packages
	if pkgs := m.getSequenceAt(doc, "test", "environment", "contents", "packages"); pkgs != nil {
		result["test.environment.contents.packages"] = nodeToStrings(pkgs)
	}

	// Subpackages
	subpackages := m.getSequenceAt(doc, "subpackages")
	if subpackages != nil {
		for i, sp := range subpackages.Content {
			if sp.Kind != yaml.MappingNode {
				continue
			}

			// Get subpackage name for path building
			spName := fmt.Sprintf("subpackages[%d]", i)
			if nameNode := m.getValueAt(sp, "name"); nameNode != nil && nameNode.Kind == yaml.ScalarNode {
				spName = fmt.Sprintf("subpackages[%s]", nameNode.Value)
			}

			// Runtime: subpackages[*].dependencies.runtime
			if pkgs := m.getSequenceAt(sp, "dependencies", "runtime"); pkgs != nil {
				path := fmt.Sprintf("%s.dependencies.runtime", spName)
				result[path] = nodeToStrings(pkgs)
			}

			// Test: subpackages[*].test.environment.contents.packages
			if pkgs := m.getSequenceAt(sp, "test", "environment", "contents", "packages"); pkgs != nil {
				path := fmt.Sprintf("%s.test.environment.contents.packages", spName)
				result[path] = nodeToStrings(pkgs)
			}
		}
	}

	return result
}

// GetPipelineUses extracts all pipeline uses from the melange YAML
// Returns map of scope (e.g., "pipeline", "test.pipeline") -> list of pipeline names
func (m *MelangeYAML) GetPipelineUses() map[string][]string {
	result := make(map[string][]string)

	if m.root.Kind != yaml.DocumentNode || len(m.root.Content) == 0 {
		return result
	}
	doc := m.root.Content[0]

	// Main build pipeline
	if uses := m.extractPipelineUses(doc, "pipeline"); len(uses) > 0 {
		result["pipeline"] = uses
	}

	// Test pipeline
	if uses := m.extractPipelineUses(doc, "test", "pipeline"); len(uses) > 0 {
		result["test.pipeline"] = uses
	}

	// Subpackages
	subpackages := m.getSequenceAt(doc, "subpackages")
	if subpackages != nil {
		for i, sp := range subpackages.Content {
			if sp.Kind != yaml.MappingNode {
				continue
			}

			spName := fmt.Sprintf("subpackages[%d]", i)
			if nameNode := m.getValueAt(sp, "name"); nameNode != nil && nameNode.Kind == yaml.ScalarNode {
				spName = fmt.Sprintf("subpackages[%s]", nameNode.Value)
			}

			// Subpackage pipeline
			if uses := m.extractPipelineUses(sp, "pipeline"); len(uses) > 0 {
				result[fmt.Sprintf("%s.pipeline", spName)] = uses
			}

			// Subpackage test pipeline
			if uses := m.extractPipelineUses(sp, "test", "pipeline"); len(uses) > 0 {
				result[fmt.Sprintf("%s.test.pipeline", spName)] = uses
			}
		}
	}

	return result
}

// extractPipelineUses extracts all "uses" values from a pipeline sequence
func (m *MelangeYAML) extractPipelineUses(node *yaml.Node, path ...string) []string {
	pipeline := m.getSequenceAt(node, path...)
	if pipeline == nil {
		return nil
	}

	var uses []string
	for _, step := range pipeline.Content {
		if step.Kind != yaml.MappingNode {
			continue
		}
		if usesNode := m.getValueAt(step, "uses"); usesNode != nil && usesNode.Kind == yaml.ScalarNode {
			uses = append(uses, usesNode.Value)
		}
		// Recurse into nested pipelines
		if nestedUses := m.extractPipelineUses(step, "pipeline"); len(nestedUses) > 0 {
			uses = append(uses, nestedUses...)
		}
	}
	return uses
}

// GetRepositories returns the repository URLs from environment.contents.repositories
func (m *MelangeYAML) GetRepositories() []string {
	if m.root.Kind != yaml.DocumentNode || len(m.root.Content) == 0 {
		return nil
	}
	doc := m.root.Content[0]

	repos := m.getSequenceAt(doc, "environment", "contents", "repositories")
	if repos == nil {
		return nil
	}
	return nodeToStrings(repos)
}

// RemovePackages removes the specified packages from the given path
// Returns the list of actually removed packages
func (m *MelangeYAML) RemovePackages(path string, packages []string) []string {
	if m.root.Kind != yaml.DocumentNode || len(m.root.Content) == 0 {
		return nil
	}
	doc := m.root.Content[0]

	// Parse the path to find the target node
	node := m.findNodeByPath(doc, path)
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}

	// Build set of packages to remove
	toRemove := make(map[string]bool)
	for _, pkg := range packages {
		toRemove[pkg] = true
	}

	// Filter out packages
	var removed []string
	var newContent []*yaml.Node
	for _, item := range node.Content {
		if item.Kind == yaml.ScalarNode && toRemove[item.Value] {
			removed = append(removed, item.Value)
		} else {
			newContent = append(newContent, item)
		}
	}

	node.Content = newContent

	// Clean up empty parent blocks if the sequence is now empty
	if len(newContent) == 0 {
		m.cleanupEmptyParents(doc, path)
	}

	return removed
}

// cleanupEmptyParents removes empty parent blocks after a sequence becomes empty
func (m *MelangeYAML) cleanupEmptyParents(doc *yaml.Node, path string) {
	parts := splitPathPreservingBrackets(path)
	if len(parts) == 0 {
		return
	}

	// Work backwards through the path, removing empty nodes
	for i := len(parts) - 1; i >= 0; i-- {
		parentPath := parts[:i]
		keyToRemove := parts[i]

		var parent *yaml.Node
		if len(parentPath) == 0 {
			parent = doc
		} else {
			parent = m.findNodeByPath(doc, strings.Join(parentPath, "."))
		}

		if parent == nil || parent.Kind != yaml.MappingNode {
			return
		}

		// Handle array indices like "subpackages[name]" - don't remove subpackage entries
		if strings.Contains(keyToRemove, "[") {
			return
		}

		// Find and check if the node is empty
		nodeToCheck := m.getValueAt(parent, keyToRemove)
		if nodeToCheck == nil {
			return
		}

		isEmpty := false
		switch nodeToCheck.Kind {
		case yaml.SequenceNode:
			isEmpty = len(nodeToCheck.Content) == 0
		case yaml.MappingNode:
			isEmpty = len(nodeToCheck.Content) == 0
		}

		if !isEmpty {
			return
		}

		// Remove the key-value pair from parent
		m.removeKeyFromMapping(parent, keyToRemove)
	}
}

// removeKeyFromMapping removes a key-value pair from a mapping node
func (m *MelangeYAML) removeKeyFromMapping(node *yaml.Node, key string) {
	if node == nil || node.Kind != yaml.MappingNode {
		return
	}

	var newContent []*yaml.Node
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind == yaml.ScalarNode && node.Content[i].Value == key {
			// Skip this key-value pair
			continue
		}
		newContent = append(newContent, node.Content[i], node.Content[i+1])
	}
	node.Content = newContent
}

// Write writes the modified YAML back to the file using yam formatting
func (m *MelangeYAML) Write() error {
	var buf bytes.Buffer

	enc := formatted.NewEncoder(&buf)

	// Try to load .yam.yaml config from the file's directory
	yamConfigPath := filepath.Join(filepath.Dir(m.filePath), ".yam.yaml")
	if f, err := os.Open(yamConfigPath); err == nil {
		defer f.Close()
		if opts, err := formatted.ReadConfigFrom(f); err == nil {
			enc, _ = enc.UseOptions(*opts)
		}
	}

	if err := enc.Encode(m.root); err != nil {
		return fmt.Errorf("encoding YAML: %w", err)
	}

	if err := os.WriteFile(m.filePath, buf.Bytes(), 0o644); err != nil {
		return fmt.Errorf("writing file: %w", err)
	}

	return nil
}

// findNodeByPath finds a node by dot-separated path
func (m *MelangeYAML) findNodeByPath(doc *yaml.Node, path string) *yaml.Node {
	parts := splitPathPreservingBrackets(path)
	current := doc

	for _, part := range parts {
		if current == nil {
			return nil
		}

		// Handle array indices like "subpackages[name]"
		// Note: name may contain brackets like "${{package.name}}-foo"
		if strings.Contains(part, "[") {
			openIdx := strings.Index(part, "[")
			closeIdx := strings.LastIndex(part, "]")
			if closeIdx <= openIdx {
				return nil
			}
			baseName := part[:openIdx]
			indexStr := part[openIdx+1 : closeIdx]

			// First find the array
			current = m.getValueAt(current, baseName)
			if current == nil || current.Kind != yaml.SequenceNode {
				return nil
			}

			// Then find the element
			found := false
			for _, elem := range current.Content {
				if elem.Kind != yaml.MappingNode {
					continue
				}
				// Try to match by name
				nameNode := m.getValueAt(elem, "name")
				if nameNode != nil && nameNode.Kind == yaml.ScalarNode && nameNode.Value == indexStr {
					current = elem
					found = true
					break
				}
			}
			if !found {
				return nil
			}
		} else {
			current = m.getValueAt(current, part)
		}
	}

	return current
}

// getSequenceAt gets a sequence node at the given path
func (m *MelangeYAML) getSequenceAt(node *yaml.Node, path ...string) *yaml.Node {
	current := node
	for _, key := range path {
		current = m.getValueAt(current, key)
		if current == nil {
			return nil
		}
	}
	if current.Kind != yaml.SequenceNode {
		return nil
	}
	return current
}

// getValueAt gets the value of a key in a mapping node
func (m *MelangeYAML) getValueAt(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Kind == yaml.ScalarNode && node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

// nodeToStrings converts a sequence node to a slice of strings
func nodeToStrings(node *yaml.Node) []string {
	if node == nil || node.Kind != yaml.SequenceNode {
		return nil
	}
	var result []string
	for _, item := range node.Content {
		if item.Kind == yaml.ScalarNode {
			result = append(result, item.Value)
		}
	}
	return result
}

// splitPathPreservingBrackets splits a path by dots but preserves content inside brackets
// e.g., "subpackages[${{package.name}}-foo].dependencies.runtime" ->
// ["subpackages[${{package.name}}-foo]", "dependencies", "runtime"]
func splitPathPreservingBrackets(path string) []string {
	var parts []string
	var current strings.Builder
	bracketDepth := 0

	for _, ch := range path {
		switch ch {
		case '[':
			bracketDepth++
			current.WriteRune(ch)
		case ']':
			bracketDepth--
			current.WriteRune(ch)
		case '.':
			if bracketDepth == 0 {
				if current.Len() > 0 {
					parts = append(parts, current.String())
					current.Reset()
				}
			} else {
				current.WriteRune(ch)
			}
		default:
			current.WriteRune(ch)
		}
	}

	if current.Len() > 0 {
		parts = append(parts, current.String())
	}

	return parts
}
