package kube

import (
	"strings"
	"testing"
)

func TestIsReadOnly(t *testing.T) {
	cases := map[string]bool{
		"get pods":                    true,
		"get secret x -o yaml":        true,
		"describe pod x":              true,
		"logs deploy/subs -c app":     true,
		"top pods":                    true,
		"events":                      true,
		"explain pods":                true,
		"api-resources":               true,
		"api-versions":                true,
		"version":                     true,
		"cluster-info":                true,
		"rollout status deploy/subs":  true,
		"rollout history deploy/subs": true,
		"auth can-i get pods":         true,
		"--request-timeout=5s get po": true,
		"rollout restart deploy/subs": false,
		"rollout undo deploy/subs":    false,
		"rollout":                     false,
		"auth reconcile -f x":         false,
		"exec -it x -- sh":            false,
		"delete pod x":                false,
		"apply -f x.yaml":             false,
		"edit deploy x":               false,
		"scale deploy x --replicas=0": false,
		"port-forward x 8080":         false,
		"":                            false,
		// A flag that takes a separate value could hide the real verb
		// ("-n get delete" deletes in namespace "get"), so it is refused.
		"-n get delete pod x": false,
	}
	for in, want := range cases {
		if got := IsReadOnly(strings.Fields(in)); got != want {
			t.Errorf("IsReadOnly(%q) = %v, want %v", in, got, want)
		}
	}
}
