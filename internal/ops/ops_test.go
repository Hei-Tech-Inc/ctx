package ops

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/kube"
	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// fakeKubectl builds a kubectl that answers by matching "$*" against the
// ordered shell glob patterns in cases; the first match runs its command.
type kcase struct{ pattern, run string }

func fakeKubectl(t *testing.T, cases []kcase) (*kube.Runner, kube.Target, string) {
	t.Helper()
	var sb strings.Builder
	sb.WriteString("case \"$*\" in\n")
	for _, c := range cases {
		sb.WriteString(c.pattern + ") " + c.run + " ;;\n")
	}
	sb.WriteString("*) echo \"unexpected: $*\" >&2; exit 1 ;;\nesac\n")
	dir, argvLog := testutil.FakeBin(t, "kubectl", sb.String())
	kc := filepath.Join(t.TempDir(), "kc.yaml")
	if err := os.WriteFile(kc, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	return &kube.Runner{Kubectl: filepath.Join(dir, "kubectl")}, kube.Target{Kubeconfig: kc, Namespace: "ns"}, argvLog
}

const deployJSON = `{"spec":{"selector":{"matchLabels":{"app":"api"}}}}`
const podsJSON = `{"items":[{"metadata":{"name":"api-1"},"spec":{"containers":[{"name":"main"}]}},{"metadata":{"name":"api-2"},"spec":{"containers":[{"name":"main"}]}}]}`

func baseCases(extra ...kcase) []kcase {
	cs := append([]kcase{}, extra...)
	return append(cs,
		kcase{`*"get deployment/api -o json"*`, `echo '` + deployJSON + `'`},
		kcase{`*"get pods -l app=api -o json"*`, `echo '` + podsJSON + `'`},
	)
}

var apiSvc = knowledge.Service{Name: "api", Kind: "deployment"}

func TestLogsAllPodsPrefixesAndPrevious(t *testing.T) {
	r, tg, argvLog := fakeKubectl(t, baseCases(
		kcase{`*"logs api-1 --previous"*`, `echo prev1`},
		kcase{`*"logs api-2 --previous"*`, `echo "Error from server (BadRequest): previous terminated container \"main\" in pod \"api-2\" not found" >&2; exit 1`},
		kcase{`*"logs api-1"*`, `echo cur1`},
		kcase{`*"logs api-2"*`, `echo cur2`},
	))
	res, err := Logs(t.Context(), r, tg, t.TempDir(), LogsOpts{Service: apiSvc, AllPods: true, Previous: true})
	if err != nil {
		t.Fatal(err)
	}
	want := "api-1 (previous): prev1\napi-1: cur1\napi-2: cur2"
	if res.Text != want {
		t.Errorf("Text = %q, want %q", res.Text, want)
	}
	if strings.Join(res.Pods, ",") != "api-1,api-2" {
		t.Errorf("Pods = %v", res.Pods)
	}
	calls := testutil.ReadArgv(t, argvLog)
	if len(calls) < 6 {
		t.Fatalf("calls = %v", calls)
	}
	for _, c := range calls {
		j := strings.Join(c, " ")
		if !strings.Contains(j, "--kubeconfig "+tg.Kubeconfig) || !strings.Contains(j, " -n ns ") {
			t.Errorf("unscoped call: %v", c)
		}
	}
}

func TestLogsGrepContext(t *testing.T) {
	r, tg, _ := fakeKubectl(t, baseCases(
		kcase{`*"logs api-1"*`, `i=1; while [ $i -le 12 ]; do if [ $i -eq 3 ] || [ $i -eq 10 ]; then echo "l$i 233244"; else echo "l$i"; fi; i=$((i+1)); done`},
	))
	res, err := Logs(t.Context(), r, tg, t.TempDir(), LogsOpts{Service: apiSvc, Pod: "api-1", Grep: "233244", Context: 2})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"api-1: l1", "api-1: l2", "api-1: l3 233244", "api-1: l4", "api-1: l5",
		"--",
		"api-1: l8", "api-1: l9", "api-1: l10 233244", "api-1: l11", "api-1: l12",
	}, "\n")
	if res.Text != want {
		t.Errorf("Text =\n%s\nwant\n%s", res.Text, want)
	}
}

