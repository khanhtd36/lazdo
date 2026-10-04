package ui

import (
	"strings"
	"testing"

	"github.com/khanhtd36/lazdo/internal/ado"
)

func TestTagsSortByVersion(t *testing.T) {
	names := []string{"1.0.0", "kafka-experimental", "8.0.0-rc1+build.1", "8.0.1", "v10.2.0", "8.0.0", "8.0.0-alpha+build.2"}
	tags := make([]ado.Tag, 0, len(names))
	for _, n := range names {
		tags = append(tags, ado.Tag{Name: n})
	}
	got := make([]string, 0, len(names))
	for _, tg := range sortTags(tags) {
		got = append(got, tg.Name)
	}
	want := "v10.2.0 8.0.1 8.0.0 8.0.0-rc1+build.1 8.0.0-alpha+build.2 1.0.0 kafka-experimental"
	if strings.Join(got, " ") != want {
		t.Fatalf("got  %s\nwant %s", strings.Join(got, " "), want)
	}
}

func historyBrowser(t *testing.T) *repoBrowser {
	t.Helper()
	b := fakeBrowser(t)
	b.update(tagsMsg{repoID: "r", tags: []ado.Tag{
		{Name: "8.0.0", ObjectID: "c1", CommitID: "c1"},
		{Name: "8.0.1", ObjectID: "t2", CommitID: "c2"},
	}})
	b.update(commitsMsg{repoID: "r", branch: "develop", commits: []ado.RepoCommit{
		{ID: "c2xxxxxxxx", Comment: "second"}, {ID: "c1xxxxxxxx", Comment: "first"},
	}})
	return b
}

func TestTagReleaseAndDiff(t *testing.T) {
	b := historyBrowser(t)
	b.key(keyMsg("3"), 160, 20)
	if tg, ok := b.selectedTag(); !ok || tg.Name != "8.0.1" {
		t.Fatal("tags list starts at the newest version")
	}
	if _, cmd := b.key(keyMsg("D"), 160, 20); cmd == nil {
		t.Fatal("D should open the combined diff")
	} else if msg, ok := cmd().(openRepoDiffMsg); !ok || msg.from != "c1" || msg.commit != "c2" {
		t.Fatalf("D diff range: %+v", msg)
	}
	b.key(keyMsg("enter"), 160, 20)
	if b.tab != repoTabCommits || b.hist.release == nil || b.hist.release.prev.Name != "8.0.0" {
		t.Fatal("enter on a tag shows its release changes since the previous tag")
	}
	if handled, _ := b.key(keyMsg("esc"), 160, 20); !handled || b.tab != repoTabTags {
		t.Fatal("esc from release changes returns to the tags")
	}
}

func TestDeleteBranchGuards(t *testing.T) {
	b := historyBrowser(t)
	b.prs = []ado.PullRequest{{ID: 77, SourceRefName: "refs/heads/feat/x", Repository: ado.Repository{ID: "r"}}}
	b.pane = paneBranches
	// The default branch is refused outright.
	if cmd := b.deleteBranchKey(); cmd == nil {
		t.Fatal("expected a refusal message")
	} else if _, isModal := cmd().(showModalMsg); isModal {
		t.Fatal("deleting the default branch must not even ask")
	}
	b.branches.move(1)
	msg, ok := b.deleteBranchKey()().(showModalMsg)
	if !ok || !strings.Contains(msg.modal.view(120), "!77 uses this branch") {
		t.Fatal("a branch used by a PR must be named in the question")
	}
}

func TestTagDialogKinds(t *testing.T) {
	b := historyBrowser(t)
	b.key(keyMsg("2"), 160, 20)
	_, cmd := b.key(keyMsg("T"), 160, 20)
	dlg := cmd().(showModalMsg).modal.(*tagDialog)
	for _, k := range []string{"v", "9"} {
		dlg.update(keyMsg(k))
	}
	if dlg.name.value != "v9" {
		t.Fatalf("typing goes into the name: %q", dlg.name.value)
	}
	if next, run := dlg.update(keyMsg("enter")); next != nil || run == nil {
		t.Fatal("enter with a name creates the tag")
	}
}
