package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/kube"
	"github.com/Hei-Tech-Inc/ctx/internal/ops"
)

type contextIn struct{}

type envIn struct {
	Env string `json:"env" jsonschema:"environment name or alias"`
}

type logsIn struct {
	Env           string `json:"env" jsonschema:"environment name or alias"`
	Service       string `json:"service" jsonschema:"service name or alias"`
	Pod           string `json:"pod,omitempty" jsonschema:"one pod of the service; default is the first pod"`
	AllPods       bool   `json:"all_pods,omitempty" jsonschema:"read every pod of the service"`
	Previous      bool   `json:"previous,omitempty" jsonschema:"also read the previous (crashed) container"`
	Container     string `json:"container,omitempty" jsonschema:"container name"`
	AllContainers bool   `json:"all_containers,omitempty" jsonschema:"read every container"`
	Since         string `json:"since,omitempty" jsonschema:"only lines newer than this duration, e.g. 1h"`
	Tail          int    `json:"tail,omitempty" jsonschema:"last N lines per container"`
	Grep          string `json:"grep,omitempty" jsonschema:"keep only lines matching this regular expression"`
	Context       int    `json:"context,omitempty" jsonschema:"lines of context around each grep match"`
	Export        bool   `json:"export,omitempty" jsonschema:"write the full output to a file and return its path"`
}

type traceIn struct {
	Env      string   `json:"env" jsonschema:"environment name or alias"`
	Services []string `json:"services" jsonschema:"service names or aliases"`
	Pattern  string   `json:"pattern" jsonschema:"regular expression to follow, e.g. a request id or MSISDN"`
	Since    string   `json:"since,omitempty" jsonschema:"only lines newer than this duration, e.g. 1h"`
	Previous bool     `json:"previous,omitempty" jsonschema:"also read previous (crashed) containers"`
	Context  int      `json:"context,omitempty" jsonschema:"lines of context around each match"`
}

type serviceIn struct {
	Env     string `json:"env" jsonschema:"environment name or alias"`
	Service string `json:"service" jsonschema:"service name or alias"`
}

type kubectlIn struct {
	Env  string   `json:"env" jsonschema:"environment name or alias"`
	Args []string `json:"args" jsonschema:"kubectl arguments without --kubeconfig, --context or -n, e.g. [get, pods]"`
}

type searchIn struct {
	Query string `json:"query" jsonschema:"text to look for (case-insensitive)"`
}

type recipeIn struct {
	Name string `json:"name" jsonschema:"recipe name"`
}

type activityIn struct {
	Env   string `json:"env,omitempty" jsonschema:"only this environment (name or alias)"`
	Tool  string `json:"tool,omitempty" jsonschema:"only this tool"`
	Since string `json:"since,omitempty" jsonschema:"only entries newer than a duration (2h) or a date (2026-10-19 or RFC 3339)"`
	Limit int    `json:"limit,omitempty" jsonschema:"maximum entries, newest first (default 50)"`
}

