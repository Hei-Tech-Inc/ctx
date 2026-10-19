package ops

import (
	"bufio"
	"context"
	"errors"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/kube"
)

// TraceOpts follows one pattern (a request id, an MSISDN) across services.
type TraceOpts struct {
	Services       []knowledge.Service
	Pattern, Since string
	Previous       bool
	Context        int
	Cap            int
}

const traceParallel = 4

// tsRe matches an RFC3339 or "yyyy-mm-dd hh:mm:ss[.fff]" timestamp at the
// start of a line.
var tsRe = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2})[T ](\d{2}:\d{2}:\d{2})(\.\d+)?(Z|[+-]\d{2}:?\d{2})?`)

func parseTS(msg string) (time.Time, bool) {
	m := tsRe.FindStringSubmatch(msg)
	if m == nil {
		return time.Time{}, false
	}
	tz := m[4]
	if tz == "" {
		tz = "Z"
	} else if tz != "Z" && !strings.Contains(tz, ":") {
		tz = tz[:3] + ":" + tz[3:]
	}
	ts, err := time.Parse(time.RFC3339Nano, m[1]+"T"+m[2]+m[3]+tz)
	return ts, err == nil
}

// logMessage drops the "<pod>[/<container>][ (previous)]: " prefix.
func logMessage(line string) string {
	if _, msg, ok := strings.Cut(line, ": "); ok {
		return msg
	}
	return line
}

type traceLine struct {
	text  string
	timed bool      // the line itself starts with a timestamp
	ts    time.Time // sort key; continuation lines inherit the previous line's
}

// Trace runs Logs for each service (four at a time) with the pattern as
// the grep, then merges the lines: by timestamp when at least 80% of them
// start with one, otherwise grouped by service. The merged output is always
// exported. Merging holds the matched lines in memory, so use a pattern.
func Trace(ctx context.Context, r *kube.Runner, t kube.Target, exportDir string, o TraceOpts) (LogsResult, error) {
	type result struct {
		res LogsResult
		err error
	}
	results := make([]result, len(o.Services))
	var wg sync.WaitGroup
	sem := make(chan struct{}, traceParallel)
	for i, svc := range o.Services {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			res, err := Logs(ctx, r, t, exportDir, LogsOpts{
				Service: svc, AllPods: true, AllContainers: true, Previous: o.Previous,
				Since: o.Since, Grep: o.Pattern, Context: o.Context, Export: true, Cap: 1,
			})
			results[i] = result{res, err}
		}()
	}
	wg.Wait()

	var errs []error
	var lines []traceLine
	var pods []string
	for _, rs := range results {
		if rs.res.ExportPath != "" {
			defer os.Remove(rs.res.ExportPath) // per-service files are merged below
		}
		if rs.err != nil {
			errs = append(errs, rs.err)
			continue
		}
		pods = append(pods, rs.res.Pods...)
		if rs.res.ExportPath == "" {
			continue
		}
		ls, err := readLines(rs.res.ExportPath)
		if err != nil {
			errs = append(errs, err)
			continue
		}
		lines = append(lines, ls...)
	}
	if err := errors.Join(errs...); err != nil {
		return LogsResult{}, err
	}

	timed, matched := 0, 0
	for _, l := range lines {
		if l.text == "--" {
			continue
		}
		matched++
		if l.timed {
			timed++
		}
	}
	if matched > 0 && timed*5 >= matched*4 {
		kept := lines[:0]
		for _, l := range lines {
			if l.text != "--" {
				kept = append(kept, l)
			}
		}
		lines = kept
		sort.SliceStable(lines, func(a, b int) bool { return lines[a].ts.Before(lines[b].ts) })
	}

	sk := newSink(exportDir, "trace", o.Cap, true)
	for _, l := range lines {
		sk.emit(l.text)
	}
	if err := sk.close(); err != nil {
		return LogsResult{}, err
	}
	return LogsResult{
		Text: strings.Join(sk.text, "\n"), Lines: sk.n, Truncated: sk.n > sk.cap,
		ExportPath: sk.path, Pods: pods,
	}, nil
}

func readLines(path string) ([]traceLine, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	var out []traceLine
	var last time.Time
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), maxLine+4096)
	for sc.Scan() {
		tl := traceLine{text: sc.Text(), ts: last}
		if tl.text != "--" {
			if ts, ok := parseTS(logMessage(tl.text)); ok {
				tl.timed, tl.ts, last = true, ts, ts
			}
		}
		out = append(out, tl)
	}
	return out, sc.Err()
}
