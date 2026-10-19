package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// elicitor is a test client's elicitation handler: it records each
// request's message and answers from a script (the last answer repeats).
type elicitor struct {
	mu       sync.Mutex
	messages []string
	answers  []string // "accept", "accept-false", "decline", "cancel", "hang"
}

func (e *elicitor) handle(ctx context.Context, req *mcp.ElicitRequest) (*mcp.ElicitResult, error) {
	e.mu.Lock()
	e.messages = append(e.messages, req.Params.Message)
	answer := "decline"
	if n := len(e.answers); n > 0 {
		answer = e.answers[0]
		if n > 1 {
			e.answers = e.answers[1:]
		}
	}
	e.mu.Unlock()
	switch answer {
	case "accept":
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": true}}, nil
	case "accept-false":
		return &mcp.ElicitResult{Action: "accept", Content: map[string]any{"confirm": false}}, nil
	case "hang":
		select {
		case <-ctx.Done():
		case <-time.After(10 * time.Second):
		}
		return nil, errors.New("no answer")
	}
	return &mcp.ElicitResult{Action: answer}, nil
}

func (e *elicitor) sent() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.messages...)
}

// connectAsking connects a client whose elicitation handler is e.
func connectAsking(t *testing.T, f *fixture, e *elicitor) *mcp.ClientSession {
	t.Helper()
	return connectClient(t, f.deps(), Options{Profile: "acme"}, nil, &mcp.ClientOptions{ElicitationHandler: e.handle})
}

const changeCases = `*"rollout restart deployment/subscription-cis-system"*) echo 'deployment.apps/subscription-cis-system restarted' ;;
*"rollout status deployment/subscription-cis-system --timeout=300s"*) echo 'successfully rolled out' ;;
`

func activityResults(t *testing.T, f *fixture, tool string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(f.ctxDir, "knowledge", "acme", "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, ln := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var e struct{ Tool, Result string }
		if err := json.Unmarshal([]byte(ln), &e); err != nil {
			t.Fatal(err)
		}
		if e.Tool == tool {
			out = append(out, e.Result)
		}
	}
	return out
}

func TestRestartAcceptedRuns(t *testing.T) {
	f := newFixture(t, changeCases)
	f.acme()
	e := &elicitor{answers: []string{"accept"}}
	cs := connectAsking(t, f, e)
	text, isErr := call(t, cs, "restart", map[string]any{"env": "pp", "service": "subs"})
	if isErr || !strings.Contains(text, "restarted") || !strings.Contains(text, "successfully rolled out") {
		t.Fatalf("restart = %v %q", isErr, text)
	}
	calls := testutil.ReadArgv(t, f.kubectlLog)
	if len(calls) != 2 {
		t.Fatalf("calls = %q", calls)
	}
	kc := filepath.Join(f.ctxDir, "kube", "acme", "nonprod.yaml")
	for i, want := range []string{"rollout restart deployment/subscription-cis-system", "rollout status deployment/subscription-cis-system --timeout=300s"} {
		j := strings.Join(calls[i], " ")
		if !strings.Contains(j, want) || !strings.Contains(j, "--kubeconfig "+kc) || !strings.Contains(j, "-n madapi-preprod") {
			t.Errorf("call %d = %q, want %q scoped", i, j, want)
		}
	}
	msgs := e.sent()
	if len(msgs) != 1 {
		t.Fatalf("elicitations = %q", msgs)
	}
	for _, want := range []string{"acme", "preprod", "nonprod", "madapi-preprod", "rollout restart deployment/subscription-cis-system", "--kubeconfig " + kc} {
		if !strings.Contains(msgs[0], want) {
			t.Errorf("elicitation lacks %q:\n%s", want, msgs[0])
		}
	}
	if got := activityResults(t, f, "restart"); len(got) != 1 || got[0] != "ok" {
		t.Errorf("activity results = %q", got)
	}
}

func assertRestartNotRun(t *testing.T, answer, wantText, wantResult string) {
	t.Helper()
	f := newFixture(t, changeCases)
	f.acme()
	e := &elicitor{answers: []string{answer}}
	cs := connectAsking(t, f, e)
	text, _ := call(t, cs, "restart", map[string]any{"env": "pp", "service": "subs"})
	if text != wantText {
		t.Errorf("restart = %q, want %q", text, wantText)
	}
	if calls := testutil.ReadArgv(t, f.kubectlLog); calls != nil {
		t.Errorf("kubectl ran: %q", calls)
	}
	if len(e.sent()) != 1 {
		t.Errorf("elicitations = %d", len(e.sent()))
	}
	if got := activityResults(t, f, "restart"); len(got) != 1 || got[0] != wantResult {
		t.Errorf("activity results = %q, want %s", got, wantResult)
	}
}

