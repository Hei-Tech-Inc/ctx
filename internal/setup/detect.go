package setup

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/ghacct"
)

// Detected is what the wizard found on the machine, each list in the order
// lib/core.sh's detect_* helpers print it.
type Detected struct {
	SSHKeys, GHAccounts, AWSProfiles, KubeContexts, AzureSubs, GCPProjects []string
}

// Detect ports detect_ssh_keys, detect_gh_accounts, detect_aws_profiles,
// detect_kube_contexts, detect_azure_subs and detect_gcp_projects. gh, az and
// gcloud are bounded by timeout (8s in production); kubectl and the file
// reads are not, as in Bash. A missing or failing tool yields an empty list.
//
// AWS profiles are sorted byte-wise (Bash's `sort -u` under LC_ALL=C); under
// a UTF-8 locale Bash's order can differ for mixed-case names.
func Detect(home string, timeout time.Duration) Detected {
	return Detected{
		SSHKeys:      detectSSHKeys(home),
		GHAccounts:   detectGH(timeout),
		AWSProfiles:  detectAWS(home),
		KubeContexts: lines(runTool(0, false, "kubectl", "config", "get-contexts", "-o", "name")),
		AzureSubs:    lines(runTool(timeout, false, "az", "account", "list", "--query", "[].name", "-o", "tsv")),
		GCPProjects:  lines(runTool(timeout, false, "gcloud", "projects", "list", "--format=value(projectId)")),
	}
}

// ScanIntro prints the lines before the scan: the "Scanning" info line, then
// either the plain "Looking for" dim line (spin == nil) or whatever spin does
// (the gum spinner), then the "Detecting" dim line. The caller prints the
// blank line, runs Detect, and prints Report between blank lines.
func ScanIntro(u Out, spin func()) {
	u.Info("Scanning local accounts/keys/profiles (this can take a few seconds)...")
	if spin != nil {
		spin()
	} else {
		u.Dim("  Looking for SSH keys, gh auth, and cloud profile references...")
	}
	u.Dim("  Detecting SSH keys / GitHub accounts / cloud profiles...")
}

// Report prints the Found/No summary lines (not the blank lines around them).
func (d Detected) Report(u Out) {
	if n := len(d.SSHKeys); n > 0 {
		u.Success(fmt.Sprintf("Found %d SSH key(s)", n))
	} else {
		u.Dim("  No SSH keys in ~/.ssh/")
	}
	if n := len(d.GHAccounts); n > 0 {
		u.Success(fmt.Sprintf("Found %d gh account(s)", n))
	} else {
		u.Dim("  No gh accounts (run: gh auth login)")
	}
	if n := len(d.AWSProfiles); n > 0 {
		u.Success(fmt.Sprintf("Found %d AWS profile(s)", n))
	}
	if n := len(d.KubeContexts); n > 0 {
		u.Success(fmt.Sprintf("Found %d kubectl context(s)", n))
	}
	if n := len(d.AzureSubs); n > 0 {
		u.Success(fmt.Sprintf("Found %d Azure subscription(s)", n))
	}
	if n := len(d.GCPProjects); n > 0 {
		u.Success(fmt.Sprintf("Found %d GCP project(s)", n))
	}
}

func isFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}

// detectSSHKeys lists ~/.ssh/*.pub that have a private file beside them, in
// glob (byte-wise) order. Like a shell glob, it skips dotfiles.
func detectSSHKeys(home string) []string {
	dir := filepath.Join(home, ".ssh")
	ents, err := os.ReadDir(dir) // sorted by filename
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") || !strings.HasSuffix(n, ".pub") {
			continue
		}
		pub := filepath.Join(dir, n)
		priv := strings.TrimSuffix(pub, ".pub")
		if isFile(pub) && isFile(priv) {
			out = append(out, priv)
		}
	}
	return out
}

func detectGH(timeout time.Duration) []string {
	return ghacct.Parse(runTool(timeout, true, "gh", "auth", "status"))
}

func detectAWS(home string) []string {
	var all []string
	if b, err := os.ReadFile(filepath.Join(home, ".aws", "credentials")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "[") {
				all = append(all, strings.NewReplacer("[", "", "]", "").Replace(l))
			}
		}
	}
	if b, err := os.ReadFile(filepath.Join(home, ".aws", "config")); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			if strings.HasPrefix(l, "[profile ") {
				l = strings.Replace(l, "[profile ", "", 1)
				all = append(all, strings.Replace(l, "]", "", 1))
			}
		}
	}
	sort.Strings(all)
	var out []string
	for i, p := range all {
		if p != "" && (i == 0 || p != all[i-1]) {
			out = append(out, p)
		}
	}
	return out
}

// runTool runs name with args and returns its stdout (plus stderr when
// merge). A missing tool, a failure or a timeout yield whatever output
// arrived before; stderr is otherwise discarded. timeout 0 means unbounded.
func runTool(timeout time.Duration, merge bool, name string, args ...string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	ctx := context.Background()
	if timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, path, args...)
	var buf bytes.Buffer
	cmd.Stdout = &buf
	if merge {
		cmd.Stderr = &buf
	}
	prepareKill(cmd)
	cmd.WaitDelay = 500 * time.Millisecond
	scans.Lock()
	if scans.stopped || cmd.Start() != nil {
		scans.Unlock()
		return buf.String()
	}
	scans.running[cmd] = true
	scans.Unlock()
	_ = cmd.Wait()
	scans.Lock()
	delete(scans.running, cmd)
	scans.Unlock()
	return buf.String()
}

// scans holds the detector processes in flight, for KillScans.
var scans = struct {
	sync.Mutex
	running map[*exec.Cmd]bool
	stopped bool
}{running: map[*exec.Cmd]bool{}}

// KillScans kills every running detector (with its process group on Unix)
// and makes later ones return no output without starting: for a SIGINT
// handler about to exit, since a detector in its own process group does not
// get the terminal's interrupt.
func KillScans() {
	scans.Lock()
	defer scans.Unlock()
	scans.stopped = true
	for cmd := range scans.running {
		_ = killScan(cmd)
	}
}

// lines splits output into non-empty lines.
func lines(s string) []string {
	var out []string
	for _, l := range strings.Split(s, "\n") {
		if l != "" {
			out = append(out, l)
		}
	}
	return out
}
