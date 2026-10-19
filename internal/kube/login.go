package kube

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"syscall"

	"github.com/Hei-Tech-Inc/ctx/internal/knowledge"
	"github.com/Hei-Tech-Inc/ctx/internal/secret"
)

// Login logs in to cluster and returns the redacted step and check output.
//
// For a command login it holds an exclusive lock on
// $CTX_DIR/kube/<profile>/.login.lock, runs each step with sh -c and
// KUBECONFIG=$CTX_DIR/kube/<profile>/<cluster>.yaml plus each login secret
// as an env var, and stops at the first failing step. It then checks that
// the kubeconfig file exists and that kubectl version works against it.
// For a kubeconfig login it checks the path exists and kubectl version
// works with --context.
//
// Output and errors are redacted with red plus the secrets given to the
// steps.
func Login(ctx context.Context, profileName string, s *knowledge.Store, cluster string, g secret.Getter, red *secret.Redactor) (string, error) {
	c, ok := s.Clusters[cluster]
	if !ok {
		names := make([]string, 0, len(s.Clusters))
		for n := range s.Clusters {
			names = append(names, n)
		}
		sort.Strings(names)
		return "", fmt.Errorf("unknown cluster %q; valid: %s", cluster, strings.Join(names, ", "))
	}
	switch c.Login.Type {
	case "command":
		return loginCommand(ctx, profileName, cluster, c.Login, g, red)
	case "kubeconfig":
		path := expandHome(c.Login.Path)
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("login %s: kubeconfig %s not found", cluster, red.String(path))
		}
		var out strings.Builder
		err := checkVersion(ctx, cluster, Target{Kubeconfig: path, Context: c.Login.Context}, red, &out)
		return out.String(), err
	default:
		return "", fmt.Errorf("login %s: unknown login type %q (use command or kubeconfig)", cluster, c.Login.Type)
	}
}

func loginCommand(ctx context.Context, profileName, cluster string, login knowledge.Login, g secret.Getter, red *secret.Redactor) (string, error) {
	dir := KubeDir(profileName)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", fmt.Errorf("login %s: %w", cluster, err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		return "", fmt.Errorf("login %s: %w", cluster, err)
	}
	unlock, err := lockFile(filepath.Join(dir, ".login.lock"))
	if err != nil {
		return "", fmt.Errorf("login %s: %w", cluster, err)
	}
	defer unlock()

	var extra, values []string
	for _, k := range login.Secrets {
		v, err := g.Get(profileName, k)
		if err != nil {
			return "", fmt.Errorf("login %s: secret %s: %s", cluster, k, red.String(err.Error()))
		}
		extra = append(extra, k+"="+v)
		values = append(values, v)
	}
	red = red.With(values...)

	kc := filepath.Join(dir, cluster+".yaml")
	var out strings.Builder
	for i, step := range login.Run {
		var buf bytes.Buffer
		cmd := exec.CommandContext(ctx, "sh", "-c", step)
		cmd.Env = childEnv(kc, extra...)
		cmd.Stdout, cmd.Stderr = &buf, &buf
		err := cmd.Run()
		out.Write(red.Bytes(buf.Bytes()))
		if err != nil {
			return out.String(), fmt.Errorf("login %s: step %d of %d failed (%s):\n%s",
				cluster, i+1, len(login.Run), red.String(err.Error()), strings.TrimSpace(red.String(buf.String())))
		}
	}
	if _, err := os.Stat(kc); err != nil {
		return out.String(), fmt.Errorf("login %s: steps finished but %s was not created", cluster, kc)
	}
	if err := os.Chmod(kc, 0o600); err != nil {
		return out.String(), fmt.Errorf("login %s: %w", cluster, err)
	}
	err = checkVersion(ctx, cluster, Target{Kubeconfig: kc}, red, &out)
	return out.String(), err
}

// checkVersion runs kubectl --kubeconfig <f> [--context <c>] version.
func checkVersion(ctx context.Context, cluster string, t Target, red *secret.Redactor, out *strings.Builder) error {
	args := []string{"--kubeconfig", t.Kubeconfig}
	if t.Context != "" {
		args = append(args, "--context", t.Context)
	}
	args = append(args, "version", "--request-timeout=10s")
	var buf bytes.Buffer
	cmd := exec.CommandContext(ctx, "kubectl", args...)
	cmd.Env = childEnv("")
	cmd.Stdout, cmd.Stderr = &buf, &buf
	err := cmd.Run()
	out.Write(red.Bytes(buf.Bytes()))
	if err != nil {
		return fmt.Errorf("login %s: kubectl version failed (%s):\n%s",
			cluster, red.String(err.Error()), strings.TrimSpace(red.String(buf.String())))
	}
	return nil
}

// lockFile takes an exclusive flock on path, waiting for other holders.
func lockFile(path string) (unlock func(), err error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	for {
		err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX)
		if err != syscall.EINTR {
			break
		}
	}
	if err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("lock %s: %w", path, err)
	}
	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
