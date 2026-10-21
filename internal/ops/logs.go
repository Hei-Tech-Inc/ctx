package ops

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/kube"
)

// LogsOpts selects pods and filters their logs.
type LogsOpts struct {
	Service                          knowledge.Service
	Pod                              string
	AllPods, Previous, AllContainers bool
	Container, Since                 string
	Tail                             int
	Grep                             string // RE2
	Context                          int
	Export                           bool
	Cap                              int // lines kept in Text; default 400
}

// LogsResult is the capped text plus where the full output went.
type LogsResult struct {
	Text       string
	Lines      int
	Truncated  bool
	ExportPath string
	Pods       []string
}

type podInfo struct {
	name       string
	containers []string
}

func kindOf(svc knowledge.Service) string {
	if svc.Kind == "" {
		return "deployment"
	}
	return svc.Kind
}

// findPods returns the pods of the workload, found through its
// .spec.selector.matchLabels.
func findPods(ctx context.Context, r *kube.Runner, t kube.Target, svc knowledge.Service) ([]podInfo, error) {
	out, err := r.Run(ctx, t, "get", kindOf(svc)+"/"+svc.Name, "-o", "json")
	if err != nil {
		return nil, err
	}
	var w struct {
		Spec struct {
			Selector struct {
				MatchLabels map[string]string `json:"matchLabels"`
			} `json:"selector"`
		} `json:"spec"`
	}
	if err := json.Unmarshal(out, &w); err != nil {
		return nil, fmt.Errorf("parse %s/%s: %w", kindOf(svc), svc.Name, err)
	}
	ml := w.Spec.Selector.MatchLabels
	if len(ml) == 0 {
		return nil, fmt.Errorf("%s/%s has no spec.selector.matchLabels", kindOf(svc), svc.Name)
	}
	var pairs []string
	for k, v := range ml {
		pairs = append(pairs, k+"="+v)
	}
	sort.Strings(pairs)
	out, err = r.Run(ctx, t, "get", "pods", "-l", strings.Join(pairs, ","), "-o", "json")
	if err != nil {
		return nil, err
	}
	var pl struct {
		Items []struct {
			Metadata struct {
				Name string `json:"name"`
			} `json:"metadata"`
			Spec struct {
				Containers []struct {
					Name string `json:"name"`
				} `json:"containers"`
			} `json:"spec"`
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &pl); err != nil {
		return nil, fmt.Errorf("parse pods of %s: %w", svc.Name, err)
	}
	var pods []podInfo
	for _, it := range pl.Items {
		p := podInfo{name: it.Metadata.Name}
		for _, c := range it.Spec.Containers {
			p.containers = append(p.containers, c.Name)
		}
		pods = append(pods, p)
	}
	sort.Slice(pods, func(i, j int) bool { return pods[i].name < pods[j].name })
	return pods, nil
}

// Logs streams the logs of the service's pods through the optional grep
// filter. Text keeps at most Cap lines; everything is written to an export
// file when Export is set or the output exceeds Cap.
func Logs(ctx context.Context, r *kube.Runner, t kube.Target, exportDir string, o LogsOpts) (LogsResult, error) {
	var re *regexp.Regexp
	if o.Grep != "" {
		var err error
		if re, err = regexp.Compile(o.Grep); err != nil {
			return LogsResult{}, fmt.Errorf("invalid grep pattern: %w", err)
		}
	}
	all, err := findPods(ctx, r, t, o.Service)
	if err != nil {
		return LogsResult{}, err
	}
	var pods []podInfo
	switch {
	case o.Pod != "":
		for _, p := range all {
			if p.name == o.Pod {
				pods = append(pods, p)
			}
		}
		if len(pods) == 0 {
			return LogsResult{}, fmt.Errorf("pod %s not found for service %s", o.Pod, o.Service.Name)
		}
	case o.AllPods:
		pods = all
	case len(all) > 0:
		pods = all[:1]
	}
	if len(pods) == 0 {
		return LogsResult{}, fmt.Errorf("no pods found for service %s", o.Service.Name)
	}

	sk := newSink(exportDir, o.Service.Name, o.Cap, o.Export)
	res := LogsResult{}
	var runErr error
loop:
	for _, p := range pods {
		res.Pods = append(res.Pods, p.name)
		for _, c := range containersFor(p, o) {
			label := p.name
			if c != "" && (o.AllContainers || o.Container != "") {
				label += "/" + c
			}
			passes := []bool{false}
			if o.Previous {
				passes = []bool{true, false}
			}
			for _, prev := range passes {
				prefix := label
				if prev {
					prefix += " (previous)"
				}
				if err := streamPod(ctx, r, t, sk, re, o, p.name, c, prev, prefix+": "); err != nil {
					runErr = err
					break loop
				}
			}
		}
	}
	if cerr := sk.close(); runErr == nil {
		runErr = cerr
	}
	if runErr != nil {
		return LogsResult{}, runErr
	}
	res.Text = strings.Join(sk.text, "\n")
	res.Lines = sk.n
	res.Truncated = sk.n > sk.cap
	res.ExportPath = sk.path
	return res, nil
}

func containersFor(p podInfo, o LogsOpts) []string {
	switch {
	case o.Container != "":
		return []string{o.Container}
	case o.AllContainers && len(p.containers) > 0:
		return p.containers
	}
	return []string{""}
}

func streamPod(ctx context.Context, r *kube.Runner, t kube.Target, sk *sink, re *regexp.Regexp, o LogsOpts, pod, container string, previous bool, prefix string) error {
	args := []string{"logs", pod}
	if container != "" {
		args = append(args, "--container", container)
	}
	if previous {
		args = append(args, "--previous")
	}
	if o.Since != "" {
		args = append(args, "--since", o.Since)
	}
	if o.Tail > 0 {
		args = append(args, "--tail", strconv.Itoa(o.Tail))
	}
	lw := &lineWriter{fn: newFilter(sk, re, o.Context, prefix).line}
	err := r.Stream(ctx, t, lw, args...)
	lw.flush()
	if err != nil && previous && isNoPrevious(err) {
		return nil
	}
	return err
}

func isNoPrevious(err error) bool {
	var re *kube.RunError
	if !errors.As(err, &re) {
		return false
	}
	low := strings.ToLower(re.Stderr)
	return strings.Contains(low, "previous terminated container") && strings.Contains(low, "not found")
}

// lineWriter splits written bytes into lines for fn without keeping more
// than one partial line (cut at maxLine).
type lineWriter struct {
	fn       func(string)
	buf      []byte
	skipRest bool
}

func (l *lineWriter) Write(p []byte) (int, error) {
	n := len(p)
	for len(p) > 0 {
		i := bytes.IndexByte(p, '\n')
		if i < 0 {
			if !l.skipRest {
				l.buf = append(l.buf, p...)
				if len(l.buf) > maxLine {
					l.buf = l.buf[:maxLine]
					l.skipRest = true
				}
			}
			break
		}
		if !l.skipRest {
			l.buf = append(l.buf, p[:i]...)
			if len(l.buf) > maxLine {
				l.buf = l.buf[:maxLine]
			}
		}
		l.fn(strings.TrimSuffix(string(l.buf), "\r"))
		l.buf, l.skipRest = l.buf[:0], false
		p = p[i+1:]
	}
	return n, nil
}

func (l *lineWriter) flush() {
	if len(l.buf) > 0 {
		l.fn(strings.TrimSuffix(string(l.buf), "\r"))
		l.buf = l.buf[:0]
	}
}

// filter is a streaming grep with context. It remembers only the last
// ctxN non-matching lines (the before-context ring).
type filter struct {
	sk      *sink
	re      *regexp.Regexp
	ctxN    int
	prefix  string
	no      int // lines seen
	lastOut int // number of the last line emitted; 0 = none yet
	after   int
	ring    []ringLine
}

type ringLine struct {
	no   int
	text string
}

func newFilter(sk *sink, re *regexp.Regexp, ctxN int, prefix string) *filter {
	return &filter{sk: sk, re: re, ctxN: max(ctxN, 0), prefix: prefix}
}

func (f *filter) out(no int, text string) {
	if (f.lastOut == 0 && f.sk.n > 0) || (f.lastOut > 0 && no > f.lastOut+1) {
		f.sk.emit("--")
	}
	f.sk.emit(text)
	f.lastOut = no
}

func (f *filter) line(raw string) {
	f.no++
	text := f.prefix + raw
	if f.re == nil {
		f.sk.emit(text)
		return
	}
	switch {
	case f.re.MatchString(raw):
		for _, rl := range f.ring {
			f.out(rl.no, rl.text)
		}
		f.ring = f.ring[:0]
		f.out(f.no, text)
		f.after = f.ctxN
	case f.after > 0:
		f.out(f.no, text)
		f.after--
	case f.ctxN > 0:
		if len(f.ring) == f.ctxN {
			copy(f.ring, f.ring[1:])
			f.ring = f.ring[:f.ctxN-1]
		}
		f.ring = append(f.ring, ringLine{f.no, text})
	}
}
