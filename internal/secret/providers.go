package secret

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
)

// Provider names, as ctx_effective_secret_provider returns them.
const (
	ProviderKeychain = "keychain"
	ProviderFile     = "file"
	ProviderPass     = "pass"
)

// goos is runtime.GOOS; tests replace it to exercise Open's availability check.
var goos = runtime.GOOS

// Store reads, writes and deletes the secrets of one provider. Get returns "" (and no
// error) for a missing secret; trailing newlines are stripped, as Bash's
// command substitution does. List is sorted like `sort` sorts.
type Store interface {
	Get(profile, key string) (string, error)
	List(profile string) ([]string, error)
	Delete(profile, key string) error
	// Set stores value, replacing any existing secret (the _secret_*_set functions).
	Set(profile, key, value string) error
}

// AsciiLower lowercases ASCII letters only, like `tr '[:upper:]' '[:lower:]'` in the C locale.
func AsciiLower(s string) string { return asciiLower(s) }

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// ConfiguredProvider mirrors ctx_secret_provider: $CTX_SECRET_PROVIDER, else
// the config's secret_provider, lowercased; anything but auto|keychain|file|pass is auto.
func ConfiguredProvider() string {
	c := os.Getenv("CTX_SECRET_PROVIDER")
	if c == "" {
		if cfg, err := ctxconfig.Load(); err == nil {
			c = cfg.SecretProvider
		}
	}
	c = asciiLower(c)
	switch c {
	case "keychain", "file", "pass":
		return c
	}
	return "auto"
}

// EffectiveProvider mirrors ctx_effective_secret_provider: auto is keychain on
// darwin and file elsewhere.
func EffectiveProvider() string {
	if p := ConfiguredProvider(); p != "auto" {
		return p
	}
	if goos == "darwin" {
		return ProviderKeychain
	}
	return ProviderFile
}

// StoreLabel mirrors ctx_secret_store_label.
func StoreLabel() string {
	switch EffectiveProvider() {
	case ProviderKeychain:
		return "Keychain"
	case ProviderPass:
		return "pass password store"
	}
	return "file store"
}

// ErrUnavailable is Delete's error when the provider's tool is missing, the
// case where the Bash dispatchers return 1.
var ErrUnavailable = fmt.Errorf("secret provider unavailable")

// unavailable stands in for a provider whose tool is missing: Get and List
// find nothing, Delete fails (the *_with_provider behavior).
type unavailable struct{}

func (unavailable) Get(string, string) (string, error) { return "", nil }
func (unavailable) List(string) ([]string, error)      { return nil, nil }
func (unavailable) Delete(string, string) error        { return ErrUnavailable }
func (unavailable) Set(string, string, string) error   { return ErrUnavailable }

// OpenWith mirrors secret_*_with_provider's availability checks: keychain needs
// darwin and `security`, pass needs `pass`. A provider that cannot be used, or
// is unknown, yields ErrUnavailable.
func OpenWith(provider string) (Store, error) {
	switch provider {
	case ProviderKeychain, ProviderFile, ProviderPass:
		st, _ := Open(provider)
		if _, bad := st.(unavailable); !bad {
			return st, nil
		}
	}
	return nil, ErrUnavailable
}

// Open returns the store for provider (keychain, file or pass). A keychain
// without darwin and `security`, or a pass without `pass`, yields a store that
// finds nothing. An unknown provider is an error.
func Open(provider string) (Store, error) {
	switch provider {
	case ProviderKeychain:
		if goos != "darwin" {
			return unavailable{}, nil
		}
		if _, err := exec.LookPath("security"); err != nil {
			return unavailable{}, nil
		}
		return Keychain{}, nil
	case ProviderFile:
		return File{}, nil
	case ProviderPass:
		if _, err := exec.LookPath("pass"); err != nil {
			return unavailable{}, nil
		}
		return Pass{}, nil
	}
	return nil, fmt.Errorf("unknown secret provider %q", provider)
}

// ProviderGetter reads secrets in process from the effective provider, like
// `ctx secret get` without the subprocess.
type ProviderGetter struct{}

// Get returns the secret, or an error shaped like CLIGetter's when it is empty.
func (ProviderGetter) Get(profileName, key string) (string, error) {
	st, err := Open(EffectiveProvider())
	if err != nil {
		return "", err
	}
	v, err := st.Get(profileName, key)
	if err != nil {
		return "", err
	}
	if v == "" {
		return "", fmt.Errorf("ctx secret get %s %s: ✗ No secret '%s' for profile '%s'", profileName, key, key, profileName)
	}
	return v, nil
}

// output runs a command with no stdin and stderr discarded, returning stdout.
func output(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	return out.String(), err
}

func trimNL(s string) string { return strings.TrimRight(s, "\n") }

// sortLines orders names with sort(1), so locale collation matches Bash. It
// falls back to byte order when sort cannot run.
func sortLines(names []string) []string {
	if len(names) < 2 {
		return names
	}
	cmd := exec.Command("sort")
	cmd.Stdin = strings.NewReader(strings.Join(names, "\n") + "\n")
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		sort.Strings(names)
		return names
	}
	return strings.Split(trimNL(out.String()), "\n")
}

// Keychain is the macOS Keychain, driven through `security`.
type Keychain struct{}

func (Keychain) service(profile, key string) string { return "ctx-" + profile + "-" + key }

// Get mirrors _secret_keychain_get.
func (k Keychain) Get(profile, key string) (string, error) {
	out, err := output("security", "find-generic-password", "-a", os.Getenv("USER"), "-s", k.service(profile, key), "-w")
	if err != nil {
		return "", nil
	}
	return trimNL(out), nil
}

