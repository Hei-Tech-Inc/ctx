package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
)

func TestWorkdirPromptMatchesBash(t *testing.T) {
	f := newFixture(t)
	clients := filepath.Join(f.home, "clients")
	extra := filepath.Join(f.home, "extra")
	extra2 := filepath.Join(f.home, "extra2")
	deep := filepath.Join(clients, "a", "b", "c")
	for _, d := range []string{deep, filepath.Join(extra, "x", "y", "z"), extra2, filepath.Join(f.home, "elsewhere")} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.profile("acme", "WORK_DIR", clients)

	cases := []struct {
		name, config, pwd string
	}{
		{"exact WORK_DIR", "", clients},
		{"depth 1", "", filepath.Join(clients, "a")},
		{"depth 2", "", filepath.Join(clients, "a", "b")},
		{"depth 3 beyond default", "", deep},
		{"depth 0 configured, exact", "prompt_workdir_max_depth=0\n", clients},
		{"depth 0 configured, child", "prompt_workdir_max_depth=0\n", filepath.Join(clients, "a")},
		{"invalid depth uses default", "prompt_workdir_max_depth=abc\n", filepath.Join(clients, "a", "b")},
		{"extra path exact", "prompt_extra_paths=" + extra + ":\n", extra},
		{"extra path last piece without trailing colon is ignored (Bash read quirk)", "prompt_extra_paths=" + extra + "\n", extra},
		{"extra path below", "prompt_extra_paths=" + extra + ":\n", filepath.Join(extra, "x", "y", "z")},
		{"extra path list, second", "prompt_extra_paths=" + extra + "/:" + extra2 + ":\n", extra2},
		{"extra path list, first of two", "prompt_extra_paths=" + extra + "/:" + extra2 + "\n", extra},
		{"extra path list, last of two", "prompt_extra_paths=" + extra + "/:" + extra2 + "\n", extra2},
		{"extra path tilde", "prompt_extra_paths=~/extra:\n", filepath.Join(extra, "x")},
		{"extra path empty pieces", "prompt_extra_paths=::" + extra + ":\n", extra},
		{"outside everything", "prompt_extra_paths=" + extra + ":\n", filepath.Join(f.home, "elsewhere")},
		{"outside, no config", "", filepath.Join(f.home, "elsewhere")},
		{"prefix sibling is not under", "prompt_extra_paths=" + extra + ":\n", extra2},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f.write("config", c.config)
			want, code := f.bash(c.pwd, "workdir-prompt")
			if code != 0 {
				t.Fatalf("bash exit %d", code)
			}
			var got bytes.Buffer
			if err := WorkdirPrompt(&got, c.pwd); err != nil {
				t.Fatal(err)
			}
			if got.String() != want {
				t.Errorf("bash %q, go %q", want, got.String())
			}
		})
	}
}

func TestWorkdirPromptNoProfiles(t *testing.T) {
	f := newFixture(t)
	want, _ := f.bash(f.home, "workdir-prompt")
	var got bytes.Buffer
	if err := WorkdirPrompt(&got, f.home); err != nil {
		t.Fatal(err)
	}
	if got.String() != want || got.Len() != 0 {
		t.Errorf("bash %q, go %q; want both empty", want, got.String())
	}
}
