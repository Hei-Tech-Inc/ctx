package mcpserver

import (
	"context"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// Plan is what a change tool will do, shown to the user before it runs.
// Commands are the full argv of each kubectl call (binary and scoping
// flags included); Writes are the files a save_* tool writes.
type Plan struct {
	Tool, Profile, Env, Cluster, Namespace string
	Commands                               [][]string
	Writes                                 []string
	Summary                                string
}

// confirmTimeout is how long confirm waits for the user's answer.
var confirmTimeout = 120 * time.Second

const (
	notRunDeclined = "Not run: you declined"
	notRunNoAnswer = "Not run: no answer"
	notRunNoAsk    = "ctx needs to ask you before running this, but this client can't show ctx's confirmation. Run it yourself:\n"
)

// confirmSchema asks for one boolean, confirm.
var confirmSchema = map[string]any{
	"type": "object",
	"properties": map[string]any{
		"confirm": map[string]any{"type": "boolean", "title": "Run this", "description": "Tick to let ctx run exactly what is shown above."},
	},
	"required": []string{"confirm"},
}

// confirm asks the user, through an MCP elicitation, to approve p. p must
// already be redacted. ok is true only for an accept with confirm=true;
// otherwise notRun is the text to return instead of running. A client
// without elicitation is never asked: notRun then tells the user what to
// run themselves.
func confirm(ctx context.Context, ss *mcp.ServerSession, p Plan) (ok bool, notRun string, err error) {
	if !canElicit(ss) {
		return false, notRunNoAsk + p.manual(), nil
	}
	ctx, cancel := context.WithTimeout(ctx, confirmTimeout)
	defer cancel()
	res, err := ss.Elicit(ctx, &mcp.ElicitParams{Mode: "form", Message: p.message(), RequestedSchema: confirmSchema})
	switch {
	case err != nil, res.Action == "cancel":
		return false, notRunNoAnswer, nil
	case res.Action == "accept" && res.Content["confirm"] == true:
		return true, "", nil
	case res.Action == "accept", res.Action == "decline":
		return false, notRunDeclined, nil
	}
	return false, notRunNoAnswer, nil
}

// canElicit reports whether the client declared form elicitation at
// initialize (an empty elicitation capability means form).
func canElicit(ss *mcp.ServerSession) bool {
	ip := ss.InitializeParams()
	if ip == nil || ip.Capabilities == nil || ip.Capabilities.Elicitation == nil {
		return false
	}
	e := ip.Capabilities.Elicitation
	return e.Form != nil || e.URL == nil
}

// message is the confirmation text: where the change goes and exactly what
// runs or is written.
func (p Plan) message() string {
	var b strings.Builder
	fmt.Fprintf(&b, "ctx %s wants to make a change for client %s.\n", p.Tool, p.Profile)
	if p.Env != "" {
		fmt.Fprintf(&b, "Environment: %s\nCluster: %s\nNamespace: %s\n", p.Env, p.Cluster, p.Namespace)
	}
	if len(p.Commands) > 0 {
		b.WriteString("\nCommands:\n")
		for _, c := range p.Commands {
			b.WriteString(shellJoin(c) + "\n")
		}
	}
	if len(p.Writes) > 0 {
		b.WriteString("\nWrites:\n")
		for _, w := range p.Writes {
			b.WriteString(w + "\n")
		}
	}
	if p.Summary != "" {
		b.WriteString("\n" + strings.TrimRight(p.Summary, "\n") + "\n")
	}
	return b.String()
}

// manual is the plan as instructions for the user to carry out by hand.
func (p Plan) manual() string {
	var b strings.Builder
	for _, c := range p.Commands {
		b.WriteString(shellJoin(c) + "\n")
	}
	for _, w := range p.Writes {
		b.WriteString("write " + w + "\n")
	}
	if p.Summary != "" {
		b.WriteString(strings.TrimRight(p.Summary, "\n") + "\n")
	}
	return b.String()
}

// redactPlan returns p with every secret value replaced.
func (s *Scope) redactPlan(p Plan) Plan {
	r := s.Redact.String
	p.Tool, p.Profile, p.Env, p.Cluster, p.Namespace, p.Summary =
		r(p.Tool), r(p.Profile), r(p.Env), r(p.Cluster), r(p.Namespace), r(p.Summary)
	p.Commands = s.redactArgv(p.Commands)
	writes := make([]string, len(p.Writes))
	for i, w := range p.Writes {
		writes[i] = r(w)
	}
	p.Writes = writes
	return p
}

// ask confirms p (redacted) with the user. It returns nil when the change
// may run, else a *notRunError carrying the text to return.
func (s *Scope) ask(ctx context.Context, ss *mcp.ServerSession, p Plan) error {
	ok, notRun, err := confirm(ctx, ss, s.redactPlan(p))
	if err != nil {
		return err
	}
	if ok {
		return nil
	}
	result := "not_run"
	if notRun == notRunDeclined {
		result = "declined"
	}
	return &notRunError{result: result, text: notRun}
}

// notRunError is a change the user did not approve. Scope.call returns its
// text as a normal result and logs result ("declined" or "not_run").
type notRunError struct{ result, text string }

func (e *notRunError) Error() string { return e.text }

var shellSafe = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

// shellJoin quotes each word for a POSIX shell where needed.
func shellJoin(argv []string) string {
	out := make([]string, len(argv))
	for i, a := range argv {
		if shellSafe.MatchString(a) {
			out[i] = a
		} else {
			out[i] = "'" + strings.ReplaceAll(a, "'", `'\''`) + "'"
		}
	}
	return strings.Join(out, " ")
}

// Limits on a body (script or recipe) shown in a confirmation. A body is
// always shown in full, so a larger one is refused rather than truncated.
const (
	maxConfirmLines = 400
	maxConfirmBytes = 64 * 1024
)

// fullBody is text for a confirmation, in full, or an error when it is too
// large to show.
func fullBody(text string) (string, error) {
	body := strings.TrimRight(text, "\n")
	lines := 0
	if body != "" {
		lines = strings.Count(body, "\n") + 1
	}
	if lines > maxConfirmLines || len(text) > maxConfirmBytes {
		return "", fmt.Errorf("too large to confirm (%d lines / %d bytes); limit is %d lines / %d KiB — split it or run it yourself",
			lines, len(text), maxConfirmLines, maxConfirmBytes/1024)
	}
	return body, nil
}
