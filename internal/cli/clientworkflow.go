package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Hei-Tech-Inc/ctx/internal/ctxconfig"
	"github.com/Hei-Tech-Inc/ctx/internal/profile"
	"github.com/Hei-Tech-Inc/ctx/internal/resolve"
)

func ClientWorkflow(w io.Writer, args []string) error {
	if args[0] == "offboard" {
		if len(args) != 2 || args[1] == "" || strings.Contains(args[1], "/") {
			return errors.New("Usage: ctx offboard <profile>")
		}
		return offboard(w, args[1])
	}
	cloud := len(args) == 2 && args[1] == "--cloud"
	if len(args) != 1 && !cloud {
		return errors.New("Usage: ctx explain [--cloud]")
	}
	cfg, err := ctxconfig.Load()
	if err != nil {
		return err
	}
	pwd, err := os.Getwd()
	if err != nil {
		return err
	}
	dir := profile.Dir()
	folder, _, err := resolve.Path(pwd, dir)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	shell := os.Getenv("CTX_ACTIVE_PROFILE")
	fmt.Fprintf(w, "Folder: %s -> %s\nSaved activation: %s (%s)\nShell profile: %s\n", pwd, fallbackLabel(folder, "none"), fallbackLabel(cfg.Active, "none"), cfg.ActiveSource, fallbackLabel(shell, "unset"))
	if cfg.ActiveSource == "manual" {
		fmt.Fprintln(w, "Manual selection directory:", cfg.ManualPWD)
	}
	name := fallbackLabel(shell, fallbackLabel(cfg.Active, folder))
	if name == "" {
		if cloud {
			return errors.New("unverified: no profile selected")
		}
		fmt.Fprintln(w, "No profile selected. Enter a client workspace or run ctx use <profile>.")
		return nil
	}
	if strings.Contains(name, "/") {
		return errors.New("Invalid profile name")
	}
	p, err := profile.Load(dir, name)
	if err != nil {
		return err
	}
	fmt.Fprintf(w, "Comparing with profile: %s\nConfigured Git email: %s\n", name, fallbackLabel(p.GitEmail, "unset"))
	for _, role := range []string{"AUTHOR", "COMMITTER"} {
		out, err := exec.Command("git", "var", "GIT_"+role+"_IDENT").Output()
		email := ""
		if err == nil {
			_, tail, ok := strings.Cut(string(out), "<")
			if ok {
				email, _, _ = strings.Cut(tail, ">")
			}
		}
		fmt.Fprintf(w, "Live Git %s: %s\n", role, fallbackLabel(email, "unavailable"))
		if p.GitEmail != "" && !strings.EqualFold(p.GitEmail, email) {
			fmt.Fprintln(w, "Check Git environment overrides and git config --show-origin --get user.email.")
		}
	}
	fmt.Fprintf(w, "Configured GitHub: %s | SSH key: %s\nConfigured AWS: %s | Azure: %s | GCP: %s | Kubernetes: %s\n", orDash(p.GithubUser), orDash(p.SSHKeyPath), orDash(p.AWSProfile), orDash(p.AzureSubscription), orDash(p.GCPProject), orDash(p.KubeContext))
	aws := os.Getenv("AWS_PROFILE")
	fmt.Fprintf(w, "Shell AWS_PROFILE: %s\n", fallbackLabel(aws, "unset"))
	if p.AWSProfile != "" && aws != p.AWSProfile {
		fmt.Fprintf(w, "AWS profile differs. Review shell overrides and reactivate %s before running AWS commands.\n", name)
	}
	fmt.Fprintf(w, "For live SSH and GitHub checks: ctx verify %s\n", name)
	if shell != cfg.Active || (folder != "" && folder != name) {
		fmt.Fprintln(w, "Selection differs across contexts. Run ctx use <intended-profile> in this terminal.")
	}
	if cloud {
		return checkCloud(w, p)
	}
	return nil
}

func fallbackLabel(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func keyPath(path string) string {
	if strings.HasPrefix(path, "~/") {
		return filepath.Join(os.Getenv("HOME"), path[2:])
	}
	return path
}

func offboard(w io.Writer, name string) error {
	dir := profile.Dir()
	p, err := profile.Load(dir, name)
	if err != nil {
		return err
	}
	key := keyPath(p.SSHKeyPath)
	fmt.Fprintf(w, "Offboarding checklist for %s (read-only)\n", name)
	fmt.Fprintf(w, "[ ] Hand over workspace %s and review local client knowledge.\n", fallbackLabel(p.WorkDir, "unconfigured"))
	fmt.Fprintf(w, "[ ] Review SSH key %s before rotation.\n", fallbackLabel(key, "unconfigured"))
	if key != "" {
		names, err := profile.List(dir)
		if err != nil {
			return err
		}
		info, _ := os.Stat(key)
		for _, other := range names {
			if other == name {
				continue
			}
			q, err := profile.Load(dir, other)
			if err != nil {
				return err
			}
			path := keyPath(q.SSHKeyPath)
			otherInfo, _ := os.Stat(path)
			if path == key || (info != nil && otherInfo != nil && os.SameFile(info, otherInfo)) {
				fmt.Fprintln(w, "    Shared SSH key with", other)
			}
		}
	}
	fmt.Fprintln(w, "[ ] Ask the client administrator to review and revoke remote access.")
	fmt.Fprintf(w, "    GitHub: %s | AWS: %s | Azure: %s | GCP: %s | Kubernetes: %s\n", orDash(p.GithubUser), orDash(p.AWSProfile), orDash(p.AzureSubscription), orDash(p.GCPProject), orDash(p.KubeContext))
	fmt.Fprintf(w, "[ ] After handoff, run ctx remove %s for local profile, Git, SSH alias and stored secret cleanup.\n", name)
	fmt.Fprintln(w, "[ ] Review retained repositories, key files, workspace configuration and open shells.")
	return nil
}
