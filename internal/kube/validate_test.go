package kube

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func TestValidateUserArgs(t *testing.T) {
	const flagMsg = "is not allowed: ctx sets the cluster and namespace from the environment"
	cases := []struct {
		args string
		want string // "" = allowed; otherwise a substring of the error
	}{
		{"get pods", ""},
		{"get pods -A", ""},
		{"get pods --all-namespaces", ""},
		{"logs deploy/subs -c app -f", ""},
		{"exec -it pod -- sh -c kubectl --token x -n y", ""}, // after -- is the remote command
		{"get pods -l app=subs -ojson", ""},
		{"get pods -o jsonpath={.items[*].metadata.name}", ""}, // not secrets
		{"get secret x -o yaml", ""},
		{"get secrets -o json", ""},
		{"get secret x -oyaml", ""},
		{"get secret x --output=json", ""},
		{"get secret -o wide", ""},
		{"get secret -o name", ""},
		{"get secret", ""},

		{"get pods --kubeconfig /x", "flag --kubeconfig " + flagMsg},
		{"get pods --kubeconfig=/x", "flag --kubeconfig " + flagMsg},
		{"get pods --context other", "flag --context " + flagMsg},
		{"get pods -n kube-system", "flag -n " + flagMsg},
		{"get pods -nkube-system", "flag -n " + flagMsg},
		{"get pods -n=kube-system", "flag -n " + flagMsg},
		{"get pods -in kube-system", "flag -n " + flagMsg},
		{"get pods --namespace kube-system", "flag --namespace " + flagMsg},
		{"get pods --namespace=kube-system", "flag --namespace " + flagMsg},
		{"get pods --cluster c", "flag --cluster " + flagMsg},
		{"get pods --user u", "flag --user " + flagMsg},
		{"get pods -s https://x", "flag -s " + flagMsg},
		{"get pods -shttps://x", "flag -s " + flagMsg},
		{"get pods --server=https://x", "flag --server " + flagMsg},
		{"get pods --token t", "flag --token " + flagMsg},
		{"get pods --as admin", "flag --as " + flagMsg},
		{"get pods --as-group=g", "flag --as-group " + flagMsg},
		{"get pods --as-uid 1", "flag --as-uid " + flagMsg},
		{"get pods --certificate-authority ca", "flag --certificate-authority " + flagMsg},
		{"get pods --client-certificate c", "flag --client-certificate " + flagMsg},
		{"get pods --client-key k", "flag --client-key " + flagMsg},
		{"get pods --insecure-skip-tls-verify", "flag --insecure-skip-tls-verify " + flagMsg},
		{"get pods --username u", "flag --username " + flagMsg},
		{"get pods --password p", "flag --password " + flagMsg},

		{"get secret x -o jsonpath={.data.password}", "output format jsonpath is not allowed for secrets"},
		{"get secret x -o=jsonpath={.data}", "output format jsonpath is not allowed for secrets"},
		{"get secret x -ojsonpath={.data}", "output format jsonpath is not allowed for secrets"},
		{"get secrets --output go-template={{.data}}", "output format go-template is not allowed for secrets"},
		{"get secret/x --output=go-template-file=f", "output format go-template-file is not allowed for secrets"},
		{"get Secret x -o template={{.data}}", "output format template is not allowed for secrets"},
		{"get secrets/x -o templatefile=f", "output format templatefile is not allowed for secrets"},
		{"get secret -o custom-columns=D:.data", "output format custom-columns is not allowed for secrets"},
		{"get secret -o custom-columns-file=f", "output format custom-columns-file is not allowed for secrets"},
		{"get secret -o jsonpath-file=f", "output format jsonpath-file is not allowed for secrets"},
		{"get cm,secret -o jsonpath={..data}", "output format jsonpath is not allowed for secrets"},
		{"get secret x --template={{.data}}", "output format go-template is not allowed for secrets"},

		// Review fix round 1.
		// C2: a bool shorthand before n/s in a group must not hide them.
		{"logs deploy/x -fn kube-system", "flag -n " + flagMsg},
		{"logs x -fs https://attacker", "flag -s " + flagMsg},
		{"logs x -pfn kube-system", "flag -n " + flagMsg},
		{"get pods -wn=kube-system", "flag -n " + flagMsg},
		{"logs x -f", ""},
		{"logs x -c sidecar", ""},
		{"logs x -csidecar", ""},
		{"get pods -lservice=subs", ""},
		{"get pods -ojsonpath={.items[*].metadata.name}", ""},
		// C3: secret references after "--" still count.
		{"get -o jsonpath={.data} -- secret x", "output format jsonpath is not allowed for secrets"},
		{"get -o yaml -- secret x", ""},
		// C4: --raw ignores -n.
		{"get --raw /api/v1/namespaces/kube-system/secrets", "flag --raw " + flagMsg},
		{"get --raw=/api/v1/namespaces/kube-system/secrets", "flag --raw " + flagMsg},
		// Objects from files could be Secrets.
		{"get -f x.yaml -o jsonpath={.data}", "output format jsonpath is not allowed for secrets"},
		{"get --filename=x.yaml -o go-template={{.data}}", "output format go-template is not allowed for secrets"},
		{"get -f x.yaml -o yaml", ""},
	}
	for _, c := range cases {
		err := ValidateUserArgs(strings.Fields(c.args))
		switch {
		case c.want == "" && err != nil:
			t.Errorf("%q: unexpected error %v", c.args, err)
		case c.want != "" && err == nil:
			t.Errorf("%q: allowed, want error %q", c.args, c.want)
		case c.want != "" && err.Error() != c.want:
			t.Errorf("%q: error %q, want %q", c.args, err, c.want)
		}
	}
}

func TestRunRejectsUserArgsWithoutExecuting(t *testing.T) {
	bin, log := fakeKubectl(t, `echo ran`)
	f := kubeconfigFile(t)
	r := &Runner{Kubectl: bin}
	target := Target{Kubeconfig: f, Namespace: "ns"}
	if _, err := r.Run(context.Background(), target, "get", "pods", "-n", "kube-system"); err == nil {
		t.Error("Run allowed -n")
	}
	var buf bytes.Buffer
	if err := r.Stream(context.Background(), target, &buf, "get", "secret", "x", "-o", "jsonpath={.data}"); err == nil {
		t.Error("Stream allowed jsonpath on secrets")
	}
	if got := testutil.ReadArgv(t, log); got != nil {
		t.Errorf("kubectl ran: %q", got)
	}
}
