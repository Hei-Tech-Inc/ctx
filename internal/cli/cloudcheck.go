package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/Hei-Tech-Inc/ctx/internal/profile"
)

// A missing CLI or expired login leaves a target unverified. Provider errors can
// contain credential details, so only successful query output is displayed.
func cloudValue(tool string, args ...string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, tool, args...)
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func checkCloud(w io.Writer, p *profile.Profile) error {
	if p.AWSProfile == "" && p.AzureSubscription == "" && p.GCPProject == "" && p.KubeContext == "" {
		return errors.New("unverified: no deployment targets configured")
	}
	ok := true
	compare := func(label, expected, actual string) {
		state := "matched"
		if expected == "" || actual == "" || actual == "(unset)" {
			state = "unverified"
		} else if expected != actual {
			state = "mismatch"
		}
		fmt.Fprintf(w, "%s: %s (expected %s, observed %s)\n", label, state, fallbackLabel(expected, "unknown"), fallbackLabel(actual, "unavailable"))
		if state != "matched" {
			ok = false
		}
	}
	if p.AWSProfile != "" {
		expected := cloudValue("aws", "--profile", p.AWSProfile, "sts", "get-caller-identity", "--query", "Account", "--output", "text")
		compare("AWS account ("+p.AWSProfile+")", expected, cloudValue("aws", "sts", "get-caller-identity", "--query", "Account", "--output", "text"))
	}
	if p.AzureSubscription != "" {
		expected := cloudValue("az", "account", "show", "--subscription", p.AzureSubscription, "--query", "id", "-o", "tsv")
		compare("Azure subscription ("+p.AzureSubscription+")", expected, cloudValue("az", "account", "show", "--query", "id", "-o", "tsv"))
	}
	if p.GCPProject != "" {
		compare("GCP project", p.GCPProject, cloudValue("gcloud", "config", "get-value", "project", "--quiet"))
	}
	if p.KubeContext != "" {
		compare("Kubernetes context", p.KubeContext, cloudValue("kubectl", "config", "current-context"))
	}
	if !ok {
		return errors.New("deployment targets did not all match")
	}
	return nil
}
