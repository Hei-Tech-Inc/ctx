package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Hit is one matching line from Search.
type Hit struct {
	File string // relative to the knowledge root
	Line int
	Text string
}

func parseRecipe(file string, data []byte) (Recipe, error) {
	fail := func(msg string) (Recipe, error) { return Recipe{}, &LoadError{File: file, Msg: msg} }
	rest, ok := bytes.CutPrefix(data, []byte("---\n"))
	if !ok {
		return fail("missing front matter (file must start with ---)")
	}
	var fm []byte
	var body []byte
	if bytes.HasPrefix(rest, []byte("---\n")) {
		body = rest[4:]
	} else {
		i := bytes.Index(rest, []byte("\n---\n"))
		if i < 0 {
			if bytes.HasSuffix(rest, []byte("\n---")) {
				fm = rest[:len(rest)-3]
			} else {
				return fail("front matter is not closed with ---")
			}
		} else {
			fm, body = rest[:i+1], rest[i+5:]
		}
	}
	var r Recipe
	dec := yaml.NewDecoder(bytes.NewReader(fm))
	dec.KnownFields(true)
	if err := dec.Decode(&r); err != nil && !errors.Is(err, io.EOF) {
		le := yamlError(file, err)
		if le.Line > 0 {
			le.Line++ // account for the opening ---
		}
		return Recipe{}, le
	}
	if r.Name == "" {
		r.Name = strings.TrimSuffix(filepath.Base(file), ".md")
	}
	r.Body = string(body)
	return r, nil
}

// Recipes returns all recipes, sorted by name.
func (s *Store) Recipes() ([]Recipe, error) {
	files, err := filepath.Glob(filepath.Join(s.Root, "recipes", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	var out []Recipe
	for _, f := range files {
		data, err := os.ReadFile(f)
		if err != nil {
			return nil, err
		}
		r, err := parseRecipe(filepath.Join("recipes", filepath.Base(f)), data)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, nil
}

// GetRecipe returns the recipe stored as recipes/<name>.md.
func (s *Store) GetRecipe(name string) (Recipe, error) {
	if err := validName("recipe", name); err != nil {
		return Recipe{}, err
	}
	data, err := os.ReadFile(filepath.Join(s.Root, "recipes", name+".md"))
	if errors.Is(err, os.ErrNotExist) {
		return Recipe{}, fmt.Errorf("unknown recipe %q", name)
	}
	if err != nil {
		return Recipe{}, err
	}
	return parseRecipe(filepath.Join("recipes", name+".md"), data)
}

// Search does a case-insensitive substring match over recipes and the YAML
// files, returning one Hit per matching line.
func (s *Store) Search(query string) ([]Hit, error) {
	if query == "" {
		return nil, nil
	}
	q := strings.ToLower(query)
	files := []string{"clusters.yaml", "environments.yaml", "services.yaml"}
	recipes, err := filepath.Glob(filepath.Join(s.Root, "recipes", "*.md"))
	if err != nil {
		return nil, err
	}
	sort.Strings(recipes)
	for _, r := range recipes {
		files = append(files, filepath.Join("recipes", filepath.Base(r)))
	}
	var hits []Hit
	for _, f := range files {
		data, err := os.ReadFile(filepath.Join(s.Root, f))
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for i, line := range strings.Split(string(data), "\n") {
			if strings.Contains(strings.ToLower(line), q) {
				hits = append(hits, Hit{File: f, Line: i + 1, Text: line})
			}
		}
	}
	return hits, nil
}
