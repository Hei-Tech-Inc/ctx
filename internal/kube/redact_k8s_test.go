package kube

import (
	"encoding/json"
	"strings"
	"testing"
)

func assertRedacted(t *testing.T, out string, values ...string) {
	t.Helper()
	for _, v := range values {
		if strings.Contains(out, v) {
			t.Errorf("value %q not redacted in:\n%s", v, out)
		}
	}
	if !strings.Contains(out, "<redacted>") {
		t.Errorf("no <redacted> marker in:\n%s", out)
	}
}

func TestRedactSecretObjectsJSON(t *testing.T) {
	in := `{
    "apiVersion": "v1",
    "data": {"password": "aHVudGVyMjI=", "user": "YWRtaW4="},
    "stringData": {"token": "plain-token"},
    "kind": "Secret",
    "metadata": {"name": "db"},
    "type": "Opaque"
}`
	out := string(RedactSecretObjects([]byte(in)))
	assertRedacted(t, out, "aHVudGVyMjI=", "YWRtaW4=", "plain-token")
	for _, keep := range []string{`"password"`, `"user"`, `"token"`, `"db"`, `"Opaque"`} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %s:\n%s", keep, out)
		}
	}
}

func TestRedactSecretObjectsJSONList(t *testing.T) {
	in := `{"apiVersion":"v1","kind":"List","items":[
  {"apiVersion":"v1","kind":"Secret","metadata":{"name":"a"},"data":{"k":"c2VjcmV0MQ=="}},
  {"apiVersion":"v1","kind":"Secret","metadata":{"name":"b"},"data":{"k":"c2VjcmV0Mg=="}},
  {"apiVersion":"v1","kind":"ConfigMap","metadata":{"name":"c"},"data":{"k":"visible"}}
]}`
	out := string(RedactSecretObjects([]byte(in)))
	assertRedacted(t, out, "c2VjcmV0MQ==", "c2VjcmV0Mg==")
	if !strings.Contains(out, "visible") {
		t.Errorf("ConfigMap data redacted:\n%s", out)
	}
}

func TestRedactSecretObjectsYAML(t *testing.T) {
	in := `apiVersion: v1
data:
  password: aHVudGVyMjI=
kind: Secret
metadata:
  name: db
stringData:
  token: plain-token
type: Opaque
`
	out := string(RedactSecretObjects([]byte(in)))
	assertRedacted(t, out, "aHVudGVyMjI=", "plain-token")
	if !strings.Contains(out, "password: <redacted>") || !strings.Contains(out, "name: db") {
		t.Errorf("unexpected output:\n%s", out)
	}
}

func TestRedactSecretObjectsYAMLList(t *testing.T) {
	in := `apiVersion: v1
items:
- apiVersion: v1
  data:
    k: c2VjcmV0MQ==
  kind: Secret
  metadata:
    name: a
kind: List
`
	assertRedacted(t, string(RedactSecretObjects([]byte(in))), "c2VjcmV0MQ==")
}

func TestRedactSecretObjectsLeavesOthersUnchanged(t *testing.T) {
	for _, in := range []string{
		`{"apiVersion":"v1","kind":"ConfigMap","data":{"k":"v"}}`,
		"apiVersion: v1\nkind: ConfigMap\ndata:\n  k: v\n",
		"NAME   READY   STATUS\nsubs   1/1     Running\n",
		"",
	} {
		if got := string(RedactSecretObjects([]byte(in))); got != in {
			t.Errorf("changed %q to %q", in, got)
		}
	}
}

func TestRedactSecretObjectsUnparseableSecretFailsClosed(t *testing.T) {
	in := "kind: Secret\ndata:\n  k: c2VjcmV0\n  : : [broken\n"
	out := string(RedactSecretObjects([]byte(in)))
	if strings.Contains(out, "c2VjcmV0") {
		t.Errorf("leaked: %q", out)
	}
}

func TestRedactSecretObjectsConcatenatedJSON(t *testing.T) {
	in := `{"kind":"Secret","data":{"k":"Zmlyc3Q="}}
{"kind":"Secret","data":{"k":"c2Vjb25k"}}`
	assertRedacted(t, string(RedactSecretObjects([]byte(in))), "Zmlyc3Q=", "c2Vjb25k")
}

// I1: kubectl apply stores the whole Secret, data included, in an annotation.
const appliedJSON = `{"apiVersion":"v1","data":{"password":"aHVudGVyMjI="},"kind":"Secret","metadata":{"annotations":{},"name":"db","namespace":"subs"}}`

func TestRedactSecretObjectsLastAppliedJSON(t *testing.T) {
	in := `{"apiVersion":"v1","kind":"Secret","data":{"password":"aHVudGVyMjI="},
"metadata":{"name":"db","annotations":{"kubectl.kubernetes.io/last-applied-configuration":` + strconvQuote(appliedJSON) + `,"team":"payments"}}}`
	out := string(RedactSecretObjects([]byte(in)))
	assertRedacted(t, out, "aHVudGVyMjI=")
	if !strings.Contains(out, "payments") {
		t.Errorf("other annotations lost:\n%s", out)
	}
}

func TestRedactSecretObjectsLastAppliedYAML(t *testing.T) {
	in := `apiVersion: v1
data:
  password: aHVudGVyMjI=
kind: Secret
metadata:
  annotations:
    kubectl.kubernetes.io/last-applied-configuration: |
      ` + appliedJSON + `
    team: payments
  name: db
type: Opaque
`
	out := string(RedactSecretObjects([]byte(in)))
	assertRedacted(t, out, "aHVudGVyMjI=")
	if !strings.Contains(out, "team: payments") {
		t.Errorf("other annotations lost:\n%s", out)
	}
}

func TestRedactSecretObjectsLastAppliedListYAML(t *testing.T) {
	in := `apiVersion: v1
items:
- apiVersion: v1
  kind: Secret
  metadata:
    annotations:
      kubectl.kubernetes.io/last-applied-configuration: '` + appliedJSON + `'
    name: db
kind: List
`
	assertRedacted(t, string(RedactSecretObjects([]byte(in))), "aHVudGVyMjI=")
}

func TestRedactSecretObjectsLastAppliedDescribeText(t *testing.T) {
	in := `Name:         db
Namespace:    subs
Labels:       <none>
Annotations:  kubectl.kubernetes.io/last-applied-configuration:
                ` + appliedJSON + `

Type:  Opaque

Data
====
password:  8 bytes
`
	out := string(RedactSecretObjects([]byte(in)))
	assertRedacted(t, out, "aHVudGVyMjI=")
	for _, keep := range []string{"Name:         db", "Type:  Opaque", "password:  8 bytes"} {
		if !strings.Contains(out, keep) {
			t.Errorf("lost %q:\n%s", keep, out)
		}
	}

	// A blob that never closes is redacted to the end.
	out = string(RedactSecretObjects([]byte("Annotations: kubectl.kubernetes.io/last-applied-configuration: {\"data\":{\"p\":\"aHVudGVyMjI=\"\n")))
	assertRedacted(t, out, "aHVudGVyMjI=")
}

func strconvQuote(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
