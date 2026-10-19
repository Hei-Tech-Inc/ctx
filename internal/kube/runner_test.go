package kube

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func TestTargetFor(t *testing.T) {
	ctxDir := t.TempDir()
	t.Setenv("CTX_DIR", ctxDir)
	s := &knowledge.Store{Clusters: map[string]knowledge.Cluster{
		"nonprod": {Login: knowledge.Login{Type: "command", Run: []string{"true"}}},
		"aks":     {Login: knowledge.Login{Type: "kubeconfig", Path: "/k/config", Context: "acs-aks"}},
	}}
	got := TargetFor("mtn", s, knowledge.Env{Name: "preprod", Cluster: "nonprod", Namespace: "subs"})
	want := Target{Kubeconfig: filepath.Join(ctxDir, "kube", "mtn", "nonprod.yaml"), Namespace: "subs"}
	if got != want {
		t.Errorf("command target = %+v, want %+v", got, want)
	}
	got = TargetFor("acs", s, knowledge.Env{Name: "dev", Cluster: "aks", Namespace: "app"})
	want = Target{Kubeconfig: "/k/config", Context: "acs-aks", Namespace: "app"}
	if got != want {
		t.Errorf("kubeconfig target = %+v, want %+v", got, want)
	}
}

func TestArgsAlwaysScoped(t *testing.T) {
	r := &Runner{}
	f := "/x/nonprod.yaml"
	got := r.Args(Target{Kubeconfig: f, Namespace: "ns"}, "get", "pods")
	want := []string{"--kubeconfig", f, "-n", "ns", "get", "pods"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args = %q, want %q", got, want)
	}
	got = r.Args(Target{Kubeconfig: f, Context: "c1", Namespace: "ns"}, "get", "pods")
	want = []string{"--kubeconfig", f, "--context", "c1", "-n", "ns", "get", "pods"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("Args with context = %q, want %q", got, want)
	}
	// C1: args after "--" are the remote command, and =false is not all.
	for _, args := range [][]string{
		{"exec", "mypod", "--", "ls", "-A"},
		{"exec", "mypod", "--", "ls", "--all-namespaces"},
		{"get", "pods", "--all-namespaces=false"},
		{"get", "pods", "-A=false"},
	} {
		got = r.Args(Target{Kubeconfig: f, Namespace: "ns"}, args...)
		want = append([]string{"--kubeconfig", f, "-n", "ns"}, args...)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Args %q = %q, want %q", args, got, want)
		}
	}
	for _, all := range []string{"-A", "--all-namespaces", "--all-namespaces=true"} {
		got = r.Args(Target{Kubeconfig: f, Namespace: "ns"}, "get", "pods", all)
		want = []string{"--kubeconfig", f, "get", "pods", all}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("Args %s = %q, want %q", all, got, want)
		}
	}
}

func fakeKubectl(t *testing.T, script string) (string, string) {
	t.Helper()
	dir, log := testutil.FakeBin(t, "kubectl", script)
	return filepath.Join(dir, "kubectl"), log
}