func TestRestartDeclinedDoesNothing(t *testing.T) {
	assertRestartNotRun(t, "decline", "Not run: you declined", "declined")
}

func TestRestartUncheckedDoesNothing(t *testing.T) {
	assertRestartNotRun(t, "accept-false", "Not run: you declined", "declined")
}

func TestRestartCancelledDoesNothing(t *testing.T) {
	assertRestartNotRun(t, "cancel", "Not run: no answer", "not_run")
}

func TestRestartTimeout(t *testing.T) {
	old := confirmTimeout
	confirmTimeout = 200 * time.Millisecond
	t.Cleanup(func() { confirmTimeout = old })
	assertRestartNotRun(t, "hang", "Not run: no answer", "not_run")
}

func TestNoElicitationNeverRuns(t *testing.T) {
	f := newFixture(t, changeCases)
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, _ := call(t, cs, "restart", map[string]any{"env": "pp", "service": "subs"})
	kc := filepath.Join(f.ctxDir, "kube", "acme", "nonprod.yaml")
	want := "ctx needs to ask you before running this, but this client can't show ctx's confirmation. Run it yourself:\n" +
		f.kubectl + " --kubeconfig " + kc + " -n madapi-preprod rollout restart deployment/subscription-cis-system\n"
	if !strings.HasPrefix(text, want) || !strings.Contains(text, "rollout status deployment/subscription-cis-system --timeout=300s") {
		t.Errorf("restart = %q, want prefix %q", text, want)
	}
	if calls := testutil.ReadArgv(t, f.kubectlLog); calls != nil {
		t.Errorf("kubectl ran: %q", calls)
	}
	if got := activityResults(t, f, "restart"); len(got) != 1 || got[0] != "not_run" {
		t.Errorf("activity results = %q", got)
	}
}

func TestProtectedEnvNeedsCanonicalName(t *testing.T) {
	f := newFixture(t, changeCases)
	f.acme()
	f.knowledge("acme", map[string]string{"environments.yaml": "environments:\n" +
		"  preprod: { cluster: nonprod, namespace: madapi-preprod, aliases: [pp] }\n" +
		"  prod: { cluster: prod, namespace: madapi-prod, protected: true, aliases: [live] }\n"})
	e := &elicitor{answers: []string{"decline"}}
	cs := connectAsking(t, f, e)
	text, isErr := call(t, cs, "restart", map[string]any{"env": "live", "service": "subs"})
	if !isErr || !strings.Contains(text, "protected") {
		t.Errorf("restart live = %v %q", isErr, text)
	}
	if n := len(e.sent()); n != 0 {
		t.Fatalf("elicitation sent for alias: %d", n)
	}
	if _, isErr := call(t, cs, "restart", map[string]any{"env": "prod", "service": "subs"}); isErr {
		t.Errorf("restart prod IsError")
	}
	if msgs := e.sent(); len(msgs) != 1 || !strings.Contains(msgs[0], "madapi-prod") {
		t.Errorf("elicitations = %q", msgs)
	}
}

func TestKubectlChangeValidatesArgs(t *testing.T) {
	f := newFixture(t, `*"scale deployment/subscription-cis-system --replicas=2"*) echo scaled ;;`+"\n")
	f.acme()
	e := &elicitor{answers: []string{"accept"}}
	cs := connectAsking(t, f, e)
	text, isErr := call(t, cs, "kubectl_change", map[string]any{"env": "pp", "args": []string{"delete", "pod", "x", "-n", "kube-system"}})
	if !isErr || !strings.Contains(text, "-n") || len(e.sent()) != 0 {
		t.Errorf("kubectl_change -n = %v %q (elicitations %d)", isErr, text, len(e.sent()))
	}
	text, isErr = call(t, cs, "kubectl_change", map[string]any{"env": "pp", "args": []string{"scale", "deployment/subscription-cis-system", "--replicas=2"}})
	if isErr || !strings.Contains(text, "scaled") {
		t.Errorf("kubectl_change scale = %v %q", isErr, text)
	}
	if msgs := e.sent(); len(msgs) != 1 || !strings.Contains(msgs[0], "scale deployment/subscription-cis-system --replicas=2") {
		t.Errorf("elicitations = %q", msgs)
	}
}