func checkCapped(t *testing.T, r *kube.Runner, tg kube.Target, n int) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "exports")
	res, err := Logs(t.Context(), r, tg, dir, LogsOpts{Service: apiSvc, Pod: "api-1", Cap: 400})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Truncated || res.Lines != n {
		t.Errorf("Truncated=%v Lines=%d, want true/%d", res.Truncated, res.Lines, n)
	}
	if got := len(strings.Split(res.Text, "\n")); got != 400 {
		t.Errorf("Text lines = %d, want 400", got)
	}
	data, err := os.ReadFile(res.ExportPath)
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Count(string(data), "\n"); got != n {
		t.Errorf("export lines = %d, want %d", got, n)
	}
	if !strings.HasPrefix(string(data), "api-1: line 1\n") || !strings.HasSuffix(string(data), "api-1: line "+strconv.Itoa(n)+"\n") {
		t.Errorf("export content wrong: %q ... %q", string(data[:20]), string(data[len(data)-20:]))
	}
	st, _ := os.Stat(res.ExportPath)
	if st.Mode().Perm() != 0o600 {
		t.Errorf("export mode = %v", st.Mode().Perm())
	}
	ds, _ := os.Stat(dir)
	if ds.Mode().Perm() != 0o700 {
		t.Errorf("export dir mode = %v", ds.Mode().Perm())
	}
	if !strings.HasSuffix(res.ExportPath, "-api.log") {
		t.Errorf("ExportPath = %s", res.ExportPath)
	}
}

func itoa(n int) string { return strings.TrimSpace(strings.Repeat(" ", 0) + string(appendInt(nil, n))) }

func appendInt(b []byte, n int) []byte {
	if n >= 10 {
		b = appendInt(b, n/10)
	}
	return append(b, byte('0'+n%10))
}

func TestLogsCapAndExport(t *testing.T) {
	r, tg, _ := fakeKubectl(t, baseCases(kcase{`*"logs api-1"*`, `seq 1 1000 | sed 's/^/line /'`}))
	checkCapped(t, r, tg, 1000)
}

func TestLogsStreamsLargeOutput(t *testing.T) {
	r, tg, _ := fakeKubectl(t, baseCases(kcase{`*"logs api-1"*`, `seq 1 200000 | sed 's/^/line /'`}))
	checkCapped(t, r, tg, 200000)
}

func TestLogsUnderCapNotExported(t *testing.T) {
	r, tg, _ := fakeKubectl(t, baseCases(kcase{`*"logs api-1"*`, `echo a; echo b`}))
	res, err := Logs(t.Context(), r, tg, t.TempDir(), LogsOpts{Service: apiSvc, Pod: "api-1"})
	if err != nil {
		t.Fatal(err)
	}
	if res.Truncated || res.ExportPath != "" || res.Lines != 2 {
		t.Errorf("res = %+v", res)
	}
}

var svcWeb = knowledge.Service{Name: "web", Kind: "deployment"}

func traceCases(apiOut, webOut string) []kcase {
	return []kcase{
		{`*"get deployment/api -o json"*`, `echo '` + deployJSON + `'`},
		{`*"get pods -l app=api -o json"*`, `echo '{"items":[{"metadata":{"name":"api-1"},"spec":{"containers":[{"name":"main"}]}}]}'`},
		{`*"get deployment/web -o json"*`, `echo '{"spec":{"selector":{"matchLabels":{"app":"web"}}}}'`},
		{`*"get pods -l app=web -o json"*`, `echo '{"items":[{"metadata":{"name":"web-1"},"spec":{"containers":[{"name":"main"}]}}]}'`},
		{`*"logs api-1"*`, apiOut},
		{`*"logs web-1"*`, webOut},
	}
}

func TestTraceSortsByTimestamp(t *testing.T) {
	r, tg, _ := fakeKubectl(t, traceCases(
		`printf '2026-01-01T10:00:01Z api req 233244\n2026-01-01T10:00:03Z api done 233244\n'`,
		`printf '2026-01-01 10:00:02.500 web hit 233244\n  continuation 233244\n2026-01-01T10:00:04Z web end 233244\n'`,
	))
	res, err := Trace(t.Context(), r, tg, t.TempDir(), TraceOpts{Services: []knowledge.Service{apiSvc, svcWeb}, Pattern: "233244"})
	if err != nil {
		t.Fatal(err)
	}
	want := strings.Join([]string{
		"api-1/main: 2026-01-01T10:00:01Z api req 233244",
		"web-1/main: 2026-01-01 10:00:02.500 web hit 233244",
		"web-1/main:   continuation 233244",
		"api-1/main: 2026-01-01T10:00:03Z api done 233244",
		"web-1/main: 2026-01-01T10:00:04Z web end 233244",
	}, "\n")
	if res.Text != want {
		t.Errorf("Text =\n%s\nwant\n%s", res.Text, want)
	}
	if res.ExportPath == "" {
		t.Error("trace must export")
	}
}

