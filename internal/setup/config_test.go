package setup

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

type batchCase struct {
	name     string
	conf     string
	existing bool // profile "acme" already exists
	noYes    bool
	dryRun   bool
	want     string
}

var batchCases = []batchCase{
	{name: "bad scope", conf: "SETUP_SCOPE=bogus\n", dryRun: true, want: "setup --config: SETUP_SCOPE must be clone or full"},
	{name: "needs yes", conf: "PROFILE_NAME=acme\n", noYes: true, dryRun: true, want: "setup --config on non-interactive stdin needs --yes or AUTO_CONFIRM=true"},
	{name: "no profile", conf: "GIT_NAME=x\n", dryRun: true, want: "setup --config: PROFILE_NAME is required"},
	{name: "exists", conf: "PROFILE_NAME=Acme\nGIT_NAME=x\n", existing: true, dryRun: true, want: "setup --config: profile 'acme' exists — set OVERWRITE=true to replace"},
	{name: "no git name", conf: "PROFILE_NAME=acme\n", dryRun: true, want: "setup --config: GIT_NAME is required (or SETUP_SCOPE=clone)"},
	{name: "bad email", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nGIT_EMAIL=jdoe\n", dryRun: true, want: "setup --config: GIT_EMAIL must look like user@domain.tld"},
	{name: "path needs EXISTING_PATH", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nWORK_MODE=path\n", dryRun: true, want: "setup --config: EXISTING_PATH required when WORK_MODE=path"},
	{name: "bad slug", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nWORK_SLUG=-n\n", dryRun: true, want: "setup --config: WORK_SLUG invalid for WORK_MODE=new"},
	{name: "bad work mode", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nWORK_MODE=zzz\n", dryRun: true, want: "setup --config: set WORK_DIR=... or WORK_MODE=current|path|new"},
	{name: "ssh existing missing", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nWORK_DIR=@HOME@/w\nSSH_MODE=existing\nSSH_KEY_PATH=~/nokey\n", want: "setup --config: SSH_KEY_PATH not found: @HOME@/nokey"},
	{name: "clone skip", conf: "SETUP_SCOPE=clone\nPROFILE_NAME=acme\nSSH_MODE=skip\n", dryRun: true, want: "setup --config: SETUP_SCOPE=clone requires SSH_MODE=generate or existing (not skip)"},
	{name: "bad ssh mode", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nSSH_MODE=zzz\n", dryRun: true, want: "setup --config: SSH_MODE must be generate|existing|skip"},
	{name: "bad signing", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nSIGNING_FORMAT=zzz\n", dryRun: true, want: "setup --config: SIGNING_FORMAT must be ssh|gpg|none"},
	{name: "gpg no key", conf: "PROFILE_NAME=acme\nGIT_NAME=x\nSIGNING_FORMAT=gpg\n", dryRun: true, want: "setup --config: SIGNING_FORMAT=gpg needs SIGNING_KEY (GPG key ID)"},
	{name: "secrets", conf: "PROFILE_NAME=Acme\nGIT_NAME=x\nSECRETS_MODE=add\n", dryRun: true, want: "setup --config: only SECRETS_MODE=skip — add secrets later with: ctx secret set acme <KEY>"},
}

func (bc batchCase) setup(t *testing.T) (home, ctxDir, conf string) {
	home = t.TempDir()
	ctxDir = filepath.Join(home, ".ctx")
	if bc.existing {
		if err := os.MkdirAll(filepath.Join(ctxDir, "profiles"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(ctxDir, "profiles", "acme.conf"), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	conf = filepath.Join(home, "setup.conf")
	if err := os.WriteFile(conf, []byte(strings.ReplaceAll(bc.conf, "@HOME@", home)), 0o600); err != nil {
		t.Fatal(err)
	}
	return
}

// fromConfig runs Preconditions then Resolve, as the batch command does.
func fromConfig(c ConfigFile, o BatchOpts) (Inputs, error) {
	if _, _, err := Preconditions(c, o); err != nil {
		return Inputs{}, err
	}
	return Resolve(c, o)
}

func TestBatchConfigErrors(t *testing.T) {
	for _, bc := range batchCases {
		bc := bc
		t.Run(bc.name, func(t *testing.T) {
			home, _, conf := bc.setup(t)
			c, err := ReadConfig(conf)
			if err != nil {
				t.Fatal(err)
			}
			_, err = fromConfig(c, BatchOpts{
				AutoYes: !bc.noYes, DryRun: bc.dryRun, PWD: "/pwd", Home: home, WorkRoot: home + "/clients",
				Exists:     func(p string) bool { return bc.existing && p == "acme" },
				FileExists: func(p string) bool { _, e := os.Stat(p); return e == nil },
			})
			want := strings.ReplaceAll(bc.want, "@HOME@", home)
			if err == nil || err.Error() != want {
				t.Fatalf("err = %v, want %q", err, want)
			}
		})
	}
}

// TestBatchConfigErrorsMatchBash runs the real Bash batch path and checks that
// it dies with the same text Preconditions/Resolve return.
func TestBatchConfigErrorsMatchBash(t *testing.T) {
	if testing.Short() {
		t.Skip("short")
	}
	for _, bc := range batchCases {
		bc := bc
		t.Run(bc.name, func(t *testing.T) {
			t.Parallel()
			home, ctxDir, conf := bc.setup(t)
			args := []string{"--no-gum"}
			if bc.dryRun {
				args = append(args, "--dry-run")
			}
			args = append(args, "setup", "--config", conf)
			if !bc.noYes {
				args = append(args, "--yes")
			}
			cmd := exec.Command(filepath.Join(repoRoot(t), "bin", "ctx"), args...)
			cmd.Dir = home
			cmd.Env = append(os.Environ(), "HOME="+home, "CTX_DIR="+ctxDir, "PATH=/usr/bin:/bin:/usr/sbin:/sbin", "CTX_CORE=off", "NO_COLOR=1", "LC_ALL=en_US.UTF-8")
			var stderr bytes.Buffer
			cmd.Stderr = &stderr
			cmd.Stdin = nil
			_ = cmd.Run()
			want := strings.ReplaceAll(bc.want, "@HOME@", home)
			if !strings.Contains(stderr.String(), want+"\n") {
				t.Errorf("bash stderr lacks %q:\n%s", want, stderr.String())
			}
		})
	}
}

func TestResolve(t *testing.T) {
	home := "/h"
	opts := BatchOpts{AutoYes: true, DryRun: true, PWD: "/pwd", Home: home, WorkRoot: "/h/clients",
		Exists: func(string) bool { return false }, FileExists: func(string) bool { return false }}
	in, err := fromConfig(ConfigFile{"PROFILE_NAME": "Acme Corp", "GIT_NAME": "J", "GIT_EMAIL": " j@c.io ", "WORK_SLUG": "A/b c", "AUTO_CONFIRM": "yes", "KUBE_CONTEXT": "k", "SIGNING_FORMAT": "GPG", "SIGNING_KEY": "~/k"}, opts)
	if err != nil {
		t.Fatal(err)
	}
	if in.Profile != "acme-corp" || in.GitEmail != "j@c.io" || in.WorkDir != "/h/clients/A-b-c" || in.Scope != "full" ||
		in.SSHKeyPath != "/h/.ssh/ctx_acme-corp" || !in.SSHKeyGenerated || in.NeedsKeygen || !in.ImportExisting ||
		in.SigningFormat != "openpgp" || in.SigningKey != "/h/k" || !in.AutoYes || in.KubeContext != "k" || in.NeedsMkdirWorkDir {
		t.Errorf("unexpected inputs: %+v", in)
	}
	opts.DryRun = false
	in, err = fromConfig(ConfigFile{"PROFILE_NAME": "a", "GIT_NAME": "J", "IMPORT_MODE": "manual", "WORK_MODE": "current"}, opts)
	if err != nil || !in.NeedsKeygen || !in.SSHKeyGenerated || in.WorkDir != "/pwd" || !in.NeedsMkdirWorkDir || in.ImportExisting {
		t.Errorf("non-dry inputs: %+v %v", in, err)
	}
	opts.FileExists = func(string) bool { return true }
	in, _ = fromConfig(ConfigFile{"PROFILE_NAME": "a", "GIT_NAME": "J"}, opts)
	if in.NeedsKeygen || in.SSHKeyGenerated {
		t.Errorf("existing key must not be regenerated: %+v", in)
	}
	in, err = fromConfig(ConfigFile{"SETUP_SCOPE": "clone", "PROFILE_NAME": "a"}, opts)
	if err != nil || in.WorkDir != "" || in.Scope != "clone" || in.NeedsMkdirWorkDir {
		t.Errorf("clone inputs: %+v %v", in, err)
	}
	noPWD := BatchOpts{AutoYes: true, DryRun: true, Exists: opts.Exists, FileExists: opts.FileExists}
	if _, err = fromConfig(ConfigFile{"PROFILE_NAME": "a", "GIT_NAME": "J", "WORK_MODE": "current"}, noPWD); err == nil || err.Error() != "setup --config: could not resolve WORK_DIR (or use SETUP_SCOPE=clone without WORK_DIR)" {
		t.Errorf("empty PWD: %v", err)
	}
	if _, err := Resolve(ConfigFile{"PROFILE_NAME": "a"}, BatchOpts{}); err == nil {
		t.Error("Resolve without Exists/FileExists must fail")
	}
}

func TestPreconditions(t *testing.T) {
	for _, tc := range []struct {
		c         ConfigFile
		o         BatchOpts
		scope     string
		autoYes   bool
		wantError string
	}{
		{ConfigFile{}, BatchOpts{AutoYes: true}, "full", true, ""},
		{ConfigFile{"SETUP_SCOPE": "clone", "AUTO_CONFIRM": "Yes"}, BatchOpts{}, "clone", true, ""},
		{ConfigFile{}, BatchOpts{StdinIsTTY: true}, "full", false, ""},
		{ConfigFile{"AUTO_CONFIRM": "0"}, BatchOpts{}, "full", false, "setup --config on non-interactive stdin needs --yes or AUTO_CONFIRM=true"},
		{ConfigFile{"SETUP_SCOPE": "x"}, BatchOpts{AutoYes: true}, "", false, "setup --config: SETUP_SCOPE must be clone or full"},
	} {
		scope, ay, err := Preconditions(tc.c, tc.o)
		msg := ""
		if err != nil {
			msg = err.Error()
		}
		if scope != tc.scope || ay != tc.autoYes || msg != tc.wantError {
			t.Errorf("Preconditions(%v, %+v) = %q %v %q", tc.c, tc.o, scope, ay, msg)
		}
	}
}

type recOut struct{ calls *[]string }

func (r recOut) Info(s string)    { *r.calls = append(*r.calls, "info:"+s) }
func (r recOut) Success(s string) {}
func (r recOut) Warn(s string)    {}
func (r recOut) Dim(s string)     {}
func (r recOut) Bold(s string)    {}
func (r recOut) Say(s string)     {}
func (r recOut) Error(s string)   {}
