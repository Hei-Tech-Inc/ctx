package knowledge

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	root := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(root, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return root
}

func loadErr(t *testing.T, files map[string]string) *LoadError {
	t.Helper()
	_, err := Load(writeFiles(t, files))
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("err = %v, want *LoadError", err)
	}
	return le
}

func TestLoadSpecExample(t *testing.T) {
	s, err := Load("testdata/spec")
	if err != nil {
		t.Fatal(err)
	}
	e, err := s.Env("pp")
	if err != nil || e.Name != "preprod" || e.Cluster != "nonprod" || e.Namespace != "madapi-preprod" {
		t.Fatalf("Env(pp) = %+v, %v", e, err)
	}
	svc, err := s.Service("subs")
	if err != nil || svc.Name != "subscription-cis-system" {
		t.Fatalf("Service(subs) = %+v, %v", svc, err)
	}
	if got := s.SecretKeys(); !reflect.DeepEqual(got, []string{"TKGI_PASSWORD"}) {
		t.Fatalf("SecretKeys = %v", got)
	}
	if s.NetworkTool.Selector != "app=network-tool" || s.NetworkTool.Namespace != "network-utility" {
		t.Fatalf("NetworkTool = %+v", s.NetworkTool)
	}
	if !s.Envs["prod"].Protected || s.Clusters["prod"].Login.Type != "command" {
		t.Fatalf("prod = %+v", s.Envs["prod"])
	}
}

func TestLoadEmptyDir(t *testing.T) {
	for _, root := range []string{t.TempDir(), filepath.Join(t.TempDir(), "missing")} {
		s, err := Load(root)
		if err != nil {
			t.Fatal(err)
		}
		if len(s.Clusters)+len(s.Envs)+len(s.Services) != 0 {
			t.Fatalf("store not empty: %+v", s)
		}
	}
}

const okCluster = "clusters:\n  c:\n    login: {type: command, run: [\"true\"]}\n"

func TestLoadRejectsDuplicateAlias(t *testing.T) {
	le := loadErr(t, map[string]string{"clusters.yaml": okCluster, "environments.yaml": "environments:\n  a: {cluster: c, namespace: x, aliases: [z]}\n  b: {cluster: c, namespace: y, aliases: [z]}\n"})
	if !strings.Contains(le.Msg, `"z"`) || !strings.Contains(le.Msg, "a") || !strings.Contains(le.Msg, "b") {
		t.Fatalf("msg = %q", le.Msg)
	}
	le = loadErr(t, map[string]string{"services.yaml": "services:\n  s1: {aliases: [q]}\n  s2: {aliases: [q]}\n"})
	if !strings.Contains(le.Msg, "s1") || !strings.Contains(le.Msg, "s2") {
		t.Fatalf("msg = %q", le.Msg)
	}
	le = loadErr(t, map[string]string{"clusters.yaml": okCluster, "environments.yaml": "environments:\n  a: {cluster: c, namespace: x}\n  b: {cluster: c, namespace: y, aliases: [a]}\n"})
	if !strings.Contains(le.Msg, "a") || !strings.Contains(le.Msg, "b") {
		t.Fatalf("msg = %q", le.Msg)
	}
}

func TestLoadRejectsBadReferences(t *testing.T) {
	le := loadErr(t, map[string]string{"clusters.yaml": okCluster, "environments.yaml": "environments:\n  a: {cluster: nope, namespace: x}\n"})
	if le.File != "environments.yaml" || !strings.Contains(le.Msg, "nope") {
		t.Fatalf("le = %+v", le)
	}
	le = loadErr(t, map[string]string{"clusters.yaml": "clusters:\n  c:\n    login: {type: tkgi}\n"})
	if le.File != "clusters.yaml" || !strings.Contains(le.Msg, "tkgi") {
		t.Fatalf("le = %+v", le)
	}
	le = loadErr(t, map[string]string{"clusters.yaml": "clusters:\n  c:\n    login: {type: command}\n"})
	if !strings.Contains(le.Msg, "run") {
		t.Fatalf("le = %+v", le)
	}
}

func TestLoadErrorHasLine(t *testing.T) {
	le := loadErr(t, map[string]string{"services.yaml": "services:\n  a: {kind: deployment\n  b: x\n"})
	if le.File != "services.yaml" || le.Line <= 0 {
		t.Fatalf("le = %+v", le)
	}
}

func TestEnvForChangeProtected(t *testing.T) {
	s, err := Load(writeFiles(t, map[string]string{"clusters.yaml": okCluster, "environments.yaml": "environments:\n  prod: {cluster: c, namespace: p, protected: true, aliases: [live]}\n  dev: {cluster: c, namespace: d, aliases: [d]}\n"}))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnvForChange("prod"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnvForChange("live"); err == nil || !strings.Contains(err.Error(), `use the full environment name "prod"`) {
		t.Fatalf("err = %v", err)
	}
	if e, err := s.EnvForChange("d"); err != nil || e.Name != "dev" {
		t.Fatalf("got %+v, %v", e, err)
	}
}

func TestUnknownEnvListsChoices(t *testing.T) {
	s, err := Load("testdata/spec")
	if err != nil {
		t.Fatal(err)
	}
	_, err = s.Env("staging")
	if err == nil || !strings.Contains(err.Error(), "preprod (pp, nonprod)") || !strings.Contains(err.Error(), "dev") {
		t.Fatalf("err = %v", err)
	}
	_, err = s.Service("zzz")
	if err == nil || !strings.Contains(err.Error(), "subscription-cis-system (subs, subscription, cis)") {
		t.Fatalf("err = %v", err)
	}
}
