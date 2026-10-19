// Package mcpserver is the ctx MCP server: scoped ops tools for the one
// client (profile) the host's folders belong to.
package mcpserver

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

// Deps are the server's outside dependencies.
type Deps struct {
	ProfilesDir string        // where <profile>.conf files live
	Getter      secret.Getter // secret values for redaction and login
	Kubectl     string        // kubectl binary; "" means kubectl on PATH
	Now         func() time.Time
}

// Options pick the session's profile: Profile (--profile) wins, then the
// client's roots, then Cwd.
type Options struct {
	Profile string
	Cwd     string
}

// legacyProtocolVersions are the SDK's protocol versions before
// 2026-07-28. That version drops server-to-client requests (roots,
// elicitation) from a session, and scoping needs roots, so the server
// negotiates an initialize-handshake version.
func legacyProtocolVersions() []string {
	var out []string
	for _, v := range mcp.SupportedProtocolVersions() {
		if v < "2026-07-28" {
			out = append(out, v)
		}
	}
	return out
}

const instructions = "ctx gives scoped access to one client's Kubernetes environments and runbooks. " +
	"Call client_context first to see the client, its environments, services and recipes."

type server struct {
	deps Deps
	opts Options

	mu       sync.Mutex
	sessions map[*mcp.ServerSession]*sessionScope

	scopedTools []string // every tool but client_context
	hideOnce    sync.Once

	forwards *forwards
}

// New returns the MCP server with the read and change tools registered.
func New(d Deps, opts Options) *mcp.Server {
	_, srv := newServer(d, opts)
	return srv
}

// Run serves one session on t until it ends or ctx is cancelled, then
// stops every port-forward the session started before returning.
func Run(ctx context.Context, d Deps, opts Options, t mcp.Transport) error {
	s, srv := newServer(d, opts)
	defer s.forwards.stopAll()
	return srv.Run(ctx, t)
}

func newServer(d Deps, opts Options) (*server, *mcp.Server) {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{deps: d, opts: opts, sessions: map[*mcp.ServerSession]*sessionScope{}, forwards: newForwards()}
	srv := mcp.NewServer(&mcp.Implementation{Name: "ctx", Version: "dev"}, &mcp.ServerOptions{
		Instructions:              instructions,
		SupportedProtocolVersions: legacyProtocolVersions(),
	})
	s.addReadTools(srv)
	s.addChangeTools(srv)
	srv.AddReceivingMiddleware(s.gate(srv))
	return s, srv
}

// gate resolves the session's scope before tools/list and tools/call. With
// no client it removes every tool but client_context (stdio serves one
// session per process, so the server's tool set is that session's), and a
// call to any other tool gets the explanation without running.
func (s *server) gate(srv *mcp.Server) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if method != "tools/list" && method != "tools/call" {
				return next(ctx, method, req)
			}
			session, ok := req.GetSession().(*mcp.ServerSession)
			if !ok {
				return next(ctx, method, req)
			}
			if _, err := s.scopeFor(ctx, session); err != nil {
				s.hideOnce.Do(func() { srv.RemoveTools(s.scopedTools...) })
				if p, ok := req.GetParams().(*mcp.CallToolParamsRaw); ok && p.Name != "client_context" {
					return errorResult(err.Error()), nil
				}
			}
			return next(ctx, method, req)
		}
	}
}

// scopeFor returns the session's scope, resolving it on first use. A nil
// scope comes with the error explaining why.
func (s *server) scopeFor(ctx context.Context, session *mcp.ServerSession) (*Scope, error) {
	s.mu.Lock()
	ss := s.sessions[session]
	if ss == nil {
		ss = &sessionScope{}
		s.sessions[session] = ss
	}
	s.mu.Unlock()
	return ss.get(ctx, session, s.deps, s.opts)
}

// reloadScope reloads the session's knowledge store after a save_* write,
// so later calls see the change.
func (s *server) reloadScope(session *mcp.ServerSession) {
	s.mu.Lock()
	ss := s.sessions[session]
	s.mu.Unlock()
	if ss != nil {
		ss.reload(s.deps)
	}
}

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}

var destructive = true

// change annotates change tools; the host should ask before running them
// (ctx asks too, see confirm).
var change = &mcp.ToolAnnotations{ReadOnlyHint: false, DestructiveHint: &destructive}

// addRead registers a read tool whose body needs a usable scope. Without
// one, the tool returns the same explanation client_context gives.
func addRead[In any](srv *mcp.Server, s *server, name, desc string,
	body func(ctx context.Context, sc *Scope, in In) (string, [][]string, error)) {
	addScoped(srv, s, name, desc, readOnly,
		func(ctx context.Context, _ *mcp.CallToolRequest, sc *Scope, in In) (string, [][]string, error) {
			return body(ctx, sc, in)
		})
}

// addChange registers a change tool. Its body gets the request so it can
// ask the user (Scope.ask) through the request's session before it acts.
func addChange[In any](srv *mcp.Server, s *server, name, desc string,
	body func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in In) (string, [][]string, error)) {
	addScoped(srv, s, name, desc, change, body)
}

func addScoped[In any](srv *mcp.Server, s *server, name, desc string, ann *mcp.ToolAnnotations,
	body func(ctx context.Context, req *mcp.CallToolRequest, sc *Scope, in In) (string, [][]string, error)) {
	s.scopedTools = append(s.scopedTools, name)
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc, Annotations: ann},
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			sc, err := s.scopeFor(ctx, req.Session)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if err := sc.unavailable(); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			res, err := sc.call(name, in, func() (string, [][]string, error) { return body(ctx, req, sc, in) })
			return res, nil, err
		})
}

func joinLines(lines []string) string { return strings.Join(lines, "\n") }
