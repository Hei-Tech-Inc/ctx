package knowledge

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type clustersFile struct {
	Clusters map[string]Cluster `yaml:"clusters"`
}

type envsFile struct {
	Environments map[string]Env `yaml:"environments"`
	NetworkTool  NetworkTool    `yaml:"network_tool"`
}

type servicesFile struct {
	Services map[string]Service `yaml:"services"`
}

var yamlLineRE = regexp.MustCompile(`line (\d+)`)

// decodeFile strictly decodes file under root into out. A missing file is
// not an error (found == false). It returns the parsed node for line lookups.
func decodeFile(root, file string, out any) (doc *yaml.Node, found bool, err error) {
	data, err := os.ReadFile(filepath.Join(root, file))
	if errors.Is(err, os.ErrNotExist) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, &LoadError{File: file, Msg: err.Error()}
	}
	doc = &yaml.Node{}
	if err := yaml.Unmarshal(data, doc); err != nil {
		return nil, true, yamlError(file, err)
	}
	if doc.Kind == 0 { // empty file
		return doc, true, nil
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(out); err != nil && !errors.Is(err, io.EOF) {
		return doc, true, yamlError(file, err)
	}
	return doc, true, nil
}

func yamlError(file string, err error) *LoadError {
	msg := strings.TrimPrefix(err.Error(), "yaml: ")
	le := &LoadError{File: file, Msg: msg}
	if m := yamlLineRE.FindStringSubmatch(msg); m != nil {
		le.Line, _ = strconv.Atoi(m[1])
	}
	return le
}

// keyLine returns the line of the entry key under top-level key section.
func keyLine(doc *yaml.Node, section, key string) int {
	if doc == nil || len(doc.Content) == 0 {
		return 0
	}
	top := doc.Content[0]
	for i := 0; i+1 < len(top.Content); i += 2 {
		if top.Content[i].Value != section {
			continue
		}
		if key == "" {
			return top.Content[i].Line
		}
		m := top.Content[i+1]
		for j := 0; j+1 < len(m.Content); j += 2 {
			if m.Content[j].Value == key {
				return m.Content[j].Line
			}
		}
		return top.Content[i].Line
	}
	return 0
}

