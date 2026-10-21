package knowledge

import (
	"bufio"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

// ActivityEntry is one audited tool call. Callers redact secrets before Append.
type ActivityEntry struct {
	Time       time.Time      `json:"time"`
	Tool       string         `json:"tool"`
	Env        string         `json:"env,omitempty"`
	Cluster    string         `json:"cluster,omitempty"`
	Namespace  string         `json:"namespace,omitempty"`
	Args       map[string]any `json:"args,omitempty"`
	Argv       [][]string     `json:"argv,omitempty"`
	Exit       int            `json:"exit"`
	DurationMS int64          `json:"duration_ms"`
	Result     string         `json:"result"` // ok|error|declined|not_run
}

// ActivityFilter selects entries; zero fields match everything.
type ActivityFilter struct {
	Env   string
	Tool  string
	Since time.Time
	Limit int
}

// Activity is the append-only activity.jsonl log.
type Activity struct{ path string }

// OpenActivity returns the activity log under the knowledge root.
func OpenActivity(root string) *Activity {
	return &Activity{path: filepath.Join(root, "activity.jsonl")}
}

// Append writes one JSON line with a single write so concurrent appenders do
// not interleave.
func (a *Activity) Append(e ActivityEntry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(a.path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(a.path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	if _, err := f.Write(append(line, '\n')); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}

// Query returns matching entries, newest first, up to f.Limit (0 = all).
// Malformed lines are skipped.
func (a *Activity) Query(f ActivityFilter) ([]ActivityEntry, error) {
	file, err := os.Open(a.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	var all []ActivityEntry
	sc := bufio.NewScanner(file)
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)
	for sc.Scan() {
		var e ActivityEntry
		if json.Unmarshal(sc.Bytes(), &e) != nil {
			continue
		}
		if (f.Env != "" && e.Env != f.Env) || (f.Tool != "" && e.Tool != f.Tool) ||
			(!f.Since.IsZero() && e.Time.Before(f.Since)) {
			continue
		}
		all = append(all, e)
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	for i, j := 0, len(all)-1; i < j; i, j = i+1, j-1 {
		all[i], all[j] = all[j], all[i]
	}
	if f.Limit > 0 && len(all) > f.Limit {
		all = all[:f.Limit]
	}
	return all, nil
}
