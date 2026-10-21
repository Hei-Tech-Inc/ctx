package testutil

import (
	"os"
	"testing"
)

// ShellMissing is called where a test is about to skip, or to narrow its
// shell list, because the shell name (zsh, fish) is not installed. Under CI
// (CI set) it fails the test instead: the CI jobs install zsh and fish (see
// .github/workflows/ci.yml), so a green run must mean those tests ran, as
// test/test.sh's guard does for the shell suites. Outside CI it returns and
// the caller skips or narrows as before.
func ShellMissing(t testing.TB, name string) {
	t.Helper()
	if os.Getenv("CI") != "" {
		t.Fatalf("CI must install %s (this test would skip it)", name)
	}
}
