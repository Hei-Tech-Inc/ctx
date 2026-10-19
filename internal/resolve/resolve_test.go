package resolve

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

func realTemp(t *testing.T) string {
	t.Helper()
	d, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func conf(t *testing.T, prof, name, workDir string) {
	t.Helper()
	body := "PROFILE_NAME=" + name + "\n"
	if workDir != "" {
		body += fmt.Sprintf("WORK_DIR=%q\n", workDir)
	}
	write(t, filepath.Join(prof, name+".conf"), body)
}

func mkdirs(t *testing.T, dirs ...string) {
	t.Helper()
	for _, d := range dirs {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

type tcase struct {
	name, pwd, wantName, wantDir string
}

func runCases(t *testing.T, prof string, cases []tcase) {
	t.Helper()
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n, wd, err := Path(c.pwd, prof)
			if err != nil {
				t.Fatal(err)
			}
			if n != c.wantName || wd != c.wantDir {
				t.Fatalf("Path(%q) = (%q, %q), want (%q, %q)", c.pwd, n, wd, c.wantName, c.wantDir)
			}
		})
	}
}

func TestPath(t *testing.T) {
	td := realTemp(t)
	prof := filepath.Join(td, "profiles")
	mkdirs(t, prof)

	// nested prefix wins
	conf(t, prof, "short", filepath.Join(td, "root"))
	conf(t, prof, "long", filepath.Join(td, "root/client"))
	mkdirs(t, filepath.Join(td, "root/client/app"))
	runCases(t, prof, []tcase{
		{"nested prefix", filepath.Join(td, "root/client/app"), "long", filepath.Join(td, "root/client")},
		{"outer prefix", filepath.Join(td, "root"), "short", filepath.Join(td, "root")},
		{"no match", "/nonexistent/nope/zz", "", ""},
	})

	// .ctx override inside a repo
	mkdirs(t, filepath.Join(td, "w/repo/sub"), filepath.Join(td, "w/repo/.git"))
	write(t, filepath.Join(td, "w/repo/.ctx"), "profile=short\n")
	conf(t, prof, "short", filepath.Join(td, "w"))
	conf(t, prof, "long", filepath.Join(td, "w/repo"))
	runCases(t, prof, []tcase{
		{".ctx override", filepath.Join(td, "w/repo/sub"), "short", filepath.Join(td, "w")},
	})

	// nearer .ctx beats repo root .ctx
	mkdirs(t, filepath.Join(td, "m/repo/deep"), filepath.Join(td, "m/repo/.git"))
	write(t, filepath.Join(td, "m/repo/.ctx"), "profile=long\n")
	write(t, filepath.Join(td, "m/repo/deep/.ctx"), "profile=short\n")
	conf(t, prof, "short", filepath.Join(td, "m"))
	conf(t, prof, "long", filepath.Join(td, "m/repo"))
	runCases(t, prof, []tcase{
		{"nearer .ctx", filepath.Join(td, "m/repo/deep"), "short", filepath.Join(td, "m")},
		{"root .ctx", filepath.Join(td, "m/repo"), "long", filepath.Join(td, "m/repo")},
	})

	// .ctx naming a missing profile is ignored
	mkdirs(t, filepath.Join(td, "x/repo/sub"), filepath.Join(td, "x/repo/.git"))
	write(t, filepath.Join(td, "x/repo/.ctx"), "profile=ghost\n")
	conf(t, prof, "short", filepath.Join(td, "x"))
	runCases(t, prof, []tcase{
		{"missing profile ignored", filepath.Join(td, "x/repo/sub"), "short", filepath.Join(td, "x")},
	})

	// sibling rule
	for _, n := range []string{"short", "long"} {
		_ = os.Remove(filepath.Join(prof, n+".conf"))
	}
	mkdirs(t, filepath.Join(td, "clients/hubtel/deep"))
	conf(t, prof, "deladetech", filepath.Join(td, "clients"))
	conf(t, prof, "hubtel", "")
	conf(t, prof, "short", filepath.Join(td, "elsewhere"))
	runCases(t, prof, []tcase{
		{"sibling no workdir", filepath.Join(td, "clients/hubtel/deep"), "hubtel", filepath.Join(td, "clients/hubtel")},
		{"broad profile", filepath.Join(td, "clients"), "deladetech", filepath.Join(td, "clients")},
	})
	// sibling with explicit WORK_DIR
	conf(t, prof, "hubtel", filepath.Join(td, "clients/hubtel"))
	runCases(t, prof, []tcase{
		{"sibling explicit", filepath.Join(td, "clients/hubtel/deep"), "hubtel", filepath.Join(td, "clients/hubtel")},
	})
}

func TestPathTrailingSlash(t *testing.T) {
	td := realTemp(t)
	prof := filepath.Join(td, "profiles")
	mkdirs(t, prof, filepath.Join(td, "acme/app"))
	conf(t, prof, "acme", filepath.Join(td, "acme")+"/")
	runCases(t, prof, []tcase{
		{"trailing slash", filepath.Join(td, "acme/app"), "acme", filepath.Join(td, "acme")},
	})
}

func TestPathMissingDir(t *testing.T) {
	if _, _, err := Path("/x", "/nonexistent/profiles"); err == nil {
		t.Fatal("want error for missing profiles dir")
	}
}