func (s *server) addReadTools(srv *mcp.Server) {
	mcp.AddTool(srv, &mcp.Tool{Name: "client_context", Annotations: readOnly,
		Description: "Call this first. Shows the active client: its environments, services with nicknames, recipes and network tool."},
		s.clientContext)

	addRead(srv, s, "login",
		"Log in to the cluster behind an environment using the client's stored login steps. Call it when a tool says you are not logged in.",
		func(ctx context.Context, sc *Scope, in envIn) (string, [][]string, error) {
			env, err := sc.Store.Env(in.Env)
			if err != nil {
				return "", nil, err
			}
			login := sc.Store.Clusters[env.Cluster].Login
			var argv [][]string
			for _, step := range login.Run {
				argv = append(argv, []string{"sh", "-c", step})
			}
			out, err := kube.Login(ctx, sc.Profile.Name, sc.Store, env.Cluster, sc.deps.Getter, sc.Redact)
			if err != nil {
				return "", argv, err
			}
			return strings.TrimSpace(out) + fmt.Sprintf("\nLogged in to %s (cluster %s).", env.Name, env.Cluster), argv, nil
		})

	addRead(srv, s, "logs",
		"Read a service's pod logs in an environment, optionally across all pods, previous containers and filtered by a regex. Long output is capped and saved to a file.",
		func(ctx context.Context, sc *Scope, in logsIn) (string, [][]string, error) {
			env, svc, err := sc.envService(in.Env, in.Service)
			if err != nil {
				return "", nil, err
			}
			rec := &argvRecorder{}
			res, err := ops.Logs(ctx, sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env), sc.exportDir(), ops.LogsOpts{
				Service: svc, Pod: in.Pod, AllPods: in.AllPods, Previous: in.Previous,
				Container: in.Container, AllContainers: in.AllContainers, Since: in.Since,
				Tail: in.Tail, Grep: in.Grep, Context: in.Context, Export: in.Export,
			})
			if err != nil {
				return "", rec.list(), err
			}
			head := fmt.Sprintf("%s in %s (namespace %s), pods: %s", svc.Name, env.Name, env.Namespace, strings.Join(res.Pods, ", "))
			return logsText(head, res), rec.list(), nil
		})

	addRead(srv, s, "trace",
		"Follow one pattern (a request id, an MSISDN) through several services' logs at once, merged by timestamp. The full result is saved to a file.",
		func(ctx context.Context, sc *Scope, in traceIn) (string, [][]string, error) {
			if in.Pattern == "" {
				return "", nil, errors.New("pattern is required")
			}
			if len(in.Services) == 0 {
				return "", nil, errors.New("services needs at least one service")
			}
			env, err := sc.Store.Env(in.Env)
			if err != nil {
				return "", nil, err
			}
			var svcs []knowledge.Service
			var names []string
			for _, n := range in.Services {
				svc, err := sc.Store.Service(n)
				if err != nil {
					return "", nil, err
				}
				svcs = append(svcs, svc)
				names = append(names, svc.Name)
			}
			rec := &argvRecorder{}
			res, err := ops.Trace(ctx, sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env), sc.exportDir(), ops.TraceOpts{
				Services: svcs, Pattern: in.Pattern, Since: in.Since, Previous: in.Previous, Context: in.Context,
			})
			if err != nil {
				return "", rec.list(), err
			}
			head := fmt.Sprintf("trace %q in %s (namespace %s) across %s", in.Pattern, env.Name, env.Namespace, strings.Join(names, ", "))
			return logsText(head, res), rec.list(), nil
		})

	addRead(srv, s, "describe_service",
		"Summarise a service: image, replicas, rollout, events, resources, probes, and where each environment variable comes from. Secret values are never shown.",
		func(ctx context.Context, sc *Scope, in serviceIn) (string, [][]string, error) {
			env, svc, err := sc.envService(in.Env, in.Service)
			if err != nil {
				return "", nil, err
			}
			rec := &argvRecorder{}
			sum, err := ops.Describe(ctx, sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env), svc)
			if err != nil {
				return "", rec.list(), err
			}
			return describeText(env, sum), rec.list(), nil
		})

	addRead(srv, s, "kubectl_read",
		"Run a read-only kubectl command (get, describe, logs, top, events, explain, api-resources, rollout status/history, auth can-i) in an environment. Secret data is redacted.",
		func(ctx context.Context, sc *Scope, in kubectlIn) (string, [][]string, error) {
			if err := kube.ValidateUserArgs(in.Args); err != nil {
				return "", nil, err
			}
			if !kube.IsReadOnly(in.Args) {
				return "", nil, fmt.Errorf("kubectl_read only runs read-only commands; use kubectl_change for %q", strings.Join(in.Args, " "))
			}
			env, err := sc.Store.Env(in.Env)
			if err != nil {
				return "", nil, err
			}
			rec := &argvRecorder{}
			out, err := sc.runner(rec).Run(ctx, kube.TargetFor(sc.Profile.Name, sc.Store, env), in.Args...)
			if err != nil {
				return "", rec.list(), err
			}
			// Always applied: it leaves output without Secret objects unchanged.
			return string(kube.RedactSecretObjects(out)), rec.list(), nil
		})

	addRead(srv, s, "discover_services",
		"List the deployments and statefulsets in an environment's namespace, showing which are already known and their nicknames.",
		func(ctx context.Context, sc *Scope, in envIn) (string, [][]string, error) {
			env, err := sc.Store.Env(in.Env)
			if err != nil {
				return "", nil, err
			}
			rec := &argvRecorder{}
			r, t := sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env)
			var lines []string
			for _, kind := range []string{"deployment", "statefulset"} {
				out, err := r.Run(ctx, t, "get", kind+"s", "-o", "name")
				if err != nil {
					return "", rec.list(), err
				}
				for _, ln := range strings.Split(strings.TrimSpace(string(out)), "\n") {
					_, name, ok := strings.Cut(strings.TrimSpace(ln), "/")
					if !ok || name == "" {
						continue
					}
					lines = append(lines, kind+" "+name+" "+knownAs(sc.Store, kind, name))
				}
			}
			if len(lines) == 0 {
				return fmt.Sprintf("no deployments or statefulsets in %s (namespace %s)", env.Name, env.Namespace), rec.list(), nil
			}
			head := fmt.Sprintf("workloads in %s (namespace %s):", env.Name, env.Namespace)
			return head + "\n" + joinLines(lines), rec.list(), nil
		})

	addRead(srv, s, "search_knowledge",
		"Search the client's recipes and knowledge files for text (case-insensitive).",
		func(_ context.Context, sc *Scope, in searchIn) (string, [][]string, error) {
			if strings.TrimSpace(in.Query) == "" {
				return "", nil, errors.New("query is required")
			}
			hits, err := sc.Store.Search(in.Query)
			if err != nil {
				return "", nil, err
			}
			if len(hits) == 0 {
				return fmt.Sprintf("no matches for %q", in.Query), nil, nil
			}
			lines := make([]string, len(hits))
			for i, h := range hits {
				lines[i] = fmt.Sprintf("%s:%d: %s", h.File, h.Line, h.Text)
			}
			return joinLines(lines), nil, nil
		})

	addRead(srv, s, "get_recipe",
		"Read one recipe (runbook) in full. Follow its steps with the other tools.",
		func(_ context.Context, sc *Scope, in recipeIn) (string, [][]string, error) {
			r, err := sc.Store.GetRecipe(in.Name)
			if err != nil {
				return "", nil, err
			}
			var b strings.Builder
			fmt.Fprintf(&b, "Recipe: %s\n", r.Name)
			if r.Description != "" {
				fmt.Fprintf(&b, "Description: %s\n", r.Description)
			}
			if len(r.Tags) > 0 {
				fmt.Fprintf(&b, "Tags: %s\n", strings.Join(r.Tags, ", "))
			}
			if len(r.Params) > 0 {
				fmt.Fprintf(&b, "Params: %s\n", strings.Join(r.Params, ", "))
			}
			fmt.Fprintf(&b, "Changes state: %v\n\n%s", r.ChangesState, r.Body)
			return b.String(), nil, nil
		})

	addRead(srv, s, "activity",
		"Show recent ctx tool calls for this client (newest first), optionally filtered by environment, tool or time.",
		func(_ context.Context, sc *Scope, in activityIn) (string, [][]string, error) {
			f := knowledge.ActivityFilter{Env: in.Env, Tool: in.Tool, Limit: in.Limit}
			if f.Limit <= 0 {
				f.Limit = 50
			}
			if in.Env != "" {
				if e, err := sc.Store.Env(in.Env); err == nil {
					f.Env = e.Name
				}
			}
			if in.Since != "" {
				t, err := parseSince(in.Since, sc.now())
				if err != nil {
					return "", nil, err
				}
				f.Since = t
			}
			entries, err := sc.Activity.Query(f)
			if err != nil {
				return "", nil, err
			}
			if len(entries) == 0 {
				return "no activity", nil, nil
			}
			lines := make([]string, 0, len(entries))
			for _, e := range entries {
				data, err := json.Marshal(e)
				if err != nil {
					return "", nil, err
				}
				lines = append(lines, string(data))
			}
			return joinLines(lines), nil, nil
		})
}

