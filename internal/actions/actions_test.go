package actions

import "testing"

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
