package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/secret"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

const secretValue = "s3cr3t-hunter-value"

// fixture is a CTX_DIR with profiles, knowledge stores, a fake kubectl and
// a fake ctx (for ctx secret get).
type fixture struct {
	t           *testing.T
	ctxDir      string
	profilesDir string
	kubectl     string
	kubectlLog  string
	ctxBin      string
}

func realDir(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir()) // macOS /var -> /private/var
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func writeFile(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
}

const subsDeploy = `{"spec":{"selector":{"matchLabels":{"app":"subs"}}}}`
const subsPods = `{"items":[{"metadata":{"name":"subs-1"},"spec":{"containers":[{"name":"main"}]}}]}`

func newFixture(t *testing.T, kubectlCases string) *fixture {
	t.Helper()
	f := &fixture{t: t, ctxDir: realDir(t)}
	t.Setenv("CTX_DIR", f.ctxDir)
	t.Setenv("HOME", realDir(t))
	f.profilesDir = filepath.Join(f.ctxDir, "profiles")
	script := "case \"$*\" in\n" + kubectlCases +
		"*\"get deployment/subscription-cis-system -o json\"*) echo '" + subsDeploy + "' ;;\n" +
		"*\"get pods -l app=subs -o json\"*) echo '" + subsPods + "' ;;\n" +
		"*\"logs subs-1\"*) echo 'line one'; echo 'line two' ;;\n" +
		"*\" version \"*) echo 'Server Version: v1.30' ;;\n" +
		"*) echo \"unexpected: $*\" >&2; exit 1 ;;\nesac\n"
	dir, log := testutil.FakeBin(t, "kubectl", script)
	f.kubectl, f.kubectlLog = filepath.Join(dir, "kubectl"), log
	cdir, _ := testutil.FakeBin(t, "ctx", `case "$4" in TKGI_PASSWORD) echo `+secretValue+` ;; *) echo "no such key" >&2; exit 1 ;; esac`)
	f.ctxBin = filepath.Join(cdir, "ctx")
	return f
}

// profile writes <name>.conf with a fresh WORK_DIR and returns that dir.
func (f *fixture) profile(name string) string {
	f.t.Helper()
	work := filepath.Join(realDir(f.t), name+"-work")
	if err := os.MkdirAll(work, 0o755); err != nil {
		f.t.Fatal(err)
	}
	writeFile(f.t, filepath.Join(f.profilesDir, name+".conf"),
		"PROFILE_NAME=\""+name+"\"\nWORK_DIR=\""+work+"\"\n")
	return work
}

// knowledge writes files under the profile's knowledge root.
func (f *fixture) knowledge(name string, files map[string]string) {
	f.t.Helper()
	for rel, body := range files {
		writeFile(f.t, filepath.Join(f.ctxDir, "knowledge", name, rel), body)
	}
}

func specFiles(t *testing.T) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, n := range []string{"clusters.yaml", "environments.yaml", "services.yaml"} {
		data, err := os.ReadFile(filepath.Join("..", "knowledge", "testdata", "spec", n))
		if err != nil {
			t.Fatal(err)
		}
		out[n] = string(data)
	}
	out["recipes/trace-msisdn.md"] = "---\nname: trace-msisdn\ndescription: Find every log line for an MSISDN across services\ntags: [trace]\nchanges_state: false\n---\n1. Call trace.\n"
	return out
}

// acme writes the acme profile with the spec knowledge store and a
// kubeconfig for the nonprod cluster. It returns acme's WORK_DIR.
func (f *fixture) acme() string {
	f.t.Helper()
	work := f.profile("acme")
	f.knowledge("acme", specFiles(f.t))
	writeFile(f.t, filepath.Join(f.ctxDir, "kube", "acme", "nonprod.yaml"), "apiVersion: v1\n")
	return work
}

func (f *fixture) deps() Deps {
	return Deps{ProfilesDir: f.profilesDir, Getter: secret.CLIGetter{CtxBin: f.ctxBin}, Kubectl: f.kubectl}
}

// connect starts the server in memory and returns a connected client.
func connect(t *testing.T, d Deps, opts Options, roots []string) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := New(d, opts)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	copts := &mcp.ClientOptions{}
	if roots == nil {
		copts.Capabilities = &mcp.ClientCapabilities{} // no roots capability
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, copts)
	for _, r := range roots {
		c.AddRoots(&mcp.Root{URI: (&url.URL{Scheme: "file", Path: r}).String()})
	}
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = ss.Wait()
	})
	return cs
}

