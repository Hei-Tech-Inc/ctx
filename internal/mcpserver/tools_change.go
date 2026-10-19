package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/kube"
)

type networkTestIn struct {
	Env    string `json:"env" jsonschema:"environment name (the full name for protected environments)"`
	Script string `json:"script,omitempty" jsonschema:"name of a script in the client's scripts/ folder"`
	Inline string `json:"inline,omitempty" jsonschema:"script text to run instead of a saved script"`
}

type portForwardIn struct {
	Action string   `json:"action" jsonschema:"start, stop or list"`
	Env    string   `json:"env,omitempty" jsonschema:"environment name, for start"`
	Target string   `json:"target,omitempty" jsonschema:"what to forward to, for start, e.g. svc/name or pod/name"`
	Ports  []string `json:"ports,omitempty" jsonschema:"ports for start, e.g. [8080:80]"`
	ID     string   `json:"id,omitempty" jsonschema:"port-forward id, for stop"`
}

type saveRecipeIn struct {
	Name         string   `json:"name" jsonschema:"recipe name: lowercase letters, digits and -"`
	Description  string   `json:"description" jsonschema:"one line saying when to use it"`
	Tags         []string `json:"tags,omitempty" jsonschema:"keywords"`
	Params       []string `json:"params,omitempty" jsonschema:"inputs the recipe needs, e.g. msisdn"`
	ChangesState bool     `json:"changes_state,omitempty" jsonschema:"true if following it changes the cluster"`
	Body         string   `json:"body" jsonschema:"markdown steps"`
}

type saveServiceIn struct {
	Name    string   `json:"name" jsonschema:"workload name as in the cluster"`
	Kind    string   `json:"kind,omitempty" jsonschema:"deployment (default) or statefulset"`
	Aliases []string `json:"aliases" jsonschema:"nicknames to add"`
}

type saveScriptIn struct {
	Name string `json:"name" jsonschema:"file name, e.g. confirm-linking.sh"`
	Body string `json:"body" jsonschema:"shell script text, run with /bin/sh in the network tool pod"`
}

// plan starts a Plan for a change in env.
func (sc *Scope) plan(tool string, env knowledge.Env) Plan {
	return Plan{Tool: tool, Profile: sc.Profile.Name, Env: env.Name, Cluster: env.Cluster, Namespace: env.Namespace}
}

