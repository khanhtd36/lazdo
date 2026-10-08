// Package update finds lazdo releases newer than the running one on GitHub
// and installs them in place of the running executable.
package update

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"
)

const repo = "khanhtd36/lazdo"

// Release is one published version.
type Release struct {
	Tag   string `json:"tag_name"`
	Notes string `json:"body"`
}

// Version is the tag without its "v".
func (r Release) Version() string { return strings.TrimPrefix(r.Tag, "v") }

// Newer lists the releases newer than current, newest first, asking GitHub.
func Newer(ctx context.Context, current string) ([]Release, error) {
	all, err := releases(ctx)
	return newerThan(current, all), err
}

// Check is Newer at most once a day: the releases GitHub listed are kept in
// the user cache folder, so most starts make no request at all.
func Check(ctx context.Context, current string) ([]Release, error) {
	if _, ok := parse(current); !ok {
		return nil, nil // a development build isn't a release
	}
	dir, err := os.UserCacheDir()
	if err != nil {
		return Newer(ctx, current)
	}
	file := filepath.Join(dir, "lazdo", "releases.json")
	var cached struct {
		Checked  time.Time `json:"checked"`
		Releases []Release `json:"releases"`
	}
	if b, err := os.ReadFile(file); err == nil && json.Unmarshal(b, &cached) == nil && time.Since(cached.Checked) < 24*time.Hour {
		return newerThan(current, cached.Releases), nil
	}
	all, err := releases(ctx)
	if err != nil {
		return nil, err
	}
	cached.Checked, cached.Releases = time.Now(), all
	if b, err := json.Marshal(cached); err == nil && os.MkdirAll(filepath.Dir(file), 0o755) == nil {
		_ = os.WriteFile(file, b, 0o644)
	}
	return newerThan(current, all), nil
}

func releases(ctx context.Context) ([]Release, error) {
	var all []Release
	err := getJSON(ctx, "https://api.github.com/repos/"+repo+"/releases?per_page=30", &all)
	return all, err
}

// newerThan keeps the releases newer than current, in GitHub's order (newest
// first). A development build ("dev") has nothing newer: it isn't a release.
func newerThan(current string, all []Release) []Release {
	cur, ok := parse(current)
	if !ok {
		return nil
	}
	var out []Release
	for _, r := range all {
		if v, ok := parse(r.Version()); ok && less(cur, v) {
			out = append(out, r)
		}
	}
	return out
}

// parse reads "1.2.3"; anything else (a dev build, a pre-release) is not a
// version to compare.
func parse(s string) ([3]int, bool) {
	var v [3]int
	parts := strings.Split(strings.TrimPrefix(s, "v"), ".")
	if len(parts) != 3 {
		return v, false
	}
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return v, false
		}
		v[i] = n
	}
	return v, true
}

func less(a, b [3]int) bool {
	for i := range a {
		if a[i] != b[i] {
			return a[i] < b[i]
		}
	}
	return false
}

// Changes lists what the releases changed, one line per change, newest
// release first, without the "Full Changelog" link and commit hashes.
func Changes(rs []Release) []string {
	var out []string
	for _, r := range rs {
		for _, line := range strings.Split(r.Notes, "\n") {
			line = strings.TrimSpace(line)
			if !strings.HasPrefix(line, "* ") && !strings.HasPrefix(line, "- ") {
				continue
			}
			line = strings.TrimSpace(line[2:])
			// goreleaser's git changelog starts each line with the commit hash.
			if hash, rest, ok := strings.Cut(line, " "); ok && len(hash) >= 7 && isHex(hash) {
				line = rest
			}
			out = append(out, line)
		}
	}
	return out
}

func isHex(s string) bool {
	_, err := hex.DecodeString(s[:len(s)/2*2])
	return err == nil
}

// Method is how this copy of lazdo was installed, which decides how to
// update it.
type Method int

const (
	ByScript   Method = iota // install.sh or install.ps1: update in place
	ByHomebrew               // leave it to brew
	ByGo                     // leave it to go install
)

