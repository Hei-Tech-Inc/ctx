package ops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/kube"
)

// EnvVar is one environment variable of the workload. Source is literal,
// configmap:<name>/<key>, secret:<name>/<key>, field:<path>,
// envFrom:configmap:<name> or envFrom:secret:<name>. Secrets are never read,
// so their Value is always empty (an envFrom secret is one entry named "*").
type EnvVar struct{ Name, Value, Source string }

// ServiceSummary is what Describe reports about a workload.
type ServiceSummary struct {
	Kind, Name, Image string
	Replicas, Ready   int
	Rollout           string
	Env               []EnvVar
	ConfigMaps        []string
	Events            []string
	Resources, Probes map[string]any
}

type container struct {
	Image string `json:"image"`
	Env   []struct {
		Name      string `json:"name"`
		Value     string `json:"value"`
		ValueFrom *struct {
			ConfigMapKeyRef *struct{ Name, Key string } `json:"configMapKeyRef"`
			SecretKeyRef    *struct{ Name, Key string } `json:"secretKeyRef"`
			FieldRef        *struct {
				FieldPath string `json:"fieldPath"`
			} `json:"fieldRef"`
		} `json:"valueFrom"`
	} `json:"env"`
	EnvFrom []struct {
		ConfigMapRef *struct{ Name string } `json:"configMapRef"`
		SecretRef    *struct{ Name string } `json:"secretRef"`
	} `json:"envFrom"`
	Resources      map[string]any `json:"resources"`
	LivenessProbe  map[string]any `json:"livenessProbe"`
	ReadinessProbe map[string]any `json:"readinessProbe"`
	StartupProbe   map[string]any `json:"startupProbe"`
}

// Describe summarises a deployment or statefulset: image, replicas, rollout
// state, environment with its sources, referenced ConfigMaps (values
// resolved), resources, probes and recent events.
func Describe(ctx context.Context, r *kube.Runner, t kube.Target, svc knowledge.Service) (ServiceSummary, error) {
	kind := kindOf(svc)
	ref := kind + "/" + svc.Name
	out, err := r.Run(ctx, t, "get", ref, "-o", "json")
	if err != nil {
		return ServiceSummary{}, err
	}
	var w struct {
		Spec struct {
			Replicas *int `json:"replicas"`
			Template struct {
				Spec struct {
					Containers []container `json:"containers"`
					Volumes    []struct {
						ConfigMap *struct{ Name string } `json:"configMap"`
					} `json:"volumes"`
				} `json:"spec"`
			} `json:"template"`
		} `json:"spec"`
		Status struct {
			ReadyReplicas int `json:"readyReplicas"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &w); err != nil {
		return ServiceSummary{}, fmt.Errorf("parse %s: %w", ref, err)
	}
	s := ServiceSummary{Kind: kind, Name: svc.Name, Ready: w.Status.ReadyReplicas}
	if w.Spec.Replicas != nil {
		s.Replicas = *w.Spec.Replicas
	} else {
		s.Replicas = 1
	}

	cms := map[string]map[string]string{} // name -> data; nil when unreadable
	getCM := func(name string) (map[string]string, error) {
		if d, ok := cms[name]; ok {
			return d, nil
		}
		out, err := r.Run(ctx, t, "get", "configmap", name, "-o", "json")
		if err != nil {
			var re *kube.RunError
			if errors.As(err, &re) && strings.Contains(strings.ToLower(re.Stderr), "not found") {
				cms[name] = nil
				return nil, nil
			}
			return nil, err
		}
		var cm struct {
			Data map[string]string `json:"data"`
		}
		if err := json.Unmarshal(out, &cm); err != nil {
			return nil, fmt.Errorf("parse configmap %s: %w", name, err)
		}
		cms[name] = cm.Data
		return cm.Data, nil
	}

	for i, c := range w.Spec.Template.Spec.Containers {
		if i == 0 {
			s.Image, s.Resources = c.Image, c.Resources
			s.Probes = map[string]any{}
			for k, p := range map[string]map[string]any{"livenessProbe": c.LivenessProbe, "readinessProbe": c.ReadinessProbe, "startupProbe": c.StartupProbe} {
				if p != nil {
					s.Probes[k] = p
				}
			}
		}
		for _, e := range c.Env {
			ev := EnvVar{Name: e.Name, Value: e.Value, Source: "literal"}
			if vf := e.ValueFrom; vf != nil {
				switch {
				case vf.ConfigMapKeyRef != nil:
					ev.Source = "configmap:" + vf.ConfigMapKeyRef.Name + "/" + vf.ConfigMapKeyRef.Key
					data, err := getCM(vf.ConfigMapKeyRef.Name)
					if err != nil {
						return ServiceSummary{}, err
					}
					ev.Value = data[vf.ConfigMapKeyRef.Key]
				case vf.SecretKeyRef != nil:
					ev.Value, ev.Source = "", "secret:"+vf.SecretKeyRef.Name+"/"+vf.SecretKeyRef.Key
				case vf.FieldRef != nil:
					ev.Value, ev.Source = "", "field:"+vf.FieldRef.FieldPath
				}
			}
			s.Env = append(s.Env, ev)
		}
		for _, ef := range c.EnvFrom {
			switch {
			case ef.ConfigMapRef != nil:
				data, err := getCM(ef.ConfigMapRef.Name)
				if err != nil {
					return ServiceSummary{}, err
				}
				keys := make([]string, 0, len(data))
				for k := range data {
					keys = append(keys, k)
				}
				sort.Strings(keys)
				for _, k := range keys {
					s.Env = append(s.Env, EnvVar{Name: k, Value: data[k], Source: "envFrom:configmap:" + ef.ConfigMapRef.Name})
				}
			case ef.SecretRef != nil:
				s.Env = append(s.Env, EnvVar{Name: "*", Source: "envFrom:secret:" + ef.SecretRef.Name})
			}
		}
	}
	for _, v := range w.Spec.Template.Spec.Volumes {
		if v.ConfigMap != nil {
			if _, err := getCM(v.ConfigMap.Name); err != nil {
				return ServiceSummary{}, err
			}
		}
	}
	for name := range cms {
		s.ConfigMaps = append(s.ConfigMaps, name)
	}
	sort.Strings(s.ConfigMaps)

	// rollout status exits non-zero while a rollout is unfinished; that is
	// the answer, not a failure.
	out, err = r.Run(ctx, t, "rollout", "status", ref, "--timeout=1s")
	s.Rollout = strings.TrimSpace(string(out))
	if err != nil {
		var re *kube.RunError
		if !errors.As(err, &re) {
			return ServiceSummary{}, err
		}
		if s.Rollout == "" {
			s.Rollout = strings.TrimSpace(re.Stderr)
		}
	}
	if i := strings.LastIndex(s.Rollout, "\n"); i >= 0 {
		s.Rollout = s.Rollout[i+1:]
	}

	out, err = r.Run(ctx, t, "get", "events", "--field-selector", "involvedObject.name="+svc.Name, "-o", "json")
	if err != nil {
		return ServiceSummary{}, err
	}
	var ev struct {
		Items []struct {
			Type, Reason, Message string
			Count                 int
		} `json:"items"`
	}
	if err := json.Unmarshal(out, &ev); err != nil {
		return ServiceSummary{}, fmt.Errorf("parse events: %w", err)
	}
	for _, e := range ev.Items {
		s.Events = append(s.Events, fmt.Sprintf("%s %s: %s", e.Type, e.Reason, e.Message))
	}
	return s, nil
}
