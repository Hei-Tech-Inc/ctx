package setup

import (
	"os"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

func TestMain(m *testing.M) {
	code := m.Run()
	testutil.CleanupSystemPath()
	os.Exit(code)
}
