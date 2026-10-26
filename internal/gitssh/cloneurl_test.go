package gitssh

import (
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestCloneURLMatchesBash sources lib/core.sh and compares, URL by URL.
func TestCloneURLMatchesBash(t *testing.T) {
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("no bash")
	}
	core, _ := filepath.Abs("../../lib/core.sh")
	urls := []string{
		"git@github.com:o/r", "git@github.com:o/r.git", "git@github.com:",
		"ssh://git@github.com/o/r", "ssh://git@github.com/o/r.git", "ssh://git@github.com//o/r", "ssh://git@github.com/",
		"https://github.com/o/r", "https://github.com/o/r.git", "https://github.com//o/r.git", "https://github.com/",
		"https://gitlab.com/o/r", "git@gitlab.com:o/r", "http://github.com/o/r", "/tmp/local", "",
		"git@github.com:o/r.git.git", "https://github.com/o/r.git.git",
	}
	for _, y := range []bool{true, false} {
		flag := "n"
		if y {
			flag = "y"
		}
		for _, u := range urls {
			cmd := exec.Command(bash, "-c", `source "$1"; github_clone_url_for_profile "$2" "$3" "$4"`, "x", core, "acme", u, flag)
			b, err := cmd.Output()
			if err != nil {
				t.Fatalf("bash %q: %v", u, err)
			}
			want := strings.TrimSuffix(string(b), "\n")
			if got := CloneURLForProfileOpt("acme", u, y); got != want {
				t.Errorf("%q (y=%v): got %q, bash %q", u, y, got, want)
			}
			if y {
				if got := CloneURLForProfile("acme", u); got != want {
					t.Errorf("%q: CloneURLForProfile %q, bash %q", u, got, want)
				}
			}
		}
	}
}
