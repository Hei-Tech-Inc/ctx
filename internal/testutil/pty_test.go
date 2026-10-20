//go:build !windows

package testutil

import (
	"os/exec"
	"strings"
	"testing"
	"time"
)

func TestRunPTY(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	got, rc := RunPTY(t, exec.Command("bash", "-c", `read -r a; echo "got:$a"`), []string{"x"}, 10*time.Second)
	if got != "got:x\n" || rc != 0 {
		t.Errorf("transcript %q rc %d, want %q rc 0", got, rc, "got:x\n")
	}

	// Several prompts, each answered after its prompt; echo is off, so the
	// answers do not show up in the transcript.
	script := `printf 'one? '; read -r a; printf 'two: '; read -r b; [ -t 0 ] && [ -t 1 ] && echo tty; stty size; echo "$a,$b"; exit 3`
	got, rc = RunPTY(t, exec.Command("bash", "-c", script), []string{"A", "B"}, 10*time.Second)
	if want := "one? two: tty\n40 120\nA,B\n"; got != want || rc != 3 {
		t.Errorf("transcript %q rc %d, want %q rc 3", got, rc, want)
	}

	// EOF sends Ctrl+D at the start of a line: read fails.
	got, rc = RunPTY(t, exec.Command("bash", "-c", `printf 'q: '; read -r a || exit 130; echo "got:$a"`), []string{"EOF"}, 10*time.Second)
	if got != "q: " || rc != 130 {
		t.Errorf("EOF: transcript %q rc %d, want %q rc 130", got, rc, "q: ")
	}

	// Death by signal is reported like a shell does: 128+signal.
	_, rc = RunPTY(t, exec.Command("bash", "-c", `kill -TERM $$`), nil, 10*time.Second)
	if rc != 143 {
		t.Errorf("signal rc = %d, want 143", rc)
	}
}

// TestRunPTYWaitsBetweenUIs: once a program has shown a full-screen UI
// (bracketed paste on), an answer is not typed into the silence after that UI
// ends; it waits for the next UI. The script drops input typed in the gap, as
// a UI shutting down can.
func TestRunPTYWaitsBetweenUIs(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	script := `on=$'\e[?2004h' off=$'\e[?2004l'
printf '%sone' "$on"; read -r a; printf '%s' "$off"
sleep 1.5
while read -r -t 0.05 junk; do :; done
printf '%stwo' "$on"; read -r b; printf '%s' "$off"
echo "got:$a,$b"`
	got, rc := RunPTY(t, exec.Command("bash", "-c", script), []string{"A", "B"}, 20*time.Second)
	if rc != 0 || !strings.HasSuffix(got, "got:A,B\n") {
		t.Errorf("transcript %q rc %d, want got:A,B", got, rc)
	}
}

// TestRunPTYWaitFallback: after a UI, a program that reads without a prompt
// or another UI still gets its answer, once the output has been quiet for
// waitGap.
func TestRunPTYWaitFallback(t *testing.T) {
	if _, err := exec.LookPath("bash"); err != nil {
		t.Skip("bash not found")
	}
	script := `printf '\e[?2004hone'; read -r a; printf '\e[?2004lworking\n'; read -r b; echo "got:$a,$b"`
	start := time.Now()
	got, rc := RunPTY(t, exec.Command("bash", "-c", script), []string{"A", "B"}, 30*time.Second)
	if rc != 0 || !strings.HasSuffix(got, "got:A,B\n") {
		t.Errorf("transcript %q rc %d, want got:A,B", got, rc)
	}
	if took := time.Since(start); took < waitGap {
		t.Errorf("second answer sent after %v, before waitGap (%v)", took, waitGap)
	}
}

func TestAnswerState(t *testing.T) {
	for _, c := range []struct {
		tail string
		ui   bool
		want int
	}{
		{"\x1b[?2004h> pick one\nenter submit", true, stateReady},
		{"final frame\x1b[?2004l", true, stateWait},
		{"", true, stateWait},
		{"\x1b[?2004l\n? Name: ", true, stateReady},
		{"\x1b[?2004l✓ Using key\n\x1b[?2004h> type here", true, stateReady},
		{"Scanning...", false, stateMaybe},
		{"", false, stateMaybe},
		{"? Name: ", false, stateReady},
	} {
		if got := answerState([]byte(c.tail), c.ui); got != c.want {
			t.Errorf("answerState(%q, %v) = %d, want %d", c.tail, c.ui, got, c.want)
		}
	}
}

func TestPromptLike(t *testing.T) {
	for s, want := range map[string]bool{
		"? Name: ":                      true,
		"? Name \x1b[2m[acme]\x1b[0m: ": true,
		"Continue? [Y/n] ":              true,
		"> ":                            true,
		"working...\n":                  false,
		"done":                          false,
		"":                              false,
	} {
		if got := promptLike([]byte(s)); got != want {
			t.Errorf("promptLike(%q) = %v, want %v", s, got, want)
		}
	}
}
