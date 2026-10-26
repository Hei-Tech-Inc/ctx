//go:build !windows

package testutil

import (
	"bytes"
	"errors"
	"os/exec"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/creack/pty"
)

// EOF, as an answer to RunPTY, sends Ctrl+D instead of a line. It follows the
// previous answer's Enter, so it is at the start of a line and a canonical-mode
// read sees end of file.
const EOF = "EOF"

// CtrlC, as an answer to RunPTY, types Ctrl+C (the terminal's interrupt key)
// with no Enter after it.
const CtrlC = "CTRL-C"

// RunPTY timing. An answer is written once the output has been quiet for
// quietGap and the output since the previous answer shows the program waiting
// for input (see answerState): a line prompt, or a full-screen UI that has
// turned bracketed paste on. A program that has never shown such a UI and
// prints something else, or nothing (it reads without prompting), gets the
// answer after slowGap of quiet instead; one that has shows a UI program
// between screens, so its answer waits for the next prompt or UI, or for
// waitGap of quiet. The gaps are stretched by loadScale
// on a busy machine, where a program can pause that long between its writes.
const (
	quietGap = 300 * time.Millisecond
	slowGap  = time.Second
	// waitGap is how long output that is not waiting for input (stateWait)
	// must stay quiet before the answer is sent anyway: a UI program that
	// later reads without a prompt or a UI still gets its answers.
	waitGap = 3 * slowGap
	poll    = 10 * time.Millisecond
	// maxScale caps loadScale.
	maxScale = 4
	// drainGap bounds how long output is read after the command exits, for
	// background children that keep the terminal open.
	drainGap = 2 * time.Second
)