// Load reads the knowledge directory at root. A missing directory or missing
// files give an empty store.
func Load(root string) (*Store, error) {
	s := &Store{Root: root, Clusters: map[string]Cluster{}, Envs: map[string]Env{}, Services: map[string]Service{}}

	var cf clustersFile
	cdoc, _, err := decodeFile(root, "clusters.yaml", &cf)
	if err != nil {
		return nil, err
	}
	for _, name := range sortedKeys(cf.Clusters) {
		c := cf.Clusters[name]
		fail := func(msg string) error {
			return &LoadError{File: "clusters.yaml", Line: keyLine(cdoc, "clusters", name), Msg: fmt.Sprintf("cluster %q: %s", name, msg)}
		}
		switch c.Login.Type {
		case "command":
			if len(c.Login.Run) == 0 {
				return nil, fail("login type command needs a non-empty run list")
			}
		case "kubeconfig":
		default:
			return nil, fail(fmt.Sprintf("unknown login type %q (use command or kubeconfig)", c.Login.Type))
		}
		s.Clusters[name] = c
	}

	var ef envsFile
	edoc, _, err := decodeFile(root, "environments.yaml", &ef)
	if err != nil {
		return nil, err
	}
	s.NetworkTool = ef.NetworkTool
	owner := map[string]string{} // name or alias -> env that owns it
	for _, name := range sortedKeys(ef.Environments) {
		owner[name] = name
	}
	for _, name := range sortedKeys(ef.Environments) {
		e := ef.Environments[name]
		e.Name = name
		line := keyLine(edoc, "environments", name)
		fail := func(msg string) error {
			return &LoadError{File: "environments.yaml", Line: line, Msg: fmt.Sprintf("environment %q: %s", name, msg)}
		}
		if _, ok := s.Clusters[e.Cluster]; !ok {
			return nil, fail(fmt.Sprintf("unknown cluster %q", e.Cluster))
		}
		if e.Namespace == "" {
			return nil, fail("namespace is required")
		}
		for _, a := range e.Aliases {
			if prev, dup := owner[a]; dup {
				return nil, &LoadError{File: "environments.yaml", Line: line, Msg: fmt.Sprintf("alias %q is used by both %q and %q", a, prev, name)}
			}
			owner[a] = name
		}
		s.Envs[name] = e
	}

	var sf servicesFile
	sdoc, _, err := decodeFile(root, "services.yaml", &sf)
	if err != nil {
		return nil, err
	}
	sowner := map[string]string{}
	for _, name := range sortedKeys(sf.Services) {
		sowner[name] = name
	}
	for _, name := range sortedKeys(sf.Services) {
		v := sf.Services[name]
		v.Name = name
		line := keyLine(sdoc, "services", name)
		switch v.Kind {
		case "":
			v.Kind = "deployment"
		case "deployment", "statefulset":
		default:
			return nil, &LoadError{File: "services.yaml", Line: line, Msg: fmt.Sprintf("service %q: unknown kind %q (use deployment or statefulset)", name, v.Kind)}
		}
		for _, a := range v.Aliases {
			if prev, dup := sowner[a]; dup {
				return nil, &LoadError{File: "services.yaml", Line: line, Msg: fmt.Sprintf("alias %q is used by both %q and %q", a, prev, name)}
			}
			sowner[a] = name
		}
		s.Services[name] = v
	}
	return s, nil
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func describe(names []string, aliases func(string) []string) string {
	parts := make([]string, 0, len(names))
	for _, n := range names {
		if a := aliases(n); len(a) > 0 {
			n += " (" + strings.Join(a, ", ") + ")"
		}
		parts = append(parts, n)
	}
	return strings.Join(parts, ", ")
}

// Env finds an environment by name or alias.
func (s *Store) Env(nameOrAlias string) (Env, error) {
	if e, ok := s.Envs[nameOrAlias]; ok {
		return e, nil
	}
	for _, n := range sortedKeys(s.Envs) {
		for _, a := range s.Envs[n].Aliases {
			if a == nameOrAlias {
				return s.Envs[n], nil
			}
		}
	}
	if len(s.Envs) == 0 {
		return Env{}, fmt.Errorf("unknown environment %q: no environments are defined (run ctx knowledge init and edit environments.yaml)", nameOrAlias)
	}
	return Env{}, fmt.Errorf("unknown environment %q; valid: %s", nameOrAlias,
		describe(sortedKeys(s.Envs), func(n string) []string { return s.Envs[n].Aliases }))
}

// EnvForChange is Env for change tools: a protected environment must be
// named in full, never by alias.
func (s *Store) EnvForChange(name string) (Env, error) {
	e, err := s.Env(name)
	if err != nil {
		return Env{}, err
	}
	if e.Protected && e.Name != name {
		return Env{}, fmt.Errorf("environment %q is protected: use the full environment name %q", name, e.Name)
	}
	return e, nil
}

// Service finds a service by name or alias.
func (s *Store) Service(nameOrAlias string) (Service, error) {
	if v, ok := s.Services[nameOrAlias]; ok {
		return v, nil
	}
	for _, n := range sortedKeys(s.Services) {
		for _, a := range s.Services[n].Aliases {
			if a == nameOrAlias {
				return s.Services[n], nil
			}
		}
	}
	if len(s.Services) == 0 {
		return Service{}, fmt.Errorf("unknown service %q: no services are defined (use discover_services and save_service)", nameOrAlias)
	}
	return Service{}, fmt.Errorf("unknown service %q; valid: %s", nameOrAlias,
		describe(sortedKeys(s.Services), func(n string) []string { return s.Services[n].Aliases }))
}

// SecretKeys returns the sorted union of every cluster's login secrets.
func (s *Store) SecretKeys() []string {
	seen := map[string]bool{}
	var out []string
	for _, c := range s.Clusters {
		for _, k := range c.Login.Secrets {
			if !seen[k] {
				seen[k] = true
				out = append(out, k)
			}
		}
	}
	sort.Strings(out)
	return out
}