func call(t *testing.T, cs *mcp.ClientSession, tool string, args map[string]any) (string, bool) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	res, err := cs.CallTool(context.Background(), &mcp.CallToolParams{Name: tool, Arguments: args})
	if err != nil {
		t.Fatalf("%s: protocol error: %v", tool, err)
	}
	var sb strings.Builder
	for _, c := range res.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			sb.WriteString(tc.Text)
		}
	}
	return sb.String(), res.IsError
}

func TestNoProfileOnlyContext(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	writeFile(t, filepath.Join(f.ctxDir, "config"), "active=acme\n")
	cs := connect(t, f.deps(), Options{Cwd: realDir(t)}, nil)

	text, isErr := call(t, cs, "client_context", nil)
	if isErr || !strings.Contains(text, "No client is active") {
		t.Fatalf("client_context = %v %q", isErr, text)
	}
	if strings.Contains(text, "acme") || strings.Contains(text, "preprod") {
		t.Errorf("client_context leaked acme: %q", text)
	}
	ltext, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"})
	if !isErr || ltext != text {
		t.Errorf("logs = %v %q, want IsError with %q", isErr, ltext, text)
	}
	if calls := testutil.ReadArgv(t, f.kubectlLog); calls != nil {
		t.Errorf("kubectl ran: %v", calls)
	}
}

func TestScopeFromRoots(t *testing.T) {
	f := newFixture(t, "")
	work := f.acme()
	sub := filepath.Join(work, "repo")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, f.deps(), Options{Cwd: realDir(t)}, []string{sub})
	text, isErr := call(t, cs, "client_context", nil)
	if isErr || !strings.Contains(text, "Client: acme") {
		t.Fatalf("client_context = %v %q", isErr, text)
	}
}

func TestScopeEmptyRootsUsesCwd(t *testing.T) {
	f := newFixture(t, "")
	work := f.acme()
	cs := connect(t, f.deps(), Options{Cwd: work}, []string{})
	text, _ := call(t, cs, "client_context", nil)
	if !strings.Contains(text, "Client: acme") {
		t.Fatalf("client_context = %q", text)
	}
}

func TestScopeConflictingRoots(t *testing.T) {
	f := newFixture(t, "")
	acmeWork := f.acme()
	globexWork := f.profile("globex")
	cs := connect(t, f.deps(), Options{Cwd: acmeWork}, []string{acmeWork, globexWork})
	text, isErr := call(t, cs, "client_context", nil)
	if isErr || !strings.Contains(text, "No client is active") ||
		!strings.Contains(text, "folders belong to more than one client: acme, globex") {
		t.Fatalf("client_context = %v %q", isErr, text)
	}
	if _, isErr := call(t, cs, "search_knowledge", map[string]any{"query": "subs"}); !isErr {
		t.Error("search_knowledge ran without a scope")
	}
}

func TestProfileFlagWins(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	f.profile("globex")
	cs := connect(t, f.deps(), Options{Profile: "acme", Cwd: realDir(t)}, nil)
	if text, _ := call(t, cs, "client_context", nil); !strings.Contains(text, "Client: acme") {
		t.Fatalf("client_context = %q", text)
	}
	cs = connect(t, f.deps(), Options{Profile: "../evil", Cwd: realDir(t)}, nil)
	if text, _ := call(t, cs, "client_context", nil); !strings.Contains(text, "No client is active") {
		t.Fatalf("bad --profile: client_context = %q", text)
	}
}

func TestClientContextListsKnowledge(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "client_context", nil)
	if isErr {
		t.Fatalf("IsError: %q", text)
	}
	for _, want := range []string{
		"Client: acme",
		"preprod (aliases: pp, nonprod): cluster nonprod, namespace madapi-preprod",
		"prod: cluster prod, namespace madapi-prod, protected",
		"dev: cluster nonprod, namespace madapi-dev",
		"subscription-cis-system (deployment; aliases: subs, subscription, cis)",
		"customer-kyc-verification-aggregator (deployment; aliases: kyc, kyc-verify)",
		"trace-msisdn: Find every log line for an MSISDN across services",
		"namespace network-utility, selector app=network-tool, container network-tool",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("client_context lacks %q:\n%s", want, text)
		}
	}
}

func TestClientContextShowsBrokenRecipe(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	f.knowledge("acme", map[string]string{"recipes/bad.md": "no front matter\n"})
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "client_context", nil)
	if isErr || !strings.Contains(text, "recipes/bad.md") || !strings.Contains(text, "subscription-cis-system") {
		t.Fatalf("client_context = %v %q", isErr, text)
	}
}