func TestNetworkTestPipesScript(t *testing.T) {
	stdinLog := filepath.Join(realDir(t), "stdin")
	f := newFixture(t, `*"get pods -l app=network-tool -o jsonpath={.items[0].metadata.name}"*) printf 'nt-1' ;;`+"\n"+
		`*"exec -i nt-1 -c network-tool -- /bin/sh -s"*) cat > `+shQuote(stdinLog)+`; echo linked ;;`+"\n")
	f.acme()
	const script = "#!/bin/sh\ncurl -s http://linking/confirm\n"
	f.knowledge("acme", map[string]string{"scripts/confirm-linking.sh": script})
	e := &elicitor{answers: []string{"accept"}}
	cs := connectAsking(t, f, e)

	if text, isErr := call(t, cs, "network_test", map[string]any{"env": "pp", "script": "../clusters.yaml"}); !isErr {
		t.Errorf("path traversal = %q", text)
	}
	if text, isErr := call(t, cs, "network_test", map[string]any{"env": "pp", "script": "confirm-linking.sh", "inline": "echo hi"}); !isErr {
		t.Errorf("both script and inline = %q", text)
	}
	text, isErr := call(t, cs, "network_test", map[string]any{"env": "pp", "script": "confirm-linking.sh"})
	if isErr || !strings.Contains(text, "linked") {
		t.Fatalf("network_test = %v %q", isErr, text)
	}
	got, err := os.ReadFile(stdinLog)
	if err != nil || string(got) != script {
		t.Errorf("stdin = %q %v, want %q", got, err, script)
	}
	calls := testutil.ReadArgv(t, f.kubectlLog)
	if len(calls) != 2 {
		t.Fatalf("calls = %q", calls)
	}
	for _, c := range calls {
		if j := strings.Join(c, " "); !strings.Contains(j, "-n network-utility") {
			t.Errorf("call not in network-utility: %q", j)
		}
	}
	if msgs := e.sent(); len(msgs) != 1 || !strings.Contains(msgs[0], "confirm-linking.sh") || !strings.Contains(msgs[0], "curl -s http://linking/confirm") {
		t.Errorf("elicitations = %q", msgs)
	}
}

