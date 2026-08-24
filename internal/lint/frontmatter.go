package lint

import (
	"bytes"
	"fmt"
	"strings"
	"unicode/utf8"

	"gopkg.in/yaml.v3"
)

type frontmatter struct {
	present bool
	root    *yaml.Node
	// Number of lines before the YAML payload. yaml.Node.Line is payload-relative.
	lineOffset int
}

func parseFrontmatter(path string, source []byte) (frontmatter, []Diagnostic) {
	loc := Location{File: path, Line: 1, Column: 1}
	if !utf8.Valid(source) {
		return frontmatter{}, []Diagnostic{diagnostic("OKF010", SeverityError, loc, Reference{}, "file is not valid UTF-8")}
	}
	lines := bytes.Split(source, []byte("\n"))
	if len(lines) == 0 || strings.TrimSpace(strings.TrimPrefix(string(lines[0]), "\ufeff")) != "---" {
		return frontmatter{}, nil
	}
	end := -1
	for i := 1; i < len(lines); i++ {
		if strings.TrimSpace(string(lines[i])) == "---" {
			end = i
			break
		}
	}
	if end < 0 {
		return frontmatter{}, []Diagnostic{diagnostic("OKF010", SeverityError, loc, Reference{}, "unterminated YAML frontmatter")}
	}
	payload := bytes.Join(lines[1:end], []byte("\n"))
	var document yaml.Node
	if err := yaml.Unmarshal(payload, &document); err != nil {
		return frontmatter{}, []Diagnostic{diagnostic("OKF010", SeverityError, loc, Reference{}, fmt.Sprintf("invalid YAML frontmatter: %v", err))}
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return frontmatter{present: true}, []Diagnostic{diagnostic("OKF011", SeverityError, loc, Reference{}, "frontmatter must be a YAML mapping")}
	}
	return frontmatter{present: true, root: document.Content[0], lineOffset: 1}, nil
}

func nodeLocation(path string, n *yaml.Node, offset int) Location {
	line, column := 1, 1
	if n != nil {
		line, column = n.Line+offset, n.Column
	}
	return Location{File: path, Line: line, Column: column}
}

func mapValue(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Kind == yaml.ScalarNode && node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}

func conceptType(fm frontmatter) (string, bool) {
	if fm.root == nil {
		return "", false
	}
	typ := mapValue(fm.root, "type")
	if typ == nil || typ.Kind != yaml.ScalarNode {
		return "", false
	}
	value := strings.TrimSpace(typ.Value)
	return value, value != ""
}

func validateConcept(path string, fm frontmatter) []Diagnostic {
	if !fm.present {
		return []Diagnostic{diagnostic("OKF010", SeverityError, Location{File: path, Line: 1, Column: 1}, Reference{}, "concept documents require YAML frontmatter")}
	}
	if fm.root == nil {
		return nil
	} // parsing already emitted OKF011
	if _, ok := conceptType(fm); !ok {
		typ := mapValue(fm.root, "type")
		loc := nodeLocation(path, typ, fm.lineOffset)
		return []Diagnostic{diagnostic("OKF012", SeverityError, loc, Reference{}, "concept frontmatter requires a non-empty scalar type")}
	}
	return nil
}

func scalarReference(path, field, kind string, n *yaml.Node, offset int, ambiguous bool) (Reference, bool) {
	if n == nil || n.Kind != yaml.ScalarNode || n.Tag == "!!null" || strings.TrimSpace(n.Value) == "" {
		return Reference{}, false
	}
	return Reference{Origin: path, Location: nodeLocation(path, n, offset), Kind: kind, Field: field, Target: n.Value, Ambiguous: ambiguous}, true
}

func frontmatterReferences(path string, fm frontmatter) []Reference {
	if fm.root == nil {
		return nil
	}
	var refs []Reference
	add := func(field, kind string, n *yaml.Node, ambiguous bool) {
		if r, ok := scalarReference(path, field, kind, n, fm.lineOffset, ambiguous); ok {
			refs = append(refs, r)
		}
	}
	add("resource", "frontmatter.resource", mapValue(fm.root, "resource"), false)
	add("computation", "frontmatter.computation", mapValue(fm.root, "computation"), false)
	for _, pair := range []struct{ key, field, kind string }{{"executor", "executor.resource", "frontmatter.executor.resource"}, {"attester", "attester.resource", "frontmatter.attester.resource"}} {
		add(pair.field, pair.kind, mapValue(mapValue(fm.root, pair.key), "resource"), false)
	}
	sources := mapValue(fm.root, "sources")
	visitSource := func(node *yaml.Node, field string) {
		resource := mapValue(node, "resource")
		ambiguous := resource != nil && !explicitLocal(resource.Value)
		add(field, "frontmatter.sources.resource", resource, ambiguous)
	}
	if sources != nil && sources.Kind == yaml.MappingNode {
		visitSource(sources, "sources.resource")
	}
	if sources != nil && sources.Kind == yaml.SequenceNode {
		for i, source := range sources.Content {
			if source.Kind == yaml.MappingNode {
				visitSource(source, fmt.Sprintf("sources[%d].resource", i))
			}
		}
	}
	return refs
}
