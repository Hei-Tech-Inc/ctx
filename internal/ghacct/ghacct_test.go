package ghacct

import (
	"reflect"
	"testing"
)

const newTwo = `github.com
  ✓ Logged in to github.com account alice (keyring)
  - Active account: false
  - Git operation protocol: https
  - Token: gho_************************************
  - Token scopes: 'repo'

  ✓ Logged in to github.com account bob-2 (keyring)
  - Active account: true
  - Git operation protocol: ssh
`

const old = `github.com
  ✓ Logged in to github.com as carol (oauth_token)
  ✓ Git operations for github.com configured to use ssh protocol.
`

const failed = `github.com
  X Failed to log in to github.com account dave (keyring)
  - Active account: true
  ✓ Logged in to github.com account erin (keyring)
  - Active account: false
`

func TestParse(t *testing.T) {
	for _, c := range []struct {
		name, in string
		want     []string
	}{
		{"new", newTwo, []string{"alice", "bob-2"}},
		{"old", old, []string{"carol"}},
		{"failed", failed, []string{"erin"}},
		{"empty", "", nil},
		{"none", "You are not logged into any GitHub hosts.\n", nil},
	} {
		if got := Parse(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: Parse = %v, want %v", c.name, got, c.want)
		}
	}
}

func TestActive(t *testing.T) {
	for _, c := range []struct{ name, in, want string }{
		{"new picks the active one", newTwo, "bob-2"},
		{"old has no marker: first", old, "carol"},
		{"failed account is not a login", failed, ""},
		{"empty", "", ""},
	} {
		if got := Active(c.in); got != c.want {
			t.Errorf("%s: Active = %q, want %q", c.name, got, c.want)
		}
	}
}
