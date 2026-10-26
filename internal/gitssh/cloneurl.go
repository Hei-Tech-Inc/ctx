package gitssh

import "strings"

// CloneURLForProfile mirrors github_clone_url_for_profile with
// https_as_ssh=y: GitHub SSH and HTTPS URLs are rewritten to Host
// github-<profile>.
func CloneURLForProfile(profile, url string) string {
	return CloneURLForProfileOpt(profile, url, true)
}

// CloneURLForProfileOpt is github_clone_url_for_profile; httpsAsSSH is its
// third argument ("y"), which only affects https://github.com/ URLs.
func CloneURLForProfileOpt(profile, url string, httpsAsSSH bool) string {
	host := "github-" + profile
	dotGit := func(p string) string { return strings.TrimSuffix(p, ".git") + ".git" }
	switch {
	case strings.HasPrefix(url, "git@github.com:"):
		return "git@" + host + ":" + strings.TrimPrefix(url, "git@github.com:")
	case strings.HasPrefix(url, "ssh://git@github.com/"):
		p := strings.TrimPrefix(strings.TrimPrefix(url, "ssh://git@github.com/"), "/")
		return "git@" + host + ":" + dotGit(p)
	case strings.HasPrefix(url, "https://github.com/") && httpsAsSSH:
		p := strings.TrimPrefix(strings.TrimPrefix(url, "https://github.com/"), "/")
		return "git@" + host + ":" + dotGit(p)
	}
	return url
}
