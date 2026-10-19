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
}

// New returns the MCP server with the read tools registered.
func New(d Deps, opts Options) *mcp.Server {
	if d.Now == nil {
		d.Now = time.Now
	}
	s := &server{deps: d, opts: opts, sessions: map[*mcp.ServerSession]*sessionScope{}}
	srv := mcp.NewServer(&mcp.Implementation{Name: "ctx", Version: "dev"}, &mcp.ServerOptions{
		Instructions:              instructions,
		SupportedProtocolVersions: legacyProtocolVersions(),
	})
	s.addReadTools(srv)
	return srv
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

var readOnly = &mcp.ToolAnnotations{ReadOnlyHint: true}

// addRead registers a read tool whose body needs a usable scope. Without
// one, the tool returns the same explanation client_context gives.
func addRead[In any](srv *mcp.Server, s *server, name, desc string,
	body func(ctx context.Context, sc *Scope, in In) (string, [][]string, error)) {
	mcp.AddTool(srv, &mcp.Tool{Name: name, Description: desc, Annotations: readOnly},
		func(ctx context.Context, req *mcp.CallToolRequest, in In) (*mcp.CallToolResult, any, error) {
			sc, err := s.scopeFor(ctx, req.Session)
			if err != nil {
				return errorResult(err.Error()), nil, nil
			}
			if err := sc.unavailable(); err != nil {
				return errorResult(err.Error()), nil, nil
			}
			res, err := sc.call(name, in, func() (string, [][]string, error) { return body(ctx, sc, in) })
			return res, nil, err
		})
}

func joinLines(lines []string) string { return strings.Join(lines, "\n") }
