// Package ops holds the read-only operations behind the MCP tools: logs,
// cross-service trace and workload description.
package ops

import (
	"bufio"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"time"
)

const defaultCap = 400

// maxLine bounds one buffered log line; the rest of a longer line is dropped.
const maxLine = 64 << 10

// openExport creates exportDir (0700) and a new 0600 file named
// <UTC yyyymmdd-hhmmss>-<label>.log in it, adding -2, -3, ... if that name
// is taken.
func openExport(exportDir, label string) (*os.File, error) {
	if err := os.MkdirAll(exportDir, 0o700); err != nil {
		return nil, err
	}
	stamp := time.Now().UTC().Format("20060102-150405")
	for i := 1; i < 1000; i++ {
		name := fmt.Sprintf("%s-%s.log", stamp, label)
		if i > 1 {
			name = fmt.Sprintf("%s-%s-%d.log", stamp, label, i)
		}
		f, err := os.OpenFile(filepath.Join(exportDir, name), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		return f, err
	}
	return nil, errors.New("cannot pick a unique export file name")
}

// sink receives output lines. It keeps at most cap lines for Text and writes
// every line to the export file once one is wanted: from the start when
// export is set, otherwise from the first line over cap.
type sink struct {
	cap       int
	export    bool
	dir, name string

	text []string
	n    int
	file *os.File
	w    *bufio.Writer
	path string
	err  error
}

func newSink(dir, name string, cap int, export bool) *sink {
	if cap <= 0 {
		cap = defaultCap
	}
	return &sink{cap: cap, export: export, dir: dir, name: name}
}

func (s *sink) emit(line string) {
	if s.err != nil {
		return
	}
	s.n++
	if s.n <= s.cap {
		s.text = append(s.text, line)
	}
	if s.file == nil && (s.export || s.n > s.cap) {
		f, err := openExport(s.dir, s.name)
		if err != nil {
			s.err = err
			return
		}
		s.file, s.w, s.path = f, bufio.NewWriter(f), f.Name()
		for _, l := range s.text[:min(len(s.text), s.n-1)] {
			s.write(l)
		}
	}
	if s.file != nil {
		s.write(line)
	}
}

func (s *sink) write(line string) {
	if _, err := s.w.WriteString(line + "\n"); err != nil && s.err == nil {
		s.err = err
	}
}

// close flushes the export file.
func (s *sink) close() error {
	if s.file != nil {
		if err := s.w.Flush(); err != nil && s.err == nil {
			s.err = err
		}
		if err := s.file.Close(); err != nil && s.err == nil {
			s.err = err
		}
	}
	return s.err
}