func TestPortForwardLifecycle(t *testing.T) {
	f := newFixture(t, `*"port-forward svc/subs 8080:80"*) echo 'Forwarding from 127.0.0.1:8080 -> 80'; exec sleep 60 ;;`+"\n")
	f.acme()
	e := &elicitor{answers: []string{"accept"}}
	ctx := context.Background()
	srv := New(f.deps(), Options{Profile: "acme"})
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, &mcp.ClientOptions{
		Capabilities: &mcp.ClientCapabilities{}, ElicitationHandler: e.handle})
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}

	text, isErr := call(t, cs, "port_forward", map[string]any{"action": "start", "env": "pp", "target": "svc/subs", "ports": []string{"8080:80"}})
	if isErr || !strings.Contains(text, "pf-1") {
		t.Fatalf("start = %v %q", isErr, text)
	}
	text, isErr = call(t, cs, "port_forward", map[string]any{"action": "list"})
	m := regexp.MustCompile(`pid (\d+)`).FindStringSubmatch(text)
	if isErr || !strings.Contains(text, "pf-1") || !strings.Contains(text, "svc/subs") || m == nil {
		t.Fatalf("list = %v %q", isErr, text)
	}
	pid, _ := strconv.Atoi(m[1])
	if len(e.sent()) != 1 {
		t.Errorf("elicitations = %d, want 1 (list needs none)", len(e.sent()))
	}

	_ = cs.Close()
	_ = ss.Wait()
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			_ = syscall.Kill(pid, syscall.SIGKILL)
			t.Fatalf("port-forward pid %d still running after session close", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

func TestPortForwardStop(t *testing.T) {
	f := newFixture(t, `*"port-forward svc/subs 8080:80"*) exec sleep 60 ;;`+"\n")
	f.acme()
	cs := connectAsking(t, f, &elicitor{answers: []string{"accept"}})
	if text, isErr := call(t, cs, "port_forward", map[string]any{"action": "start", "env": "pp", "target": "svc/subs", "ports": []string{"8080:80"}}); isErr {
		t.Fatalf("start = %q", text)
	}
	if text, isErr := call(t, cs, "port_forward", map[string]any{"action": "stop", "id": "pf-1"}); isErr || !strings.Contains(text, "stopped pf-1") {
		t.Errorf("stop = %v %q", isErr, text)
	}
	if text, _ := call(t, cs, "port_forward", map[string]any{"action": "list"}); strings.Contains(text, "pf-1") {
		t.Errorf("list after stop = %q", text)
	}
}

func TestSaveRecipeNeedsConfirm(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	e := &elicitor{answers: []string{"decline", "accept"}}
	cs := connectAsking(t, f, e)
	args := map[string]any{"name": "fix-subs", "description": "Restart subs when stuck", "body": "1. Call restart.\n"}
	path := filepath.Join(f.ctxDir, "knowledge", "acme", "recipes", "fix-subs.md")

	if text, _ := call(t, cs, "save_recipe", args); text != "Not run: you declined" {
		t.Errorf("declined save = %q", text)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file written after decline: %v", err)
	}
	if text, isErr := call(t, cs, "save_recipe", args); isErr || !strings.Contains(text, path) {
		t.Fatalf("accepted save = %v %q", isErr, text)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	text, isErr := call(t, cs, "get_recipe", map[string]any{"name": "fix-subs"})
	if isErr || !strings.Contains(text, "Restart subs when stuck") || !strings.Contains(text, "1. Call restart.") {
		t.Errorf("get_recipe = %v %q", isErr, text)
	}
	if msgs := e.sent(); len(msgs) != 2 || !strings.Contains(msgs[1], path) {
		t.Errorf("elicitations = %q", msgs)
	}
}

func TestSaveServiceRefreshesStore(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connectAsking(t, f, &elicitor{answers: []string{"accept"}})
	if text, isErr := call(t, cs, "save_service", map[string]any{"name": "billing-api", "aliases": []string{"bill"}}); isErr {
		t.Fatalf("save_service = %q", text)
	}
	if text, _ := call(t, cs, "client_context", nil); !strings.Contains(text, "billing-api (deployment; aliases: bill)") {
		t.Errorf("client_context after save_service = %q", text)
	}
}

func TestSaveScriptUsableByNetworkTest(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connectAsking(t, f, &elicitor{answers: []string{"accept"}})
	if text, isErr := call(t, cs, "save_script", map[string]any{"name": "ping-it.sh", "body": "echo ping\n"}); isErr {
		t.Fatalf("save_script = %q", text)
	}
	data, err := os.ReadFile(filepath.Join(f.ctxDir, "knowledge", "acme", "scripts", "ping-it.sh"))
	if err != nil || string(data) != "echo ping\n" {
		t.Errorf("script = %q %v", data, err)
	}
}

func TestChangeToolsAnnotatedDestructive(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"restart": true, "network_test": true, "port_forward": true, "kubectl_change": true,
		"save_recipe": true, "save_service": true, "save_script": true}
	for _, tool := range res.Tools {
		if !want[tool.Name] {
			continue
		}
		delete(want, tool.Name)
		a := tool.Annotations
		if a == nil || a.ReadOnlyHint || a.DestructiveHint == nil || !*a.DestructiveHint {
			t.Errorf("%s annotations = %+v", tool.Name, a)
		}
	}
	for name := range want {
		t.Errorf("missing tool %s", name)
	}
}

func TestChangeToolsHiddenWithoutClient(t *testing.T) {
	f := newFixture(t, changeCases)
	f.acme()
	cs := connectClient(t, f.deps(), Options{Cwd: realDir(t)}, nil, &mcp.ClientOptions{ElicitationHandler: (&elicitor{answers: []string{"accept"}}).handle})
	if text, isErr := call(t, cs, "restart", map[string]any{"env": "pp", "service": "subs"}); !isErr || !strings.Contains(text, "No client is active") {
		t.Errorf("restart = %v %q", isErr, text)
	}
	assertOnlyContextTool(t, cs)
	if calls := testutil.ReadArgv(t, f.kubectlLog); calls != nil {
		t.Errorf("kubectl ran: %q", calls)
	}
}
