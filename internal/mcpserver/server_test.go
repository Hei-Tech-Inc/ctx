package mcpserver

import (
	"context"
	"encoding/json"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

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
	return newFixtureSecret(t, kubectlCases, secretValue)
}

func shQuote(s string) string { return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'" }

// newFixtureSecret is newFixture with the value ctx secret get returns for
// TKGI_PASSWORD.
func newFixtureSecret(t *testing.T, kubectlCases, secretVal string) *fixture {
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
	cdir, _ := testutil.FakeBin(t, "ctx", `case "$4" in TKGI_PASSWORD) printf '%s\n' `+shQuote(secretVal)+` ;; *) echo "no such key" >&2; exit 1 ;; esac`)
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
	return connectClient(t, d, opts, roots, &mcp.ClientOptions{})
}

// connectClient is connect with the client's options (e.g. an elicitation
// handler). roots == nil turns off the roots capability.
func connectClient(t *testing.T, d Deps, opts Options, roots []string, copts *mcp.ClientOptions) *mcp.ClientSession {
	t.Helper()
	ctx := context.Background()
	srv := New(d, opts)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
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

func toolNames(t *testing.T, cs *mcp.ClientSession) []string {
	t.Helper()
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, tool := range res.Tools {
		names = append(names, tool.Name)
	}
	return names
}

func assertOnlyContextTool(t *testing.T, cs *mcp.ClientSession) {
	t.Helper()
	if names := toolNames(t, cs); len(names) != 1 || names[0] != "client_context" {
		t.Errorf("tools/list = %v, want only client_context", names)
	}
}

func TestNoProfileOnlyContext(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	writeFile(t, filepath.Join(f.ctxDir, "config"), "active=acme\n")
	cs := connect(t, f.deps(), Options{Cwd: realDir(t)}, nil)
	assertOnlyContextTool(t, cs)

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
	assertOnlyContextTool(t, cs)
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
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	res, err := cs.ListTools(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{"client_context": true, "login": true, "logs": true, "trace": true,
		"describe_service": true, "kubectl_read": true, "discover_services": true,
		"search_knowledge": true, "get_recipe": true, "activity": true}
	for _, tool := range res.Tools {
		if changeTool[tool.Name] {
			continue
		}
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

var changeTool = map[string]bool{"restart": true, "network_test": true, "port_forward": true,
	"kubectl_change": true, "save_recipe": true, "save_service": true, "save_script": true}

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

// An early tools/call, before any tools/list, gets the explanation and runs
// nothing.
func TestNoProfileEarlyCallExplains(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	cs := connect(t, f.deps(), Options{Cwd: realDir(t)}, nil)
	text, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"})
	if !isErr || !strings.Contains(text, "No client is active") {
		t.Errorf("logs = %v %q", isErr, text)
	}
	if calls := testutil.ReadArgv(t, f.kubectlLog); calls != nil {
		t.Errorf("kubectl ran: %v", calls)
	}
	assertOnlyContextTool(t, cs)
}

func TestBrokenKnowledgeKeepsTools(t *testing.T) {
	f := newFixture(t, "")
	f.acme()
	f.knowledge("acme", map[string]string{"services.yaml": "services:\n  foo: { kind: bogus }\n"})
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	if names := toolNames(t, cs); len(names) != 10+len(changeTool) {
		t.Errorf("tools/list = %v, want all %d", names, 10+len(changeTool))
	}
}

func TestJSONEscapableSecretRedacted(t *testing.T) {
	const pw = `p&ss<w>"rd\x`
	f := newFixtureSecret(t, `*"logs subs-1"*) printf 'args=%s\n' "$*" ;;`+"\n", pw)
	f.acme()
	cs := connect(t, f.deps(), Options{Profile: "acme"}, nil)
	text, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs", "since": pw})
	if isErr || !strings.Contains(text, "--since <redacted>") {
		t.Fatalf("logs = %v %q", isErr, text)
	}
	atext, isErr := call(t, cs, "activity", nil)
	if isErr {
		t.Fatalf("activity: %q", atext)
	}
	data, err := os.ReadFile(filepath.Join(f.ctxDir, "knowledge", "acme", "activity.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	escaped, _ := json.Marshal(pw)
	forms := []string{pw, strings.Trim(string(escaped), `"`), `p&ss<w>\"rd\\x`}
	for name, out := range map[string]string{"logs": text, "activity tool": atext, "activity.jsonl": string(data)} {
		for _, form := range forms {
			if strings.Contains(out, form) {
				t.Errorf("%s contains %q: %s", name, form, out)
			}
		}
	}
	if !strings.Contains(string(data), `"since":"\u003credacted\u003e"`) {
		t.Errorf("activity.jsonl args not redacted: %s", data)
	}
}

// connectRoots is connectClient that also returns the client, so a test can
// change its roots mid-session.
func connectRoots(t *testing.T, d Deps, opts Options, roots []string, copts *mcp.ClientOptions) (*mcp.Client, *mcp.ClientSession) {
	t.Helper()
	ctx := context.Background()
	srv := New(d, opts)
	ct, st := mcp.NewInMemoryTransports()
	ss, err := srv.Connect(ctx, st, nil)
	if err != nil {
		t.Fatal(err)
	}
	c := mcp.NewClient(&mcp.Implementation{Name: "test", Version: "v0"}, copts)
	for _, r := range roots {
		c.AddRoots(&mcp.Root{URI: fileURI(r)})
	}
	cs, err := c.Connect(ctx, ct, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_ = cs.Close()
		_ = ss.Wait()
	})
	return c, cs
}

func fileURI(p string) string { return (&url.URL{Scheme: "file", Path: p}).String() }

// waitOnlyContextTool polls tools/list until only client_context is left.
func waitOnlyContextTool(t *testing.T, cs *mcp.ClientSession) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for {
		names := toolNames(t, cs)
		if len(names) == 1 && names[0] == "client_context" {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("tools/list = %v, want only client_context", names)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Adding another client's folder mid-session drops the session to no scope:
// it never keeps serving the first client, nor switches to the second.
func TestRootsChangedToAnotherClientFailsClosed(t *testing.T) {
	f := newFixture(t, `*"port-forward svc/subs 8080:80"*) exec sleep 60 ;;`+"\n")
	acmeWork := f.acme()
	globexWork := f.profile("globex")
	e := &elicitor{answers: []string{"accept"}}
	c, cs := connectRoots(t, f.deps(), Options{Cwd: acmeWork}, []string{acmeWork}, &mcp.ClientOptions{ElicitationHandler: e.handle})
	if text, _ := call(t, cs, "client_context", nil); !strings.Contains(text, "Client: acme") {
		t.Fatalf("client_context = %q", text)
	}
	text, isErr := call(t, cs, "port_forward", map[string]any{"action": "start", "env": "pp", "target": "svc/subs", "ports": []string{"8080:80"}})
	m := regexp.MustCompile(`pid (\d+)`).FindStringSubmatch(text)
	if isErr || m == nil {
		t.Fatalf("port_forward start = %v %q", isErr, text)
	}
	pid, _ := strconv.Atoi(m[1])
	t.Cleanup(func() { _ = syscall.Kill(pid, syscall.SIGKILL) })

	c.AddRoots(&mcp.Root{URI: fileURI(globexWork)})
	waitOnlyContextTool(t, cs)

	text, isErr = call(t, cs, "client_context", nil)
	if isErr || !strings.Contains(text, "No client is active") || !strings.Contains(text, "acme, globex") || strings.Contains(text, "preprod") {
		t.Errorf("client_context after roots change = %v %q", isErr, text)
	}
	before := len(testutil.ReadArgv(t, f.kubectlLog))
	if ltext, isErr := call(t, cs, "logs", map[string]any{"env": "pp", "service": "subs"}); !isErr || ltext != text {
		t.Errorf("logs after roots change = %v %q, want IsError %q", isErr, ltext, text)
	}
	if after := len(testutil.ReadArgv(t, f.kubectlLog)); after != before {
		t.Errorf("kubectl ran after scope was dropped")
	}
	deadline := time.Now().Add(5 * time.Second)
	for syscall.Kill(pid, 0) == nil {
		if time.Now().After(deadline) {
			t.Fatalf("port-forward pid %d still running after scope was dropped", pid)
		}
		time.Sleep(20 * time.Millisecond)
	}

	// Removing the other folder again does not bring the old scope back.
	c.RemoveRoots(fileURI(globexWork))
	time.Sleep(100 * time.Millisecond)
	assertOnlyContextTool(t, cs)
}

// Switching the open folders to only another client's folder also fails closed.
func TestRootsReplacedByAnotherClientFailsClosed(t *testing.T) {
	f := newFixture(t, "")
	acmeWork := f.acme()
	globexWork := f.profile("globex")
	c, cs := connectRoots(t, f.deps(), Options{Cwd: acmeWork}, []string{acmeWork}, &mcp.ClientOptions{})
	if text, _ := call(t, cs, "client_context", nil); !strings.Contains(text, "Client: acme") {
		t.Fatalf("client_context = %q", text)
	}
	c.AddRoots(&mcp.Root{URI: fileURI(globexWork)})
	c.RemoveRoots(fileURI(acmeWork))
	waitOnlyContextTool(t, cs)
	if text, _ := call(t, cs, "client_context", nil); !strings.Contains(text, "No client is active") || strings.Contains(text, "Client: globex") {
		t.Errorf("client_context = %q", text)
	}
}

// A roots change that still resolves to the same client keeps the scope.
func TestRootsChangedSameClientKeepsScope(t *testing.T) {
	f := newFixture(t, "")
	acmeWork := f.acme()
	sub := filepath.Join(acmeWork, "repo")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatal(err)
	}
	c, cs := connectRoots(t, f.deps(), Options{Cwd: acmeWork}, []string{acmeWork}, &mcp.ClientOptions{})
	n := len(toolNames(t, cs))
	c.AddRoots(&mcp.Root{URI: fileURI(sub)})
	time.Sleep(100 * time.Millisecond)
	if text, _ := call(t, cs, "client_context", nil); !strings.Contains(text, "Client: acme") {
		t.Errorf("client_context = %q", text)
	}
	if got := len(toolNames(t, cs)); got != n {
		t.Errorf("tools = %d, want %d", got, n)
	}
}
