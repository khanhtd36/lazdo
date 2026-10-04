// Package actions runs the side effects triggered from the dashboard.
package actions

import (
	"fmt"
	"net/url"
	"os/exec"
	"runtime"
	"strings"

	"github.com/atotto/clipboard"
)

func OpenBrowser(u string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", u)
	case "darwin":
		cmd = exec.Command("open", u)
	default:
		cmd = exec.Command("xdg-open", u)
	}
	return cmd.Start()
}

func CopyToClipboard(s string) error { return clipboard.WriteAll(s) }

// RepoKey identifies an Azure DevOps repo as "org/project/repo", lowercased.
func RepoKey(org, project, repo string) string {
	return strings.ToLower(org + "/" + project + "/" + repo)
}

// RemoteRepoKey parses an Azure DevOps git remote URL into a RepoKey.
// Supports https://[user@]dev.azure.com/org/project/_git/repo,
// https://org.visualstudio.com/project/_git/repo and
// git@ssh.dev.azure.com:v3/org/project/repo.
func RemoteRepoKey(remote string) (string, bool) {
	remote = strings.TrimSuffix(strings.TrimSpace(remote), ".git")
	if rest, ok := strings.CutPrefix(remote, "git@ssh.dev.azure.com:v3/"); ok {
		parts := strings.Split(rest, "/")
		if len(parts) != 3 {
			return "", false
		}
		return keyFromEscaped(parts[0], parts[1], parts[2])
	}
	u, err := url.Parse(remote)
	if err != nil {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.EscapedPath(), "/"), "/")
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "dev.azure.com" && len(parts) == 4 && parts[2] == "_git":
		return keyFromEscaped(parts[0], parts[1], parts[3])
	case strings.HasSuffix(host, ".visualstudio.com") && len(parts) == 3 && parts[1] == "_git":
		return keyFromEscaped(strings.TrimSuffix(host, ".visualstudio.com"), parts[0], parts[2])
	}
	return "", false
}

func keyFromEscaped(org, project, repo string) (string, bool) {
	parts := []string{org, project, repo}
	for i, p := range parts {
		unescaped, err := url.PathUnescape(p)
		if err != nil {
			return "", false
		}
		parts[i] = unescaped
	}
	return RepoKey(parts[0], parts[1], parts[2]), true
}

func run(name string, args ...string) error {
	out, err := exec.Command(name, args...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			return err
		}
		lines := strings.Split(msg, "\n")
		return fmt.Errorf("%s %s: %s", name, args[0], lines[len(lines)-1])
	}
	return nil
}
