package knowledge

import (
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func TestSaveRecipeRoundTrip(t *testing.T) {
	root := filepath.Join(t.TempDir(), "k")
	r := Recipe{Name: "trace-msisdn", Description: "Find lines", Tags: []string{"trace", "logs"}, Params: []string{"msisdn", "env"}, ChangesState: false, Body: "1. do `logs`.\n"}
	p, err := SaveRecipe(root, r)
	if err != nil {
		t.Fatal(err)
	}
	if p != filepath.Join(root, "recipes", "trace-msisdn.md") {
		t.Fatalf("path = %s", p)
	}
	s, _ := Load(root)
	got, err := s.GetRecipe("trace-msisdn")
	if err != nil || !reflect.DeepEqual(got, r) {
		t.Fatalf("got %+v, %v", got, err)
	}
	list, err := s.Recipes()
	if err != nil || len(list) != 1 {
		t.Fatalf("list = %v, %v", list, err)
	}
}

func TestSaveRejectsBadName(t *testing.T) {
	root := t.TempDir()
	for _, n := range []string{"../x", "A B", "", "-a"} {
		if _, err := SaveRecipe(root, Recipe{Name: n}); err == nil {
			t.Errorf("recipe %q accepted", n)
		}
		if _, err := SaveScript(root, n, "x"); err == nil {
			t.Errorf("script %q accepted", n)
		}
		if err := SaveService(root, Service{Name: n}); err == nil {
			t.Errorf("service %q accepted", n)
		}
	}
	if p, err := SaveScript(root, "probe.sh", "echo hi\n"); err != nil || filepath.Base(p) != "probe.sh" {
		t.Fatalf("probe.sh: %v %v", p, err)
	}
}

func TestSaveServiceMergesAliases(t *testing.T) {
	root := t.TempDir()
	if err := SaveService(root, Service{Name: "svc", Aliases: []string{"a", "b"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveService(root, Service{Name: "svc", Aliases: []string{"b", "c"}}); err != nil {
		t.Fatal(err)
	}
	if err := SaveService(root, Service{Name: "other"}); err != nil {
		t.Fatal(err)
	}
	s, err := Load(root)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Services["svc"]; !reflect.DeepEqual(got.Aliases, []string{"a", "b", "c"}) || got.Kind != "deployment" {
		t.Fatalf("svc = %+v", got)
	}
	if _, ok := s.Services["other"]; !ok {
		t.Fatal("other missing")
	}
}

func TestSaveModes(t *testing.T) {
	root := filepath.Join(t.TempDir(), "k")
	p, err := SaveRecipe(root, Recipe{Name: "r", Body: "b"})
	if err != nil {
		t.Fatal(err)
	}
	sp, _ := SaveScript(root, "s.sh", "x")
	if err := SaveService(root, Service{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]os.FileMode{
		root: 0o700, filepath.Join(root, "recipes"): 0o700, filepath.Join(root, "scripts"): 0o700,
		p: 0o600, sp: 0o600, filepath.Join(root, "services.yaml"): 0o600,
	} {
		fi, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != want {
			t.Errorf("%s mode = %o, want %o", path, fi.Mode().Perm(), want)
		}
	}
}

func TestSearch(t *testing.T) {
	root := t.TempDir()
	if _, err := SaveRecipe(root, Recipe{Name: "r", Description: "d", Body: "Check the KYC flow\n"}); err != nil {
		t.Fatal(err)
	}
	if err := SaveService(root, Service{Name: "kyc-svc"}); err != nil {
		t.Fatal(err)
	}
	s, _ := Load(root)
	hits, err := s.Search("kyc")
	if err != nil || len(hits) != 2 {
		t.Fatalf("hits = %+v, %v", hits, err)
	}
}

func TestActivityAppendQuery(t *testing.T) {
	a := OpenActivity(t.TempDir())
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i, env := range []string{"dev", "prod", "dev", "dev"} {
		e := ActivityEntry{Time: base.Add(time.Duration(i) * time.Hour), Tool: "logs", Env: env, Result: "ok", Args: map[string]any{"i": i}}
		if err := a.Append(e); err != nil {
			t.Fatal(err)
		}
	}
	got, err := a.Query(ActivityFilter{Env: "dev"})
	if err != nil || len(got) != 3 || !got[0].Time.After(got[1].Time) {
		t.Fatalf("got %+v, %v", got, err)
	}
	got, _ = a.Query(ActivityFilter{Env: "dev", Since: base.Add(90 * time.Minute)})
	if len(got) != 2 {
		t.Fatalf("since: %+v", got)
	}
	got, _ = a.Query(ActivityFilter{Limit: 1})
	if len(got) != 1 || !got[0].Time.Equal(base.Add(3*time.Hour)) {
		t.Fatalf("limit: %+v", got)
	}
	got, _ = a.Query(ActivityFilter{Tool: "nope"})
	if len(got) != 0 {
		t.Fatalf("tool: %+v", got)
	}
	empty, err := OpenActivity(t.TempDir()).Query(ActivityFilter{})
	if err != nil || len(empty) != 0 {
		t.Fatalf("empty: %v %v", empty, err)
	}
}