func (s *server) addChangeTools(srv *mcp.Server) {
	addChange(srv, s, "restart",
		"Restart a service in an environment (rollout restart) and wait for the rollout. ctx asks the user to confirm first.",
		func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in serviceIn) (string, [][]string, error) {
			env, err := sc.Store.EnvForChange(in.Env)
			if err != nil {
				return "", nil, err
			}
			svc, err := sc.Store.Service(in.Service)
			if err != nil {
				return "", nil, err
			}
			rec := &argvRecorder{}
			r, t := sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env)
			obj := svc.Kind + "/" + svc.Name
			restart := []string{"rollout", "restart", obj}
			status := []string{"rollout", "status", obj, "--timeout=300s"}
			p := sc.plan("restart", env)
			p.Commands = [][]string{r.Argv(t, restart...), r.Argv(t, status...)}
			if err := sc.ask(ctx, req.Session, p); err != nil {
				return "", nil, err
			}
			out1, err := r.Run(ctx, t, restart...)
			if err != nil {
				return "", rec.list(), err
			}
			out2, err := r.Run(ctx, t, status...)
			if err != nil {
				return "", rec.list(), fmt.Errorf("%s\nrollout status failed: %w", strings.TrimSpace(string(out1)), err)
			}
			return strings.TrimSpace(string(out1)) + "\n" + strings.TrimSpace(string(out2)), rec.list(), nil
		})

	addChange(srv, s, "network_test",
		"Run a saved script (or inline script text) in the environment's network tool pod, e.g. to test a downstream connection. ctx asks the user to confirm first.",
		func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in networkTestIn) (string, [][]string, error) {
			if (in.Script == "") == (in.Inline == "") {
				return "", nil, errors.New("give exactly one of script (a saved script name) or inline (script text)")
			}
			env, err := sc.Store.EnvForChange(in.Env)
			if err != nil {
				return "", nil, err
			}
			nt := networkToolFor(sc.Store, env)
			if nt.Namespace == "" || nt.Selector == "" {
				return "", nil, fmt.Errorf("no network tool is configured for %s: set network_tool (namespace, selector, container) in environments.yaml", env.Name)
			}
			label, body := "inline script", in.Inline
			if in.Script != "" {
				path, data, err := sc.Store.Script(in.Script)
				if err != nil {
					return "", nil, err
				}
				label, body = "script "+in.Script+" ("+path+")", string(data)
			}
			rec := &argvRecorder{}
			r, t := sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env)
			t.Namespace = nt.Namespace
			out, err := r.Run(ctx, t, "get", "pods", "-l", nt.Selector, "-o", "jsonpath={.items[0].metadata.name}")
			if err != nil {
				return "", rec.list(), err
			}
			pod := strings.TrimSpace(string(out))
			if pod == "" {
				return "", rec.list(), fmt.Errorf("no pod matches %s in namespace %s", nt.Selector, nt.Namespace)
			}
			args := []string{"exec", "-i", pod}
			if nt.Container != "" {
				args = append(args, "-c", nt.Container)
			}
			args = append(args, "--", "/bin/sh", "-s")
			p := sc.plan("network_test", env)
			p.Namespace = nt.Namespace
			p.Commands = [][]string{r.Argv(t, args...)}
			p.Summary = "Stdin: " + label + "\n" + firstLines(body, 20)
			if err := sc.ask(ctx, req.Session, p); err != nil {
				return "", rec.list(), err
			}
			res, err := r.RunStdin(ctx, t, strings.NewReader(body), args...)
			if err != nil {
				return "", rec.list(), err
			}
			return string(res), rec.list(), nil
		})

	addChange(srv, s, "port_forward",
		"Start, stop or list background port-forwards owned by ctx (stopped when the session ends). Starting one asks the user to confirm.",
		func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in portForwardIn) (string, [][]string, error) {
			switch in.Action {
			case "list":
				lines := s.forwards.list(req.Session)
				if len(lines) == 0 {
					return "no port-forwards", nil, nil
				}
				return joinLines(lines), nil, nil
			case "stop":
				if in.ID == "" {
					return "", nil, errors.New("stop needs id (see port_forward list)")
				}
				if err := s.forwards.stop(req.Session, in.ID); err != nil {
					return "", nil, err
				}
				return "stopped " + in.ID, nil, nil
			case "start":
			default:
				return "", nil, fmt.Errorf("unknown action %q: use start, stop or list", in.Action)
			}
			if in.Target == "" || len(in.Ports) == 0 {
				return "", nil, errors.New("start needs target (e.g. svc/name) and ports (e.g. [8080:80])")
			}
			args := append([]string{"port-forward", in.Target}, in.Ports...)
			if err := kube.ValidateUserArgs(args); err != nil {
				return "", nil, err
			}
			env, err := sc.Store.EnvForChange(in.Env)
			if err != nil {
				return "", nil, err
			}
			if err := refuseAllNamespaces(args, env); err != nil {
				return "", nil, err
			}
			rec := &argvRecorder{}
			r, t := sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env)
			p := sc.plan("port_forward", env)
			p.Commands = [][]string{r.Argv(t, args...)}
			p.Summary = "Runs in the background until stopped or the session ends."
			if err := sc.ask(ctx, req.Session, p); err != nil {
				return "", nil, err
			}
			f, err := s.forwards.start(req.Session, r, t, env.Name, in.Target, in.Ports)
			if err != nil {
				return "", rec.list(), err
			}
			return fmt.Sprintf("started %s: %s %s in %s (pid %d). Stop it with port_forward action=stop id=%s.",
				f.id, in.Target, strings.Join(in.Ports, " "), env.Name, f.proc.Pid(), f.id), rec.list(), nil
		})

	addChange(srv, s, "kubectl_change",
		"Run any kubectl command that changes the cluster (scale, patch, delete, set image, exec...) in an environment. ctx asks the user to confirm first.",
		func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in kubectlIn) (string, [][]string, error) {
			if len(in.Args) == 0 {
				return "", nil, errors.New("args is required, e.g. [scale, deployment/name, --replicas=2]")
			}
			if err := kube.ValidateUserArgs(in.Args); err != nil {
				return "", nil, err
			}
			env, err := sc.Store.EnvForChange(in.Env)
			if err != nil {
				return "", nil, err
			}
			if err := refuseAllNamespaces(in.Args, env); err != nil {
				return "", nil, err
			}
			rec := &argvRecorder{}
			r, t := sc.runner(rec), kube.TargetFor(sc.Profile.Name, sc.Store, env)
			p := sc.plan("kubectl_change", env)
			p.Commands = [][]string{r.Argv(t, in.Args...)}
			if err := sc.ask(ctx, req.Session, p); err != nil {
				return "", nil, err
			}
			out, err := r.Run(ctx, t, in.Args...)
			if err != nil {
				return "", rec.list(), err
			}
			if strings.TrimSpace(string(out)) == "" {
				return "done (no output)", rec.list(), nil
			}
			return string(kube.RedactSecretObjects(out)), rec.list(), nil
		})

	addChange(srv, s, "save_recipe",
		"Save a recipe (runbook) so it can be found and followed next time. Replaces a recipe of the same name. ctx asks the user to confirm first.",
		func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in saveRecipeIn) (string, [][]string, error) {
			path, err := knowledge.RecipePath(sc.Store.Root, in.Name)
			if err != nil {
				return "", nil, err
			}
			p := Plan{Tool: "save_recipe", Profile: sc.Profile.Name, Writes: []string{path}}
			p.Summary = replaces(path) + fmt.Sprintf("Recipe %s: %s\n", in.Name, in.Description)
			if len(in.Tags) > 0 {
				p.Summary += "Tags: " + strings.Join(in.Tags, ", ") + "\n"
			}
			if len(in.Params) > 0 {
				p.Summary += "Params: " + strings.Join(in.Params, ", ") + "\n"
			}
			p.Summary += fmt.Sprintf("Changes state: %v\n%s", in.ChangesState, firstLines(in.Body, 20))
			if err := sc.ask(ctx, req.Session, p); err != nil {
				return "", nil, err
			}
			path, err = knowledge.SaveRecipe(sc.Store.Root, knowledge.Recipe{Name: in.Name, Description: in.Description,
				Tags: in.Tags, Params: in.Params, ChangesState: in.ChangesState, Body: in.Body})
			if err != nil {
				return "", nil, err
			}
			s.reloadScope(req.Session)
			return "saved recipe " + in.Name + " to " + path, nil, nil
		})

	addChange(srv, s, "save_service",
		"Add a service, or nicknames for one, to the client's services.yaml. ctx asks the user to confirm first.",
		func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in saveServiceIn) (string, [][]string, error) {
			svc := knowledge.Service{Name: in.Name, Kind: in.Kind, Aliases: in.Aliases}
			if err := knowledge.ValidateService(svc); err != nil {
				return "", nil, err
			}
			path := knowledge.ServicesPath(sc.Store.Root)
			p := Plan{Tool: "save_service", Profile: sc.Profile.Name, Writes: []string{path}}
			kind := in.Kind
			if kind == "" {
				kind = "deployment unless already set"
			}
			p.Summary = fmt.Sprintf("Service %s (%s), add aliases: %s", in.Name, kind, strings.Join(in.Aliases, ", "))
			if err := sc.ask(ctx, req.Session, p); err != nil {
				return "", nil, err
			}
			if err := knowledge.SaveService(sc.Store.Root, svc); err != nil {
				return "", nil, err
			}
			s.reloadScope(req.Session)
			return "saved service " + in.Name + " to " + path, nil, nil
		})

	addChange(srv, s, "save_script",
		"Save a shell script to the client's scripts/ folder for network_test. Replaces a script of the same name. ctx asks the user to confirm first.",
		func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in saveScriptIn) (string, [][]string, error) {
			path, err := knowledge.ScriptPath(sc.Store.Root, in.Name)
			if err != nil {
				return "", nil, err
			}
			p := Plan{Tool: "save_script", Profile: sc.Profile.Name, Writes: []string{path}}
			p.Summary = replaces(path) + firstLines(in.Body, 20)
			if err := sc.ask(ctx, req.Session, p); err != nil {
				return "", nil, err
			}
			if path, err = knowledge.SaveScript(sc.Store.Root, in.Name, in.Body); err != nil {
				return "", nil, err
			}
			s.reloadScope(req.Session)
			return "saved script " + in.Name + " to " + path, nil, nil
		})
}

// refuseAllNamespaces rejects change args that would act outside the
// environment's namespace (the confirmation names that namespace).
func refuseAllNamespaces(args []string, env knowledge.Env) error {
	if kube.WantsAllNamespaces(args) {
		return fmt.Errorf("all-namespaces is not allowed for changes: they run in the environment's namespace (%s)", env.Namespace)
	}
	return nil
}

// networkToolFor is the environment's network tool: its own settings,
// falling back field by field to the store's default.
func networkToolFor(st *knowledge.Store, env knowledge.Env) knowledge.NetworkTool {
	nt := st.NetworkTool
	if o := env.NetworkTool; o != nil {
		if o.Namespace != "" {
			nt.Namespace = o.Namespace
		}
		if o.Selector != "" {
			nt.Selector = o.Selector
		}
		if o.Container != "" {
			nt.Container = o.Container
		}
	}
	return nt
}

// replaces notes in a plan that path already exists and will be replaced.
func replaces(path string) string {
	if _, err := os.Stat(path); err == nil {
		return "Replaces the existing file.\n"
	}
	return ""
}
