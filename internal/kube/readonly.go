package kube

import "strings"

var readVerbs = map[string]bool{
	"get": true, "describe": true, "logs": true, "top": true, "events": true,
	"explain": true, "api-resources": true, "api-versions": true, "version": true,
	"cluster-info": true,
}

var readPairs = map[string]bool{
	"rollout status": true, "rollout history": true, "auth can-i": true,
}

// IsReadOnly reports whether kubectl args only read: the first non-flag
// arg is a read verb, or the first two are "rollout status",
// "rollout history" or "auth can-i". A flag before the verb must be in
// --name=value form; any other leading flag returns false, because its
// value could be mistaken for the verb ("-n get delete pod x").
func IsReadOnly(args []string) bool {
	i := 0
	for i < len(args) && strings.HasPrefix(args[i], "-") {
		if !strings.HasPrefix(args[i], "--") || !strings.Contains(args[i], "=") {
			return false
		}
		i++
	}
	if i >= len(args) {
		return false
	}
	if readVerbs[args[i]] {
		return true
	}
	return i+1 < len(args) && readPairs[args[i]+" "+args[i+1]]
}
