package kube

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"regexp"

	"gopkg.in/yaml.v3"
)

const redacted = "<redacted>"

// secretKindRE spots a Secret in output that could not be parsed.
var secretKindRE = regexp.MustCompile(`(?m)("kind"\s*:\s*"Secret"|^\s*-?\s*kind:\s*["']?Secret["']?\s*$)`)

// RedactSecretObjects replaces every data/stringData value of Kubernetes
// Secret objects (alone or inside lists) in kubectl -o json or -o yaml
// output with <redacted>. Output without Secrets is returned unchanged.
// Output that mentions kind Secret but cannot be parsed is replaced whole.
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
		if secretKindRE.Match(out) {
			return []byte(redacted + " (Secret output could not be parsed)\n")
		}
		return out
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
	changed := false
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
		docs = append(docs, &n)
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
					val.Content[j] = &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!str", Value: redacted}
					changed = true
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

func yamlIsSecret(n *yaml.Node) bool {
	for i := 0; i+1 < len(n.Content); i += 2 {
		if n.Content[i].Value == "kind" && n.Content[i+1].Value == "Secret" {
			return true
		}
	}
	return false
}