// HowInstalled tells from where the executable lives.
func HowInstalled(exe string) Method {
	p := filepath.ToSlash(strings.ToLower(exe))
	switch {
	case strings.Contains(p, "/cellar/") || strings.Contains(p, "/homebrew/") || strings.Contains(p, "/linuxbrew/"):
		return ByHomebrew
	case strings.Contains(p, "/go/bin/"):
		return ByGo
	}
	return ByScript
}

// Command is what to run instead for a copy lazdo doesn't update itself.
func (m Method) Command() string {
	switch m {
	case ByHomebrew:
		return "brew upgrade lazdo"
	case ByGo:
		return "go install github.com/" + repo + "@latest"
	case ByScript:
	}
	return ""
}

// Install downloads release r for this platform, checks it against the
// release's checksums and puts it in place of exe. On Windows the running
// file can't be overwritten but can be renamed, so it moves to exe+".old"
// first; Cleanup removes that on the next start.
func Install(ctx context.Context, r Release, exe string) error {
	arch := runtime.GOARCH
	if arch == "amd64" {
		arch = "x86_64"
	}
	ext := ".tar.gz"
	if runtime.GOOS == "windows" {
		ext = ".zip"
	}
	asset := fmt.Sprintf("lazdo_%s_%s_%s%s", r.Version(), runtime.GOOS, arch, ext)
	base := "https://github.com/" + repo + "/releases/download/" + r.Tag + "/"

	archive, err := get(ctx, base+asset)
	if err != nil {
		return err
	}
	sums, err := get(ctx, base+"checksums.txt")
	if err != nil {
		return err
	}
	if err := verify(archive, sums, asset); err != nil {
		return err
	}
	bin, err := extract(archive, ext)
	if err != nil {
		return err
	}
	return replace(exe, bin)
}

func verify(archive, sums []byte, asset string) error {
	sum := sha256.Sum256(archive)
	got := hex.EncodeToString(sum[:])
	for _, line := range strings.Split(string(sums), "\n") {
		if f := strings.Fields(line); len(f) == 2 && f[1] == asset {
			if !strings.EqualFold(f[0], got) {
				return fmt.Errorf("%s: checksum mismatch, not installing", asset)
			}
			return nil
		}
	}
	return fmt.Errorf("checksums.txt has no entry for %s", asset)
}

func binaryName() string {
	if runtime.GOOS == "windows" {
		return "lazdo.exe"
	}
	return "lazdo"
}

func extract(archive []byte, ext string) ([]byte, error) {
	if ext == ".zip" {
		zr, err := zip.NewReader(bytes.NewReader(archive), int64(len(archive)))
		if err != nil {
			return nil, err
		}
		for _, f := range zr.File {
			if filepath.Base(f.Name) == binaryName() {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, errors.New("the release archive has no " + binaryName())
	}
	gz, err := gzip.NewReader(bytes.NewReader(archive))
	if err != nil {
		return nil, err
	}
	tr := tar.NewReader(gz)
	for {
		h, err := tr.Next()
		if err != nil {
			return nil, errors.New("the release archive has no " + binaryName())
		}
		if filepath.Base(h.Name) == binaryName() {
			return io.ReadAll(tr)
		}
	}
}

// replace writes bin next to exe, then swaps it in.
func replace(exe string, bin []byte) error {
	next := exe + ".new"
	if err := os.WriteFile(next, bin, 0o755); err != nil {
		return err
	}
	if runtime.GOOS == "windows" {
		old := exe + ".old"
		_ = os.Remove(old)
		if err := os.Rename(exe, old); err != nil {
			_ = os.Remove(next)
			return err
		}
		if err := os.Rename(next, exe); err != nil {
			_ = os.Rename(old, exe) // put the running one back
			return err
		}
		return nil
	}
	return os.Rename(next, exe) // Unix replaces a running file's name freely
}

// Cleanup removes the copy a previous update moved aside.
func Cleanup(exe string) { _ = os.Remove(exe + ".old") }

func get(ctx context.Context, url string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, 2*time.Minute)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("GET %s: %s", url, resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func getJSON(ctx context.Context, url string, out any) error {
	b, err := get(ctx, url)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
