package hookenv

import (
	"reflect"
	"testing"
)

func TestStateRoundTrip(t *testing.T) {
	cases := []State{
		{},
		{PWD: "/w/a", Target: "a", Keys: []string{"AWS_PROFILE", "CTX_ACTIVE_PROFILE"}},
		{PWD: "/w/a b|c%d", Target: "x|y", Keys: []string{"K"}},
		{PWD: "/w/new\nline\ttab", Target: "", Keys: nil},
		{PWD: "/w/%7C", Target: "p q"},
		{PWD: "/w/ünï/日本語", Target: "prøfile-é", Keys: []string{"A"}},
		{PWD: "/w/cr\rlf\r\n", Target: "t\r"},
		{PWD: "/w/del\x7f\x7f", Target: "x\x7fy"},
		{PWD: "/w/mixed é\x7f|%\r\x01", Target: "日|本"},
	}
	for _, s := range cases {
		enc := s.Encode()
		got, ok := Decode(enc)
		if !ok {
			t.Errorf("Decode(%q) failed", enc)
			continue
		}
		if got.PWD != s.PWD || got.Target != s.Target || !reflect.DeepEqual(append([]string{}, got.Keys...), append([]string{}, s.Keys...)) {
			t.Errorf("round trip %#v -> %q -> %#v", s, enc, got)
		}
	}
}

func TestStateEncodePrintable(t *testing.T) {
	enc := State{PWD: "/a\r\x7f\n", Target: "é"}.Encode()
	if enc != "v1|/a%0D%7F%0A|é|" {
		t.Errorf("Encode = %q", enc)
	}
}

func TestStateEncodeFormat(t *testing.T) {
	s := State{PWD: "/a b|c", Target: "p", Keys: []string{"A", "B"}}
	if got, want := s.Encode(), "v1|/a b%7Cc|p|A B"; got != want {
		t.Errorf("Encode = %q, want %q", got, want)
	}
}

func TestStateDecodeRejects(t *testing.T) {
	for _, s := range []string{
		"",
		"v2|/a|p|A",
		"v1|/a|p",
		"v1|/a|p|A|extra",
		"v1|/a%zz|p|",
		"v1|/a%7|p|",
		"v1|/a|p|BAD-KEY",
		"/a|p",
	} {
		if got, ok := Decode(s); ok {
			t.Errorf("Decode(%q) = %#v, want rejected", s, got)
		}
	}
}
