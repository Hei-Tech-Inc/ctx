package kube

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

type mapGetter map[string]string

func (m mapGetter) Get(_, key string) (string, error) {
	if v, ok := m[key]; ok {
		return v, nil
	}
	return "", errors.New("error: secret not set")
}

const password = "hunter22-pw"

// loginEnv sets CTX_DIR and HOME to temp dirs and puts a fake kubectl on PATH.
func loginEnv(t *testing.T, kubectlScript string) (ctxDir, home, kubectlLog string) {
	t.Helper()
	ctxDir, home = t.TempDir(), t.TempDir()
	t.Setenv("CTX_DIR", ctxDir)
	t.Setenv("HOME", home)
	t.Setenv("KUBECONFIG", filepath.Join(home, ".kube", "config"))
	dir, log := testutil.FakeBin(t, "kubectl", kubectlScript)
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	return ctxDir, home, log
}

func commandStore(steps ...string) *knowledge.Store {
	return &knowledge.Store{Clusters: map[string]knowledge.Cluster{
		"nonprod": {Login: knowledge.Login{Type: "command", Secrets: []string{"TKGI_PASSWORD"}, Run: steps}},
	}}
}

func TestLoginRunsStepsWithEnv(t *testing.T) {
	ctxDir, _, klog := loginEnv(t, `echo "Client Version: v1.30"`)
	rec := filepath.Join(t.TempDir(), "rec")
	s := commandStore(
		`printf 'step1 %s %s\n' "$KUBECONFIG" "$TKGI_PASSWORD" >> `+rec,
		`printf 'step2 %s\n' "$KUBECONFIG" >> `+rec+` && printf 'apiVersion: v1\n' > "$KUBECONFIG"`,
	)
	red := secret.NewRedactor([]string{password})
	out, err := Login(context.Background(), "mtn", s, "nonprod", mapGetter{"TKGI_PASSWORD": password}, red)
	if err != nil {
		t.Fatal(err)
	}
	kc := filepath.Join(ctxDir, "kube", "mtn", "nonprod.yaml")
	data, _ := os.ReadFile(rec)
	want := "step1 " + kc + " " + password + "\nstep2 " + kc + "\n"
	if string(data) != want {
		t.Errorf("steps recorded %q, want %q", data, want)
	}
	if strings.Contains(out, password) {
		t.Errorf("output leaked secret: %q", out)
	}
	wantArgv := [][]string{{"--kubeconfig", kc, "version", "--request-timeout=10s"}}
	if got := testutil.ReadArgv(t, klog); !reflect.DeepEqual(got, wantArgv) {
		t.Errorf("kubectl argv = %q, want %q", got, wantArgv)
	}
	st, err := os.Stat(kc)
	if err != nil {
		t.Fatal(err)
	}
	if st.Mode().Perm() != 0o600 {
		t.Errorf("kubeconfig mode = %v, want 0600", st.Mode().Perm())
	}
	dst, _ := os.Stat(filepath.Dir(kc))
	if dst.Mode().Perm() != 0o700 {
		t.Errorf("kube dir mode = %v, want 0700", dst.Mode().Perm())
	}
}

func TestLoginStopsOnFailure(t *testing.T) {
	_, _, klog := loginEnv(t, `exit 0`)
	rec := filepath.Join(t.TempDir(), "rec")
	s := commandStore(`echo one >> `+rec, `echo boom; exit 2`, `echo three >> `+rec)
	_, err := Login(context.Background(), "mtn", s, "nonprod", mapGetter{"TKGI_PASSWORD": password}, nil)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "boom") {
		t.Errorf("error lacks step output: %v", err)
	}
	data, _ := os.ReadFile(rec)
	if string(data) != "one\n" {
		t.Errorf("steps run = %q, want only step 1", data)
	}
	if got := testutil.ReadArgv(t, klog); got != nil {
		t.Errorf("kubectl ran after failure: %q", got)
	}
}

