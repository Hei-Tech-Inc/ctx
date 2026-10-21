package resolve

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestMatchesBash checks Path against ctx_resolve_path_with_root in lib/core.sh.
// The trailing-slash WORK_DIR case is excluded: Bash does not match it
// (it compares against "<wd>//"), while Go deliberately normalizes it.
func TestMatchesBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash not available")
	}
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git not available")
	}
	core, err := filepath.Abs("../../lib/core.sh")
	if err != nil {
		t.Fatal(err)
	}
	td := realTemp(t)
	prof := filepath.Join(td, "profiles")
	mkdirs(t, prof)

	tpl := filepath.Join(td, "gittpl")
	mkdirs(t, tpl)
	gitInit := func(dir string) {
		t.Helper()
		cmd := exec.Command(git, "init", "-q", dir)
		cmd.Env = append(os.Environ(), "GIT_TEMPLATE_DIR="+tpl)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git init: %v: %s", err, out)
		}
	}

	var pwds []string
	add := func(rel ...string) {
		for _, r := range rel {
			pwds = append(pwds, filepath.Join(td, r))
		}
	}

	conf(t, prof, "short", filepath.Join(td, "root"))
	conf(t, prof, "long", filepath.Join(td, "root/client"))
	mkdirs(t, filepath.Join(td, "root/client/app"))
	add("root/client/app", "root", "root/other")

	mkdirs(t, filepath.Join(td, "w/repo/sub"))
	gitInit(filepath.Join(td, "w/repo"))
	write(t, filepath.Join(td, "w/repo/.ctx"), "profile=short\n")
	add("w/repo/sub", "w/repo", "w")

	mkdirs(t, filepath.Join(td, "m/repo/deep"))
	gitInit(filepath.Join(td, "m/repo"))
	write(t, filepath.Join(td, "m/repo/.ctx"), "profile=long\n")
	write(t, filepath.Join(td, "m/repo/deep/.ctx"), "profile=short\n")
	add("m/repo/deep", "m/repo")

	mkdirs(t, filepath.Join(td, "x/repo/sub"))
	gitInit(filepath.Join(td, "x/repo"))
	write(t, filepath.Join(td, "x/repo/.ctx"), "profile=ghost\n")
	add("x/repo/sub")

	mkdirs(t, filepath.Join(td, "clients/hubtel/deep"))
	conf(t, prof, "deladetech", filepath.Join(td, "clients"))
	conf(t, prof, "hubtel", "")
	add("clients/hubtel/deep", "clients/hubtel", "clients")

	// profile dirs for w, m, x
	conf(t, prof, "wprof", filepath.Join(td, "w"))
	conf(t, prof, "mprof", filepath.Join(td, "m"))
	conf(t, prof, "xprof", filepath.Join(td, "x"))
	pwds = append(pwds, "/nonexistent/nope/zz", td)

	for _, pwd := range pwds {
		t.Run(strings.TrimPrefix(pwd, td), func(t *testing.T) {
			out, err := exec.Command(bash, "-c",
				`source "$1"; ctx_resolve_path_with_root "$2" "$3"`, "_", core, pwd, prof).Output()
			if err != nil {
				t.Fatalf("bash: %v", err)
			}
			wantName, wantDir, _ := strings.Cut(string(out), "\t")
			n, wd, err := Path(pwd, prof)
			if err != nil {
				t.Fatal(err)
			}
			if n != wantName || wd != wantDir {
				t.Fatalf("Go (%q, %q) != Bash (%q, %q)", n, wd, wantName, wantDir)
			}
		})
	}
}
