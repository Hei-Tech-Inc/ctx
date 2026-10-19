package kube

import (
	"fmt"
	"strings"
)

// blockedLong are kubectl flags that would change which cluster, identity
// or namespace a call uses. ctx sets these from the environment.
var blockedLong = map[string]bool{
	"--kubeconfig": true, "--context": true, "--namespace": true, "--cluster": true,
	"--user": true, "--server": true, "--token": true, "--as": true, "--as-group": true,
	"--as-uid": true, "--certificate-authority": true, "--client-certificate": true,
	"--client-key": true, "--insecure-skip-tls-verify": true, "--username": true,
	"--password": true,
	// --raw sends a request to any API path, ignoring -n.
	"--raw": true,
}

// leadingValueShort are shorthands that take a value. When one starts a
// shorthand group, the rest of the group is its value ("-ojson",
// "-lapp=x", "-cname"), not more flags.
var leadingValueShort = map[byte]bool{'o': true, 'l': true, 'c': true}

var secretSafeFormats = map[string]bool{"": true, "json": true, "yaml": true, "wide": true, "name": true}

// ValidateUserArgs rejects caller-supplied kubectl args that would override
// the cluster, identity or namespace ctx sets, and, when the args may name
// Secrets, output formats that would print Secret data unredacted.
//
// Flags are only read before "--" (kubectl parses no flags after it). A
// shorthand group that does not start with o, l or c is rejected if it
// contains n or s anywhere: pflag reads "-fn x" as "-f -n x", and which
// letters are bools depends on the subcommand, so this errs on refusing.
// Secret references are looked for in every arg, before and after "--".
func ValidateUserArgs(args []string) error {
	flags := args
	if i := indexOf(args, "--"); i >= 0 {
		flags = args[:i]
	}
	var formats []string
	fromFiles := false // -f/-k: the objects could be Secrets
	for i := 0; i < len(flags); i++ {
		a := flags[i]
		next := func() string {
			if i+1 < len(flags) {
				return flags[i+1]
			}
			return ""
		}
		switch {
		case strings.HasPrefix(a, "--"):
			name, val, hasVal := strings.Cut(a, "=")
			if blockedLong[name] {
				return blockedFlag(name)
			}
			switch name {
			case "--output":
				if !hasVal {
					val = next()
				}
				formats = append(formats, val)
			case "--template":
				// kubectl implies -o go-template when --template is set.
				formats = append(formats, "go-template")
			case "--filename", "--kustomize":
				fromFiles = true
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			g := a[1:]
			if leadingValueShort[g[0]] {
				if g[0] == 'o' {
					formats = append(formats, shortValue(g, next))
				}
				continue
			}
			if j := strings.IndexAny(g, "ns"); j >= 0 {
				return blockedFlag("-" + string(g[j]))
			}
			if strings.ContainsAny(g, "fk") {
				fromFiles = true
			}
			if j := strings.IndexByte(g, 'o'); j >= 0 {
				formats = append(formats, shortValue(g[j:], next))
			}
		}
	}
	if !fromFiles && !namesSecrets(args) {
		return nil
	}
	for _, f := range formats {
		name, _, _ := strings.Cut(f, "=")
		if !secretSafeFormats[strings.ToLower(name)] {
			return fmt.Errorf("output format %s is not allowed for secrets", name)
		}
	}
	return nil
}

// shortValue is the value of the shorthand starting g ("ojson", "o=json",
// or "o" followed by the next arg).
func shortValue(g string, next func() string) string {
	if v := strings.TrimPrefix(g[1:], "="); v != "" {
		return v
	}
	return next()
}

func blockedFlag(f string) error {
	return fmt.Errorf("flag %s is not allowed: ctx sets the cluster and namespace from the environment", f)
}

// namesSecrets reports whether any arg refers to the Secret resource:
// "secret", "secrets", "secret/x", "secrets.v1", or such an item in a
// comma list ("cm,secret").
func namesSecrets(args []string) bool {
	for _, a := range args {
		for _, part := range strings.Split(strings.ToLower(a), ",") {
			for _, s := range []string{"secret", "secrets"} {
				if part == s || strings.HasPrefix(part, s+"/") || strings.HasPrefix(part, s+".") {
					return true
				}
			}
		}
	}
	return false
}

func indexOf(args []string, s string) int {
	for i, a := range args {
		if a == s {
			return i
		}
	}
	return -1
}