func kubeconfigFile(t *testing.T) string {
	t.Helper()
	f := filepath.Join(t.TempDir(), "c.yaml")
	if err := os.WriteFile(f, []byte("apiVersion: v1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestRunClearsKUBECONFIG(t *testing.T) {
	t.Setenv("KUBECONFIG", "/bad")
	bin, log := fakeKubectl(t, `printf '[%s]' "$KUBECONFIG"`)
	f := kubeconfigFile(t)
	out, err := (&Runner{Kubectl: bin}).Run(context.Background(), Target{Kubeconfig: f, Namespace: "ns"}, "get", "pods")
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != "[]" {
		t.Errorf("child KUBECONFIG = %q, want empty", out)
	}
	want := [][]string{{"--kubeconfig", f, "-n", "ns", "get", "pods"}}
	if got := testutil.ReadArgv(t, log); !reflect.DeepEqual(got, want) {
		t.Errorf("argv = %q, want %q", got, want)
	}
}

func TestRunRedacts(t *testing.T) {
	bin, _ := fakeKubectl(t, `echo "out hunter22"; echo "err hunter22" >&2; exit 3`)
	f := kubeconfigFile(t)
	r := &Runner{Kubectl: bin, Redact: secret.NewRedactor([]string{"hunter22"})}
	out, err := r.Run(context.Background(), Target{Kubeconfig: f, Namespace: "ns"}, "get", "cm", "hunter22")
	var re *RunError
	if !errors.As(err, &re) {
		t.Fatalf("err = %T %v, want *RunError", err, err)
	}
	if re.Exit != 3 {
		t.Errorf("Exit = %d", re.Exit)
	}
	for _, s := range []string{string(out), err.Error(), re.Stderr, strings.Join(re.Argv, " ")} {
		if strings.Contains(s, "hunter22") {
			t.Errorf("secret leaked: %q", s)
		}
	}
	if !strings.Contains(re.Stderr, "err <redacted>") || !strings.Contains(string(out), "out <redacted>") {
		t.Errorf("stdout %q stderr %q", out, re.Stderr)
	}
}

func TestStreamRedacts(t *testing.T) {
	t.Setenv("KUBECONFIG", "/bad")
	bin, _ := fakeKubectl(t, `printf 'a hunter22 b\n'; printf 'kc=[%s] tail hunter22' "$KUBECONFIG"`)
	f := kubeconfigFile(t)
	var buf bytes.Buffer
	r := &Runner{Kubectl: bin, Redact: secret.NewRedactor([]string{"hunter22"})}
	if err := r.Stream(context.Background(), Target{Kubeconfig: f, Namespace: "ns"}, &buf, "logs", "x"); err != nil {
		t.Fatal(err)
	}
	if got := buf.String(); got != "a <redacted> b\nkc=[] tail <redacted>" {
		t.Errorf("stream = %q", got)
	}
}

func TestRunMapsAuthErrors(t *testing.T) {
	bin, _ := fakeKubectl(t, `echo "error: You must be logged in to the server (Unauthorized)" >&2; exit 1`)
	f := kubeconfigFile(t)
	r := &Runner{Kubectl: bin}
	_, err := r.Run(context.Background(), Target{Kubeconfig: f, Namespace: "ns"}, "get", "pods")
	aerr := AsAuthError(err, "preprod")
	if !errors.Is(aerr, ErrNotLoggedIn) {
		t.Fatalf("AsAuthError = %v, want ErrNotLoggedIn", aerr)
	}
	if aerr.Error() != `not logged in to preprod: call login(env="preprod")` {
		t.Errorf("message = %q", aerr.Error())
	}

	// A missing kubeconfig file gives the same error, without running kubectl.
	bin2, log2 := fakeKubectl(t, `exit 0`)
	_, err = (&Runner{Kubectl: bin2}).Run(context.Background(), Target{Kubeconfig: filepath.Join(t.TempDir(), "nope.yaml"), Namespace: "ns"}, "get", "pods")
	aerr = AsAuthError(err, "preprod")
	if !errors.Is(aerr, ErrNotLoggedIn) || aerr.Error() != `not logged in to preprod: call login(env="preprod")` {
		t.Errorf("missing kubeconfig: %v", aerr)
	}
	if got := testutil.ReadArgv(t, log2); got != nil {
		t.Errorf("kubectl ran with missing kubeconfig: %q", got)
	}

	// Other errors pass through unchanged.
	bin3, _ := fakeKubectl(t, `echo 'Error from server (NotFound): pods "x" not found' >&2; exit 1`)
	_, err = (&Runner{Kubectl: bin3}).Run(context.Background(), Target{Kubeconfig: f, Namespace: "ns"}, "get", "pod", "x")
	if aerr := AsAuthError(err, "preprod"); aerr != err || errors.Is(aerr, ErrNotLoggedIn) {
		t.Errorf("non-auth error mapped: %v", aerr)
	}
	if AsAuthError(nil, "preprod") != nil {
		t.Error("AsAuthError(nil) != nil")
	}
}

func TestRunnerObserveSeesRedactedArgv(t *testing.T) {
	bin, _ := fakeKubectl(t, `echo ok`)
	f := kubeconfigFile(t)
	var seen [][]string
	r := &Runner{Kubectl: bin, Redact: secret.NewRedactor([]string{"hunter22"}),
		Observe: func(argv []string) { seen = append(seen, argv) }}
	tg := Target{Kubeconfig: f, Namespace: "ns"}
	if _, err := r.Run(context.Background(), tg, "get", "cm", "hunter22"); err != nil {
		t.Fatal(err)
	}
	if err := r.Stream(context.Background(), tg, io.Discard, "logs", "x"); err != nil {
		t.Fatal(err)
	}
	// refused and not-run calls are not observed
	_, _ = r.Run(context.Background(), tg, "get", "pods", "--context", "x")
	_, _ = r.Run(context.Background(), Target{Kubeconfig: f + ".missing", Namespace: "ns"}, "get", "pods")
	want := [][]string{
		{bin, "--kubeconfig", f, "-n", "ns", "get", "cm", "<redacted>"},
		{bin, "--kubeconfig", f, "-n", "ns", "logs", "x"},
	}
	if !reflect.DeepEqual(seen, want) {
		t.Errorf("observed = %q, want %q", seen, want)
	}
}
