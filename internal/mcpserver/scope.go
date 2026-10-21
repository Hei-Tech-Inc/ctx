package mcpserver

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

// Scope is everything one session may reach: one profile, its knowledge
// store, the redactor for its secrets and its activity log. When the store
// fails to load, Store is nil and LoadErr says why.
type Scope struct {
	Profile  *profile.Profile
	Store    *knowledge.Store
	LoadErr  error
	Redact   *secret.Redactor
	Activity *knowledge.Activity

	deps Deps
}

// noScopeError is the "no client for this session" answer. resolveScope
// returns no other error, so the answer is fixed for the session.
type noScopeError struct{ reason string }

func (e *noScopeError) Error() string {
	return "No client is active: " + e.reason + ". ctx tools work only inside a client's WORK_DIR " +
		"(or start ctx mcp with --profile NAME)."
}

func noScope(format string, a ...any) error {
	return &noScopeError{reason: fmt.Sprintf(format, a...)}
}

// resolveScope picks the session's profile: --profile when set, else the
// client's file:// roots (all matching roots must agree), else Cwd. It
// never falls back to the globally active profile.
func resolveScope(ctx context.Context, session *mcp.ServerSession, d Deps, opts Options) (*Scope, error) {
	name, err := resolveProfileName(ctx, session, d, opts)
	if err != nil {
		return nil, err
	}
	p, err := profile.Load(d.ProfilesDir, name)
	if err != nil {
		return nil, noScope("%v", err)
	}
	return newScope(p, d), nil
}

func validProfileName(name string) bool {
	return name != "" && name != "." && name != ".." && !strings.ContainsAny(name, "/\\")
}

func resolveProfileName(ctx context.Context, session *mcp.ServerSession, d Deps, opts Options) (string, error) {
	if opts.Profile != "" {
		if !validProfileName(opts.Profile) {
			return "", noScope("invalid profile name %q", opts.Profile)
		}
		return opts.Profile, nil
	}
	roots, err := listRoots(ctx, session)
	if err != nil {
		return "", err
	}
	if len(roots) == 0 {
		name, _, err := resolve.Path(opts.Cwd, d.ProfilesDir)
		if err != nil {
			return "", noScope("cannot resolve %s: %v", opts.Cwd, err)
		}
		if name == "" {
			return "", noScope("%s is not inside any client's WORK_DIR", opts.Cwd)
		}
		return name, nil
	}
	found := map[string]bool{}
	for _, r := range roots {
		name, _, err := resolve.Path(r, d.ProfilesDir)
		if err != nil {
			return "", noScope("cannot resolve %s: %v", r, err)
		}
		if name != "" {
			found[name] = true
		}
	}
	names := make([]string, 0, len(found))
	for n := range found {
		names = append(names, n)
	}
	sort.Strings(names)
	switch len(names) {
	case 0:
		return "", noScope("none of the open folders (%s) is inside a client's WORK_DIR", strings.Join(roots, ", "))
	case 1:
		return names[0], nil
	default:
		return "", noScope("folders belong to more than one client: %s", strings.Join(names, ", "))
	}
}

// listRoots returns the local paths of the client's file:// roots, or nil
// when the client does not support roots.
func listRoots(ctx context.Context, session *mcp.ServerSession) ([]string, error) {
	ip := session.InitializeParams()
	if ip == nil || ip.Capabilities == nil || ip.Capabilities.RootsV2 == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	res, err := session.ListRoots(ctx, nil)
	if err != nil {
		return nil, noScope("could not list the client's folders (roots): %v", err)
	}
	var paths []string
	for _, r := range res.Roots {
		u, err := url.Parse(r.URI)
		if err != nil || u.Scheme != "file" || u.Path == "" {
			continue
		}
		paths = append(paths, u.Path)
	}
	if len(res.Roots) > 0 && len(paths) == 0 {
		return nil, noScope("the client reported folders but none is a local file:// path")
	}
	return paths, nil
}

// newScope loads the profile's knowledge store, redactor and activity log.
func newScope(p *profile.Profile, d Deps) *Scope {
	root := knowledge.Root(p.Name)
	s := &Scope{Profile: p, Activity: knowledge.OpenActivity(root), deps: d}
	store, err := knowledge.Load(root)
	if err != nil {
		s.LoadErr = err
	} else {
		s.Store = store
	}
	s.Redact = secret.LoadRedactor(p, s.Store, d.Getter)
	return s
}

// unavailable is the explanation every tool but client_context returns
// when there is no usable scope; nil when the scope is usable.
func (s *Scope) unavailable() error {
	if s.LoadErr != nil {
		return fmt.Errorf("knowledge for client %s could not be loaded: %s (fix the file in %s and call the tool again)",
			s.Profile.Name, s.Redact.String(s.LoadErr.Error()), knowledge.Root(s.Profile.Name))
	}
	return nil
}

// sessionScope caches one session's scope. The answer (a scope, or no
// client) is kept for the session, so the tool list never changes back; a
// scope whose knowledge failed to load is rebuilt on the next call so a
// fixed file is picked up. When the client's roots change (stale), the
// next get resolves them again: a different client, or none, drops the
// session to no scope for good. It never switches to another client.
type sessionScope struct {
	mu    sync.Mutex
	done  bool
	stale bool
	scope *Scope
	err   error
}

// markStale notes that the client's roots changed.
func (ss *sessionScope) markStale() {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	ss.stale = ss.done
}

// get returns the session's scope. dropped is true on the call that took a
// scope away because the roots changed.
func (ss *sessionScope) get(ctx context.Context, session *mcp.ServerSession, d Deps, opts Options) (sc *Scope, dropped bool, err error) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if !ss.done {
		ss.scope, ss.err = resolveScope(ctx, session, d, opts)
		ss.done = true
		return ss.scope, false, ss.err
	}
	if ss.stale {
		ss.stale = false
		if ss.scope != nil && opts.Profile == "" {
			if err := rootsStillMatch(ctx, session, d, opts, ss.scope.Profile.Name); err != nil {
				ss.scope, ss.err = nil, err
				return nil, true, ss.err
			}
		}
	}
	if ss.scope != nil && ss.scope.LoadErr != nil {
		ss.scope = newScope(ss.scope.Profile, d)
	}
	return ss.scope, false, ss.err
}

// rootsStillMatch resolves the client's roots again and returns the
// no-scope error to switch to unless they still resolve to current.
func rootsStillMatch(ctx context.Context, session *mcp.ServerSession, d Deps, opts Options, current string) error {
	name, err := resolveProfileName(ctx, session, d, opts)
	if err == nil && name == current {
		return nil
	}
	reason := fmt.Sprintf("they now belong to client %s", name)
	var ns *noScopeError
	if errors.As(err, &ns) {
		reason = ns.reason
	} else if err != nil {
		reason = err.Error()
	}
	return noScope("the open folders changed during this session (%s), so ctx stopped serving client %s; start a new session", reason, current)
}

// reload rebuilds the scope from disk (after a save_* write).
func (ss *sessionScope) reload(d Deps) {
	ss.mu.Lock()
	defer ss.mu.Unlock()
	if ss.scope != nil {
		ss.scope = newScope(ss.scope.Profile, d)
	}
}
