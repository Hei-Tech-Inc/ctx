package knowledge

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInitDoesNotOverwrite(t *testing.T) {
	root := filepath.Join(t.TempDir(), "k")
	created, err := Init(root)
	if err != nil || len(created) != 5 {
		t.Fatalf("created = %v, %v", created, err)
	}
	if _, err := Load(root); err != nil {
		t.Fatalf("templates must load: %v", err)
	}
	custom := filepath.Join(root, "services.yaml")
	if err := os.WriteFile(custom, []byte("services: {}\n# mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	created, err = Init(root)
	if err != nil || len(created) != 0 {
		t.Fatalf("second init created %v, %v", created, err)
	}
	b, _ := os.ReadFile(custom)
	if string(b) != "services: {}\n# mine\n" {
		t.Fatalf("overwritten: %q", b)
	}
	fi, _ := os.Stat(filepath.Join(root, "scripts"))
	if fi == nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("scripts dir: %v", fi)
	}
}
