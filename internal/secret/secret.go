// Package secret fetches secret values from the ctx secret store and
// redacts them from text.
package secret

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os/exec"
	"sort"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// Placeholder replaces every redacted value.
const Placeholder = "<redacted>"

// minLen is the shortest value that is redacted; shorter values would
// mangle ordinary output.
const minLen = 4

// Getter returns the value of one secret key for a profile.
type Getter interface {
	Get(profile, key string) (string, error)
}

// CLIGetter reads secrets with `<CtxBin> secret get <profile> <key>`.
// CtxBin defaults to "ctx" on PATH.
type CLIGetter struct{ CtxBin string }

// Get runs ctx secret get and trims one trailing newline. Errors carry
// ctx's stderr, never stdout.
func (g CLIGetter) Get(profileName, key string) (string, error) {
	bin := g.CtxBin
	if bin == "" {
		bin = "ctx"
	}
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(bin, "secret", "get", profileName, key)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("ctx secret get %s %s: %s", profileName, key, msg)
	}
	return strings.TrimSuffix(stdout.String(), "\n"), nil
}

// Redactor replaces known secret values with Placeholder. A nil *Redactor
// leaves text unchanged.
type Redactor struct {
	secrets  []string // the values given, len >= minLen
	values   []string // secrets plus their JSON forms, unique, longest first
	replacer *strings.Replacer
}

// NewRedactor redacts each value of at least 4 characters, longest first,
// so a value that contains another is replaced whole. Each value's
// JSON-escaped forms (with and without HTML escaping of & < >) are
// redacted too, so a secret is caught inside JSON text as well.
func NewRedactor(values []string) *Redactor {
	seen := map[string]bool{}
	var vs, secrets []string
	for _, v := range values {
		if len(v) < minLen {
			continue
		}
		secrets = append(secrets, v)
		for _, form := range append([]string{v}, jsonForms(v)...) {
			if !seen[form] {
				seen[form] = true
				vs = append(vs, form)
			}
		}
	}
	sort.SliceStable(vs, func(i, j int) bool { return len(vs[i]) > len(vs[j]) })
	r := &Redactor{secrets: secrets, values: vs}
	if len(vs) > 0 {
		pairs := make([]string, 0, 2*len(vs))
		for _, v := range vs {
			pairs = append(pairs, v, Placeholder)
		}
		// strings.Replacer prefers earlier pairs when several match at one
		// position, so longest-first ordering wins.
		r.replacer = strings.NewReplacer(pairs...)
	}
	return r
}

// jsonForms returns v as it appears inside a JSON string literal.
func jsonForms(v string) []string {
	var out []string
	for _, html := range []bool{true, false} {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(html)
		if enc.Encode(v) != nil {
			continue
		}
		form := strings.TrimSuffix(buf.String(), "\n")
		form = form[1 : len(form)-1] // drop the quotes
		if form != v {
			out = append(out, form)
		}
	}
	return out
}

// With returns a new Redactor that also redacts extra.
func (r *Redactor) With(extra ...string) *Redactor {
	var vs []string
	if r != nil {
		vs = append(vs, r.secrets...)
	}
	return NewRedactor(append(vs, extra...))
}

// String returns s with every secret value replaced.
func (r *Redactor) String(s string) string {
	if r == nil || r.replacer == nil {
		return s
	}
	return r.replacer.Replace(s)
}

// Bytes returns b with every secret value replaced.
func (r *Redactor) Bytes(b []byte) []byte {
	if r == nil || r.replacer == nil {
		return b
	}
	return []byte(r.replacer.Replace(string(b)))
}

// LoadRedactor fetches every key in the profile's SECRET_KEYS and every
// cluster's login secrets. Keys that cannot be fetched are skipped.
func LoadRedactor(p *profile.Profile, s *knowledge.Store, g Getter) *Redactor {
	var keys []string
	if p != nil {
		keys = append(keys, p.SecretKeys...)
	}
	if s != nil {
		keys = append(keys, s.SecretKeys()...)
	}
	name := ""
	if p != nil {
		name = p.Name
	}
	seen := map[string]bool{}
	var values []string
	for _, k := range keys {
		if seen[k] {
			continue
		}
		seen[k] = true
		if v, err := g.Get(name, k); err == nil {
			values = append(values, v)
		}
	}
	return NewRedactor(values)
}