func TestTraceGroupsWithoutTimestamps(t *testing.T) {
	r, tg, _ := fakeKubectl(t, traceCases(
		`printf 'a1 233244\na2 233244\n'`,
		`printf 'w1 233244\n'`,
	))
	res, err := Trace(t.Context(), r, tg, t.TempDir(), TraceOpts{Services: []knowledge.Service{apiSvc, svcWeb}, Pattern: "233244"})
	if err != nil {
		t.Fatal(err)
	}
	want := "api-1/main: a1 233244\napi-1/main: a2 233244\nweb-1/main: w1 233244"
	if res.Text != want {
		t.Errorf("Text =\n%s\nwant\n%s", res.Text, want)
	}
	if res.Lines != 3 || res.Truncated {
		t.Errorf("res = %+v", res)
	}
}

const describeDeploy = `{
 "spec":{"replicas":3,"template":{"spec":{
  "volumes":[{"name":"v","configMap":{"name":"vol-cm"}}],
  "containers":[{"name":"main","image":"reg/api:1.2",
   "env":[
    {"name":"LIT","value":"hello"},
    {"name":"CMREF","valueFrom":{"configMapKeyRef":{"name":"app-cm","key":"color"}}},
    {"name":"SECREF","valueFrom":{"secretKeyRef":{"name":"app-sec","key":"pw"}}},
    {"name":"POD","valueFrom":{"fieldRef":{"fieldPath":"metadata.name"}}}],
   "envFrom":[{"configMapRef":{"name":"bulk-cm"}},{"secretRef":{"name":"bulk-sec"}}],
   "resources":{"limits":{"memory":"1Gi"}},
   "readinessProbe":{"httpGet":{"path":"/ready"}}}]}}},
 "status":{"readyReplicas":2}}`

func deployFile(t *testing.T) string {
	p := filepath.Join(t.TempDir(), "deploy.json")
	if err := os.WriteFile(p, []byte(describeDeploy), 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestDescribeEnvSources(t *testing.T) {
	r, tg, argvLog := fakeKubectl(t, []kcase{
		{`*"get deployment/api -o json"*`, "cat " + deployFile(t)},
		{`*"get configmap app-cm -o json"*`, `echo '{"data":{"color":"blue"}}'`},
		{`*"get configmap bulk-cm -o json"*`, `echo '{"data":{"A":"1","B":"2"}}'`},
		{`*"get configmap vol-cm -o json"*`, `echo '{"data":{}}'`},
		{`*"get events"*`, `echo '{"items":[{"type":"Warning","reason":"BackOff","message":"restarting"}]}'`},
		{`*"rollout status deployment/api --timeout=1s"*`, `echo "Waiting for rollout to finish: 2 of 3 updated"; exit 1`},
	})
	s, err := Describe(t.Context(), r, tg, apiSvc)
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]EnvVar{}
	for _, e := range s.Env {
		got[e.Name] = e
	}
	want := []EnvVar{
		{"LIT", "hello", "literal"},
		{"CMREF", "blue", "configmap:app-cm/color"},
		{"SECREF", "", "secret:app-sec/pw"},
		{"POD", "", "field:metadata.name"},
		{"A", "1", "envFrom:configmap:bulk-cm"},
		{"B", "2", "envFrom:configmap:bulk-cm"},
		{"*", "", "envFrom:secret:bulk-sec"},
	}
	for _, w := range want {
		if got[w.Name] != w {
			t.Errorf("env %s = %+v, want %+v", w.Name, got[w.Name], w)
		}
	}
	if s.Kind != "deployment" || s.Name != "api" || s.Image != "reg/api:1.2" || s.Replicas != 3 || s.Ready != 2 {
		t.Errorf("summary = %+v", s)
	}
	if !strings.Contains(s.Rollout, "2 of 3") {
		t.Errorf("Rollout = %q", s.Rollout)
	}
	if len(s.Events) != 1 || !strings.Contains(s.Events[0], "BackOff") {
		t.Errorf("Events = %v", s.Events)
	}
	if strings.Join(s.ConfigMaps, ",") != "app-cm,bulk-cm,vol-cm" {
		t.Errorf("ConfigMaps = %v", s.ConfigMaps)
	}
	if s.Resources["limits"] == nil || s.Probes["readinessProbe"] == nil {
		t.Errorf("resources/probes = %v %v", s.Resources, s.Probes)
	}
	for _, c := range testutil.ReadArgv(t, argvLog) {
		if strings.Contains(strings.Join(c, " "), "secret") && c[len(c)-1] != "--timeout=1s" {
			t.Errorf("a secret was fetched: %v", c)
		}
	}
}