func (s *server) clientContext(ctx context.Context, req *mcp.CallToolRequest, _ contextIn) (*mcp.CallToolResult, any, error) {
	sc, err := s.scopeFor(ctx, req.Session)
	if err != nil {
		return textResult(err.Error()), nil, nil
	}
	res, err := sc.call("client_context", nil, func() (string, [][]string, error) {
		return sc.contextText(), nil, nil
	})
	return res, nil, err
}

func (sc *Scope) contextText() string {
	var b strings.Builder
	fmt.Fprintf(&b, "Client: %s\n", sc.Profile.Name)
	if sc.Profile.WorkDir != "" {
		fmt.Fprintf(&b, "Work dir: %s\n", sc.Profile.WorkDir)
	}
	fmt.Fprintf(&b, "Knowledge: %s\n", knowledge.Root(sc.Profile.Name))
	if err := sc.unavailable(); err != nil {
		fmt.Fprintf(&b, "\n%s\nOther tools are unavailable until it loads.\n", err)
		return b.String()
	}
	st := sc.Store

	b.WriteString("\nEnvironments:\n")
	if len(st.Envs) == 0 {
		b.WriteString("  none (run ctx knowledge init and edit environments.yaml)\n")
	}
	for _, n := range sortedKeys(st.Envs) {
		e := st.Envs[n]
		name := e.Name
		if len(e.Aliases) > 0 {
			name += " (aliases: " + strings.Join(e.Aliases, ", ") + ")"
		}
		fmt.Fprintf(&b, "- %s: cluster %s, namespace %s", name, e.Cluster, e.Namespace)
		if e.Protected {
			b.WriteString(", protected (change tools need the full name)")
		}
		if e.NetworkTool != nil {
			fmt.Fprintf(&b, ", network tool %s", networkTool(*e.NetworkTool))
		}
		b.WriteString("\n")
	}

	b.WriteString("\nServices:\n")
	if len(st.Services) == 0 {
		b.WriteString("  none (use discover_services)\n")
	}
	for _, n := range sortedKeys(st.Services) {
		v := st.Services[n]
		desc := v.Kind
		if len(v.Aliases) > 0 {
			desc += "; aliases: " + strings.Join(v.Aliases, ", ")
		}
		fmt.Fprintf(&b, "- %s (%s)\n", v.Name, desc)
	}

	b.WriteString("\nRecipes:\n")
	recipes, err := st.Recipes()
	switch {
	case err != nil:
		fmt.Fprintf(&b, "  could not be listed: %v\n", err)
	case len(recipes) == 0:
		b.WriteString("  none\n")
	}
	for _, r := range recipes {
		fmt.Fprintf(&b, "- %s: %s\n", r.Name, r.Description)
	}

	if st.NetworkTool != (knowledge.NetworkTool{}) {
		fmt.Fprintf(&b, "\nNetwork tool: %s\n", networkTool(st.NetworkTool))
	}
	return b.String()
}

