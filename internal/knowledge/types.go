// Package knowledge is the per-profile knowledge store: clusters,
// environments, services, recipes, scripts and the activity log.
package knowledge

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
)

// Store is the loaded contents of one profile's knowledge directory.
type Store struct {
	Root        string
	Clusters    map[string]Cluster
	Envs        map[string]Env
	Services    map[string]Service
	NetworkTool NetworkTool
}

// Cluster describes how to log in to one cluster.
type Cluster struct {
	Login Login `yaml:"login"`
}

// Login is a cluster login. Type is "command" or "kubeconfig".
type Login struct {
	Type    string   `yaml:"type"`
	Secrets []string `yaml:"secrets,omitempty"`
	Run     []string `yaml:"run,omitempty"`
	Path    string   `yaml:"path,omitempty"`
	Context string   `yaml:"context,omitempty"`
}

// Env maps an environment name to a cluster and namespace. Name is the map
// key and is filled in on load.
type Env struct {
	Name        string       `yaml:"-"`
	Cluster     string       `yaml:"cluster"`
	Namespace   string       `yaml:"namespace"`
	Aliases     []string     `yaml:"aliases,omitempty"`
	Protected   bool         `yaml:"protected,omitempty"`
	NetworkTool *NetworkTool `yaml:"network_tool,omitempty"`
}

// Service is a workload with nicknames. Name is the map key.
type Service struct {
	Name    string   `yaml:"-"`
	Kind    string   `yaml:"kind,omitempty"` // deployment (default) or statefulset
	Aliases []string `yaml:"aliases,omitempty"`
}

// NetworkTool locates the pod used by network_test.
type NetworkTool struct {
	Namespace string `yaml:"namespace,omitempty"`
	Selector  string `yaml:"selector,omitempty"`
	Container string `yaml:"container,omitempty"`
}

// Recipe is a runbook: YAML front matter plus a markdown body.
type Recipe struct {
	Name         string   `yaml:"name"`
	Description  string   `yaml:"description,omitempty"`
	Tags         []string `yaml:"tags,omitempty"`
	Params       []string `yaml:"params,omitempty"`
	ChangesState bool     `yaml:"changes_state"`
	Body         string   `yaml:"-"`
}

// LoadError is a problem in one knowledge file. Line is 0 when unknown.
type LoadError struct {
	File string
	Line int
	Msg  string
}

func (e *LoadError) Error() string {
	if e.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", e.File, e.Line, e.Msg)
	}
	return fmt.Sprintf("%s: %s", e.File, e.Msg)
}

// Root returns $CTX_DIR/knowledge/<profile>, with CTX_DIR defaulting to ~/.ctx.
func Root(profile string) string {
	base := os.Getenv("CTX_DIR")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".ctx")
	}
	return filepath.Join(base, "knowledge", profile)
}

var nameRE = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,62}$`)

func validName(kind, name string) error {
	if !nameRE.MatchString(name) {
		return fmt.Errorf("invalid %s name %q: use lowercase letters, digits and '-' (max 63 chars)", kind, name)
	}
	return nil
}
