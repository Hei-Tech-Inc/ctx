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
}

// blockedShort are the shorthand forms (-n namespace, -s server).
var blockedShort = map[byte]string{'n': "-n", 's': "-s"}

// valueShort are shorthands that take a value; the rest of a combined
// shorthand group ("-ojson", "-lapp=x") is that value, not more flags.
var valueShort = map[byte]bool{'o': true, 'c': true, 'l': true, 'f': true, 'k': true}

var secretSafeFormats = map[string]bool{"": true, "json": true, "yaml": true, "wide": true, "name": true}

// ValidateUserArgs rejects caller-supplied kubectl args that would override
// the cluster, identity or namespace ctx sets, and, when the args name
// Secrets, output formats that would print Secret data unredacted.
// Arguments after "--" belong to the remote command and are not checked.
func ValidateUserArgs(args []string) error {
	if i := indexOf(args, "--"); i >= 0 {
		args = args[:i]
	}
	var formats []string
	for i := 0; i < len(args); i++ {
		a := args[i]
		next := func() string {
			if i+1 < len(args) {
				return args[i+1]
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
			}
		case strings.HasPrefix(a, "-") && len(a) > 1:
			// A shorthand group like "-it" or "-in ns" (= -i -n ns).
			for j := 1; j < len(a); j++ {
				c := a[j]
				if c == '=' {
					break
				}
				if f, ok := blockedShort[c]; ok {
					return blockedFlag(f)
				}
				if valueShort[c] {
					if c == 'o' {
						val := strings.TrimPrefix(a[j+1:], "=")
						if val == "" {
							val = next()
						}
						formats = append(formats, val)
					}
					break
				}
			}
		}
	}
	if !namesSecrets(args) {
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
