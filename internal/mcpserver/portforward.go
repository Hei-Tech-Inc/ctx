package mcpserver

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"

	"github.com/Hei-Tech-Inc/ctx/internal/kube"
)

// forward is one background kubectl port-forward.
type forward struct {
	id, env, target string
	ports           []string
	proc            *kube.Process
	out             *tailBuffer
	done            chan struct{} // closed when the process has exited
	exitErr         error         // set before done is closed
}

func (f *forward) exited() bool {
	select {
	case <-f.done:
		return true
	default:
		return false
	}
}

// stop kills the process and waits (briefly) for it to be reaped.
func (f *forward) stop() {
	if !f.exited() {
		_ = f.proc.Kill()
	}
	select {
	case <-f.done:
	case <-time.After(5 * time.Second):
	}
}

// forwards are the port-forwards the server owns, per session. A session's
// forwards are stopped when the session ends.
type forwards struct {
	mu        sync.Mutex
	next      int
	bySession map[*mcp.ServerSession]map[string]*forward
}

func newForwards() *forwards {
	return &forwards{bySession: map[*mcp.ServerSession]map[string]*forward{}}
}

// start runs port-forward in the background for session and returns it.
func (fs *forwards) start(session *mcp.ServerSession, r *kube.Runner, t kube.Target, env, target string, ports []string) (*forward, error) {
	out := &tailBuffer{max: 4096}
	proc, err := r.Start(t, out, append([]string{"port-forward", target}, ports...)...)
	if err != nil {
		return nil, err
	}
	fs.mu.Lock()
	fs.next++
	f := &forward{id: "pf-" + strconv.Itoa(fs.next), env: env, target: target, ports: ports,
		proc: proc, out: out, done: make(chan struct{})}
	m := fs.bySession[session]
	if m == nil {
		m = map[string]*forward{}
		fs.bySession[session] = m
		go func() {
			_ = session.Wait()
			fs.stopSession(session)
		}()
	}
	m[f.id] = f
	fs.mu.Unlock()
	go func() {
		f.exitErr = proc.Wait()
		close(f.done)
	}()
	return f, nil
}

// stop stops one of session's forwards and forgets it.
func (fs *forwards) stop(session *mcp.ServerSession, id string) error {
	fs.mu.Lock()
	f := fs.bySession[session][id]
	if f != nil {
		delete(fs.bySession[session], id)
	}
	fs.mu.Unlock()
	if f == nil {
		return fmt.Errorf("no port-forward %q (see port_forward list)", id)
	}
	f.stop()
	return nil
}

// list describes session's forwards, oldest first.
func (fs *forwards) list(session *mcp.ServerSession) []string {
	fs.mu.Lock()
	var fws []*forward
	for _, f := range fs.bySession[session] {
		fws = append(fws, f)
	}
	fs.mu.Unlock()
	sort.Slice(fws, func(i, j int) bool {
		a, _ := strconv.Atoi(strings.TrimPrefix(fws[i].id, "pf-"))
		b, _ := strconv.Atoi(strings.TrimPrefix(fws[j].id, "pf-"))
		return a < b
	})
	lines := make([]string, 0, len(fws))
	for _, f := range fws {
		state := "running"
		if f.exited() {
			state = "exited"
			if f.exitErr != nil {
				state += ": " + f.exitErr.Error()
			}
		}
		line := fmt.Sprintf("%s: %s %s in %s, pid %d, %s", f.id, f.target, strings.Join(f.ports, " "), f.env, f.proc.Pid(), state)
		if out := strings.TrimSpace(f.out.String()); out != "" {
			line += "\n  " + strings.ReplaceAll(out, "\n", "\n  ")
		}
		lines = append(lines, line)
	}
	return lines
}

// stopSession stops and forgets every forward of session.
func (fs *forwards) stopSession(session *mcp.ServerSession) {
	fs.mu.Lock()
	m := fs.bySession[session]
	delete(fs.bySession, session)
	fs.mu.Unlock()
	for _, f := range m {
		f.stop()
	}
}

// stopAll stops every forward of every session.
func (fs *forwards) stopAll() {
	fs.mu.Lock()
	var sessions []*mcp.ServerSession
	for s := range fs.bySession {
		sessions = append(sessions, s)
	}
	fs.mu.Unlock()
	for _, s := range sessions {
		fs.stopSession(s)
	}
}

// tailBuffer keeps the last max bytes written to it; safe for concurrent use.
type tailBuffer struct {
	mu  sync.Mutex
	max int
	buf []byte
}

func (b *tailBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf = append(b.buf, p...)
	if len(b.buf) > b.max {
		b.buf = append([]byte(nil), b.buf[len(b.buf)-b.max:]...)
	}
	return len(p), nil
}

func (b *tailBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return string(b.buf)
}
