package kube

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"

	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

const redacted = secret.Placeholder

// lastApplied is the annotation kubectl apply writes; on a Secret it holds
// the full object, data included.
const (
	lastApplied    = "last-applied-configuration"
	lastAppliedKey = "kubectl.kubernetes.io/" + lastApplied
)

// errNotObjects means the output parsed as YAML but is not Kubernetes
// objects (for example describe or table output).
var errNotObjects = errors.New("not kubernetes objects")

// secretKindRE spots a Secret in output that could not be parsed.
var secretKindRE = regexp.MustCompile(`(?m)("kind"\s*:\s*"Secret"|^\s*-?\s*kind:\s*["']?Secret["']?\s*$)`)

// RedactSecretObjects replaces every data/stringData value and the
// last-applied-configuration annotation of Kubernetes Secret objects (alone
// or inside lists) in kubectl -o json or -o yaml output with <redacted>.
// Objects without Secrets are returned unchanged. Output that mentions kind
// Secret but cannot be parsed is replaced whole. Other text (describe,
// tables) has any last-applied-configuration JSON blob redacted.
func RedactSecretObjects(out []byte) []byte {
	trimmed := bytes.TrimSpace(out)
	if len(trimmed) == 0 {
		return out
	}
	var res []byte
	var changed bool
	var err error
	if trimmed[0] == '{' || trimmed[0] == '[' {
		res, changed, err = redactJSON(trimmed)
	} else {
		res, changed, err = redactYAML(out)
	}
	if err != nil {
		text := redactLastAppliedText(out)
		if secretKindRE.Match(text) {
			return []byte(redacted + " (Secret output could not be parsed)\n")
		}
		return text
	}
	if !changed {
		return out
	}
	return res
}

func redactJSON(in []byte) ([]byte, bool, error) {
	var v any
	dec := json.NewDecoder(bytes.NewReader(in))
	dec.UseNumber() // keep large integers exact when re-encoding
	if err := dec.Decode(&v); err != nil {
		return nil, false, err
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return nil, false, errors.New("unexpected data after JSON value")
	}
	if !walkJSON(v) {
		return nil, false, nil
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "    ")
	if err := enc.Encode(v); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

func walkJSON(v any) bool {
	changed := false
	switch x := v.(type) {
	case map[string]any:
		if x["kind"] == "Secret" {
			for _, field := range []string{"data", "stringData"} {
				if m, ok := x[field].(map[string]any); ok {
					for k := range m {
						m[k] = redacted
						changed = true
					}
				}
			}
			if md, ok := x["metadata"].(map[string]any); ok {
				if ann, ok := md["annotations"].(map[string]any); ok {
					if _, ok := ann[lastAppliedKey]; ok {
						ann[lastAppliedKey] = redacted
						changed = true
					}
				}
			}
		}
		for _, child := range x {
			if walkJSON(child) {
				changed = true
			}
		}
	case []any:
		for _, child := range x {
			if walkJSON(child) {
				changed = true
			}
		}
	}
	return changed
}

func redactYAML(in []byte) ([]byte, bool, error) {
	dec := yaml.NewDecoder(bytes.NewReader(in))
	var docs []*yaml.Node
	changed, objects := false, false
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, false, err
		}
		if walkYAML(&n) {
			changed = true
		}
		if len(n.Content) > 0 && n.Content[0].Kind == yaml.MappingNode && yamlValue(n.Content[0], "kind") != nil {
			objects = true
		}
		docs = append(docs, &n)
	}
	if !objects {
		return nil, false, errNotObjects
	}
	if !changed {
		return nil, false, nil
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	for _, d := range docs {
		if err := enc.Encode(d); err != nil {
			return nil, false, err
		}
	}
	if err := enc.Close(); err != nil {
		return nil, false, err
	}
	return buf.Bytes(), true, nil
}

func walkYAML(n *yaml.Node) bool {
	changed := false
	if n.Kind == yaml.MappingNode && yamlIsSecret(n) {
		for i := 0; i+1 < len(n.Content); i += 2 {
			key, val := n.Content[i], n.Content[i+1]
			if (key.Value == "data" || key.Value == "stringData") && val.Kind == yaml.MappingNode {
				for j := 1; j < len(val.Content); j += 2 {
					val.Content[j] = redactedNode()
					changed = true
				}
			}
		}
		if md := yamlValue(n, "metadata"); md != nil && md.Kind == yaml.MappingNode {
			if ann := yamlValue(md, "annotations"); ann != nil && ann.Kind == yaml.MappingNode {
				for j := 0; j+1 < len(ann.Content); j += 2 {
					if ann.Content[j].Value == lastAppliedKey {
						ann.Content[j+1] = redactedNode()
						changed = true
					}
				}
			}
		}
	}
	for _, c := range n.Content {
		if walkYAML(c) {
			changed = true
		}
	}
	return changed
}

func redactedNode() *yaml.Node {
	return &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: redacted}
}

// yamlValue returns the value node for key in mapping n, or nil.
func yamlValue(n *yaml.Node, key string) *yaml.Node {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == key {
			return n.Content[i+1]
		}
	}
	return nil
}

func yamlIsSecret(n *yaml.Node) bool {
	v := yamlValue(n, "kind")
	return v != nil && v.Value == "Secret"
}

// redactLastAppliedText replaces the JSON blob after each
// last-applied-configuration marker in plain text (such as describe
// output). If the blob does not start with '{' or never closes, everything
// after the marker is redacted.
func redactLastAppliedText(out []byte) []byte {
	s := string(out)
	if !strings.Contains(s, lastApplied) {
		return out
	}
	var b strings.Builder
	for {
		i := strings.Index(s, lastApplied)
		if i < 0 {
			b.WriteString(s)
			break
		}
		i += len(lastApplied)
		b.WriteString(s[:i])
		rest := strings.TrimLeft(s[i:], ":=\"' \t\r\n")
		end := jsonObjectEnd(rest)
		b.WriteString(": " + redacted)
		if end < 0 {
			b.WriteString("\n")
			break
		}
		s = rest[end:]
	}
	return []byte(b.String())
}

// jsonObjectEnd returns the index just past the JSON object that starts s,
// or -1 if s does not start with '{' or the object does not close.
func jsonObjectEnd(s string) int {
	if !strings.HasPrefix(s, "{") {
		return -1
	}
	depth, inStr, esc := 0, false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case inStr && c == '\\':
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return i + 1
			}
		}
	}
	return -1
}