func TestServerReportsBrokenKnowledge(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	f.knowledge("acme", map[string]string{"services.yaml": "services:\n  foo: { kind: bogus }\n"})
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "client_context", nil)
	if isErr || !regexp.MustCompile(`services\.yaml:\d+:`).MatchString(text) {
		t.Fatalf("client_context = %v %q", isErr, text)
	}
	ltext, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"})
	if !isErr || !regexp.MustCompile(`services\.yaml:\d+:`).MatchString(ltext) {
		t.Errorf("logs = %v %q", isErr, ltext)
	}

	// once fixed, the next call sees the store
	f.knowledge("acme", specFiles(t))
	if _, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"}); isErr {
		t.Error("logs still failing after the file was fixed")
	}
}

func TestLogsToolScopedArgv(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"})
	if isErr || !strings.Contains(text, "subs-1: line one") {
		t.Fatalf("logs = %v %q", isErr, text)
	}
	calls := testutil.ReadArgv(t, f.kubectlLog)
	if len(calls) < 3 {
		t.Fatalf("calls = %q", calls)
	}
	kc := filepath.Join(f.ctxDir, "kube", "acme", "nonprod.yaml")
	for _, c := range calls {
		j := strings.Join(c, " ")
		if !strings.Contains(j, "--kubeconfig "+kc) || !strings.Contains(j, "-n madapi-preprod") {
			t.Errorf("unscoped call: %q", c)
		}
	}
}

func TestToolOutputRedacted(t *testing.T) {
	f := newFixture(t, `*"logs subs-1"*) echo "token=`+secretValue+`" ;;`+"\n")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs", "grep": "token", "since": secretValue})
	if isErr {
		t.Fatalf("logs IsError: %q", text)
	}
	if strings.Contains(text, secretValue) || !strings.Contains(text, "token=<redacted>") {
		t.Errorf("logs = %q", text)
	}
	data, err := os.ReadFile(filepath.Join(f.ctxDir, "knowledge", "acme", "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), secretValue) {
		t.Errorf("activity leaked secret: %s", data)
	}
	if !strings.Contains(string(data), `"since":"\u003credacted\u003e"`) && !strings.Contains(string(data), `"since":"<redacted>"`) {
		t.Errorf("activity args not recorded redacted: %s", data)
	}
}

func TestKubectlReadRejectsChange(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "kubectl_read", map[string]any{"env": "pp", "args": []string{"delete", "pod", "x"}})
	if !isErr || !strings.Contains(text, "kubectl_change") {
		t.Errorf("kubectl_read delete = %v %q", isErr, text)
	}
	text, isErr = call(t, cs, "kubectl_read", map[string]any{"env": "pp", "args": []string{"get", "pods", "--context", "other"}})
	if !isErr || !strings.Contains(text, "--context") {
		t.Errorf("kubectl_read --context = %v %q", isErr, text)
	}
	if calls := testutil.ReadArgv(t, f.kubectlLog); calls != nil {
		t.Errorf("kubectl ran: %q", calls)
	}
}

func TestKubectlReadRedactsSecrets(t *testing.T) {
	f := newFixture(t, `*"get secret db -o json"*) echo '{"kind":"Secret","metadata":{"name":"db"},"data":{"pw":"c2VjcmV0LWRhdGE="}}' ;;`+"\n")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "kubectl_read", map[string]any{"env": "pp", "args": []string{"get", "secret", "db", "-o", "json"}})
	if isErr || strings.Contains(text, "c2VjcmV0LWRhdGE=") || !strings.Contains(text, "<redacted>") {
		t.Errorf("kubectl_read secret = %v %q", isErr, text)
	}
}

func TestOtherProfileInvisible(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	f.knowledge("acme", map[string]string{"recipes/acme-fix.md": "---\nname: acme-fix\n---\nneedle in acme\n"})
	f.profile("globex")
	f.knowledge("globex", map[string]string{
		"clusters.yaml":         "clusters:\n  gc:\n    login: { type: kubeconfig, path: /x, context: g }\n",
		"environments.yaml":     "environments:\n  globex-only-env: { cluster: gc, namespace: globex-ns }\n",
		"recipes/globex-fix.md": "---\nname: globex-fix\n---\nneedle in globex\n",
	})
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "search_knowledge", map[string]any{"query": "needle"})
	if isErr || !strings.Contains(text, "acme-fix") || strings.Contains(text, "globex") {
		t.Errorf("search_knowledge = %v %q", isErr, text)
	}
	if text, isErr := call(t, cs, "get_recipe", map[string]any{"name": "globex-fix"}); !isErr {
		t.Errorf("get_recipe globex-fix = %q", text)
	}
	text, isErr = call(t, cs, "logs", map[string]any{"env": "globex-only-env", "service": "subs"})
	if !isErr || !strings.Contains(text, "unknown environment") || !strings.Contains(text, "preprod") || strings.Contains(text, "globex-ns") {
		t.Errorf("logs globex env = %v %q", isErr, text)
	}
	if strings.Count(text, "globex") != 1 { // only the requested name itself
		t.Errorf("logs listed globex names: %q", text)
	}
}

