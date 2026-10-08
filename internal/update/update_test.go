package update

import (
	"archive/zip"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestNewerThan(t *testing.T) {
	all := []Release{{Tag: "v0.2.14"}, {Tag: "v0.2.13"}, {Tag: "v0.2.10"}, {Tag: "v0.2.9"}, {Tag: "v1.0.0-rc1"}}
	got := make([]string, 0, len(all))
	for _, r := range newerThan("0.2.10", all) {
		got = append(got, r.Tag)
	}
	if strings.Join(got, " ") != "v0.2.14 v0.2.13" {
		t.Fatalf("newer than 0.2.10 (numeric, not text, order): %v", got)
	}
	if newerThan("dev", all) != nil {
		t.Fatal("a dev build has nothing newer")
	}
}

func TestChanges(t *testing.T) {
	rs := []Release{
		{Notes: "## Changelog\n* 10529aa fix(checks): list a policy set on both branch and repo once\n* abc1234def feat(update): U updates lazdo\n\n**Full Changelog**: https://…"},
		{Notes: "**Full Changelog**: https://…"},
	}
	want := "fix(checks): list a policy set on both branch and repo once|feat(update): U updates lazdo"
	if got := strings.Join(Changes(rs), "|"); got != want {
		t.Fatalf("got %q", got)
	}
}

func TestHowInstalled(t *testing.T) {
	for exe, want := range map[string]Method{
		`C:\Users\me\AppData\Local\Programs\lazdo\lazdo.exe`: ByScript,
		"/home/me/.local/bin/lazdo":                          ByScript,
		"/opt/homebrew/Cellar/lazdo/0.2.13/bin/lazdo":        ByHomebrew,
		"/home/linuxbrew/.linuxbrew/bin/lazdo":               ByHomebrew,
		`C:\Users\me\go\bin\lazdo.exe`:                       ByGo,
	} {
		if got := HowInstalled(exe); got != want {
			t.Errorf("%s: got %d, want %d", exe, got, want)
		}
	}
}

func TestVerifyAndExtract(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, _ := zw.Create("lazdo_1.0.0/" + binaryName())
	_, _ = w.Write([]byte("new binary"))
	_ = zw.Close()
	sum := sha256.Sum256(buf.Bytes())
	sums := []byte(hex.EncodeToString(sum[:]) + "  lazdo_1.0.0_windows_x86_64.zip\n")
	if err := verify(buf.Bytes(), sums, "lazdo_1.0.0_windows_x86_64.zip"); err != nil {
		t.Fatal(err)
	}
	if err := verify(append(buf.Bytes(), 0), sums, "lazdo_1.0.0_windows_x86_64.zip"); err == nil {
		t.Fatal("a changed archive must fail the checksum")
	}
	if bin, err := extract(buf.Bytes(), ".zip"); err != nil || string(bin) != "new binary" {
		t.Fatalf("extract: %q %v", bin, err)
	}
}

func TestReplaceKeepsOldAside(t *testing.T) {
	exe := filepath.Join(t.TempDir(), "lazdo.exe")
	if err := os.WriteFile(exe, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := replace(exe, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(exe); string(b) != "new" {
		t.Fatalf("exe holds %q", b)
	}
	Cleanup(exe)
	if _, err := os.Stat(exe + ".old"); !os.IsNotExist(err) {
		t.Fatal("Cleanup should remove the old copy")
	}
}
