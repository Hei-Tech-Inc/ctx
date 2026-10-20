package cli

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

type deactivateRun struct {
	stdout, stderr, config string
	code                   int
	sshAdd                 [][]string
}

func TestDeactivateMatchesBash(t *testing.T) {
	const profileBody = "SECRET_KEYS=\"API_KEY DB_PASS 1bad\"\nEXTRA_ENVS=\"FOO=bar\nBAZ\n\n2x=y\"\nWORK_DIR=\"%s\"\n"
	cases := []struct {
		name   string
		config string
		args   []string
		quiet  bool
		inWork bool // run from inside the profile's WORK_DIR
		noProf bool // do not write the profile file
	}{
		{name: "nothing active", config: "work_root=/x\n"},
		{name: "nothing active quiet", config: "", quiet: true},
		{name: "active bash", config: "active=acme\nactive_source=manual\nmanual_pwd=L3g=\nkeep=1\n", args: []string{"--eval", "bash"}},
		{name: "active no eval tips", config: "active=acme\n"},
		{name: "active quiet", config: "active=acme\n", quiet: true},
		{name: "active inside work dir bash", config: "active=acme\n", inWork: true},
		{name: "active fish", config: "active=acme\n", args: []string{"--eval", "FISH"}},
		{name: "active fish inside work dir", config: "active=acme\n", args: []string{"--eval", "fish"}, inWork: true},
		{name: "eval without shell", config: "active=acme\n", args: []string{"--eval"}},
		{name: "eval followed by flag", config: "active=acme\n", args: []string{"--eval", "--other", "fish"}},
		{name: "unknown shell", config: "active=acme\n", args: []string{"--eval", "zsh"}},
		{name: "agent key added", config: "active=acme\nssh_agent_added=/k/id one\nother=2\n", args: []string{"--eval", "bash"}},
		{name: "agent key added nothing active", config: "ssh_agent_added=/k/id\n"},
		{name: "missing profile", config: "active=ghost\n", noProf: true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newFixture(t)
			t.Setenv("CTX_QUIET", "")
			work := filepath.Join(f.home, "wo rk's")
			sub := filepath.Join(work, "sub")
			for _, d := range []string{sub, f.home} {
				if err := os.MkdirAll(d, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			cwd := f.home
			if c.inWork {
				cwd = sub
			}
			bin, argvLog := testutil.FakeBin(t, "ssh-add", "exit 0")
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			setup := func() {
				if err := os.RemoveAll(f.dir); err != nil {
					t.Fatal(err)
				}
				_ = os.Remove(argvLog)
				f.write("config", c.config)
				if !c.noProf {
					f.write("profiles/acme.conf", fmt.Sprintf(profileBody, work))
				}
			}
			collect := func(out, errb string, code int) deactivateRun {
				cfg, _ := os.ReadFile(filepath.Join(f.dir, "config"))
				return deactivateRun{out, errb, string(cfg), code, testutil.ReadArgv(t, argvLog)}
			}
			env := append(os.Environ(), "CTX_CORE=off", "HOME="+f.home, "CTX_DIR="+f.dir, "PWD="+cwd)
			bashArgs := []string{"deactivate"} // CTX_QUIET is "" here; --quiet sets it
			if c.quiet {
				bashArgs = []string{"--quiet", "deactivate"}
			}

			setup()
			cmd := exec.Command(bashBin(t), append(bashArgs, c.args...)...)
			cmd.Dir, cmd.Env = cwd, env
			var bo, be bytes.Buffer
			cmd.Stdout, cmd.Stderr = &bo, &be
			code := 0
			if err := cmd.Run(); err != nil {
				ee, ok := err.(*exec.ExitError)
				if !ok {
					t.Fatal(err)
				}
				code = ee.ExitCode()
			}
			want := collect(bo.String(), be.String(), code)

			setup()
			t.Chdir(cwd)
			if c.quiet {
				t.Setenv("CTX_QUIET", "1")
			}
			var go1, go2 bytes.Buffer
			gcode := Deactivate(&go1, &go2, c.args)
			got := collect(go1.String(), go2.String(), gcode)

			if !reflect.DeepEqual(got, want) {
				t.Errorf("Go and Bash differ\n go:   %+v\n bash: %+v", got, want)
			}
			if c.name == "agent key added" {
				if len(want.sshAdd) != 1 || want.sshAdd[0][0] != "-d" {
					t.Errorf("expected one ssh-add -d call, got %v", want.sshAdd)
				}
			}
			if c.name == "active bash" && want.stdout == "" {
				t.Error("bash produced no unsets; test is vacuous")
			}
		})
	}
}