// RunPTY runs cmd with a new pseudo-terminal as its stdin, stdout, stderr and
// controlling terminal (replacing any streams set on cmd): echo off, a 120×40
// window. Each answer is typed followed by Enter (\r) when the program waits
// for input (see quietGap); EOF sends Ctrl+D and CtrlC sends Ctrl+C. Answers left over when cmd exits
// are not sent. It returns everything cmd wrote, with \r\n turned into \n, and
// its exit code (128+signal when a signal killed it, as a shell reports). If
// cmd has not exited within timeout, it is killed and the test fails with the
// transcript so far.
//
// The timing is a heuristic for programs without a full-screen UI: one that
// pauses for more than slowGap (times loadScale) before printing its prompt
// can receive an answer early. Answers typed early still wait in the
// terminal's line buffer for a canonical-mode read. Once a program has shown
// a full-screen UI, answers wait for a prompt or the next UI instead (input
// typed between two UIs can be read and dropped by the one shutting down), or
// for waitGap of quiet when neither comes.
func RunPTY(t *testing.T, cmd *exec.Cmd, answers []string, timeout time.Duration) (transcript string, exit int) {
	t.Helper()
	ptmx, tty, err := pty.Open()
	if err != nil {
		t.Fatalf("RunPTY: open pty: %v", err)
	}
	defer func() { _ = ptmx.Close() }()
	if err := echoOff(tty); err != nil {
		_ = tty.Close()
		t.Fatalf("RunPTY: echo off: %v", err)
	}
	if err := pty.Setsize(ptmx, &pty.Winsize{Rows: 40, Cols: 120}); err != nil {
		_ = tty.Close()
		t.Fatalf("RunPTY: window size: %v", err)
	}
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setsid = true
	cmd.SysProcAttr.Setctty = true
	cmd.SysProcAttr.Ctty = 0 // the child's stdin, the tty
	err = cmd.Start()
	_ = tty.Close()
	if err != nil {
		t.Fatalf("RunPTY: start: %v", err)
	}

	var (
		mu      sync.Mutex
		out     []byte
		lastOut = time.Now()
	)
	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buf := make([]byte, 4096)
		for {
			n, err := ptmx.Read(buf)
			if n > 0 {
				mu.Lock()
				out = append(out, buf[:n]...)
				lastOut = time.Now()
				mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	waitDone := make(chan error, 1)
	go func() { waitDone <- cmd.Wait() }()
	snapshot := func() string {
		mu.Lock()
		defer mu.Unlock()
		return strings.ReplaceAll(string(out), "\r\n", "\n")
	}
	fail := func(format string, args ...any) {
		t.Helper()
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		<-waitDone
		t.Fatalf("RunPTY: "+format+"; transcript so far:\n%s", append(args, snapshot())...)
	}

	deadline := time.Now().Add(timeout)
	var waitErr error
	exited := false
	mark, lastWrite := 0, time.Now()
	for i := 0; i < len(answers) && !exited; {
		select {
		case waitErr = <-waitDone:
			exited = true
			continue
		default:
		}
		if time.Now().After(deadline) {
			fail("timed out after %v waiting to send answer %d (%q)", timeout, i+1, answers[i])
		}
		mu.Lock()
		since := lastOut
		state := answerState(out[mark:], bytes.Contains(out, pasteOn))
		mu.Unlock()
		if lastWrite.After(since) {
			since = lastWrite
		}
		gap := slowGap
		switch state {
		case stateReady:
			gap = quietGap
		case stateWait:
			gap = waitGap
		}
		if time.Since(since) < scaled(gap) {
			time.Sleep(poll)
			continue
		}
		data := []byte(answers[i] + "\r")
		switch answers[i] {
		case EOF:
			data = []byte{0x04}
		case CtrlC:
			data = []byte{0x03}
		}
		if _, err := ptmx.Write(data); err != nil {
			fail("write answer %d: %v", i+1, err)
		}
		mu.Lock()
		mark = len(out)
		mu.Unlock()
		lastWrite = time.Now()
		i++
	}
	if !exited {
		select {
		case waitErr = <-waitDone:
		case <-time.After(time.Until(deadline)):
			fail("timed out after %v waiting for exit", timeout)
		}
	}
	select {
	case <-readDone:
	case <-time.After(scaled(drainGap)):
	}

	exit = 0
	var ee *exec.ExitError
	switch {
	case waitErr == nil:
	case errors.As(waitErr, &ee):
		exit = ee.ExitCode()
		if ws, ok := ee.Sys().(syscall.WaitStatus); ok && ws.Signaled() {
			exit = 128 + int(ws.Signal())
		}
	default:
		t.Fatalf("RunPTY: wait: %v", waitErr)
	}
	return snapshot(), exit
}

// loadScale is the one-minute load average per CPU, between 1 and maxScale:
// how much busier than fully used the machine is. 1 where the load average
// is not available.
func loadScale() float64 {
	l, ok := loadAvg()
	if !ok {
		return 1
	}
	return min(max(l/float64(runtime.NumCPU()), 1), maxScale)
}

// scaled stretches gap by the current loadScale.
func scaled(gap time.Duration) time.Duration {
	return time.Duration(float64(gap) * loadScale())
}

// Bracketed paste mode on and off: a full-screen UI (bubbletea, readline)
// turns it on once it is reading input and off when it ends.
var (
	pasteOn  = []byte("\x1b[?2004h")
	pasteOff = []byte("\x1b[?2004l")
)

const (
	stateReady = iota // waiting for input: answer after quietGap
	stateMaybe        // unclear: answer after slowGap
	stateWait         // not waiting for input yet: answer only after waitGap
)

// answerState tells from tail, the output since the previous answer, whether
// the program waits for the next one. ui is whether it has ever shown a
// full-screen UI.
//
// A UI turned on in tail and not off again is ready. After a UI turned off
// (and while a UI program prints between two UIs) only a line prompt is: an
// answer typed before the next UI starts can be swallowed by the one
// shutting down; RunPTY still answers after waitGap of quiet. A program that
// never showed a UI may read without prompting, so silence or other output
// only delays the answer.
func answerState(tail []byte, ui bool) int {
	on, off := bytes.LastIndex(tail, pasteOn), bytes.LastIndex(tail, pasteOff)
	if on > off {
		return stateReady
	}
	if off >= 0 {
		tail = tail[off+len(pasteOff):]
	}
	switch {
	case promptLike(tail):
		return stateReady
	case ui:
		return stateWait
	}
	return stateMaybe
}

// promptLike reports whether output ends like a line prompt: after dropping
// escape sequences and trailing spaces, its last byte is one of : ? > ] ).
func promptLike(b []byte) bool {
	s := bytes.TrimRight(stripANSI(b), " \t")
	return len(s) > 0 && strings.IndexByte(":?>])", s[len(s)-1]) >= 0
}

// stripANSI drops CSI (ESC [ … final), OSC (ESC ] … BEL or ESC \) and other
// two-byte ESC sequences from b.
func stripANSI(b []byte) []byte {
	var o []byte
	for i := 0; i < len(b); i++ {
		if b[i] != 0x1b {
			o = append(o, b[i])
			continue
		}
		if i+1 >= len(b) {
			break
		}
		switch b[i+1] {
		case '[':
			j := i + 2
			for j < len(b) && (b[j] < 0x40 || b[j] > 0x7e) {
				j++
			}
			i = j
		case ']':
			j := i + 2
			for j < len(b) && b[j] != 0x07 && !(b[j] == 0x1b && j+1 < len(b) && b[j+1] == '\\') {
				j++
			}
			if j < len(b) && b[j] == 0x1b {
				j++
			}
			i = j
		default:
			i++
		}
	}
	return o
}