func networkTool(n knowledge.NetworkTool) string {
	return fmt.Sprintf("namespace %s, selector %s, container %s", n.Namespace, n.Selector, n.Container)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func (sc *Scope) envService(envName, svcName string) (knowledge.Env, knowledge.Service, error) {
	env, err := sc.Store.Env(envName)
	if err != nil {
		return knowledge.Env{}, knowledge.Service{}, err
	}
	svc, err := sc.Store.Service(svcName)
	if err != nil {
		return knowledge.Env{}, knowledge.Service{}, err
	}
	return env, svc, nil
}

func (sc *Scope) exportDir() string { return filepath.Join(sc.Store.Root, "exports") }

func logsText(head string, res ops.LogsResult) string {
	var b strings.Builder
	b.WriteString(head + "\n")
	if res.Text == "" {
		b.WriteString("(no matching lines)\n")
	} else {
		b.WriteString(res.Text + "\n")
	}
	if res.Truncated {
		fmt.Fprintf(&b, "[showing the first %d of %d lines]\n", strings.Count(res.Text, "\n")+1, res.Lines)
	}
	if res.ExportPath != "" {
		fmt.Fprintf(&b, "Full output: %s\n", res.ExportPath)
	}
	return b.String()
}

func describeText(env knowledge.Env, s ops.ServiceSummary) string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s/%s in %s (namespace %s)\n", s.Kind, s.Name, env.Name, env.Namespace)
	fmt.Fprintf(&b, "Image: %s\nReplicas: %d ready of %d\nRollout: %s\n", s.Image, s.Ready, s.Replicas, s.Rollout)
	if len(s.Env) > 0 {
		b.WriteString("\nEnvironment variables:\n")
		for _, e := range s.Env {
			switch {
			case strings.HasPrefix(e.Source, "secret:"), strings.HasPrefix(e.Source, "envFrom:secret:"):
				fmt.Fprintf(&b, "- %s from %s (value not shown)\n", e.Name, e.Source)
			case strings.HasPrefix(e.Source, "field:"):
				fmt.Fprintf(&b, "- %s from %s\n", e.Name, e.Source)
			default:
				fmt.Fprintf(&b, "- %s=%s (%s)\n", e.Name, e.Value, e.Source)
			}
		}
	}
	if len(s.ConfigMaps) > 0 {
		fmt.Fprintf(&b, "\nConfigMaps: %s\n", strings.Join(s.ConfigMaps, ", "))
	}
	if len(s.Resources) > 0 {
		data, _ := json.Marshal(s.Resources)
		fmt.Fprintf(&b, "\nResources: %s\n", data)
	}
	for _, k := range sortedKeys(s.Probes) {
		data, _ := json.Marshal(s.Probes[k])
		fmt.Fprintf(&b, "%s: %s\n", k, data)
	}
	if len(s.Events) > 0 {
		b.WriteString("\nRecent events:\n")
		for _, e := range s.Events {
			fmt.Fprintf(&b, "- %s\n", e)
		}
	}
	return b.String()
}

// knownAs says whether a discovered workload is in services.yaml.
func knownAs(st *knowledge.Store, kind, name string) string {
	v, ok := st.Services[name]
	if !ok || v.Kind != kind {
		return "(not in services.yaml)"
	}
	if len(v.Aliases) == 0 {
		return "(known, no aliases)"
	}
	return "(aliases: " + strings.Join(v.Aliases, ", ") + ")"
}

// parseSince accepts a duration back from now ("2h"), a date
// ("2026-10-19") or an RFC 3339 time.
func parseSince(s string, now time.Time) (time.Time, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return now.Add(-d), nil
	}
	for _, layout := range []string{time.RFC3339, "2006-01-02"} {
		if t, err := time.Parse(layout, s); err == nil {
			return t, nil
		}
	}
	return time.Time{}, fmt.Errorf("invalid since %q: use a duration like 2h, a date like 2026-10-19 or an RFC 3339 time", s)
}