func TestLoginRedactsStepOutput(t *testing.T) {
	loginEnv(t, `exit 0`)
	s := commandStore(`echo "bad password $TKGI_PASSWORD"; echo "stderr $TKGI_PASSWORD" >&2; exit 1`)
	// The caller's redactor does not know the value: Login must still redact
	// the secrets it handed to the steps.
	out, err := Login(context.Background(), "mtn", s, "nonprod", mapGetter{"TKGI_PASSWORD": password}, secret.NewRedactor(nil))
	if err == nil {
		t.Fatal("expected error")
	}
	if strings.Contains(err.Error(), password) || strings.Contains(out, password) {
		t.Fatalf("secret leaked: err=%q out=%q", err, out)
	}
	if !strings.Contains(err.Error(), "bad password <redacted>") || !strings.Contains(err.Error(), "stderr <redacted>") {
		t.Errorf("error = %q", err)
	}
}

func TestLoginMissingKubeconfigAfterSteps(t *testing.T) {
	loginEnv(t, `exit 0`)
	_, err := Login(context.Background(), "mtn", commandStore(`true`), "nonprod", mapGetter{"TKGI_PASSWORD": password}, nil)
	if err == nil || !strings.Contains(err.Error(), "nonprod.yaml") {
		t.Errorf("err = %v", err)
	}
}

func TestLoginVersionFailure(t *testing.T) {
	loginEnv(t, `echo "error: You must be logged in to the server (Unauthorized)" >&2; exit 1`)
	_, err := Login(context.Background(), "mtn", commandStore(`: > "$KUBECONFIG"`), "nonprod", mapGetter{"TKGI_PASSWORD": password}, nil)
	if err == nil || !strings.Contains(err.Error(), "Unauthorized") {
		t.Errorf("err = %v", err)
	}
}

func TestLoginMissingSecret(t *testing.T) {
	loginEnv(t, `exit 0`)
	_, err := Login(context.Background(), "mtn", commandStore(`true`), "nonprod", mapGetter{}, nil)
	if err == nil || !strings.Contains(err.Error(), "TKGI_PASSWORD") {
		t.Errorf("err = %v", err)
	}
}

func TestLoginUnknownCluster(t *testing.T) {
	loginEnv(t, `exit 0`)
	_, err := Login(context.Background(), "mtn", commandStore(`true`), "nope", mapGetter{}, nil)
	if err == nil || !strings.Contains(err.Error(), "nope") {
		t.Errorf("err = %v", err)
	}
}

func TestLoginNeverTouchesHomeKubeConfig(t *testing.T) {
	_, home, _ := loginEnv(t, `exit 0`)
	s := commandStore(`printf 'apiVersion: v1\n' > "$KUBECONFIG"`)
	if _, err := Login(context.Background(), "mtn", s, "nonprod", mapGetter{"TKGI_PASSWORD": password}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(home, ".kube", "config")); !os.IsNotExist(err) {
		t.Errorf("$HOME/.kube/config exists or stat failed: %v", err)
	}
}

func TestLoginKubeconfigType(t *testing.T) {
	_, home, klog := loginEnv(t, `exit 0`)
	path := filepath.Join(home, "existing.yaml")
	if err := os.WriteFile(path, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	s := &knowledge.Store{Clusters: map[string]knowledge.Cluster{
		"aks": {Login: knowledge.Login{Type: "kubeconfig", Path: path, Context: "acs-aks"}},
	}}
	if _, err := Login(context.Background(), "acs", s, "aks", mapGetter{}, nil); err != nil {
		t.Fatal(err)
	}
	want := [][]string{{"--kubeconfig", path, "--context", "acs-aks", "version", "--request-timeout=10s"}}
	if got := testutil.ReadArgv(t, klog); !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, want %q", got, want)
	}

	s.Clusters["aks"] = knowledge.Cluster{Login: knowledge.Login{Type: "kubeconfig", Path: filepath.Join(home, "missing.yaml"), Context: "c"}}
	if _, err := Login(context.Background(), "acs", s, "aks", mapGetter{}, nil); err == nil || !strings.Contains(err.Error(), "missing.yaml") {
		t.Errorf("missing path err = %v", err)
	}

	s.Clusters["aks"] = knowledge.Cluster{Login: knowledge.Login{Type: "kubeconfig", Path: path}}
	if _, err := Login(context.Background(), "acs", s, "aks", mapGetter{}, nil); err == nil || !strings.Contains(err.Error(), "no context") {
		t.Errorf("no-context err = %v", err)
	}
	if got := testutil.ReadArgv(t, klog); len(got) != 1 {
		t.Errorf("kubectl ran without a context: %q", got)
	}
}
