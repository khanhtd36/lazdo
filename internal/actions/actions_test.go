package actions

import (
	"os"
	"path/filepath"
	"testing"
)

func TestPlanCheckout(t *testing.T) {
	dir := t.TempDir()
	if p := PlanCheckout(filepath.Join(dir, "new"), "o/p/r"); p.Kind != PlanClone {
		t.Errorf("missing folder: %+v, want clone", p)
	}
	if p := PlanCheckout(dir, "o/p/r"); p.Kind != PlanClone {
		t.Errorf("empty folder: %+v, want clone", p)
	}
	if err := os.WriteFile(filepath.Join(dir, "x.txt"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if p := PlanCheckout(dir, "o/p/r"); p.Kind != PlanRefuse {
		t.Errorf("non-empty non-clone: %+v, want refuse", p)
	}
	if p := PlanCheckout(filepath.Join(dir, "x.txt"), "o/p/r"); p.Kind != PlanRefuse {
		t.Errorf("file: %+v, want refuse", p)
	}
}

func TestCompleteDir(t *testing.T) {
	dir := t.TempDir()
	for _, d := range []string{"alpha-one", "alpha-two", "beta"} {
		if err := os.Mkdir(filepath.Join(dir, d), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	if got := CompleteDir(filepath.Join(dir, "al")); got != filepath.Join(dir, "alpha-") {
		t.Errorf("shared prefix: got %q", got)
	}
	if got := CompleteDir(filepath.Join(dir, "b")); got != filepath.Join(dir, "beta")+string(filepath.Separator) {
		t.Errorf("single match: got %q", got)
	}
}

func TestCloneURL(t *testing.T) {
	if got := CloneURL("arbinSW", "MITS 11", "Repo"); got != "https://arbinSW@dev.azure.com/arbinSW/MITS%2011/_git/Repo" {
		t.Errorf("got %q", got)
	}
}

func TestRemoteRepoKey(t *testing.T) {
	want := "arbinsw/mits 11/repo"
	for _, remote := range []string{
		"https://arbinSW@dev.azure.com/arbinSW/MITS%2011/_git/Repo",
		"https://dev.azure.com/arbinSW/MITS%2011/_git/Repo.git",
		"https://arbinsw.visualstudio.com/MITS%2011/_git/Repo",
		"git@ssh.dev.azure.com:v3/arbinSW/MITS%2011/Repo\n",
	} {
		got, ok := RemoteRepoKey(remote)
		if !ok || got != want {
			t.Errorf("RemoteRepoKey(%q) = %q, %v; want %q", remote, got, ok, want)
		}
	}
	if _, ok := RemoteRepoKey("https://github.com/khanhtd36/lazdo"); ok {
		t.Error("github remote should not parse")
	}
}