func TestActivityRecorded(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	if text, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"}); isErr {
		t.Fatalf("logs: %q", text)
	}
	text, isErr := call(t, cs, "activity", nil)
	if isErr {
		t.Fatalf("activity: %q", text)
	}
	lines := strings.Split(strings.TrimSpace(text), "\n")
	if len(lines) != 1 {
		t.Fatalf("activity lines = %q", lines)
	}
	var e struct {
		Tool, Env, Cluster, Namespace, Result string
		Argv                                  [][]string
	}
	if err := json.Unmarshal([]byte(lines[0]), &e); err != nil {
		t.Fatalf("parse %q: %v", lines[0], err)
	}
	if e.Tool != "logs" || e.Env != "preprod" || e.Result != "ok" || e.Cluster != "nonprod" || e.Namespace != "madapi-preprod" {
		t.Errorf("entry = %+v", e)
	}
	if len(e.Argv) < 3 || e.Argv[0][0] != f.kubectl {
		t.Errorf("argv = %q", e.Argv)
	}
}

func TestDiscoverServices(t *testing.T) {
	f := newFixture(t, `*"get deployments -o name"*) printf 'deployment.apps/subscription-cis-system\ndeployment.apps/new-thing\n' ;;`+"\n"+
		`*"get statefulsets -o name"*) echo 'statefulset.apps/db' ;;`+"\n")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "discover_services", map[string]any{"env": "pp"})
	if isErr {
		t.Fatalf("discover_services: %q", text)
	}
	for _, want := range []string{
		"deployment subscription-cis-system (aliases: subs, subscription, cis)",
		"deployment new-thing (not in services.yaml)",
		"statefulset db (not in services.yaml)",
	} {
		if !strings.Contains(text, want) {
			t.Errorf("lacks %q:\n%s", want, text)
		}
	}
}

func TestToolsAnnotatedReadOnly(t *testing.T) {
	f := newFixture(t, "")
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"client_context": true, "login": true, "logs": true, "trace": true,
		"describe_service": true, "kubectl_read": true, "discover_services": true,
		"search_knowledge": true, "get_recipe": true, "activity": true}
	for _, tool := range res.Tools {
		if !want[tool.Name] {
			t.Errorf("unexpected tool %s", tool.Name)
			continue
		}
		delete(want, tool.Name)
		if tool.Annotations == nil || !tool.Annotations.ReadOnlyHint {
			t.Errorf("%s not readOnly", tool.Name)
		}
	}
	for name := range want {
		t.Errorf("missing tool %s", name)
	}
}

func TestTraceNeedsPattern(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "trace", map[string]any{"env": "pp", "services": []string{"subs"}, "pattern": ""})
	if !isErr || !strings.Contains(text, "pattern is required") {
		t.Errorf("trace = %v %q", isErr, text)
	}
}

func TestNotLoggedInSaysLogin(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	if err := os.Remove(filepath.Join(f.ctxDir, "kube", "acme", "nonprod.yaml")); err != nil {
		t.Fatal(err)
	}
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"})
	if !isErr || !strings.Contains(text, `call login(env="preprod")`) {
		t.Errorf("logs = %v %q", isErr, text)
	}
}

func TestLoginTool(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	f.knowledge("acme", map[string]string{"clusters.yaml": "clusters:\n  nonprod:\n    login:\n      type: command\n      secrets: [TKGI_PASSWORD]\n      run:\n        - echo pw=$TKGI_PASSWORD\n        - touch \"$KUBECONFIG\"\n  prod:\n    login: { type: kubeconfig, path: /nope, context: p }\n"})
	t.Setenv("PATH", filepath.Dir(f.kubectl)+string(os.PathListSeparator)+os.Getenv("PATH"))
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "login", map[string]any{"env": "pp"})
	if isErr || !strings.Contains(text, "pw=<redacted>") || strings.Contains(text, secretValue) || !strings.Contains(text, "Logged in to preprod") {
		t.Errorf("login = %v %q", isErr, text)
	}
}