// Set mirrors _secret_keychain_set: delete, then add with an empty trusted-app list.
func (k Keychain) Set(profile, key, value string) error {
	svc := k.service(profile, key)
	user := os.Getenv("USER")
	_, _ = output("security", "delete-generic-password", "-a", user, "-s", svc)
	_, err := output("security", "add-generic-password", "-a", user, "-s", svc, "-w", value, "-T", "", "-U")
	return err
}

// Delete mirrors _secret_keychain_delete (failures ignored).
func (k Keychain) Delete(profile, key string) error {
	_, _ = output("security", "delete-generic-password", "-a", os.Getenv("USER"), "-s", k.service(profile, key))
	return nil
}

// List mirrors _secret_keychain_list_keys: the quoted "ctx-<profile>-<KEY>"
// strings in the keychain dump, with the prefix removed, sorted. The profile
// is matched literally (Bash feeds it to grep and sed as a pattern).
func (Keychain) List(profile string) ([]string, error) {
	out, _ := output("security", "dump-keychain")
	prefix := `"ctx-` + profile + `-`
	re := regexp.MustCompile(regexp.QuoteMeta(prefix) + `[^"\n]*"`)
	var keys []string
	for _, m := range re.FindAllString(out, -1) {
		keys = append(keys, strings.TrimSuffix(strings.TrimPrefix(m, prefix), `"`))
	}
	return sortLines(keys), nil
}

// File keeps one file per secret at $CTX_DIR/secrets/<profile>/<KEY>.
type File struct{}

func ctxDir() string {
	if d := os.Getenv("CTX_DIR"); d != "" {
		return d
	}
	return filepath.Join(os.Getenv("HOME"), ".ctx")
}

func (File) path(profile, key string) string {
	return ctxDir() + "/secrets/" + profile + "/" + key
}

// Get mirrors _secret_file_get.
func (f File) Get(profile, key string) (string, error) {
	p := f.path(profile, key)
	if st, err := os.Stat(p); err != nil || !st.Mode().IsRegular() {
		return "", nil
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return "", nil
	}
	return trimNL(string(bytes.ReplaceAll(data, []byte{0}, nil))), nil
}

// Set mirrors _secret_file_set: the profile dir is 0700 (as is secrets/), the
// file 0600, and the value is written without a trailing newline.
func (f File) Set(profile, key, value string) error {
	p := f.path(profile, key)
	if err := os.MkdirAll(filepath.Dir(p), 0o777); err != nil {
		return err
	}
	_ = os.Chmod(ctxDir()+"/secrets", 0o700)
	_ = os.Chmod(ctxDir()+"/secrets/"+profile, 0o700)
	if err := os.WriteFile(p, []byte(value), 0o666); err != nil {
		return err
	}
	_ = os.Chmod(p, 0o600)
	return nil
}

// Delete mirrors _secret_file_delete (rm -f, failures ignored).
func (f File) Delete(profile, key string) error {
	_ = os.Remove(f.path(profile, key))
	return nil
}

// List mirrors _secret_file_list_keys: the visible entries of the profile's dir.
func (File) List(profile string) ([]string, error) {
	dir := ctxDir() + "/secrets/" + profile
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, nil
	}
	return sortLines(globNames(dir, "")), nil
}

// globNames lists the non-hidden entries of dir that exist (symlinks followed)
// and end in suffix, with suffix removed, like `for f in dir/*suffix`.
func globNames(dir, suffix string) []string {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var names []string
	for _, e := range ents {
		n := e.Name()
		if strings.HasPrefix(n, ".") || !strings.HasSuffix(n, suffix) {
			continue
		}
		if _, err := os.Stat(filepath.Join(dir, n)); err != nil {
			continue
		}
		names = append(names, strings.TrimSuffix(n, suffix))
	}
	return names
}

// Pass is the pass(1) password store, under ctx/<profile>/<KEY>.
type Pass struct{}

func (Pass) path(profile, key string) string { return "ctx/" + profile + "/" + key }

// Get mirrors _secret_pass_get: the first line of `pass show`.
func (p Pass) Get(profile, key string) (string, error) {
	out, _ := output("pass", "show", p.path(profile, key))
	out = trimNL(out)
	if out == "" {
		return "", nil
	}
	first, _, _ := strings.Cut(out, "\n")
	return first, nil
}

// Set mirrors _secret_pass_set: `pass insert -m -f` fed the value and a newline.
func (p Pass) Set(profile, key, value string) error {
	cmd := exec.Command("pass", "insert", "-m", "-f", p.path(profile, key))
	cmd.Stdin = strings.NewReader(value + "\n")
	return cmd.Run()
}

// Delete mirrors _secret_pass_delete (failures ignored).
func (p Pass) Delete(profile, key string) error {
	_, _ = output("pass", "rm", "-f", p.path(profile, key))
	return nil
}

// List mirrors _secret_pass_list_keys: the *.gpg entries of the profile's store dir.
func (Pass) List(profile string) ([]string, error) {
	base := os.Getenv("PASSWORD_STORE_DIR")
	if base == "" {
		base = filepath.Join(os.Getenv("HOME"), ".password-store")
	}
	dir := base + "/ctx/" + profile
	if st, err := os.Stat(dir); err != nil || !st.IsDir() {
		return nil, nil
	}
	return sortLines(globNames(dir, ".gpg")), nil
}
