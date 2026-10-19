package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// writeAtomic writes data to path via a temp file in the same directory and
// a rename. Files are 0600 and parent directories 0700.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	name := tmp.Name()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		_ = os.Remove(name)
		return err
	}
	if err := tmp.Close(); err != nil {
		_ = os.Remove(name)
		return err
	}
	if err := os.Rename(name, path); err != nil {
		_ = os.Remove(name)
		return err
	}
	return nil
}

// SaveRecipe writes recipes/<name>.md and returns its path.
func SaveRecipe(root string, r Recipe) (string, error) {
	if err := validName("recipe", r.Name); err != nil {
		return "", err
	}
	fm, err := yaml.Marshal(r)
	if err != nil {
		return "", err
	}
	var b bytes.Buffer
	b.WriteString("---\n")
	b.Write(fm)
	b.WriteString("---\n")
	b.WriteString(r.Body)
	path := filepath.Join(root, "recipes", r.Name+".md")
	return path, writeAtomic(path, b.Bytes())
}

// SaveScript writes scripts/<name> (name may end in .sh) and returns its path.
func SaveScript(root, name, body string) (string, error) {
	if err := validName("script", strings.TrimSuffix(name, ".sh")); err != nil {
		return "", err
	}
	path := filepath.Join(root, "scripts", name)
	return path, writeAtomic(path, []byte(body))
}

// SaveService adds svc to services.yaml, merging aliases into an existing
// entry. Comments and other entries in the file are preserved.
func SaveService(root string, svc Service) error {
	if err := validName("service", svc.Name); err != nil {
		return err
	}
	switch svc.Kind {
	case "", "deployment", "statefulset":
	default:
		return fmt.Errorf("unknown kind %q (use deployment or statefulset)", svc.Kind)
	}
	path := filepath.Join(root, "services.yaml")
	data, err := os.ReadFile(path)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return fmt.Errorf("services.yaml: %w", err)
	}
	if doc.Kind == 0 {
		doc = yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{{Kind: yaml.MappingNode}}}
	}
	top := doc.Content[0]
	if top.Kind != yaml.MappingNode {
		return errors.New("services.yaml: top level is not a mapping")
	}
	var section *yaml.Node
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value == "services" {
			section = top.Content[i+1]
		}
	}
	if section == nil {
		section = &yaml.Node{Kind: yaml.MappingNode}
		top.Content = append(top.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: "services"}, section)
	}
	if section.Kind != yaml.MappingNode {
		return errors.New("services.yaml: services is not a mapping")
	}

	merged := Service{Kind: svc.Kind, Aliases: svc.Aliases}
	idx := -1
	for i := 0; i+1 < len(section.Content); i += 2 {
		if section.Content[i].Value == svc.Name {
			idx = i + 1
		}
	}
	if idx >= 0 {
		var old Service
		if err := section.Content[idx].Decode(&old); err != nil {
			return fmt.Errorf("services.yaml: %w", err)
		}
		merged.Aliases = mergeAliases(old.Aliases, svc.Aliases)
		if merged.Kind == "" {
			merged.Kind = old.Kind
		}
	}
	if merged.Kind == "" {
		merged.Kind = "deployment"
	}
	var val yaml.Node
	if err := val.Encode(merged); err != nil {
		return err
	}
	val.Style = yaml.FlowStyle
	if idx >= 0 {
		section.Content[idx] = &val
	} else {
		section.Style = 0
		section.Content = append(section.Content, &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: svc.Name}, &val)
	}
	out, err := yaml.Marshal(&doc)
	if err != nil {
		return err
	}
	// Refuse to write a file that would not load (e.g. alias clashes).
	tmpRoot, err := os.MkdirTemp("", "ctx-knowledge-check-")
	if err != nil {
		return err
	}
	defer func() { _ = os.RemoveAll(tmpRoot) }()
	if err := os.WriteFile(filepath.Join(tmpRoot, "services.yaml"), out, 0o600); err != nil {
		return err
	}
	if _, err := Load(tmpRoot); err != nil {
		return err
	}
	return writeAtomic(path, out)
}

func mergeAliases(a, b []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, x := range append(append([]string{}, a...), b...) {
		if !seen[x] {
			seen[x] = true
			out = append(out, x)
		}
	}
	sort.Strings(out)
	return out
}
