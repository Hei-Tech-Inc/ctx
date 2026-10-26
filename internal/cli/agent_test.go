package cli

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func TestAgentHasKey(t *testing.T) {
	keygen, _ := testutil.FakeBin(t, "ssh-keygen", `echo "256 SHA256:abc comment (ED25519)"`)
	add, _ := testutil.FakeBin(t, "ssh-add", `echo "256 SHA256:abc other (ED25519)"`)
	t.Setenv("PATH", keygen+string(os.PathListSeparator)+add+string(os.PathListSeparator)+os.Getenv("PATH"))
	key := filepath.Join(t.TempDir(), "id")
	if agentHasKey(key) {
		t.Error("no .pub file: want false")
	}
	if err := os.WriteFile(key+".pub", []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !agentHasKey(key) {
		t.Error("fingerprint listed by ssh-add: want true")
	}
	keygen2, _ := testutil.FakeBin(t, "ssh-keygen", `echo "256 SHA256:zzz c (ED25519)"`)
	t.Setenv("PATH", keygen2+string(os.PathListSeparator)+os.Getenv("PATH"))
	if agentHasKey(key) {
		t.Error("fingerprint not listed: want false")
	}
}
