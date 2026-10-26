//go:build !windows

package cli

import (
	"strings"
	"testing"

	"github.com/Hei-Tech-Inc/ctx/internal/testutil"
)

// fullConf is a profile as a full setup writes it (bash 4+ printf %q).
func fullConf(home, extra string) string {
	return "# ctx profile: acme\n# Generated: Mon Jan  1 00:00:00 UTC 2026\n\n" +
		"PROFILE_NAME=acme\nGIT_NAME=Dev\nGIT_EMAIL=dev@acme.com\nWORK_DIR='" + home + "/cur'\n" +
		"GITHUB_USER=alice\nSSH_KEY_PATH='" + home + "/.ssh/id_ed25519'\nAWS_PROFILE_NAME=dev\n" +
		"AZURE_SUBSCRIPTION=''\nAZURE_TENANT=''\nGCP_PROJECT=''\nGCP_ACCOUNT=''\nKUBE_CONTEXT=''\n" + extra
}

func seedConf(body func(home string) string) func(t *testing.T, home string) {
	return func(t *testing.T, home string) {
		seedFile(t, home, ".ctx/profiles/acme.conf", body(home))
	}
}

func extendCases() []cloneCase {
	ext := []string{"--extend", "acme"}
	eof := testutil.EOF
	// A hand-written .conf with only PROFILE_NAME (R13: missing keys are empty).
	minimal := seedConf(func(string) string { return "PROFILE_NAME=acme\n" })
	// What a clone setup writes: SSH key only.
	clone := seedConf(func(home string) string {
		return "PROFILE_NAME=acme\nGIT_NAME=''\nGIT_EMAIL=''\nWORK_DIR=''\nGITHUB_USER=''\n" +
			"SSH_KEY_PATH='" + home + "/.ssh/id_ed25519'\nAWS_PROFILE_NAME=''\nAZURE_SUBSCRIPTION=''\n" +
			"AZURE_TENANT=''\nGCP_PROJECT=''\nGCP_ACCOUNT=''\nKUBE_CONTEXT=''\nSECRET_KEYS=''\n" +
			"EXTRA_ENVS=''\nSIGNING_FORMAT=''\nSIGNING_KEY=''\n"
	})
	full := func(extra string) func(t *testing.T, home string) {
		return seedConf(func(home string) string { return fullConf(home, extra) })
	}
	return []cloneCase{
		{name: "minimal conf, every section yes", args: ext, seed: minimal,
			answers: []string{"Dev", "dev@acme.com", "2", // identity, SSH signing
				"y", "2", "~/proj/x", // work dir: existing path (created)
				"y", "2", "n", "", // SSH: generate
				"bob",                                                  // GitHub user
				"y", "awsp", "sub1", "ten1", "proj1", "a@b.co", "kctx", // cloud
				"y", "y", "tok", "y", "BAD-KEY", "y", "NPM_TOKEN", "npm", "n", // secrets
				"y", "1BAD", "NODE_ENV", "dev", "", // env vars
				"y"},
			check: expect(0, "Extend profile: acme", "SSH key generated", "Invalid key 'BAD-KEY'", "Invalid key '1BAD'",
				"GH_TOKEN → file store", "NPM_TOKEN → file store", "Profile 'acme' updated.", "Commits signed with SSH key")},
		{name: "clone profile extended to full", args: ext, seed: clone,
			answers: []string{"Dev", "dev@acme.com", "1", "y", "1", "carol", "n", "n", "n", "y"},
			check:   expect(0, "Work directory: ", "Profile 'acme' updated.", "mise.toml generated")},
		{name: "full profile, every section no", args: ext, seed: full("SECRET_KEYS=GH_TOKEN\nSIGNING_FORMAT=ssh\nSIGNING_KEY=''\n"),
			answers: []string{"n", "n", "n", "y"},
			check:   expect(0, "Profile 'acme' updated.", "Commits signed with SSH key")},
		{name: "identity update keeps gpg signing, email retry", args: ext,
			seed:    full("SIGNING_FORMAT=openpgp\nSIGNING_KEY=ABC123\n"),
			answers: []string{"y", "", "bad email", "new@acme.com", "1", "n", "n", "y"},
			check:   expect(0, "Expected user@domain.tld", "Keep current (openpgp)", "Commits signed with GPG key: ABC123")},
		{name: "identity update, signing off", args: ext, seed: full("SIGNING_FORMAT=ssh\n"),
			answers: []string{"y", "New Name", "", "2", "n", "n", "y"}},
		{name: "existing secrets kept once R26", args: ext, seed: full("SECRET_KEYS=GH_TOKEN\\ OLD\n"),
			answers: []string{"n", "y", "y", "t1", "y", "OLD", "v", "y", "NEW", "n1", "y", "NEW", "n2", "n", "n", "y"},
			check: func(t *testing.T, r cloneRun) {
				expect(0, "NEW → file store")(t, r)
				if c := r.tree[".ctx/profiles/acme.conf"]; !strings.Contains(c, "SECRET_KEYS=GH_TOKEN\\ OLD\\ NEW\n") {
					t.Errorf("SECRET_KEYS not kept once:\n%s", c)
				}
			}},
		{name: "extra envs with = and quotes", args: ext,
			seed:    full("EXTRA_ENVS=$'A=x=y\\nB=\"q \\'u\"\\n'\n"),
			answers: []string{"n", "n", "y", "C", "3", "", "y"},
			check:   expect(0, "mise.toml generated")},
		{name: "extra envs without trailing newline", args: ext, seed: full("EXTRA_ENVS=A=1\n"),
			answers: []string{"n", "n", "y", "B", "2", "", "y"}},
		{name: "folder under work root, picks skipped", args: ext, seed: minimal,
			answers: []string{"Dev", "", "0", "y", "0", "", "y", "0", "n", "", "", "n", "n", "n", "y"},
			check:   expect(0, "clients/acme")},
		{name: "existing key path", args: ext, seed: minimal,
			answers: []string{"", "", "1", "n", "y", "1", "~/.ssh/id_work", "", "n", "n", "y"}},
		{name: "existing key not found", args: ext, seed: minimal,
			answers: []string{"", "", "1", "n", "y", "1", "~/.ssh/nope"},
			check:   expect(1, "SSH key not found: ", "ctx doctor")},
		{name: "ssh-keygen fails R25", args: ext, seed: minimal, env: []string{"FAKE_KEYGEN_RC=1"},
			answers: []string{"", "", "1", "n", "y", "2", "n"},
			check:   expect(1, "ssh-keygen failed for ")},
		{name: "dry run", dry: true, args: ext, seed: minimal,
			answers: []string{"Dev", "dev@acme.com", "1", "y", "2", "~/proj/x", "y", "2", "", "n",
				"y", "y", "tok", "n", "n"},
			check: expect(0, "[dry-run] would create directory", "[dry-run] would generate SSH key",
				"[dry-run] would store secret GH_TOKEN", "[DRY RUN]")},
		{name: "review no", args: ext, seed: full(""), answers: []string{"n", "n", "n", "n"},
			check: expect(1, "Aborted before write.")},
		{name: "-y skips the review", args: []string{"-y", "--extend", "acme"}, seed: full(""),
			answers: []string{"n", "n", "n"}, check: expect(0, "Confirming automatically", "updated.")},
		{name: "missing profile", args: []string{"--extend", "nope"},
			check: expect(1, "setup --extend: profile 'nope' not found")},
		{name: "cancel at identity", args: ext, seed: full(""), answers: []string{eof},
			check: expect(130, "Cancelled by user.")},
		{name: "cancel at secret value", args: ext, seed: full(""), answers: []string{"n", "y", "y", eof},
			check: expect(130, "Cancelled by user.")},
	}
}

// TestExtendMatchesBash drives `ctx --no-gum setup --extend acme` on a pty
// through Bash (CTX_CORE=off) and through ctx-core with the same answers,
// and compares the transcript, exit code, HOME and tool argv.
func TestExtendMatchesBash(t *testing.T) {
	t.Parallel()
	fakes := cloneFakes(t)
	core := buildCore(t)
	for _, c := range extendCases() {
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			runBoth(t, fakes, core, c)
		})
	}
}
