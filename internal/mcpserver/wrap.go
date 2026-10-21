package mcpserver

import (
	"encoding/json"
	"errors"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/kube"
)

// call runs one tool body under the scope. It redacts the text and error,
// turns kubectl auth failures into "call login(env)" errors, appends an
// activity entry (args redacted, argv as returned by fn, duration, result)
// and returns failures as IsError tool results, never as protocol errors.
//
// args is the tool's input struct; its "env" field, when it names one of
// the scope's environments, fills the entry's env, cluster and namespace.
// fn returns the text to show and the argv of every command it ran. A
// *notRunError from fn (a change the user did not approve) is shown as a
// normal result and logged with its result, "declined" or "not_run".
func (s *Scope) call(tool string, args any, fn func() (string, [][]string, error)) (*mcp.CallToolResult, error) {
	start := s.now()
	text, argv, err := fn()
	elapsed := s.now().Sub(start)

	entry := knowledge.ActivityEntry{
		Time: start.UTC(), Tool: tool, Args: s.redactArgs(args), Argv: s.redactArgv(argv),
		DurationMS: elapsed.Milliseconds(), Result: "ok",
	}
	if name, _ := entry.Args["env"].(string); name != "" {
		entry.Env = name
		if s.Store != nil {
			if e, lerr := s.Store.Env(name); lerr == nil {
				entry.Env, entry.Cluster, entry.Namespace = e.Name, e.Cluster, e.Namespace
			}
		}
	}
	if ns := argvNamespace(entry.Argv); ns != "" {
		entry.Namespace = ns // where commands actually ran (network_test uses the tool's namespace)
	}
	var nr *notRunError
	if errors.As(err, &nr) {
		entry.Result, entry.Exit = nr.result, -1
		text, err = nr.text, nil
	}
	if err != nil {
		entry.Result, entry.Exit = "error", -1 // -1: no command exit status
		var re *kube.RunError
		if errors.As(err, &re) {
			entry.Exit = re.Exit
		}
		if entry.Env != "" {
			err = kube.AsAuthError(err, entry.Env)
		}
	}
	if aerr := s.Activity.Append(entry); aerr != nil {
		text += "\n(warning: activity log not written: " + aerr.Error() + ")"
	}
	if err != nil {
		return errorResult(s.Redact.String(err.Error())), nil
	}
	return textResult(s.Redact.String(text)), nil
}

func (s *Scope) now() time.Time {
	if s.deps.Now != nil {
		return s.deps.Now()
	}
	return time.Now()
}

// redactArgs turns the input struct into a JSON object with every secret
// value replaced. It redacts the decoded strings, not the encoded JSON:
// encoding escapes & < > " and \, so a secret holding one of them would
// not match in encoded form.
func (s *Scope) redactArgs(args any) map[string]any {
	if args == nil {
		return nil
	}
	data, err := json.Marshal(args)
	if err != nil {
		return nil
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil || len(m) == 0 {
		return nil
	}
	return s.redactValue(m).(map[string]any)
}

// redactValue redacts every string (and map key) in a decoded JSON value.
func (s *Scope) redactValue(v any) any {
	switch x := v.(type) {
	case string:
		return s.Redact.String(x)
	case []any:
		for i := range x {
			x[i] = s.redactValue(x[i])
		}
		return x
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[s.Redact.String(k)] = s.redactValue(val)
		}
		return out
	}
	return v
}

// argvNamespace is the -n value of the first recorded command, or "".
func argvNamespace(argv [][]string) string {
	for _, a := range argv {
		for i := 0; i+1 < len(a); i++ {
			if a[i] == "-n" {
				return a[i+1]
			}
		}
	}
	return ""
}

func (s *Scope) redactArgv(argv [][]string) [][]string {
	out := make([][]string, len(argv))
	for i, a := range argv {
		out[i] = make([]string, len(a))
		for j, v := range a {
			out[i][j] = s.Redact.String(v)
		}
	}
	return out
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errorResult(text string) *mcp.CallToolResult {
	r := textResult(text)
	r.IsError = true
	return r
}

// argvRecorder collects the argv of every kubectl call a tool makes; trace
// runs calls in parallel, so it is safe for concurrent use.
type argvRecorder struct {
	mu   sync.Mutex
	argv [][]string
}

func (r *argvRecorder) add(argv []string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.argv = append(r.argv, append([]string(nil), argv...))
}

func (r *argvRecorder) list() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([][]string(nil), r.argv...)
}

// runner returns a kubectl runner for the scope that records argv into rec.
func (s *Scope) runner(rec *argvRecorder) *kube.Runner {
	return &kube.Runner{Kubectl: s.deps.Kubectl, Redact: s.Redact, Observe: rec.add}
}
